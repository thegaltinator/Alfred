package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"alfred-cloud/streams"
	"github.com/gorilla/mux"
)

type HeartbeatRequest struct {
	BundleID    string `json:"bundle_id"`
	WindowTitle string `json:"window_title"`
	URL         string `json:"url"`
	ActivityID  string `json:"activity_id"`
	Timestamp   string `json:"ts"`
	ThreadID    string `json:"thread_id"`
}

func registerProdHeuristicRoutes(r *mux.Router, streams *streams.StreamsHelper) {
	r.HandleFunc("/prod/heartbeat", func(w http.ResponseWriter, req *http.Request) {
		var payload HeartbeatRequest
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Default to test-user since we don't have auth yet
		userID := "test-user"

		// Construct stream payload
		values := map[string]interface{}{
			"bundle_id":    payload.BundleID,
			"window_title": payload.WindowTitle,
			"url":          payload.URL,
			"activity_id":  payload.ActivityID,
			"ts":           payload.Timestamp,
		}
		if values["ts"] == "" {
			values["ts"] = time.Now().UTC().Format(time.RFC3339)
		}
		if strings.TrimSpace(payload.ThreadID) != "" {
			values["thread_id"] = strings.TrimSpace(payload.ThreadID)
		}

		streamKey := "user:" + userID + ":in:prod"
		_, err := streams.AppendToStream(req.Context(), streamKey, values)
		if err != nil {
			http.Error(w, "failed to enqueue heartbeat", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusAccepted)
	}).Methods("POST")
}
