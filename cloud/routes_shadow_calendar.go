package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"alfred-cloud/subagents/calendar_planner"
	"github.com/gorilla/mux"
)

type shadowCalendarHandler struct {
	service *calendar_planner.ShadowCalendarService
}

func registerShadowCalendarRoutes(router *mux.Router, service *calendar_planner.ShadowCalendarService) {
	if service == nil {
		return
	}
	handler := &shadowCalendarHandler{service: service}
	router.HandleFunc("/calendar/shadow/{userID}", handler.handleGetSnapshot).Methods("GET")
}

func (h *shadowCalendarHandler) handleGetSnapshot(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := strings.TrimSpace(vars["userID"])
	if userID == "" {
		http.Error(w, "userID is required", http.StatusBadRequest)
		return
	}
	snapshot, err := h.service.GetSnapshot(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snapshot)
}
