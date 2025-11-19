package productivity

import (
	"context"
	"testing"
	"time"
)

// Integration test that hits the live GPT-5 Nano endpoint when PRODUCTIVITY_MODEL_API_KEY is set.
func TestNanoGeneratorLive(t *testing.T) {
	gen, err := NewNanoGeneratorFromEnv()
	if err != nil {
		t.Skipf("init nano generator skipped: %v", err)
		return
	}

	now := time.Now()
	payload := EventPayload{
		UserID:      "test-user",
		EventID:     "evt-live",
		Title:       "Coding session",
		Description: "Implement calendar heuristic",
		StartTime:   now,
		EndTime:     now.Add(1 * time.Hour),
	}

	apps, err := gen.ExpectedApps(context.Background(), payload)
	if err != nil {
		t.Fatalf("nano call failed: %v", err)
	}
	if len(apps) == 0 {
		t.Fatalf("expected non-empty apps from nano model")
	}
}
