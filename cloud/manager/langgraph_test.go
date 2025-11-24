package manager

import (
	"context"
	"testing"
)

func TestOrchestratorHandle(t *testing.T) {
	resetLLMClientForTest()
	SetLLMClientForTestFunc(func(ctx context.Context, evt Event) (Decision, error) {
		return Decision{Action: ActionRoute, RouteTo: "calendar"}, nil
	})

	orch, err := NewOrchestrator()
	if err != nil {
		t.Fatalf("failed to init orchestrator: %v", err)
	}

	outcome, err := orch.Handle(context.Background(), OrchestratorInput{
		UserID:   "user-1",
		ThreadID: "thread-1",
		Source:   "calendar",
		Kind:     "planned_update",
		Payload:  map[string]any{"foo": "bar"},
	})
	if err != nil {
		t.Fatalf("handle returned error: %v", err)
	}
	if outcome.UserID != "user-1" {
		t.Fatalf("unexpected user id %s", outcome.UserID)
	}
	if outcome.ThreadID != "thread-1" {
		t.Fatalf("unexpected thread id %s", outcome.ThreadID)
	}
	if outcome.Decision.Action != ActionRoute || outcome.Decision.RouteTo != "calendar" {
		t.Fatalf("unexpected decision %#v", outcome.Decision)
	}
}
