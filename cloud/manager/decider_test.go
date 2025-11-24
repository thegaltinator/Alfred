package manager

import (
	"context"
	"testing"
)

func TestDecideProductivityNudge(t *testing.T) {
	resetLLMClientForTest()
	SetLLMClientForTestFunc(func(ctx context.Context, evt Event) (Decision, error) {
		return Decision{Action: ActionAskUser, Prompt: "ask user to refocus", Reason: "productivity_nudge"}, nil
	})

	decision, err := Decide(Event{
		Source: "prod",
		Kind:   "nudge",
	})
	if err != nil {
		t.Fatalf("decide returned error: %v", err)
	}

	if decision.Action != ActionAskUser {
		t.Fatalf("expected action ask_user, got %s", decision.Action)
	}
	if decision.Prompt != "ask user to refocus" {
		t.Fatalf("unexpected prompt: %s", decision.Prompt)
	}
	if decision.Reason != "productivity_nudge" {
		t.Fatalf("unexpected reason: %s", decision.Reason)
	}
}

func TestDecideWithCompoundKind(t *testing.T) {
	resetLLMClientForTest()
	SetLLMClientForTestFunc(func(ctx context.Context, evt Event) (Decision, error) {
		return Decision{Action: ActionAskUser, Prompt: "ask user to refocus", Reason: "productivity_nudge"}, nil
	})

	decision, err := Decide(Event{
		Kind: "prod.nudge",
	})
	if err != nil {
		t.Fatalf("decide returned error: %v", err)
	}

	if decision.Action != ActionAskUser {
		t.Fatalf("expected action ask_user, got %s", decision.Action)
	}
	if decision.Prompt != "ask user to refocus" {
		t.Fatalf("unexpected prompt: %s", decision.Prompt)
	}
}
