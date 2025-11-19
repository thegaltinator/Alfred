package productivity

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestReproLiveEvent(t *testing.T) {
	if os.Getenv("PRODUCTIVITY_MODEL_API_KEY") == "" {
		t.Skip("Skipping live repro test")
	}

	gen, err := NewNanoGeneratorFromEnv()
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	// Event from Redis
	// Title: "Meeting with big booty Latina "
	// Start: 14:05 -0700 (21:05 UTC)
	// End: 15:05 -0700 (22:05 UTC)
	
	// We use a time in the future relative to now? No, just the raw strings.
	start, _ := time.Parse(time.RFC3339, "2025-11-18T14:05:00-07:00")
	end, _ := time.Parse(time.RFC3339, "2025-11-18T15:05:00-07:00")

	payload := EventPayload{
		UserID:      "amanrayan1@gmail.com",
		EventID:     "repro-test",
		Title:       "Coding Session",
		Description: "Working on backend API",
		StartTime:   start,
		EndTime:     end,
	}

	t.Logf("Testing event: %s (%s)", payload.Title, payload.TimeBlock())

	apps, err := gen.ExpectedApps(context.Background(), payload)
	if err != nil {
		t.Fatalf("ExpectedApps failed: %v", err)
	}

	t.Logf("Generated Heuristic Apps: %v", apps)
	
	// Check for new format (domain: or title: prefixes)
	hasNewFormat := false
	for _, a := range apps {
		if len(a) > 7 && (a[:7] == "domain:" || a[:6] == "title:") {
			hasNewFormat = true
			break
		}
	}
	if !hasNewFormat {
		t.Log("WARNING: No domain: or title: prefixes found. Model might have ignored JSON schema or returned empty lists.")
	} else {
		t.Log("SUCCESS: Found domain/title prefixes, confirming new schema usage.")
	}
}

