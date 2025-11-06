package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"alfred-cloud/streams"
)

// Test setup helper that creates a clean Redis test environment
func setupTestRedis(t *testing.T) (*redis.Client, string) {
	// Connect to test Redis instance
	client, err := streams.Init(context.Background())
	if err != nil {
		t.Skipf("Redis not available for testing: %v", err)
		return nil, ""
	}

	// Use a unique test stream for each test
	testStream := fmt.Sprintf("test:user:%d:in:productivity", time.Now().UnixNano())

	return client, testStream
}

// Helper to clean up test data
func cleanupTestStream(ctx context.Context, client *redis.Client, stream string) {
	client.Del(ctx, stream)
}

func TestHeartbeatHandler_Success(t *testing.T) {
	ctx := context.Background()
	client, testStream := setupTestRedis(t)
	defer client.Close()
	defer cleanupTestStream(ctx, client, testStream)

	// Override global productivityStream for test
	originalStream := productivityStream
	productivityStream = testStream
	defer func() { productivityStream = originalStream }()

	// Prepare valid heartbeat payload
	payload := heartbeatRequest{
		BundleID:    "com.test.App",
		WindowTitle: "Test Window",
		URL:         "https://example.com",
		ActivityID:  "test-activity-123",
		Timestamp:   "2025-01-06T12:00:00Z",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/prod/heartbeat", bytes.NewReader(body))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	// Call the handler
	heartbeatHandler(w, req)

	// Verify HTTP response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if response["ok"] != true {
		t.Error("Expected ok=true in response")
	}

	if response["stream"] != testStream {
		t.Errorf("Expected stream=%s, got %v", testStream, response["stream"])
	}

	// Verify Redis stream entry was created
	streamLen, err := client.XLen(ctx, testStream).Result()
	if err != nil {
		t.Fatalf("Failed to get stream length: %v", err)
	}

	if streamLen != 1 {
		t.Errorf("Expected stream length 1, got %d", streamLen)
	}

	// Verify stream content
	entries, err := client.XRange(ctx, testStream, "-", "+").Result()
	if err != nil {
		t.Fatalf("Failed to read stream: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	values := entry.Values

	if values["bundle_id"] != payload.BundleID {
		t.Errorf("Expected bundle_id=%s, got %v", payload.BundleID, values["bundle_id"])
	}

	if values["window_title"] != payload.WindowTitle {
		t.Errorf("Expected window_title=%s, got %v", payload.WindowTitle, values["window_title"])
	}

	if values["url"] != payload.URL {
		t.Errorf("Expected url=%s, got %v", payload.URL, values["url"])
	}

	if values["activity_id"] != payload.ActivityID {
		t.Errorf("Expected activity_id=%s, got %v", payload.ActivityID, values["activity_id"])
	}

	if values["ts"] != payload.Timestamp {
		t.Errorf("Expected ts=%s, got %v", payload.Timestamp, values["ts"])
	}
}

func TestHeartbeatHandler_MissingBundleID(t *testing.T) {
	payload := heartbeatRequest{
		WindowTitle: "Test Window",
		URL:         "https://example.com",
		ActivityID:  "test-activity-123",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/prod/heartbeat", bytes.NewReader(body))
	w := httptest.NewRecorder()

	heartbeatHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	expectedError := "bundle_id required"
	actualError := w.Body.String()
	// Trim whitespace for comparison
	if len(actualError) > 0 && actualError[len(actualError)-1] == '\n' {
		actualError = actualError[:len(actualError)-1]
	}
	if actualError != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, actualError)
	}
}

func TestHeartbeatHandler_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest("POST", "/prod/heartbeat", bytes.NewReader([]byte("invalid json")))
	w := httptest.NewRecorder()

	heartbeatHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	expectedError := "invalid JSON body"
	actualError := w.Body.String()
	// Trim whitespace for comparison
	if len(actualError) > 0 && actualError[len(actualError)-1] == '\n' {
		actualError = actualError[:len(actualError)-1]
	}
	if actualError != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, actualError)
	}
}

func TestHeartbeatHandler_DefaultTimestamp(t *testing.T) {
	ctx := context.Background()
	client, testStream := setupTestRedis(t)
	defer client.Close()
	defer cleanupTestStream(ctx, client, testStream)

	originalStream := productivityStream
	productivityStream = testStream
	defer func() { productivityStream = originalStream }()

	// Payload without timestamp
	payload := heartbeatRequest{
		BundleID:    "com.test.App",
		WindowTitle: "Test Window",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/prod/heartbeat", bytes.NewReader(body))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	beforeRequest := time.Now().UTC()
	heartbeatHandler(w, req)
	afterRequest := time.Now().UTC()

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Verify timestamp was added
	entries, err := client.XRange(ctx, testStream, "-", "+").Result()
	if err != nil {
		t.Fatalf("Failed to read stream: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	timestampStr := entries[0].Values["ts"].(string)
	timestamp, err := time.Parse(time.RFC3339Nano, timestampStr)
	if err != nil {
		t.Fatalf("Failed to parse timestamp: %v", err)
	}

	if timestamp.Before(beforeRequest) || timestamp.After(afterRequest) {
		t.Errorf("Timestamp %v is outside expected range [%v, %v]", timestamp, beforeRequest, afterRequest)
	}
}

func TestHeartbeatHandler_StreamLengthIncrement(t *testing.T) {
	ctx := context.Background()
	client, testStream := setupTestRedis(t)
	defer client.Close()
	defer cleanupTestStream(ctx, client, testStream)

	originalStream := productivityStream
	productivityStream = testStream
	defer func() { productivityStream = originalStream }()

	// Get initial stream length
	initialLen, err := client.XLen(ctx, testStream).Result()
	if err != nil {
		t.Fatalf("Failed to get initial stream length: %v", err)
	}

	// Send first heartbeat
	payload1 := heartbeatRequest{
		BundleID: "com.test.App1",
	}
	body1, _ := json.Marshal(payload1)
	req1 := httptest.NewRequest("POST", "/prod/heartbeat", bytes.NewReader(body1))
	req1 = req1.WithContext(ctx)
	w1 := httptest.NewRecorder()

	heartbeatHandler(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("First request failed with status %d", w1.Code)
	}

	// Verify stream length increased by 1
	lenAfterFirst, err := client.XLen(ctx, testStream).Result()
	if err != nil {
		t.Fatalf("Failed to get stream length after first request: %v", err)
	}

	expectedLen := initialLen + 1
	if lenAfterFirst != expectedLen {
		t.Errorf("Expected stream length %d after first request, got %d", expectedLen, lenAfterFirst)
	}

	// Send second heartbeat
	payload2 := heartbeatRequest{
		BundleID: "com.test.App2",
	}
	body2, _ := json.Marshal(payload2)
	req2 := httptest.NewRequest("POST", "/prod/heartbeat", bytes.NewReader(body2))
	req2 = req2.WithContext(ctx)
	w2 := httptest.NewRecorder()

	heartbeatHandler(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("Second request failed with status %d", w2.Code)
	}

	// Verify stream length increased by another 1
	finalLen, err := client.XLen(ctx, testStream).Result()
	if err != nil {
		t.Fatalf("Failed to get final stream length: %v", err)
	}

	expectedFinalLen := initialLen + 2
	if finalLen != expectedFinalLen {
		t.Errorf("Expected final stream length %d, got %d", expectedFinalLen, finalLen)
	}
}

func TestHeartbeatHandler_PartialPayload(t *testing.T) {
	ctx := context.Background()
	client, testStream := setupTestRedis(t)
	defer client.Close()
	defer cleanupTestStream(ctx, client, testStream)

	originalStream := productivityStream
	productivityStream = testStream
	defer func() { productivityStream = originalStream }()

	// Payload with only required field
	payload := heartbeatRequest{
		BundleID: "com.test.Minimal",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/prod/heartbeat", bytes.NewReader(body))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	heartbeatHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Verify only provided fields are in the stream
	entries, err := client.XRange(ctx, testStream, "-", "+").Result()
	if err != nil {
		t.Fatalf("Failed to read stream: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	values := entries[0].Values

	// Required field should be present
	if values["bundle_id"] != payload.BundleID {
		t.Errorf("Expected bundle_id=%s, got %v", payload.BundleID, values["bundle_id"])
	}

	// Optional fields should be present but empty
	if values["window_title"] != "" {
		t.Errorf("Expected empty window_title, got %v", values["window_title"])
	}

	if values["url"] != "" {
		t.Errorf("Expected empty url, got %v", values["url"])
	}

	if values["activity_id"] != "" {
		t.Errorf("Expected empty activity_id, got %v", values["activity_id"])
	}

	// Timestamp should be auto-generated
	if values["ts"] == "" {
		t.Error("Expected timestamp to be auto-generated")
	}
}