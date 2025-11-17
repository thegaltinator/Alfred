package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"alfred-cloud/subagents/calendar_planner"
)

func TestCalendarManagerHandlerSuccess(t *testing.T) {
	resetCalendarManagerTestState()

	runCalendarManager = func(ctx context.Context, req calendarManagerRequest) (*calendar_planner.CalendarPlan, error) {
		if req.TimeBlock != "coding 10-12" {
			t.Fatalf("unexpected time block %s", req.TimeBlock)
		}
		if req.PlanDate == "" {
			t.Fatalf("expected plan date to be set")
		}
		return &calendar_planner.CalendarPlan{
			Notes: []string{"Lock VSCode + Terminal"},
			Blocks: []calendar_planner.PlanBlock{
				{Title: "Warm-up", StartTime: "2025-11-15T09:00:00-08:00", EndTime: "2025-11-15T09:30:00-08:00"},
				{Title: "Coding Sprint", StartTime: "2025-11-15T09:30:00-08:00", EndTime: "2025-11-15T11:30:00-08:00"},
			},
			Events: []calendar_planner.GoogleCalendarEvent{
				{
					Summary: "Warm-up",
					Start:   calendar_planner.GoogleCalendarTime{DateTime: "2025-11-15T09:00:00-08:00", TimeZone: "UTC-08:00"},
					End:     calendar_planner.GoogleCalendarTime{DateTime: "2025-11-15T09:30:00-08:00", TimeZone: "UTC-08:00"},
				},
			},
		}, nil
	}
	t.Cleanup(func() {
		runCalendarManager = invokeCalendarManager
	})

	body := bytes.NewBufferString(`{"time_block":"coding 10-12","activity_type":"development","user_id":"user-123"}`)
	req := httptest.NewRequest("POST", "/planner/run", body)
	resp := httptest.NewRecorder()

	calendarManagerHandler(resp, req)

	result := resp.Result()
	defer result.Body.Close()

	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 got %d", result.StatusCode)
	}

	correlation := result.Header.Get("X-Correlation-ID")
	if correlation == "" {
		t.Fatalf("expected correlation id header")
	}

	var payload calendarManagerResponse
	if err := json.NewDecoder(result.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !payload.OK {
		t.Fatalf("expected ok response")
	}

	if len(payload.Notes) == 0 {
		t.Fatalf("expected notes to be populated")
	}
	if len(payload.Blocks) != 2 || payload.Blocks[1].Title != "Coding Sprint" {
		t.Fatalf("unexpected blocks %#v", payload.Blocks)
	}
	if len(payload.Events) == 0 {
		t.Fatalf("expected at least one event")
	}
}

func TestCalendarManagerHandlerValidation(t *testing.T) {
	resetCalendarManagerTestState()

	t.Run("missing time block", func(t *testing.T) {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/planner/run", bytes.NewBufferString(`{"activity_type":"dev"}`))

		calendarManagerHandler(resp, req)

		if resp.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 got %d", resp.Code)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/planner/run", bytes.NewBufferString(`{"time_block":`))

		calendarManagerHandler(resp, req)

		if resp.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 got %d", resp.Code)
		}
	})
}

func TestCalendarManagerHandlerFailure(t *testing.T) {
	resetCalendarManagerTestState()

	runCalendarManager = func(ctx context.Context, req calendarManagerRequest) (*calendar_planner.CalendarPlan, error) {
		return nil, errors.New("llm unavailable")
	}
	t.Cleanup(func() {
		runCalendarManager = invokeCalendarManager
	})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/planner/run", bytes.NewBufferString(`{"time_block":"coding 10-12"}`))

	calendarManagerHandler(resp, req)

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 got %d", resp.Code)
	}
}

func TestCalendarManagerHealthHandler(t *testing.T) {
	resetCalendarManagerTestState()

	t.Run("planner not initialized", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/planner/health", nil)
		resp := httptest.NewRecorder()

		calendarManagerHealthHandler(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", resp.Code)
		}

		var payload map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode health response: %v", err)
		}

		if payload["ok"].(bool) {
			t.Fatalf("expected health ok false when planner not initialized")
		}
	})

	t.Run("planner ready", func(t *testing.T) {
		resetCalendarManagerTestState()
		calendarManagerSvc = calendar_planner.NewCalendarManagerService("../python_helper/planner_tool.py")

		req := httptest.NewRequest("GET", "/planner/health", nil)
		resp := httptest.NewRecorder()

		calendarManagerHealthHandler(resp, req)

		var payload map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("failed to decode health response: %v", err)
		}

		if !payload["ok"].(bool) {
			t.Fatalf("expected health ok true when planner initialized")
		}
	})
}

func TestEnsureCalendarManagerServiceMissingScript(t *testing.T) {
	resetCalendarManagerTestState()
	t.Setenv("PLANNER_SCRIPT", "/tmp/fake-planner-script.py")

	if _, err := ensureCalendarManagerService(); err == nil {
		t.Fatalf("expected script missing error")
	}
}

func resetCalendarManagerTestState() {
	calendarManagerSvc = nil
	calendarManagerInitErr = nil
	calendarManagerInitOnce = sync.Once{}
	runCalendarManager = invokeCalendarManager
}
