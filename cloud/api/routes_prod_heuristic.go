package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"alfred-cloud/streams"
)

type heartbeatRequest struct {
	BundleID    string `json:"bundle_id"`
	WindowTitle string `json:"window_title"`
	URL         string `json:"url"`
	ActivityID  string `json:"activity_id"`
	Timestamp   string `json:"ts"`
}

// heartbeatMetrics tracks processing statistics
type heartbeatMetrics struct {
	Processed    int64     `json:"processed"`
	Errors       int64     `json:"errors"`
	LastProcess  time.Time `json:"last_process"`
	LastError    time.Time `json:"last_error"`
}

var metrics heartbeatMetrics

func heartbeatHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Generate correlation ID for tracing
	correlationID := uuid.New().String()[:8]

	// Add correlation ID to context for downstream calls
	ctx := context.WithValue(r.Context(), "correlation_id", correlationID)

	defer r.Body.Close()

	// Log incoming request with correlation ID
	log.Printf("[heartbeat:%s] Incoming request from %s", correlationID, r.RemoteAddr)

	var req heartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[heartbeat:%s] JSON decode error: %v", correlationID, err)
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		recordError()
		return
	}

	// Validate required field
	if req.BundleID == "" {
		log.Printf("[heartbeat:%s] Validation error: missing bundle_id", correlationID)
		http.Error(w, "bundle_id required", http.StatusBadRequest)
		recordError()
		return
	}

	// Auto-generate timestamp if not provided
	if req.Timestamp == "" {
		req.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
		log.Printf("[heartbeat:%s] Auto-generated timestamp: %s", correlationID, req.Timestamp)
	}

	// Prepare stream values (only heartbeat data per architecture spec)
	values := map[string]any{
		"bundle_id":    req.BundleID,
		"window_title": req.WindowTitle,
		"url":          req.URL,
		"activity_id":  req.ActivityID,
		"ts":           req.Timestamp,
	}

	// Add to Redis stream with timeout
	streamCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	id, err := streams.Client.XAdd(streamCtx, &redis.XAddArgs{
		Stream: productivityStream,
		Values: values,
	}).Result()

	if err != nil {
		log.Printf("[heartbeat:%s] Redis XADD failed: %v (stream: %s)", correlationID, err, productivityStream)
		http.Error(w, "failed to enqueue heartbeat", http.StatusInternalServerError)
		recordError()
		return
	}

	// Record successful processing
	metrics.Processed++
	metrics.LastProcess = time.Now()

	duration := time.Since(startTime)
	log.Printf("[heartbeat:%s] Success: bundle=%s stream=%s entry_id=%s duration=%v",
		correlationID, req.BundleID, productivityStream, id, duration)

	// Prepare response
	resp := map[string]any{
		"ok":           true,
		"stream":       productivityStream,
		"entry_id":     id,
		"correlation":  correlationID,
		"processed_at": time.Now().UTC().Format(time.RFC3339Nano),
		"metrics":      metrics,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-ID", correlationID)
	json.NewEncoder(w).Encode(resp)
}

// recordError increments error counters and timestamps
func recordError() {
	metrics.Errors++
	metrics.LastError = time.Now()
}

// getClientIP extracts the real client IP address, considering proxies
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP from the comma-separated list
		for i, char := range xff {
			if char == ',' {
				return xff[:i]
			}
		}
		return xff
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	return r.RemoteAddr
}
