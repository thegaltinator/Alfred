package productivity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// EventPayload represents the calendar payload the heuristic generator expects.
type EventPayload struct {
	UserID      string
	EventID     string
	Title       string
	Description string
	StartTime   time.Time
	EndTime     time.Time
}

// TimeBlock returns a compact human-readable time range for the event.
func (p EventPayload) TimeBlock() string {
	if p.StartTime.IsZero() || p.EndTime.IsZero() {
		return ""
	}
	return fmt.Sprintf("%s-%s", p.StartTime.Format("3:04PM"), p.EndTime.Format("3:04PM"))
}

// EventHeuristic is the persisted expected-apps view for a calendar event.
type EventHeuristic struct {
	UserID       string    `json:"user_id"`
	EventID      string    `json:"event_id"`
	Title        string    `json:"title"`
	Description  string    `json:"description,omitempty"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	ExpectedApps []string  `json:"expected_apps"`
	GeneratedAt  time.Time `json:"generated_at"`
}

// ExpectedAppsGenerator produces the expected apps/tabs list for an event.
type ExpectedAppsGenerator interface {
	ExpectedApps(ctx context.Context, payload EventPayload) ([]string, error)
}

// HeuristicStore persists heuristics in Redis keyed by user + event.
type HeuristicStore struct {
	client *redis.Client
}

func NewHeuristicStore(client *redis.Client) *HeuristicStore {
	return &HeuristicStore{client: client}
}

func (s *HeuristicStore) Save(ctx context.Context, heuristic *EventHeuristic) error {
	if s == nil || s.client == nil {
		return errors.New("heuristic store not initialized")
	}
	if heuristic == nil {
		return errors.New("heuristic is required")
	}
	if heuristic.UserID == "" || heuristic.EventID == "" {
		return errors.New("user_id and event_id are required")
	}

	data, err := json.Marshal(heuristic)
	if err != nil {
		return fmt.Errorf("marshal heuristic: %w", err)
	}

	ttl := heuristicTTL(heuristic.EndTime)
	key := heuristicKey(heuristic.UserID, heuristic.EventID)

	if err := s.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("store heuristic: %w", err)
	}
	indexKey := heuristicIndexKey(heuristic.UserID)
	if err := s.client.SAdd(ctx, indexKey, heuristic.EventID).Err(); err != nil {
		return fmt.Errorf("index heuristic: %w", err)
	}
	_ = s.client.Expire(ctx, indexKey, ttl+24*time.Hour)

	return nil
}

func (s *HeuristicStore) GetByEvent(ctx context.Context, userID, eventID string) (*EventHeuristic, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("heuristic store not initialized")
	}
	if userID == "" || eventID == "" {
		return nil, errors.New("user_id and event_id are required")
	}
	raw, err := s.client.Get(ctx, heuristicKey(userID, eventID)).Result()
	if err == redis.Nil {
		_ = s.client.SRem(ctx, heuristicIndexKey(userID), eventID)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read heuristic: %w", err)
	}

	var heuristic EventHeuristic
	if err := json.Unmarshal([]byte(raw), &heuristic); err != nil {
		return nil, fmt.Errorf("decode heuristic: %w", err)
	}
	return &heuristic, nil
}

func (s *HeuristicStore) GetActive(ctx context.Context, userID string, now time.Time) (*EventHeuristic, error) {
	if now.IsZero() {
		now = time.Now()
	}
	ids, err := s.client.SMembers(ctx, heuristicIndexKey(userID)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list heuristics: %w", err)
	}

	for _, id := range ids {
		heuristic, err := s.GetByEvent(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		if heuristic == nil {
			continue
		}
		if isActive(heuristic, now) {
			return heuristic, nil
		}
	}
	return nil, nil
}

// List returns all persisted heuristics for a user.
func (s *HeuristicStore) List(ctx context.Context, userID string) ([]*EventHeuristic, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("heuristic store not initialized")
	}
	ids, err := s.client.SMembers(ctx, heuristicIndexKey(userID)).Result()
	if err == redis.Nil {
		return []*EventHeuristic{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list heuristics: %w", err)
	}

	results := make([]*EventHeuristic, 0, len(ids))
	for _, id := range ids {
		heuristic, err := s.GetByEvent(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		if heuristic != nil {
			results = append(results, heuristic)
		}
	}
	return results, nil
}

// HeuristicService coordinates the generator and store.
type HeuristicService struct {
	store     *HeuristicStore
	generator ExpectedAppsGenerator
}

func NewHeuristicService(store *HeuristicStore, generator ExpectedAppsGenerator) (*HeuristicService, error) {
	if store == nil {
		return nil, errors.New("heuristic store is required")
	}
	if generator == nil {
		var err error
		generator, err = NewNanoGeneratorFromEnv()
		if err != nil {
			return nil, err
		}
	}
	return &HeuristicService{
		store:     store,
		generator: generator,
	}, nil
}

func (s *HeuristicService) UpsertEventHeuristic(ctx context.Context, payload EventPayload) (*EventHeuristic, error) {
	if s == nil {
		return nil, errors.New("heuristic service not initialized")
	}
	if payload.UserID == "" || payload.EventID == "" {
		return nil, errors.New("user_id and event_id are required")
	}
	start := payload.StartTime
	end := payload.EndTime
	if start.IsZero() || end.IsZero() {
		return nil, errors.New("start_time and end_time are required")
	}
	if !end.After(start) {
		end = start.Add(30 * time.Minute)
	}

	apps, err := s.generator.ExpectedApps(ctx, payload)
	if err != nil {
		return nil, err
	}

	heuristic := &EventHeuristic{
		UserID:       payload.UserID,
		EventID:      payload.EventID,
		Title:        payload.Title,
		Description:  payload.Description,
		StartTime:    start,
		EndTime:      end,
		ExpectedApps: apps,
		GeneratedAt:  time.Now().UTC(),
	}
	if err := s.store.Save(ctx, heuristic); err != nil {
		return nil, err
	}
	return heuristic, nil
}

func (s *HeuristicService) ActiveHeuristic(ctx context.Context, userID string, now time.Time) (*EventHeuristic, error) {
	if s == nil {
		return nil, errors.New("heuristic service not initialized")
	}
	return s.store.GetActive(ctx, userID, now)
}

func (s *HeuristicService) ListHeuristics(ctx context.Context, userID string) ([]*EventHeuristic, error) {
	if s == nil {
		return nil, errors.New("heuristic service not initialized")
	}
	return s.store.List(ctx, userID)
}

func (s *HeuristicService) CompareForeground(ctx context.Context, userID, foreground string, now time.Time) (*EventHeuristic, bool, error) {
	if s == nil {
		return nil, false, errors.New("heuristic service not initialized")
	}
	heuristic, err := s.store.GetActive(ctx, userID, now)
	if err != nil || heuristic == nil {
		return heuristic, false, err
	}
	return heuristic, ForegroundMatches(heuristic, foreground), nil
}

// ForegroundMatches performs a simple, extendable comparison of the active app/tab.
func ForegroundMatches(heuristic *EventHeuristic, foreground string) bool {
	if heuristic == nil {
		return false
	}
	active := strings.ToLower(strings.TrimSpace(foreground))
	if active == "" {
		return false
	}
	for _, expected := range heuristic.ExpectedApps {
		match := strings.ToLower(strings.TrimSpace(expected))
		if match == "" {
			continue
		}
		if strings.Contains(active, match) || strings.Contains(match, active) {
			return true
		}
	}
	return false
}

func heuristicTTL(end time.Time) time.Duration {
	if end.IsZero() {
		return 24 * time.Hour
	}
	ttl := time.Until(end)
	if ttl < time.Hour {
		ttl = 24 * time.Hour
	}
	return ttl + time.Hour
}

func heuristicKey(userID, eventID string) string {
	return fmt.Sprintf("prod:heuristic:%s:%s", userID, eventID)
}

func heuristicIndexKey(userID string) string {
	return fmt.Sprintf("prod:heuristic:index:%s", userID)
}

func isActive(heuristic *EventHeuristic, now time.Time) bool {
	if heuristic == nil || heuristic.StartTime.IsZero() || heuristic.EndTime.IsZero() || now.IsZero() {
		return false
	}
	if !heuristic.EndTime.After(heuristic.StartTime) {
		return false
	}
	return !now.Before(heuristic.StartTime) && now.Before(heuristic.EndTime)
}
