package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"

	"alfred-cloud/subagents/calendar_planner"
)

type calendarManagerRequest struct {
	TimeBlock    string `json:"time_block"`
	ActivityType string `json:"activity_type,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	PlanDate     string `json:"plan_date,omitempty"`
}

type calendarManagerResponse struct {
	OK          bool                                   `json:"ok"`
	Notes       []string                               `json:"notes"`
	Blocks      []calendar_planner.PlanBlock           `json:"blocks"`
	Events      []calendar_planner.GoogleCalendarEvent `json:"events"`
	Correlation string                                 `json:"correlation"`
	ProcessedAt string                                 `json:"processed_at"`
}

type calendarManagerRunner func(context.Context, calendarManagerRequest) (*calendar_planner.CalendarPlan, error)

var (
	calendarManagerInitOnce sync.Once
	calendarManagerSvc      *calendar_planner.CalendarManagerService
	calendarManagerInitErr  error

	runCalendarManager calendarManagerRunner = invokeCalendarManager
)

func calendarManagerHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	correlationID := uuid.New().String()

	ctx := context.WithValue(r.Context(), "correlation_id", correlationID)

	log.Printf("[calendar-manager:%s] Incoming calendar manager request from %s", correlationID, r.RemoteAddr)

	defer r.Body.Close()

	var req calendarManagerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[calendar-manager:%s] JSON decode error: %v", correlationID, err)
		writeCalendarManagerError(w, http.StatusBadRequest, correlationID, "invalid JSON body")
		return
	}

	req.TimeBlock = strings.TrimSpace(req.TimeBlock)
	req.ActivityType = strings.TrimSpace(req.ActivityType)
	req.UserID = strings.TrimSpace(req.UserID)

	if req.TimeBlock == "" {
		log.Printf("[calendar-manager:%s] Validation error: missing time_block", correlationID)
		writeCalendarManagerError(w, http.StatusBadRequest, correlationID, "time_block is required")
		return
	}

	if req.UserID == "" {
		req.UserID = "user-dev"
	}

	if req.PlanDate == "" {
		req.PlanDate = time.Now().Format("2006-01-02")
	}

	requestCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	result, err := runCalendarManager(requestCtx, req)
	if err != nil {
		log.Printf("[calendar-manager:%s] Calendar manager generation failed: %v", correlationID, err)
		writeCalendarManagerError(w, http.StatusInternalServerError, correlationID, fmt.Sprintf("failed to generate plan: %v", err))
		return
	}

	duration := time.Since(startTime)
	log.Printf("[calendar-manager:%s] Success: time_block=%s blocks=%d duration=%v",
		correlationID, req.TimeBlock, len(result.Blocks), duration)

	resp := calendarManagerResponse{
		OK:          true,
		Notes:       append([]string(nil), result.Notes...),
		Blocks:      append([]calendar_planner.PlanBlock(nil), result.Blocks...),
		Events:      append([]calendar_planner.GoogleCalendarEvent(nil), result.Events...),
		Correlation: correlationID,
		ProcessedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}

	if resp.Notes == nil {
		resp.Notes = []string{}
	}
	if resp.Blocks == nil {
		resp.Blocks = []calendar_planner.PlanBlock{}
	}
	if resp.Events == nil {
		resp.Events = []calendar_planner.GoogleCalendarEvent{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-ID", correlationID)

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("[calendar-manager:%s] Failed to encode response: %v", correlationID, err)
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}

func registerCalendarManagerRoutes(router *mux.Router) {
	if _, err := ensureCalendarManagerService(); err != nil {
		log.Fatalf("Failed to initialize calendar manager tool: %v", err)
	}

	router.HandleFunc("/planner/run", calendarManagerHandler).Methods("POST")
	router.HandleFunc("/planner/health", calendarManagerHealthHandler).Methods("GET")

	log.Println("Calendar manager routes registered: POST /planner/run, GET /planner/health")
}

func calendarManagerHealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	status := map[string]interface{}{
		"ok":         calendarManagerSvc != nil && calendarManagerInitErr == nil,
		"service":    "calendar-manager-tool",
		"model":      "gpt-4o-2024-08-06 (python_helper)",
		"checked_at": time.Now().UTC().Format(time.RFC3339Nano),
	}

	if calendarManagerInitErr != nil {
		status["error"] = calendarManagerInitErr.Error()
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
}

func ensureCalendarManagerService() (*calendar_planner.CalendarManagerService, error) {
	calendarManagerInitOnce.Do(func() {
		scriptPath := strings.TrimSpace(os.Getenv("PLANNER_SCRIPT"))
		if scriptPath == "" {
			scriptPath = "../python_helper/planner_tool.py"
		}
		if !filepath.IsAbs(scriptPath) {
			if abs, err := filepath.Abs(scriptPath); err == nil {
				scriptPath = abs
			}
		}

		if _, err := os.Stat(scriptPath); err != nil {
			calendarManagerInitErr = fmt.Errorf("planner script not found: %w", err)
			return
		}

		calendarManagerSvc = calendar_planner.NewCalendarManagerService(scriptPath)
	})

	return calendarManagerSvc, calendarManagerInitErr
}

func invokeCalendarManager(ctx context.Context, req calendarManagerRequest) (*calendar_planner.CalendarPlan, error) {
	service, err := ensureCalendarManagerService()
	if err != nil {
		return nil, err
	}

	return service.GenerateCalendarPlan(ctx, req.PlanDate, req.TimeBlock, req.ActivityType)
}

func writeCalendarManagerError(w http.ResponseWriter, code int, correlationID, message string) {
	payload := map[string]interface{}{
		"ok":          false,
		"error":       message,
		"correlation": correlationID,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-ID", correlationID)
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}
