package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"alfred-cloud/manager"
)

type managerRequest struct {
	Source   string         `json:"source,omitempty"`
	Kind     string         `json:"kind"`
	Payload  map[string]any `json:"payload,omitempty"`
	UserID   string         `json:"user_id,omitempty"`
	ThreadID string         `json:"thread_id,omitempty"`
}

type managerResponse struct {
	OK          bool   `json:"ok"`
	Action      string `json:"action"`
	Prompt      string `json:"prompt,omitempty"`
	RouteTo     string `json:"route_to,omitempty"`
	Reason      string `json:"reason,omitempty"`
	ThreadID    string `json:"thread_id,omitempty"`
	ProcessedAt string `json:"processed_at"`
}

func registerManagerRoutes(r *mux.Router) {
	r.HandleFunc("/manager/decide", managerHandler).Methods("POST")
}

func managerHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req managerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	req.Source = strings.TrimSpace(req.Source)
	req.Kind = strings.TrimSpace(req.Kind)
	req.ThreadID = strings.TrimSpace(req.ThreadID)

	if req.Kind == "" && req.Source == "" {
		http.Error(w, "kind is required", http.StatusBadRequest)
		return
	}

	orch, err := manager.NewOrchestrator()
	if err != nil {
		http.Error(w, "manager not configured: "+err.Error(), http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
	defer cancel()

	outcome, err := orch.Handle(ctx, manager.OrchestratorInput{
		UserID:   req.UserID,
		ThreadID: req.ThreadID,
		Source:   req.Source,
		Kind:     req.Kind,
		Payload:  req.Payload,
	})
	if err != nil {
		http.Error(w, "manager decision failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := managerResponse{
		OK:          true,
		Action:      string(outcome.Decision.Action),
		Prompt:      outcome.Decision.Prompt,
		RouteTo:     outcome.Decision.RouteTo,
		Reason:      outcome.Decision.Reason,
		ThreadID:    outcome.ThreadID,
		ProcessedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, "encode response failed", http.StatusInternalServerError)
		return
	}
}
