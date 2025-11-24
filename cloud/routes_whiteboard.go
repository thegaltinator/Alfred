package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"alfred-cloud/wb"
)

type whiteboardHandler struct {
	bus *wb.Bus
}

type appendRequest struct {
	UserID   string         `json:"user_id"`
	ThreadID string         `json:"thread_id"`
	Values   map[string]any `json:"values"`
}

type appendResponse struct {
	OK         bool   `json:"ok"`
	ID         string `json:"id"`
	Stream     string `json:"stream"`
	UserID     string `json:"user_id"`
	ThreadID   string `json:"thread_id"`
	AppendedAt string `json:"appended_at"`
}

func registerWhiteboardRoutes(r *mux.Router, bus *wb.Bus) {
	h := &whiteboardHandler{bus: bus}
	r.HandleFunc("/wb/stream", h.handleSSE).Methods("GET")
	r.HandleFunc("/wb/ws", h.handleWebSocket).Methods("GET")
	r.HandleFunc("/admin/wb/append", h.handleAppend).Methods("POST")
}

func (h *whiteboardHandler) handleAppend(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	if h.bus == nil {
		http.Error(w, "whiteboard bus unavailable", http.StatusServiceUnavailable)
		return
	}

	var req appendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" {
		req.UserID = "test-user"
	}

	req.ThreadID = strings.TrimSpace(req.ThreadID)
	if req.ThreadID == "" {
		http.Error(w, "thread_id is required", http.StatusBadRequest)
		return
	}

	if len(req.Values) == 0 {
		http.Error(w, "values is required", http.StatusBadRequest)
		return
	}

	if req.Values == nil {
		req.Values = make(map[string]any)
	}
	// Persist thread_id on the stored payload so downstream consumers (Manager) always see it.
	req.Values["thread_id"] = req.ThreadID

	req.ThreadID = strings.TrimSpace(req.ThreadID)

	id, err := h.bus.AppendWithThread(r.Context(), req.UserID, req.ThreadID, req.Values)
	if err != nil {
		http.Error(w, fmt.Sprintf("append failed: %v", err), http.StatusInternalServerError)
		return
	}

	resp := appendResponse{
		OK:         true,
		ID:         id,
		Stream:     wb.StreamKey(req.UserID),
		UserID:     req.UserID,
		ThreadID:   req.ThreadID,
		AppendedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *whiteboardHandler) handleSSE(w http.ResponseWriter, r *http.Request) {
	if h.bus == nil {
		http.Error(w, "whiteboard bus unavailable", http.StatusServiceUnavailable)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if userID == "" {
		userID = "test-user"
	}

	lastID := strings.TrimSpace(r.URL.Query().Get("after"))
	threadFilter := strings.TrimSpace(r.URL.Query().Get("thread_id"))

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx := r.Context()
	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-keepalive.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
			continue
		default:
		}

		events, nextID, err := h.bus.Tail(ctx, userID, lastID)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Printf("whiteboard tail error for %s: %v", userID, err)
			time.Sleep(300 * time.Millisecond)
			continue
		}

		if len(events) == 0 {
			continue
		}

		lastID = nextID
		for _, evt := range events {
			if threadFilter != "" && evt.ThreadID != threadFilter {
				continue
			}
			payload, err := json.Marshal(evt)
			if err != nil {
				log.Printf("whiteboard encode error: %v", err)
				continue
			}
			fmt.Fprintf(w, "id: %s\n", evt.ID)
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

var wbUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Client is trusted (output-only surface).
		return true
	},
}

func (h *whiteboardHandler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if h.bus == nil {
		http.Error(w, "whiteboard bus unavailable", http.StatusServiceUnavailable)
		return
	}

	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if userID == "" {
		userID = "test-user"
	}
	lastID := strings.TrimSpace(r.URL.Query().Get("after"))
	threadFilter := strings.TrimSpace(r.URL.Query().Get("thread_id"))

	conn, err := wbUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx := r.Context()

	for {
		events, nextID, err := h.bus.Tail(ctx, userID, lastID)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			time.Sleep(300 * time.Millisecond)
			continue
		}
		if len(events) == 0 {
			continue
		}

		lastID = nextID
		for _, evt := range events {
			if threadFilter != "" && evt.ThreadID != threadFilter {
				continue
			}
			if err := conn.WriteJSON(evt); err != nil {
				return
			}
		}
	}
}
