package productivity

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClassifierEmitsOnceAfterTwoMinutes(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newTestRedis(t)
	defer cleanup()

	store := NewHeuristicStore(client)
	svc, err := NewHeuristicService(store, &staticGenerator{apps: []string{"com.microsoft.VSCode"}})
	require.NoError(t, err)

	now := time.Now()
	_, err = svc.UpsertEventHeuristic(ctx, EventPayload{
		UserID:    "user-1",
		EventID:   "evt-1",
		Title:     "Coding",
		StartTime: now.Add(-time.Minute),
		EndTime:   now.Add(time.Hour),
	})
	require.NoError(t, err)

	classifier, err := NewClassifier(svc)
	require.NoError(t, err)

	// Off-track heartbeat (Finder) starts the mismatch timer.
	decision, err := classifier.ProcessHeartbeat(ctx, Heartbeat{
		UserID:    "user-1",
		BundleID:  "com.apple.finder",
		Timestamp: now,
	})
	require.NoError(t, err)
	require.Nil(t, decision)

	// Still off-track but before 120s window expires.
	decision, err = classifier.ProcessHeartbeat(ctx, Heartbeat{
		UserID:    "user-1",
		BundleID:  "com.apple.finder",
		Timestamp: now.Add(119 * time.Second),
	})
	require.NoError(t, err)
	require.Nil(t, decision)

	// Cross the 2-minute threshold -> single decision.
	decision, err = classifier.ProcessHeartbeat(ctx, Heartbeat{
		UserID:    "user-1",
		BundleID:  "com.apple.finder",
		Timestamp: now.Add(121 * time.Second),
	})
	require.NoError(t, err)
	require.NotNil(t, decision)
	require.Equal(t, DecisionUnderrun, decision.Kind)

	// Further mismatched heartbeats should not duplicate the decision.
	decision, err = classifier.ProcessHeartbeat(ctx, Heartbeat{
		UserID:    "user-1",
		BundleID:  "com.apple.finder",
		Timestamp: now.Add(200 * time.Second),
	})
	require.NoError(t, err)
	require.Nil(t, decision)

	require.Len(t, classifier.Decisions("user-1"), 1)
}

func TestClassifierResetsTimerOnMatch(t *testing.T) {
	ctx := context.Background()
	client, cleanup := newTestRedis(t)
	defer cleanup()

	store := NewHeuristicStore(client)
	svc, err := NewHeuristicService(store, &staticGenerator{apps: []string{"com.microsoft.VSCode"}})
	require.NoError(t, err)

	now := time.Now()
	_, err = svc.UpsertEventHeuristic(ctx, EventPayload{
		UserID:    "user-2",
		EventID:   "evt-2",
		Title:     "Coding",
		StartTime: now.Add(-time.Minute),
		EndTime:   now.Add(time.Hour),
	})
	require.NoError(t, err)

	classifier, err := NewClassifier(svc)
	require.NoError(t, err)

	// Start with an unexpected app.
	_, err = classifier.ProcessHeartbeat(ctx, Heartbeat{
		UserID:    "user-2",
		BundleID:  "com.apple.finder",
		Timestamp: now,
	})
	require.NoError(t, err)

	// Still unexpected but before the 2-minute mark—no decision yet.
	decision, err := classifier.ProcessHeartbeat(ctx, Heartbeat{
		UserID:    "user-2",
		BundleID:  "com.apple.finder",
		Timestamp: now.Add(90 * time.Second),
	})
	require.NoError(t, err)
	require.Nil(t, decision)

	// Matching heartbeat resets the mismatch timer.
	decision, err = classifier.ProcessHeartbeat(ctx, Heartbeat{
		UserID:    "user-2",
		BundleID:  "com.microsoft.VSCode",
		Timestamp: now.Add(95 * time.Second),
	})
	require.NoError(t, err)
	require.Nil(t, decision)

	// New mismatch window starts here.
	decision, err = classifier.ProcessHeartbeat(ctx, Heartbeat{
		UserID:    "user-2",
		BundleID:  "com.apple.finder",
		Timestamp: now.Add(200 * time.Second),
	})
	require.NoError(t, err)
	require.Nil(t, decision)

	// After another full 120s off-track, a single decision is recorded.
	decision, err = classifier.ProcessHeartbeat(ctx, Heartbeat{
		UserID:    "user-2",
		BundleID:  "com.apple.finder",
		Timestamp: now.Add(321 * time.Second),
	})
	require.NoError(t, err)
	require.NotNil(t, decision)
	require.Equal(t, DecisionNudge, decision.Kind)

	require.Len(t, classifier.Decisions("user-2"), 1)
}
