package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"alfred-cloud/security"

	"github.com/gorilla/mux"
)

// GoogleAuthHandler handles Google OAuth authentication
type GoogleAuthHandler struct {
	gmailClient    *security.GoogleServiceClient
	calendarClient *security.GoogleServiceClient
}

type serviceClient struct {
	service security.ServiceScope
	client  *security.GoogleServiceClient
}

func (h *GoogleAuthHandler) availableClients() []serviceClient {
	clients := []serviceClient{}
	if h.gmailClient != nil {
		clients = append(clients, serviceClient{service: security.ServiceGmail, client: h.gmailClient})
	}
	if h.calendarClient != nil {
		clients = append(clients, serviceClient{service: security.ServiceCalendar, client: h.calendarClient})
	}
	return clients
}

func (h *GoogleAuthHandler) clientForService(service security.ServiceScope) (*security.GoogleServiceClient, error) {
	switch service {
	case security.ServiceGmail:
		if h.gmailClient == nil {
			return nil, fmt.Errorf("gmail OAuth not configured")
		}
		return h.gmailClient, nil
	case security.ServiceCalendar:
		if h.calendarClient == nil {
			return nil, fmt.Errorf("calendar OAuth not configured")
		}
		return h.calendarClient, nil
	default:
		return nil, fmt.Errorf("invalid service. Must be 'gmail' or 'calendar'")
	}
}

func (h *GoogleAuthHandler) resolveAuthContext(ctx context.Context, serviceHint security.ServiceScope, userID, state string) (*security.GoogleServiceClient, security.ServiceScope, string, error) {
	if state == "" {
		return nil, "", "", fmt.Errorf("state parameter is required")
	}

	if serviceHint != "" {
		client, err := h.clientForService(serviceHint)
		if err != nil {
			return nil, "", "", err
		}
		if userID == "" {
			resolvedUser, err := client.ResolveUserIDFromState(ctx, state)
			if err != nil {
				return nil, "", "", fmt.Errorf("invalid or expired state parameter")
			}
			userID = resolvedUser
		}
		return client, serviceHint, userID, nil
	}

	clients := h.availableClients()
	if len(clients) == 0 {
		return nil, "", "", fmt.Errorf("no OAuth services configured")
	}

	if userID != "" {
		for _, entry := range clients {
			service, err := entry.client.ResolveServiceFromState(ctx, userID, state)
			if err == nil && service == entry.service {
				return entry.client, service, userID, nil
			}
		}
	} else {
		for _, entry := range clients {
			resolvedUser, err := entry.client.ResolveUserIDFromState(ctx, state)
			if err == nil {
				service, svcErr := entry.client.ResolveServiceFromState(ctx, resolvedUser, state)
				if svcErr == nil && service == entry.service {
					return entry.client, service, resolvedUser, nil
				}
			}
		}
	}

	return nil, "", "", fmt.Errorf("invalid or expired state parameter")
}

// NewGoogleAuthHandler creates a new Google auth handler
func NewGoogleAuthHandler(gmailClient, calendarClient *security.GoogleServiceClient) *GoogleAuthHandler {
	return &GoogleAuthHandler{
		gmailClient:    gmailClient,
		calendarClient: calendarClient,
	}
}

// AuthRequest represents an authentication request
type AuthRequest struct {
	UserID  string                `json:"user_id"`
	Service security.ServiceScope `json:"service"`
}

// AuthResponse represents an authentication response
type AuthResponse struct {
	AuthURL string `json:"auth_url"`
	State   string `json:"state"`
}

// CallbackResponse represents OAuth callback response
type CallbackResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Service string `json:"service,omitempty"`
}

// StatusResponse represents service status response
type StatusResponse struct {
	UserID   string                           `json:"user_id"`
	Services map[security.ServiceScope]string `json:"services"`
}

// RegisterGoogleAuthRoutes registers Google authentication routes
func (h *GoogleAuthHandler) RegisterRoutes(router *mux.Router) {
	// Authentication initiation
	router.HandleFunc("/auth/google", h.StartAuth).Methods("POST")

	// OAuth callback
	router.HandleFunc("/auth/google/callback", h.HandleCallback).Methods("GET")

	// Service status
	router.HandleFunc("/auth/status", h.GetStatus).Methods("GET")

	// Service validation
	router.HandleFunc("/auth/validate/{service}", h.ValidateService).Methods("GET")

	// Revoke access
	router.HandleFunc("/auth/revoke/{service}", h.RevokeAccess).Methods("DELETE")
}

// StartAuth initiates OAuth authentication for a service
func (h *GoogleAuthHandler) StartAuth(w http.ResponseWriter, r *http.Request) {
	var req AuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.UserID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}

	if req.Service != security.ServiceGmail && req.Service != security.ServiceCalendar {
		http.Error(w, "invalid service. Must be 'gmail' or 'calendar'", http.StatusBadRequest)
		return
	}

	client, err := h.clientForService(req.Service)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	authURL, state, err := client.GetAuthURL(r.Context(), req.Service, req.UserID)
	if err != nil {
		log.Printf("Failed to generate auth URL: %v", err)
		http.Error(w, "Failed to generate authentication URL", http.StatusInternalServerError)
		return
	}

	response := AuthResponse{
		AuthURL: authURL,
		State:   state,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleCallback handles OAuth callback from Google
func (h *GoogleAuthHandler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Get OAuth parameters
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errorParam := r.URL.Query().Get("error")

	// Handle OAuth errors
	if errorParam != "" {
		log.Printf("OAuth error: %s", errorParam)
		http.Error(w, fmt.Sprintf("OAuth failed: %s", errorParam), http.StatusBadRequest)
		return
	}

	if code == "" {
		http.Error(w, "Authorization code is required", http.StatusBadRequest)
		return
	}

	if state == "" {
		http.Error(w, "State parameter is required", http.StatusBadRequest)
		return
	}

	serviceHint := security.ServiceScope(r.URL.Query().Get("service"))
	userID := r.URL.Query().Get("user_id")

	client, service, resolvedUserID, err := h.resolveAuthContext(ctx, serviceHint, userID, state)
	if err != nil {
		log.Printf("OAuth callback resolution failed: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	userID = resolvedUserID

	if client == nil {
		http.Error(w, "OAuth client not initialized", http.StatusBadRequest)
		return
	}

	_, err = client.ExchangeCodeForToken(ctx, service, userID, code, state)
	if err != nil {
		log.Printf("Failed to exchange code for token: %v", err)
		http.Error(w, "Failed to exchange authorization code for token", http.StatusInternalServerError)
		return
	}

	log.Printf("Successfully authenticated user %s for service %s", userID, service)

	// Validate the service access immediately
	switch service {
	case security.ServiceGmail:
		if err := client.ValidateGmailAccess(ctx, userID); err != nil {
			log.Printf("Gmail access validation failed: %v", err)
		}
	case security.ServiceCalendar:
		if err := client.ValidateCalendarAccess(ctx, userID); err != nil {
			log.Printf("Calendar access validation failed: %v", err)
		}
	}

	// Return success response
	response := CallbackResponse{
		Success: true,
		Message: fmt.Sprintf("Successfully authenticated for %s", service),
		Service: string(service),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetStatus returns authentication status for all services
func (h *GoogleAuthHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		http.Error(w, "user_id parameter is required", http.StatusBadRequest)
		return
	}

	clients := h.availableClients()
	if len(clients) == 0 {
		http.Error(w, "OAuth services not configured", http.StatusServiceUnavailable)
		return
	}

	status := make(map[security.ServiceScope]string)
	for _, entry := range clients {
		for svc, val := range entry.client.GetServiceStatus(r.Context(), userID) {
			status[svc] = val
		}
	}

	response := StatusResponse{
		UserID:   userID,
		Services: status,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ValidateService validates access to a specific service
func (h *GoogleAuthHandler) ValidateService(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serviceStr := vars["service"]
	userID := r.URL.Query().Get("user_id")

	if userID == "" {
		http.Error(w, "user_id parameter is required", http.StatusBadRequest)
		return
	}

	service := security.ServiceScope(serviceStr)
	client, err := h.clientForService(service)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	switch service {
	case security.ServiceGmail:
		err = client.ValidateGmailAccess(r.Context(), userID)
	case security.ServiceCalendar:
		err = client.ValidateCalendarAccess(r.Context(), userID)
	default:
		http.Error(w, "invalid service. Must be 'gmail' or 'calendar'", http.StatusBadRequest)
		return
	}

	response := map[string]interface{}{
		"valid":   err == nil,
		"service": serviceStr,
		"user_id": userID,
	}

	if err != nil {
		response["error"] = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// RevokeAccess revokes access for a specific service
func (h *GoogleAuthHandler) RevokeAccess(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	serviceStr := vars["service"]
	userID := r.URL.Query().Get("user_id")

	if userID == "" {
		http.Error(w, "user_id parameter is required", http.StatusBadRequest)
		return
	}

	service := security.ServiceScope(serviceStr)
	client, err := h.clientForService(service)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err = client.RevokeServiceAccess(r.Context(), service, userID)

	response := map[string]interface{}{
		"success": err == nil,
		"service": serviceStr,
		"user_id": userID,
	}

	if err != nil {
		response["error"] = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
