package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"alfred-cloud/security"
	"alfred-cloud/streams"
	"github.com/redis/go-redis/v9"
)

func TestHandleRegisterWebhook(t *testing.T) {
	// This test requires real Redis and Google Calendar credentials
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("Skipping test: REDIS_URL environment variable not set")
	}

	client, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("Failed to parse Redis URL: %v", err)
	}
	redisClient := redis.NewClient(client)

	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Fatalf("Failed to connect to Redis: %v", err)
	}

	tokenStore := security.NewTokenStore(redisClient)
	streamsHelper := streams.NewStreamsHelper(redisClient)
	handler := NewCalendarWebhookHandler(redisClient, tokenStore, streamsHelper, nil)

	t.Run("missing user_id", func(t *testing.T) {
		reqBody := map[string]string{
			"calendar_id": "primary",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/calendar/webhook/register", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler.handleRegisterWebhook(rr, req)

		if status := rr.Code; status != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, status)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/calendar/webhook/register", bytes.NewBuffer([]byte("invalid json")))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler.handleRegisterWebhook(rr, req)

		if status := rr.Code; status != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, status)
		}
	})
}

func TestHandleWebhookNotification(t *testing.T) {
	// This test requires real Redis connection and Google Calendar credentials
	// Skip if not running in integration environment
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Test with real Redis - requires REDIS_URL environment variable
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("Skipping test: REDIS_URL environment variable not set")
	}

	// Initialize real Redis client
	client, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("Failed to parse Redis URL: %v", err)
	}
	redisClient := redis.NewClient(client)

	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Fatalf("Failed to connect to Redis: %v", err)
	}

	// Initialize components with real Redis
	tokenStore := security.NewTokenStore(redisClient)
	streamsHelper := streams.NewStreamsHelper(redisClient)
	handler := NewCalendarWebhookHandler(redisClient, tokenStore, streamsHelper, nil)

	// Test sync notification (validation)
	t.Run("sync notification", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/calendar/webhook/notification", nil)
		req.Header.Set("X-Goog-Channel-ID", "test-channel-id")
		req.Header.Set("X-Goog-Resource-ID", "test-resource-id")
		req.Header.Set("X-Goog-Resource-State", "sync")

		rr := httptest.NewRecorder()
		handler.handleWebhookNotification(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, status)
		}
	})

	// Test missing headers
	t.Run("missing headers", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/calendar/webhook/notification", nil)
		req.Header.Set("X-Goog-Channel-ID", "test-channel-id")
		// Missing other required headers

		rr := httptest.NewRecorder()
		handler.handleWebhookNotification(rr, req)

		if status := rr.Code; status != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, status)
		}
	})

	// Test with real calendar webhook data (requires actual Google Calendar setup)
	t.Run("real webhook notification", func(t *testing.T) {
		userID := os.Getenv("TEST_CALENDAR_USER_ID")
		if userID == "" {
			t.Skip("Skipping real webhook test: TEST_CALENDAR_USER_ID not set")
		}

		// First register a webhook for the user
		reqBody := map[string]string{
			"user_id": userID,
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/calendar/webhook/register", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler.handleRegisterWebhook(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Logf("Webhook registration returned status %d: %s", status, rr.Body.String())
			t.Skip("Skipping webhook notification test: webhook registration failed")
		}

		// Now test webhook notification with real data
		webhookReq := httptest.NewRequest("POST", "/calendar/webhook/notification", nil)
		webhookReq.Header.Set("X-Goog-Channel-ID", "test-channel")
		webhookReq.Header.Set("X-Goog-Resource-ID", "test-resource")
		webhookReq.Header.Set("X-Goog-Resource-State", "exists")
		webhookReq.Header.Set("X-Goog-Resource-URI", "https://www.googleapis.com/calendar/v3/calendars/primary/events/test-event")

		webhookRR := httptest.NewRecorder()
		handler.handleWebhookNotification(webhookRR, webhookReq)

		if status := webhookRR.Code; status != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, status)
		}

		// Check that data was written to the stream
		streamKey := fmt.Sprintf("user:%s:in:calendar", userID)
		streamLength, err := streamsHelper.GetStreamLength(ctx, streamKey)
		if err != nil {
			t.Errorf("Failed to get stream length: %v", err)
		}
		if streamLength == 0 {
			t.Error("Expected stream to contain data, but it was empty")
		}
	})
}
