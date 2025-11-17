package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"alfred-cloud/security"
	"alfred-cloud/streams"
	"alfred-cloud/subagents/calendar_planner"
	"alfred-cloud/subagents/email_triage"
	"github.com/gorilla/mux"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

type HealthResponse struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
	Service string `json:"service"`
}

const VERSION = "0.0.1"

func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: .env file not found, using environment variables")
	}

	log.Println("Starting Alfred Cloud Server...")

	// Initialize Redis
	redisURL := getEnv("REDIS_URL", "localhost:6379")
	// Remove redis:// prefix if present
	if strings.HasPrefix(redisURL, "redis://") {
		redisURL = strings.TrimPrefix(redisURL, "redis://")
	}
	redisClient := redis.NewClient(&redis.Options{
		Addr: redisURL,
	})

	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("Connected to Redis")

	// Initialize OAuth (separate stores per service)
	gmailTokenStore := security.NewTokenStore(redisClient)
	calendarTokenStore := security.NewTokenStore(redisClient)
	googleAuthHandler := initGoogleAuthForMain(gmailTokenStore, calendarTokenStore)

	// Initialize streams helper
	streamsHelper := streams.NewStreamsHelper(redisClient)

	// Initialize Calendar Webhook Handler
	calendarWebhookHandler := NewCalendarWebhookHandler(redisClient, calendarTokenStore, streamsHelper)

	// Initialize Email Poller
	var emailPoller *email_triage.EmailPoller
	isEmailEnabled := globalGmailClient != nil
	if isEmailEnabled {
		userIDs := parseUserList("EMAIL_POLLER_USERS", "test-user")
		if len(userIDs) > 0 {
			emailPoller = email_triage.NewEmailPoller(globalGmailClient, redisClient, userIDs)
			go func() {
				if err := emailPoller.Start(ctx); err != nil {
					log.Printf("Failed to start email poller: %v", err)
				}
			}()
		} else {
			log.Println("Email poller disabled: EMAIL_POLLER_USERS empty")
		}
	}

	// Initialize shadow calendar service (planner subagent)
	var shadowCalendarService *calendar_planner.ShadowCalendarService
	shadowUsers := parseUserList("CALENDAR_SHADOW_USERS", "test-user")
	if len(shadowUsers) > 0 {
		plannerScript := strings.TrimSpace(os.Getenv("PLANNER_SCRIPT"))
		plannerRunner := calendar_planner.NewCalendarManagerService(plannerScript)
		shadowService, err := calendar_planner.NewShadowCalendarService(redisClient, plannerRunner, calendar_planner.ShadowCalendarOptions{
			UserIDs: shadowUsers,
		})
		if err != nil {
			log.Fatalf("Failed to initialize shadow calendar service: %v", err)
		}
		if err := shadowService.Start(ctx); err != nil {
			log.Fatalf("Failed to start shadow calendar service: %v", err)
		}
		shadowCalendarService = shadowService
		defer shadowCalendarService.Stop()
	} else {
		log.Println("Shadow calendar service disabled: CALENDAR_SHADOW_USERS empty")
	}

	r := mux.NewRouter()

	// Health check endpoint
	r.HandleFunc("/healthz", healthHandler).Methods("GET")
	r.HandleFunc("/", rootHandler).Methods("GET")
	r.HandleFunc("/api/cerberas/chat", cerberasProxyHandler).Methods("POST")

	// OAuth endpoints
	googleAuthHandler.RegisterRoutes(r)

	// Calendar webhook endpoints
	calendarWebhookHandler.RegisterRoutes(r)

	// Calendar manager tool endpoints
	registerCalendarManagerRoutes(r)
	registerShadowCalendarRoutes(r, shadowCalendarService)
	registerProposalConfirmRoutes(r, shadowCalendarService, globalCalendarClient)

	// Test endpoint to easily get auth URL
	r.HandleFunc("/test/gmail-auth-url", getGmailAuthURL).Methods("GET")
	r.HandleFunc("/test/calendar-auth-url", getCalendarAuthURL).Methods("GET")

	// Configure server
	port := getEnv("PORT", "8080")
	srv := &http.Server{
		Handler:      r,
		Addr:         "0.0.0.0:" + port,
		WriteTimeout: 180 * time.Second,
		ReadTimeout:  180 * time.Second,
	}

	log.Printf("Alfred Cloud Server v%s starting on %s", VERSION, srv.Addr)

	// Setup graceful shutdown
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Stop email poller
	if emailPoller != nil {
		emailPoller.Stop()
	}

	// Shutdown server
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := HealthResponse{
		OK:      true,
		Version: VERSION,
		Service: "alfred-cloud",
	}

	json.NewEncoder(w).Encode(response)
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]string{
		"message": "Alfred Cloud API Server",
		"version": VERSION,
		"docs":    "/docs",
	}

	json.NewEncoder(w).Encode(response)
}

// Helper function to get environment variable with default
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Test endpoint to generate Gmail OAuth URL
var (
	globalGmailClient    *security.GoogleServiceClient
	globalCalendarClient *security.GoogleServiceClient
)

func getGmailAuthURL(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		userID = "test-user"
	}

	if globalGmailClient == nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Google client not initialized"})
		return
	}

	ctx := r.Context()
	authURL, state, err := globalGmailClient.GetAuthURL(ctx, security.ServiceGmail, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	response := map[string]interface{}{
		"auth_url": authURL,
		"state":    state,
		"user_id":  userID,
		"service":  "gmail",
		"instructions": []string{
			"1. Visit the auth_url above in your browser",
			"2. Complete Google OAuth authentication with your Gmail account",
			"3. Check /auth/status?user_id=" + userID + " to see authentication status",
			"4. Check Redis keys: oauth_token:" + userID + ":gmail to verify token storage",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func parseUserList(envKey, defaultValue string) []string {
	raw := getEnv(envKey, defaultValue)
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{})
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

// Initialize Google Auth with environment variables for main server
func initGoogleAuthForMain(gmailStore, calendarStore *security.TokenStore) *GoogleAuthHandler {
	redirectURL := getEnv("OAUTH_REDIRECT_URL", "http://localhost:8080/auth/google/callback")

	// Get Gmail client secret from environment
	gmailClientSecret := os.Getenv("GMAIL_CLIENT_SECRET")
	if gmailClientSecret == "" {
		log.Fatal("GMAIL_CLIENT_SECRET environment variable is required")
	}

	gmailClient := security.NewGoogleServiceClient(gmailStore)
	gmailClient.InitializeGmailOnly(gmailClientSecret, redirectURL)
	log.Printf("Initialized Gmail OAuth with client ID: %s", security.DefaultGmailClientID)

	// Get Calendar client credentials from environment (optional)
	calendarClientID := os.Getenv("CALENDAR_CLIENT_ID")
	calendarClientSecret := os.Getenv("CALENDAR_CLIENT_SECRET")

	var calendarClient *security.GoogleServiceClient

	if calendarClientID != "" && calendarClientSecret != "" {
		calendarClient = security.NewGoogleServiceClient(calendarStore)
		calendarClient.InitializeCalendarOnly(calendarClientID, calendarClientSecret, redirectURL)
		log.Printf("Initialized Calendar OAuth with client ID: %s", calendarClientID)
	} else {
		log.Printf("Calendar OAuth credentials not provided, Calendar features disabled")
	}

	// Store global reference for test endpoint
	globalGmailClient = gmailClient
	globalCalendarClient = calendarClient

	return NewGoogleAuthHandler(gmailClient, calendarClient)
}

// Test endpoint to generate Calendar OAuth URL
func getCalendarAuthURL(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		userID = "test-user"
	}

	if globalCalendarClient == nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Google client not initialized"})
		return
	}

	ctx := r.Context()
	authURL, state, err := globalCalendarClient.GetAuthURL(ctx, security.ServiceCalendar, userID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	response := map[string]interface{}{
		"auth_url": authURL,
		"state":    state,
		"user_id":  userID,
		"service":  "calendar",
		"instructions": []string{
			"1. Visit the auth_url above in your browser",
			"2. Complete Google OAuth authentication with your Google Calendar account",
			"3. Check /auth/status?user_id=" + userID + " to see authentication status",
			"4. Check Redis keys: oauth_token:" + userID + ":calendar to verify token storage",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
