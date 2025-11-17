package calendar_planner

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseCalendarDelta(t *testing.T) {
	values := map[string]interface{}{
		"event_id":          "evt-1",
		"user_id":           "user-1",
		"calendar_id":       "primary",
		"event_summary":     "Deep Work",
		"event_description": "Block coding time",
		"event_location":    "Desk",
		"start_time":        "2024-02-01T10:00:00Z",
		"end_time":          "2024-02-01T11:00:00Z",
		"status":            "confirmed",
		"change_type":       "updated",
		"sequence":          "5",
		"all_day":           "false",
	}
	delta, err := parseCalendarDelta(values)
	require.NoError(t, err)
	require.NotNil(t, delta.Event)
	require.False(t, delta.Deleted)
	require.Equal(t, "evt-1", delta.Event.EventID)
	require.Equal(t, "user-1", delta.Event.UserID)
	require.Equal(t, "Deep Work", delta.Event.Summary)
	require.Equal(t, 5, delta.Event.Sequence)
	require.Equal(t, time.Date(2024, 2, 1, 10, 0, 0, 0, time.UTC), delta.Event.StartTime)
}

func TestEnsureProposalCreatesPlan(t *testing.T) {
	planner := &stubPlanner{plan: &CalendarPlan{Notes: []string{"add buffer"}, Blocks: []PlanBlock{{Title: "Resolve", StartTime: "2024-02-01T10:00:00Z", EndTime: "2024-02-01T11:00:00Z"}}}}
	store := newMemoryShadowStore()
	svc := &ShadowCalendarService{planner: planner, store: store}
	ctx := context.Background()
	start := time.Date(2024, 2, 1, 10, 0, 0, 0, time.UTC)
	eventA := &ShadowEvent{EventID: "evt-a", Summary: "Call", StartTime: start, EndTime: start.Add(time.Hour)}
	eventB := &ShadowEvent{EventID: "evt-b", Summary: "Review", StartTime: start.Add(30 * time.Minute), EndTime: start.Add(2 * time.Hour)}

	require.NoError(t, svc.ensureProposal(ctx, "user-1", eventA, eventB))
	proposals, err := store.ListProposals(ctx, "user-1")
	require.NoError(t, err)
	require.Len(t, proposals, 1)
	require.Equal(t, "pending", proposals[0].Status)
	require.Equal(t, 1, planner.calls)

	// Duplicate conflict should not create a second proposal
	require.NoError(t, svc.ensureProposal(ctx, "user-1", eventA, eventB))
	proposals, err = store.ListProposals(ctx, "user-1")
	require.NoError(t, err)
	require.Len(t, proposals, 1)
	require.Equal(t, 1, planner.calls)
}

func TestEvaluateConflictsDetectsOverlap(t *testing.T) {
	planner := &stubPlanner{plan: &CalendarPlan{Notes: []string{"resolve"}, Blocks: []PlanBlock{{Title: "Conflict", StartTime: "2024-02-01T10:00:00Z", EndTime: "2024-02-01T11:00:00Z"}}}}
	store := newMemoryShadowStore()
	svc := &ShadowCalendarService{planner: planner, store: store}
	ctx := context.Background()
	start := time.Date(2024, 2, 1, 15, 0, 0, 0, time.UTC)
	existing := &ShadowEvent{UserID: "user-1", EventID: "existing", Summary: "Interview", StartTime: start, EndTime: start.Add(time.Hour)}
	require.NoError(t, store.UpsertEvent(ctx, existing))
	newEvent := &ShadowEvent{UserID: "user-1", EventID: "new", Summary: "Demo", StartTime: start.Add(30 * time.Minute), EndTime: start.Add(90 * time.Minute)}
	store.UpsertEvent(ctx, newEvent)
	require.NoError(t, svc.evaluateConflicts(ctx, "user-1", newEvent))
	proposals, err := store.ListProposals(ctx, "user-1")
	require.NoError(t, err)
	require.Len(t, proposals, 1)
	require.Equal(t, 1, planner.calls)
}

func TestEventsOverlapAllDay(t *testing.T) {
	start := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	a := &ShadowEvent{EventID: "a", StartTime: start, EndTime: start.Add(24 * time.Hour), AllDay: true}
	b := &ShadowEvent{EventID: "b", StartTime: start.Add(12 * time.Hour), EndTime: start.Add(36 * time.Hour), AllDay: true}
	require.True(t, eventsOverlap(a, b))
}

func TestSanitizeUserIDs(t *testing.T) {
	ids := sanitizeUserIDs([]string{" user-1 ", "user-2", "user-1", ""})
	require.Equal(t, []string{"user-1", "user-2"}, ids)
}

type stubPlanner struct {
	plan  *CalendarPlan
	calls int
	err   error
}

func (s *stubPlanner) GenerateCalendarPlan(ctx context.Context, planDate, timeBlock, activityType string) (*CalendarPlan, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.plan, nil
}

type memoryShadowStore struct {
	mu        sync.Mutex
	events    map[string]map[string]*ShadowEvent
	proposals map[string]map[string]*ShadowProposal
	conflicts map[string]map[string]string
}

func newMemoryShadowStore() *memoryShadowStore {
	return &memoryShadowStore{
		events:    make(map[string]map[string]*ShadowEvent),
		proposals: make(map[string]map[string]*ShadowProposal),
		conflicts: make(map[string]map[string]string),
	}
}

func (m *memoryShadowStore) UpsertEvent(ctx context.Context, event *ShadowEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if event == nil {
		return errors.New("nil event")
	}
	byUser := m.events[event.UserID]
	if byUser == nil {
		byUser = make(map[string]*ShadowEvent)
		m.events[event.UserID] = byUser
	}
	copy := *event
	byUser[event.EventID] = &copy
	return nil
}

func (m *memoryShadowStore) RemoveEvent(ctx context.Context, userID, eventID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if byUser := m.events[userID]; byUser != nil {
		delete(byUser, eventID)
	}
	return nil
}

func (m *memoryShadowStore) ListEvents(ctx context.Context, userID string) ([]*ShadowEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	byUser := m.events[userID]
	if byUser == nil {
		return nil, nil
	}
	result := make([]*ShadowEvent, 0, len(byUser))
	for _, evt := range byUser {
		copy := *evt
		result = append(result, &copy)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].StartTime.Before(result[j].StartTime)
	})
	return result, nil
}

func (m *memoryShadowStore) SaveProposal(ctx context.Context, proposal *ShadowProposal) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	byUser := m.proposals[proposal.UserID]
	if byUser == nil {
		byUser = make(map[string]*ShadowProposal)
		m.proposals[proposal.UserID] = byUser
	}
	copy := *proposal
	byUser[proposal.ID] = &copy
	if m.conflicts[proposal.UserID] == nil {
		m.conflicts[proposal.UserID] = make(map[string]string)
	}
	m.conflicts[proposal.UserID][proposal.ConflictKey] = proposal.ID
	return nil
}

func (m *memoryShadowStore) ListProposals(ctx context.Context, userID string) ([]*ShadowProposal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	byUser := m.proposals[userID]
	result := make([]*ShadowProposal, 0, len(byUser))
	for _, proposal := range byUser {
		copy := *proposal
		result = append(result, &copy)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

func (m *memoryShadowStore) FindProposalByConflict(ctx context.Context, userID, conflictKey string) (*ShadowProposal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.conflicts[userID][conflictKey]
	if id == "" {
		return nil, nil
	}
	proposal := m.proposals[userID][id]
	if proposal == nil {
		return nil, nil
	}
	copy := *proposal
	return &copy, nil
}

func (m *memoryShadowStore) GetProposal(ctx context.Context, userID, proposalID string) (*ShadowProposal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	proposal := m.proposals[userID][proposalID]
	if proposal == nil {
		return nil, nil
	}
	copy := *proposal
	return &copy, nil
}

func (m *memoryShadowStore) RemoveProposalsForEvent(ctx context.Context, userID, eventID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	byUser := m.proposals[userID]
	if byUser == nil {
		return nil
	}
	for id, proposal := range byUser {
		if proposal.PrimaryEvent.EventID != eventID && proposal.ConflictingEvent.EventID != eventID {
			continue
		}
		delete(byUser, id)
		if idx := m.conflicts[userID]; idx != nil {
			delete(idx, proposal.ConflictKey)
		}
	}
	return nil
}
