package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"alfred-cloud/manager"
)

func TestManagerHandlerHandlesProdNudge(t *testing.T) {
	manager.ResetLLMClientForTest()
	manager.SetLLMClientForTestFunc(func(ctx context.Context, evt manager.Event) (manager.Decision, error) {
		return manager.Decision{Action: manager.ActionAskUser, Prompt: "ask user to refocus", Reason: "productivity_nudge"}, nil
	})

	body := strings.NewReader(`{"kind":"prod.nudge","thread_id":"thread-123"}`)
	req := httptest.NewRequest("POST", "/manager/decide", body)
	resp := httptest.NewRecorder()

	managerHandler(resp, req)

	result := resp.Result()
	defer result.Body.Close()

	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 got %d", result.StatusCode)
	}

	var payload managerResponse
	if err := json.NewDecoder(result.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if payload.Action != string(manager.ActionAskUser) {
		t.Fatalf("unexpected action %s", payload.Action)
	}
	if payload.Prompt != "ask user to refocus" {
		t.Fatalf("unexpected prompt %s", payload.Prompt)
	}
	if payload.ThreadID != "thread-123" {
		t.Fatalf("unexpected thread id %s", payload.ThreadID)
	}
}

func TestManagerHandlerRequiresKind(t *testing.T) {
	manager.ResetLLMClientForTest()

	req := httptest.NewRequest("POST", "/manager/decide", strings.NewReader(`{}`))
	resp := httptest.NewRecorder()

	managerHandler(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d", resp.Code)
	}
}
