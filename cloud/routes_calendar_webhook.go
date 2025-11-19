package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"alfred-cloud/security"
	"alfred-cloud/streams"
	"alfred-cloud/subagents/calendar_planner"
	"alfred-cloud/subagents/productivity"
	"github.com/gorilla/mux"
	"github.com/redis/go-redis/v9"
	calendar "google.golang.org/api/calendar/v3"
	"google.golang.org/api/googleapi"
)

const calendarSyncLookback = 30 * 24 * time.Hour

// CalendarWebhookHandler manages Google Calendar webhook endpoints
type CalendarWebhookHandler struct {
	redisClient      *redis.Client
	tokenStore       *security.TokenStore
	webhookRegistrar *calendar_planner.WebhookRegistrar
	streamsHelper    *streams.StreamsHelper
	heuristicService *productivity.HeuristicService
}

// NewCalendarWebhookHandler creates a new calendar webhook handler
func NewCalendarWebhookHandler(redisClient *redis.Client, tokenStore *security.TokenStore, streamsHelper *streams.StreamsHelper, heuristicService *productivity.HeuristicService) *CalendarWebhookHandler {
	return &CalendarWebhookHandler{
		redisClient:      redisClient,
		tokenStore:       tokenStore,
		streamsHelper:    streamsHelper,
		heuristicService: heuristicService,
	}
}

// RegisterRoutes registers calendar webhook routes
func (h *CalendarWebhookHandler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/calendar/webhook/register", h.handleRegisterWebhook).Methods("POST")
	r.HandleFunc("/calendar/webhook/notification", h.handleWebhookNotification).Methods("POST")
	r.HandleFunc("/calendar/webhook/unregister", h.handleUnregisterWebhook).Methods("POST")
	r.HandleFunc("/calendar/webhook/status", h.handleWebhookStatus).Methods("GET")
	r.HandleFunc("/calendar/webhook/manual_sync", h.handleManualSync).Methods("POST")
}

// WebhookRegistrationRequest represents a request to register a webhook
type WebhookRegistrationRequest struct {
	UserID     string `json:"user_id"`
	CalendarID string `json:"calendar_id,omitempty"`
	WebhookURL string `json:"webhook_url,omitempty"`
}

// WebhookRegistrationResponse represents the response from webhook registration
type WebhookRegistrationResponse struct {
	ChannelID  string    `json:"channel_id"`
	ResourceID string    `json:"resource_id"`
	Expiration time.Time `json:"expiration"`
	WebhookURL string    `json:"webhook_url"`
	Status     string    `json:"status"`
}

// WebhookNotification represents a Google Calendar push notification
type WebhookNotification struct {
	ChannelID     string `json:"channelId"`
	ResourceID    string `json:"resourceId"`
	ResourceState string `json:"resourceState"`
	ResourceURI   string `json:"resourceUri"`
}

// handleRegisterWebhook registers a new webhook for Google Calendar notifications
func (h *CalendarWebhookHandler) handleRegisterWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req WebhookRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.UserID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}

	// Default to primary calendar if not specified
	if req.CalendarID == "" {
		req.CalendarID = "primary"
	}

	// Get Google Calendar service
	googleClient := security.NewGoogleServiceClient(h.tokenStore)
	calendarService, err := googleClient.GetCalendarService(ctx, req.UserID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get Calendar service: %v", err), http.StatusUnauthorized)
		return
	}

	// Generate webhook URL if not provided
	webhookURL := req.WebhookURL
	if webhookURL == "" {
		// Use the server's base URL + webhook notification endpoint
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		webhookURL = fmt.Sprintf("%s://%s/calendar/webhook/notification", scheme, r.Host)
	}

	// Initialize webhook registrar if not already done
	if h.webhookRegistrar == nil {
		h.webhookRegistrar = calendar_planner.NewWebhookRegistrar(h.redisClient, calendarService)
	}

	// Register webhook
	channel, err := h.webhookRegistrar.RegisterWebhook(ctx, req.UserID, req.CalendarID, webhookURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to register webhook: %v", err), http.StatusInternalServerError)
		return
	}

	// Store webhook registration in Redis
	registrationKey := fmt.Sprintf("calendar_webhook:%s:%s", req.UserID, channel.Id)
	registrationData := map[string]interface{}{
		"channel_id":  channel.Id,
		"resource_id": channel.ResourceId,
		"calendar_id": req.CalendarID,
		"webhook_url": webhookURL,
		"expiration":  channel.Expiration,
		"created_at":  time.Now(),
		"user_id":     req.UserID,
	}

	if err := h.redisClient.HMSet(ctx, registrationKey, registrationData).Err(); err != nil {
		log.Printf("Warning: Failed to store webhook registration: %v", err)
		// Continue with response even if Redis storage fails
	}

	// Set expiration on the key - convert from milliseconds to time.Time
	expirationTime := time.Unix(channel.Expiration/1000, 0)
	if err := h.redisClient.Expire(ctx, registrationKey, time.Until(expirationTime)).Err(); err != nil {
		log.Printf("Warning: Failed to set expiration on webhook registration: %v", err)
	}

	if _, _, err := h.getOrCreateSyncToken(ctx, calendarService, req.UserID, req.CalendarID); err != nil {
		log.Printf("Warning: Failed to prime calendar sync token: %v", err)
	}

	response := WebhookRegistrationResponse{
		ChannelID:  channel.Id,
		ResourceID: channel.ResourceId,
		Expiration: expirationTime,
		WebhookURL: webhookURL,
		Status:     "registered",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleWebhookNotification handles incoming webhook notifications from Google Calendar
func (h *CalendarWebhookHandler) handleWebhookNotification(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Verify request headers
	channelID := r.Header.Get("X-Goog-Channel-ID")
	resourceID := r.Header.Get("X-Goog-Resource-ID")
	resourceState := r.Header.Get("X-Goog-Resource-State")

	if channelID == "" || resourceID == "" || resourceState == "" {
		http.Error(w, "Missing required Google headers", http.StatusBadRequest)
		return
	}

	// Log the notification for debugging
	log.Printf("Calendar webhook notification: ChannelID=%s, ResourceID=%s, ResourceState=%s",
		channelID, resourceID, resourceState)

	// Handle webhook validation (Google sends a sync notification first)
	if resourceState == "sync" {
		log.Printf("Webhook validation successful for channel %s", channelID)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Read request body if present (for change notifications)
	var notification WebhookNotification
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&notification); err != nil {
			log.Printf("Warning: Failed to decode notification body: %v", err)
			// Continue with header-based processing
		}
	}

	// Find the user associated with this webhook
	userID, err := h.findUserByChannel(ctx, channelID)
	if err != nil {
		log.Printf("Warning: Could not find user for channel %s: %v", channelID, err)
		w.WriteHeader(http.StatusOK) // Still return 200 to Google
		return
	}

	// Get the resource URI from headers or body
	resourceURI := r.Header.Get("X-Goog-Resource-URI")
	if notification.ResourceURI != "" {
		resourceURI = notification.ResourceURI
	}

	calendarID, err := h.getCalendarIDForChannel(ctx, userID, channelID)
	if err != nil {
		log.Printf("Warning: Failed to resolve calendar for channel %s: %v", channelID, err)
		w.WriteHeader(http.StatusOK)
		return
	}

	changes, err := h.collectCalendarChanges(ctx, userID, calendarID)
	if err != nil {
		log.Printf("Warning: Failed to collect calendar changes for user %s: %v", userID, err)
		w.WriteHeader(http.StatusOK)
		return
	}

	if len(changes) == 0 {
		log.Printf("Calendar webhook: no deltas for user=%s calendar=%s; forcing full fetch window", userID, calendarID)
		googleClient := security.NewGoogleServiceClient(h.tokenStore)
		calendarService, err := googleClient.GetCalendarService(ctx, userID)
		if err != nil {
			log.Printf("Warning: fallback calendar service failed for user %s: %v", userID, err)
		}
		recent, err := h.fetchRecentEvents(ctx, calendarService, calendarID, time.Now().Add(-24*time.Hour))
		if err != nil {
			log.Printf("Warning: recent fallback failed for user %s: %v", userID, err)
		} else if len(recent) > 0 {
			changes = append(changes, recent...)
			log.Printf("Calendar webhook: fallback captured %d events for user=%s calendar=%s", len(recent), userID, calendarID)
		}
		if len(changes) == 0 {
			log.Printf("Calendar webhook: still no changes; enqueueing minimal marker for user=%s calendar=%s", userID, calendarID)
			changes = append(changes, map[string]interface{}{
				"type":              "calendar_delta",
				"user_id":           userID,
				"calendar_id":       calendarID,
				"resource_state":    "exists",
				"resource_uri":      resourceURI,
				"channel_id":        channelID,
				"resource_id":       resourceID,
				"notified_at":       time.Now().UTC().Format(time.RFC3339Nano),
				"change_type":       "noop",
				"event_summary":     "noop",
				"event_id":          "",
				"event_description": "",
			})
		}
	}

	if h.heuristicService != nil {
		for _, changeData := range changes {
			if fmt.Sprint(changeData["change_type"]) == "deleted" {
				continue
			}
			payload, err := h.buildHeuristicPayload(changeData, userID)
			if err != nil {
				log.Printf("Warning: skipping heuristic for event %v: %v", changeData["event_id"], err)
				continue
			}
			log.Printf("Prod heuristic: generating expected apps for user=%s event=%s title=%q", userID, payload.EventID, payload.Title)
			if stored, err := h.heuristicService.UpsertEventHeuristic(ctx, payload); err != nil {
				log.Printf("Warning: failed to persist productivity heuristic for event %s: %v", payload.EventID, err)
			} else {
				log.Printf("Prod heuristic: stored event=%s apps=%v start=%s end=%s", stored.EventID, stored.ExpectedApps, stored.StartTime, stored.EndTime)
			}
		}
	}

	// Store the calendar change notification in the input stream
	if h.streamsHelper != nil {
		inputKey := fmt.Sprintf("user:%s:in:calendar", userID)
		for _, changeData := range changes {
			changeData["channel_id"] = channelID
			changeData["resource_id"] = resourceID
			changeData["resource_state"] = resourceState
			changeData["resource_uri"] = resourceURI
			changeData["notified_at"] = time.Now().UTC().Format(time.RFC3339Nano)
			changeData["user_id"] = userID
			changeData["calendar_id"] = calendarID

			if _, err := h.streamsHelper.AppendToStream(ctx, inputKey, changeData); err != nil {
				log.Printf("Warning: Failed to store calendar change in stream: %v", err)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

// handleUnregisterWebhook unregisters a calendar webhook
func (h *CalendarWebhookHandler) handleUnregisterWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req WebhookRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.UserID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}

	// Find webhook registration for this user
	pattern := fmt.Sprintf("calendar_webhook:%s:*", req.UserID)
	keys, err := h.redisClient.Keys(ctx, pattern).Result()
	if err != nil {
		http.Error(w, "Failed to find webhooks", http.StatusInternalServerError)
		return
	}

	if len(keys) == 0 {
		http.Error(w, "No webhooks found for user", http.StatusNotFound)
		return
	}

	// Get registration details
	registrationData, err := h.redisClient.HMGet(ctx, keys[0], "channel_id", "resource_id").Result()
	if err != nil {
		http.Error(w, "Failed to get webhook details", http.StatusInternalServerError)
		return
	}

	channelID, ok1 := registrationData[0].(string)
	resourceID, ok2 := registrationData[1].(string)
	if !ok1 || !ok2 {
		http.Error(w, "Invalid webhook registration data", http.StatusInternalServerError)
		return
	}

	// Initialize webhook registrar if not already done
	if h.webhookRegistrar == nil {
		googleClient := security.NewGoogleServiceClient(h.tokenStore)
		calendarService, err := googleClient.GetCalendarService(ctx, req.UserID)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to get Calendar service: %v", err), http.StatusUnauthorized)
			return
		}
		h.webhookRegistrar = calendar_planner.NewWebhookRegistrar(h.redisClient, calendarService)
	}

	// Unregister webhook
	if err := h.webhookRegistrar.UnregisterWebhook(ctx, channelID, resourceID); err != nil {
		http.Error(w, fmt.Sprintf("Failed to unregister webhook: %v", err), http.StatusInternalServerError)
		return
	}

	// Remove from Redis
	if err := h.redisClient.Del(ctx, keys[0]).Err(); err != nil {
		log.Printf("Warning: Failed to remove webhook registration from Redis: %v", err)
	}

	response := map[string]string{
		"status":     "unregistered",
		"channel_id": channelID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleWebhookStatus returns the status of registered webhooks
func (h *CalendarWebhookHandler) handleWebhookStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		http.Error(w, "user_id parameter is required", http.StatusBadRequest)
		return
	}

	// Find webhook registrations for this user
	pattern := fmt.Sprintf("calendar_webhook:%s:*", userID)
	keys, err := h.redisClient.Keys(ctx, pattern).Result()
	if err != nil {
		http.Error(w, "Failed to find webhooks", http.StatusInternalServerError)
		return
	}

	webhooks := make([]map[string]interface{}, 0, len(keys))
	for _, key := range keys {
		data, err := h.redisClient.HMGet(ctx, key,
			"channel_id", "resource_id", "calendar_id", "webhook_url", "expiration", "created_at").Result()
		if err != nil {
			log.Printf("Warning: Failed to get webhook data for %s: %v", key, err)
			continue
		}

		webhook := map[string]interface{}{
			"registration_key": key,
		}
		if data[0] != nil {
			webhook["channel_id"] = data[0].(string)
		}
		if data[1] != nil {
			webhook["resource_id"] = data[1].(string)
		}
		if data[2] != nil {
			webhook["calendar_id"] = data[2].(string)
		}
		if data[3] != nil {
			webhook["webhook_url"] = data[3].(string)
		}
		if data[4] != nil {
			webhook["expiration"] = data[4].(string)
		}
		if data[5] != nil {
			webhook["created_at"] = data[5].(string)
		}

		webhooks = append(webhooks, webhook)
	}

	response := map[string]interface{}{
		"user_id":  userID,
		"webhooks": webhooks,
		"count":    len(webhooks),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// findUserByChannel finds the user associated with a given channel ID
func (h *CalendarWebhookHandler) findUserByChannel(ctx context.Context, channelID string) (string, error) {
	pattern := "calendar_webhook:*"
	keys, err := h.redisClient.Keys(ctx, pattern).Result()
	if err != nil {
		return "", fmt.Errorf("failed to search webhook keys: %w", err)
	}

	for _, key := range keys {
		channel, err := h.redisClient.HGet(ctx, key, "channel_id").Result()
		if err == nil && channel == channelID {
			userID, err := h.redisClient.HGet(ctx, key, "user_id").Result()
			if err == nil {
				return userID, nil
			}
		}
	}

	return "", fmt.Errorf("no user found for channel %s", channelID)
}

func (h *CalendarWebhookHandler) getCalendarIDForChannel(ctx context.Context, userID, channelID string) (string, error) {
	key := fmt.Sprintf("calendar_webhook:%s:%s", userID, channelID)
	calendarID, err := h.redisClient.HGet(ctx, key, "calendar_id").Result()
	if err != nil {
		return "", fmt.Errorf("failed to lookup calendar for channel %s: %w", channelID, err)
	}
	if calendarID == "" {
		return "", fmt.Errorf("calendar id missing for channel %s", channelID)
	}
	return calendarID, nil
}

func (h *CalendarWebhookHandler) collectCalendarChanges(ctx context.Context, userID, calendarID string) ([]map[string]interface{}, error) {
	googleClient := security.NewGoogleServiceClient(h.tokenStore)
	calendarService, err := googleClient.GetCalendarService(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get Calendar service for user %s: %w", userID, err)
	}

	syncToken, isNew, err := h.getOrCreateSyncToken(ctx, calendarService, userID, calendarID)
	if err != nil {
		return nil, err
	}

	if isNew {
		log.Printf("Initialized calendar sync token for user %s calendar %s", userID, calendarID)
		return nil, nil
	}

	events, nextToken, err := h.fetchChangedEvents(ctx, calendarService, calendarID, syncToken)
	if err != nil {
		if apiErr, ok := err.(*googleapi.Error); ok && apiErr.Code == http.StatusGone {
			log.Printf("Sync token expired for user %s calendar %s, refreshing", userID, calendarID)
			if _, refreshErr := h.refreshSyncToken(ctx, calendarService, userID, calendarID); refreshErr != nil {
				return nil, refreshErr
			}
			return nil, nil
		}
		return nil, err
	}

	if nextToken != "" {
		if err := h.storeSyncToken(ctx, userID, calendarID, nextToken); err != nil {
			log.Printf("Warning: failed to persist next sync token: %v", err)
		}
	}

	changes := make([]map[string]interface{}, 0, len(events))
	for _, event := range events {
		changes = append(changes, normalizeEventChange(event, userID, calendarID))
	}

	return changes, nil
}

func (h *CalendarWebhookHandler) getOrCreateSyncToken(ctx context.Context, calendarService *calendar.Service, userID, calendarID string) (string, bool, error) {
	key := h.syncTokenKey(userID, calendarID)
	token, err := h.redisClient.Get(ctx, key).Result()
	if err == nil && token != "" {
		return token, false, nil
	}
	if err != nil && err != redis.Nil {
		return "", false, fmt.Errorf("failed to read calendar sync token: %w", err)
	}

	token, err = h.fetchInitialSyncToken(ctx, calendarService, calendarID)
	if err != nil {
		return "", false, err
	}

	if err := h.redisClient.Set(ctx, key, token, 0).Err(); err != nil {
		return "", false, fmt.Errorf("failed to store calendar sync token: %w", err)
	}

	return token, true, nil
}

func (h *CalendarWebhookHandler) refreshSyncToken(ctx context.Context, calendarService *calendar.Service, userID, calendarID string) (string, error) {
	token, err := h.fetchInitialSyncToken(ctx, calendarService, calendarID)
	if err != nil {
		return "", err
	}
	if err := h.storeSyncToken(ctx, userID, calendarID, token); err != nil {
		return "", err
	}
	return token, nil
}

func (h *CalendarWebhookHandler) fetchInitialSyncToken(ctx context.Context, calendarService *calendar.Service, calendarID string) (string, error) {
	call := calendarService.Events.List(calendarID).
		ShowDeleted(true).
		SingleEvents(true).
		TimeMin(time.Now().Add(-calendarSyncLookback).Format(time.RFC3339))

	resp, err := call.Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("failed to fetch initial sync token: %w", err)
	}

	if resp.NextSyncToken == "" {
		return "", fmt.Errorf("calendar API did not return nextSyncToken")
	}

	return resp.NextSyncToken, nil
}

func (h *CalendarWebhookHandler) fetchChangedEvents(ctx context.Context, calendarService *calendar.Service, calendarID, syncToken string) ([]*calendar.Event, string, error) {
	var (
		pageToken string
		events    []*calendar.Event
		nextToken string
	)

	for {
		call := calendarService.Events.List(calendarID).
			ShowDeleted(true).
			SingleEvents(true).
			SyncToken(syncToken)

		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Context(ctx).Do()
		if err != nil {
			return nil, "", fmt.Errorf("failed to fetch changed events: %w", err)
		}

		events = append(events, resp.Items...)

		if resp.NextPageToken != "" {
			pageToken = resp.NextPageToken
			continue
		}

		nextToken = resp.NextSyncToken
		break
	}

	return events, nextToken, nil
}

func (h *CalendarWebhookHandler) fetchRecentEvents(ctx context.Context, calendarService *calendar.Service, calendarID string, since time.Time) ([]map[string]interface{}, error) {
	call := calendarService.Events.List(calendarID).
		ShowDeleted(true).
		SingleEvents(true).
		TimeMin(since.Format(time.RFC3339)).
		TimeMax(time.Now().Add(48 * time.Hour).Format(time.RFC3339))

	resp, err := call.Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("fetch recent events failed: %w", err)
	}

	changes := make([]map[string]interface{}, 0, len(resp.Items))
	for _, event := range resp.Items {
		changes = append(changes, normalizeEventChange(event, "", calendarID))
	}
	return changes, nil
}

// handleManualSync pulls recent events and pushes them through the same pipeline as webhook notifications.
func (h *CalendarWebhookHandler) handleManualSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req struct {
		UserID     string `json:"user_id"`
		CalendarID string `json:"calendar_id,omitempty"`
		LookbackH  int    `json:"lookback_hours,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.UserID == "" {
		http.Error(w, "user_id required", http.StatusBadRequest)
		return
	}
	if req.CalendarID == "" {
		req.CalendarID = "primary"
	}
	lookback := time.Duration(req.LookbackH) * time.Hour
	if lookback <= 0 {
		lookback = 24 * time.Hour
	}

	googleClient := security.NewGoogleServiceClient(h.tokenStore)
	calendarService, err := googleClient.GetCalendarService(ctx, req.UserID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get calendar service: %v", err), http.StatusUnauthorized)
		return
	}

	events, err := h.fetchRecentEvents(ctx, calendarService, req.CalendarID, time.Now().Add(-lookback))
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to fetch events: %v", err), http.StatusInternalServerError)
		return
	}

	if h.streamsHelper != nil {
		inputKey := fmt.Sprintf("user:%s:in:calendar", req.UserID)
		for _, changeData := range events {
			changeData["channel_id"] = "manual-sync"
			changeData["resource_state"] = "exists"
			changeData["resource_uri"] = fmt.Sprint(changeData["resource_uri"])
			changeData["notified_at"] = time.Now().UTC().Format(time.RFC3339Nano)
			changeData["user_id"] = req.UserID
			changeData["calendar_id"] = req.CalendarID

			if _, err := h.streamsHelper.AppendToStream(ctx, inputKey, changeData); err != nil {
				log.Printf("Manual sync: failed to store calendar change: %v", err)
			}
		}
	}

	if h.heuristicService != nil {
		for _, changeData := range events {
			if fmt.Sprint(changeData["change_type"]) == "deleted" {
				continue
			}
			payload, err := h.buildHeuristicPayload(changeData, req.UserID)
			if err != nil {
				log.Printf("Manual sync: skipping heuristic for event %v: %v", changeData["event_id"], err)
				continue
			}
			log.Printf("Manual sync: heuristic generate user=%s event=%s title=%q", req.UserID, payload.EventID, payload.Title)
			if stored, err := h.heuristicService.UpsertEventHeuristic(ctx, payload); err != nil {
				log.Printf("Manual sync: heuristic persist failed for event %s: %v", payload.EventID, err)
			} else {
				log.Printf("Manual sync: stored heuristic event=%s apps=%v", stored.EventID, stored.ExpectedApps)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"user_id":  req.UserID,
		"calendar": req.CalendarID,
		"count":    len(events),
		"status":   "synced",
	})
}

func (h *CalendarWebhookHandler) storeSyncToken(ctx context.Context, userID, calendarID, token string) error {
	return h.redisClient.Set(ctx, h.syncTokenKey(userID, calendarID), token, 0).Err()
}

func (h *CalendarWebhookHandler) syncTokenKey(userID, calendarID string) string {
	return fmt.Sprintf("calendar_sync:%s:%s", userID, calendarID)
}

func normalizeEventChange(event *calendar.Event, userID, calendarID string) map[string]interface{} {
	startTime, startTZ, startAllDay := formatEventDateTime(event.Start)
	endTime, endTZ, _ := formatEventDateTime(event.End)

	attendeesJSON, _ := json.Marshal(event.Attendees)
	rawJSON, _ := json.Marshal(event)

	creatorEmail := ""
	if event.Creator != nil {
		creatorEmail = event.Creator.Email
	}

	organizerEmail := ""
	if event.Organizer != nil {
		organizerEmail = event.Organizer.Email
	}

	changeType := determineChangeType(event)

	return map[string]interface{}{
		"type":               "calendar_delta",
		"user_id":            userID,
		"calendar_id":        calendarID,
		"event_id":           event.Id,
		"event_summary":      event.Summary,
		"event_description":  event.Description,
		"event_location":     event.Location,
		"start_time":         startTime,
		"end_time":           endTime,
		"start_timezone":     startTZ,
		"end_timezone":       endTZ,
		"all_day":            strconv.FormatBool(startAllDay),
		"status":             event.Status,
		"change_type":        changeType,
		"html_link":          event.HtmlLink,
		"creator_email":      creatorEmail,
		"organizer_email":    organizerEmail,
		"attendees_json":     string(attendeesJSON),
		"attendees_count":    strconv.Itoa(len(event.Attendees)),
		"sequence":           strconv.Itoa(int(event.Sequence)),
		"created":            event.Created,
		"updated":            event.Updated,
		"hangout_link":       event.HangoutLink,
		"recurring_event_id": event.RecurringEventId,
		"raw_event":          string(rawJSON),
	}
}

func (h *CalendarWebhookHandler) buildHeuristicPayload(change map[string]interface{}, userID string) (productivity.EventPayload, error) {
	payload := productivity.EventPayload{
		UserID:      userID,
		EventID:     fmt.Sprint(change["event_id"]),
		Title:       fmt.Sprint(change["event_summary"]),
		Description: fmt.Sprint(change["event_description"]),
	}
	if payload.EventID == "" {
		return payload, fmt.Errorf("event_id missing")
	}

	start, err := parseEventTimestamp(fmt.Sprint(change["start_time"]))
	if err != nil {
		return payload, fmt.Errorf("invalid start_time: %w", err)
	}
	payload.StartTime = start

	endRaw := fmt.Sprint(change["end_time"])
	if endRaw != "" {
		if end, err := parseEventTimestamp(endRaw); err == nil {
			payload.EndTime = end
		} else {
			return payload, fmt.Errorf("invalid end_time: %w", err)
		}
	}
	if payload.EndTime.IsZero() {
		payload.EndTime = payload.StartTime.Add(time.Hour)
	}

	return payload, nil
}

func formatEventDateTime(dt *calendar.EventDateTime) (string, string, bool) {
	if dt == nil {
		return "", "", false
	}
	if dt.DateTime != "" {
		return dt.DateTime, dt.TimeZone, false
	}
	if dt.Date != "" {
		return dt.Date, dt.TimeZone, true
	}
	return "", dt.TimeZone, false
}

func determineChangeType(event *calendar.Event) string {
	if event.Status == "cancelled" {
		return "deleted"
	}

	created, errCreated := time.Parse(time.RFC3339, event.Created)
	updated, errUpdated := time.Parse(time.RFC3339, event.Updated)

	if errCreated == nil && errUpdated == nil {
		if updated.Sub(created) < time.Second {
			return "created"
		}
		if updated.After(created) {
			return "updated"
		}
	}

	if len(event.Recurrence) > 0 {
		return "recurrence_update"
	}

	return "updated"
}

func parseEventTimestamp(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05-07:00",
		"2006-01-02",
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, nil
		}
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("could not parse time %q", raw)
}
