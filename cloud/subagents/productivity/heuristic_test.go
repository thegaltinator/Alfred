package productivity

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type staticGenerator struct {
	apps []string
}

func (s *staticGenerator) ExpectedApps(ctx context.Context, payload EventPayload) ([]string, error) {
	return append([]string(nil), s.apps...), nil
}

func TestHeuristicServiceUpsertAndCompare(t *testing.T) {
	client, cleanup := newTestRedis(t)
	defer cleanup()

	store := NewHeuristicStore(client)
	svc, err := NewHeuristicService(store, &staticGenerator{apps: []string{"Cursor", "Chrome: GitHub", "Terminal"}})
	require.NoError(t, err)

	now := time.Now()
	payload := EventPayload{
		UserID:      "user-1",
		EventID:     "evt-1",
		Title:       "Coding time",
		Description: "implement feature",
		StartTime:   now.Add(5 * time.Minute),
		EndTime:     now.Add(65 * time.Minute),
	}

	heuristic, err := svc.UpsertEventHeuristic(context.Background(), payload)
	require.NoError(t, err)
	require.NotNil(t, heuristic)
	require.NotZero(t, heuristic.GeneratedAt)
	require.NotEmpty(t, heuristic.ExpectedApps)

	active, err := svc.ActiveHeuristic(context.Background(), payload.UserID, now.Add(30*time.Minute))
	require.NoError(t, err)
	require.NotNil(t, active)

	_, match, err := svc.CompareForeground(context.Background(), payload.UserID, "cursor", now.Add(30*time.Minute))
	require.NoError(t, err)
	require.True(t, match, "expected cursor to match coding heuristic")
}

func TestForegroundMatches(t *testing.T) {
	heuristic := &EventHeuristic{
		ExpectedApps: []string{"Chrome: GitHub", "Terminal"},
		StartTime:    time.Now().Add(-time.Minute),
		EndTime:      time.Now().Add(time.Hour),
	}

	require.True(t, ForegroundMatches(heuristic, "chrome: github repo"))
	require.False(t, ForegroundMatches(heuristic, "Xcode"))
}

func TestHeuristicStoreActiveWindow(t *testing.T) {
	client, cleanup := newTestRedis(t)
	defer cleanup()

	store := NewHeuristicStore(client)
	svc, err := NewHeuristicService(store, &staticGenerator{apps: []string{"Chrome: Docs"}})
	require.NoError(t, err)
	now := time.Now()
	heuristic := &EventHeuristic{
		UserID:    "user-2",
		EventID:   "evt-2",
		Title:     "Docs review",
		StartTime: now.Add(-10 * time.Minute),
		EndTime:   now.Add(50 * time.Minute),
		ExpectedApps: []string{
			"Chrome: Docs",
		},
		GeneratedAt: now,
	}
	require.NoError(t, store.Save(context.Background(), heuristic))

	active, err := svc.store.GetActive(context.Background(), heuristic.UserID, now)
	require.NoError(t, err)
	require.NotNil(t, active)
	require.Equal(t, heuristic.EventID, active.EventID)
}

func newTestRedis(t *testing.T) (*redis.Client, func()) {
	t.Helper()

	server, err := miniredis.Run()
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{
		Addr: server.Addr(),
	})

	cleanup := func() {
		_ = client.Close()
		server.Close()
	}

	return client, cleanup
}
