package calendar_planner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// PlannerRunner describes the planner interface used by the shadow calendar.
type PlannerRunner interface {
	GenerateCalendarPlan(ctx context.Context, planDate, timeBlock, activityType string) (*CalendarPlan, error)
}

// ShadowCalendarOptions controls how the service connects to Redis streams.
type ShadowCalendarOptions struct {
	UserIDs     []string
	GroupName   string
	BatchSize   int64
	PollTimeout time.Duration
	Store       ShadowStore
}

// ShadowCalendarService keeps a per-user shadow calendar using Redis streams.
type ShadowCalendarService struct {
	redisClient *redis.Client
	planner     PlannerRunner
	store       ShadowStore
	groupName   string
	userIDs     []string
	batchSize   int64
	pollTimeout time.Duration
	consumerID  string
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

const (
	shadowDefaultGroup   = "calendar-shadow"
	shadowDefaultBatch   = 32
	shadowDefaultTimeout = 2 * time.Second
)

// ShadowStore persists shadow calendar state.
type ShadowStore interface {
	UpsertEvent(ctx context.Context, event *ShadowEvent) error
	RemoveEvent(ctx context.Context, userID, eventID string) error
	ListEvents(ctx context.Context, userID string) ([]*ShadowEvent, error)
	SaveProposal(ctx context.Context, proposal *ShadowProposal) error
	GetProposal(ctx context.Context, userID, proposalID string) (*ShadowProposal, error)
	ListProposals(ctx context.Context, userID string) ([]*ShadowProposal, error)
	FindProposalByConflict(ctx context.Context, userID, conflictKey string) (*ShadowProposal, error)
	RemoveProposalsForEvent(ctx context.Context, userID, eventID string) error
}

// ShadowEvent captures the latest version of a calendar event.
type ShadowEvent struct {
	UserID         string    `json:"user_id"`
	CalendarID     string    `json:"calendar_id"`
	EventID        string    `json:"event_id"`
	Summary        string    `json:"summary"`
	Description    string    `json:"description,omitempty"`
	Location       string    `json:"location,omitempty"`
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time"`
	StartTimezone  string    `json:"start_timezone,omitempty"`
	EndTimezone    string    `json:"end_timezone,omitempty"`
	AllDay         bool      `json:"all_day"`
	Status         string    `json:"status"`
	ChangeType     string    `json:"change_type"`
	HTMLLink       string    `json:"html_link,omitempty"`
	CreatorEmail   string    `json:"creator_email,omitempty"`
	OrganizerEmail string    `json:"organizer_email,omitempty"`
	Sequence       int       `json:"sequence"`
	Updated        time.Time `json:"updated"`
	NotifiedAt     time.Time `json:"notified_at"`
	RawPayload     string    `json:"raw_payload,omitempty"`
	ChannelID      string    `json:"channel_id,omitempty"`
	RecordedAt     time.Time `json:"recorded_at"`
}

// ShadowProposal captures a planner run that resolves a conflict between events.
type ShadowProposal struct {
	ID               string              `json:"id"`
	UserID           string              `json:"user_id"`
	PrimaryEvent     *ShadowEventSummary `json:"primary_event"`
	ConflictingEvent *ShadowEventSummary `json:"conflicting_event"`
	ConflictKey      string              `json:"conflict_key"`
	Reason           string              `json:"reason"`
	Plan             *CalendarPlan       `json:"plan"`
	Status           string              `json:"status"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
}

// ShadowEventSummary is a trimmed down view of an event stored alongside proposals.
type ShadowEventSummary struct {
	EventID    string `json:"event_id"`
	CalendarID string `json:"calendar_id,omitempty"`
	Summary    string `json:"summary"`
	StartISO   string `json:"start_iso"`
	EndISO     string `json:"end_iso"`
	Location   string `json:"location,omitempty"`
}

// ShadowSnapshot is returned via HTTP for debugging/tests.
type ShadowSnapshot struct {
	UserID    string            `json:"user_id"`
	Events    []ShadowEventView `json:"events"`
	Proposals []*ShadowProposal `json:"proposals"`
}

// ShadowEventView is a DTO for API responses.
type ShadowEventView struct {
	EventID    string `json:"event_id"`
	Summary    string `json:"summary"`
	StartISO   string `json:"start_iso"`
	EndISO     string `json:"end_iso"`
	AllDay     bool   `json:"all_day"`
	Status     string `json:"status"`
	ChangeType string `json:"change_type"`
	Location   string `json:"location,omitempty"`
}

// NewShadowCalendarService wires the service to Redis streams.
func NewShadowCalendarService(redisClient *redis.Client, planner PlannerRunner, opts ShadowCalendarOptions) (*ShadowCalendarService, error) {
	if redisClient == nil {
		return nil, errors.New("redis client is required")
	}
	if planner == nil {
		return nil, errors.New("planner runner is required")
	}
	store := opts.Store
	if store == nil {
		store = &redisShadowStore{client: redisClient}
	}
	group := opts.GroupName
	if group == "" {
		group = shadowDefaultGroup
	}
	batch := opts.BatchSize
	if batch <= 0 {
		batch = shadowDefaultBatch
	}
	timeout := opts.PollTimeout
	if timeout <= 0 {
		timeout = shadowDefaultTimeout
	}
	userIDs := sanitizeUserIDs(opts.UserIDs)
	if len(userIDs) == 0 {
		return nil, errors.New("at least one user id required for shadow calendar")
	}
	return &ShadowCalendarService{
		redisClient: redisClient,
		planner:     planner,
		store:       store,
		groupName:   group,
		userIDs:     userIDs,
		batchSize:   batch,
		pollTimeout: timeout,
		consumerID:  uuid.New().String(),
	}, nil
}

// Start launches consumers for each user's calendar input stream.
func (s *ShadowCalendarService) Start(ctx context.Context) error {
	if s.ctx != nil {
		return errors.New("shadow calendar already started")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	s.ctx = ctx
	s.cancel = cancel
	for _, userID := range s.userIDs {
		streamKey := userCalendarStream(userID)
		if err := s.ensureGroup(ctx, streamKey); err != nil {
			return err
		}
		consumerName := fmt.Sprintf("%s-%s", s.consumerID, userID)
		s.wg.Add(1)
		go s.consumeLoop(ctx, userID, streamKey, consumerName)
	}
	return nil
}

// Stop stops background consumers.
func (s *ShadowCalendarService) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	s.ctx = nil
}

// GetSnapshot returns the stored events + proposals for debugging/tests.
func (s *ShadowCalendarService) GetSnapshot(ctx context.Context, userID string) (*ShadowSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	events, err := s.store.ListEvents(ctx, userID)
	if err != nil {
		return nil, err
	}
	proposals, err := s.store.ListProposals(ctx, userID)
	if err != nil {
		return nil, err
	}
	views := make([]ShadowEventView, 0, len(events))
	for _, evt := range events {
		views = append(views, ShadowEventView{
			EventID:    evt.EventID,
			Summary:    evt.Summary,
			StartISO:   evt.StartTime.Format(time.RFC3339),
			EndISO:     evt.EndTime.Format(time.RFC3339),
			AllDay:     evt.AllDay,
			Status:     evt.Status,
			ChangeType: evt.ChangeType,
			Location:   evt.Location,
		})
	}
	sort.SliceStable(proposals, func(i, j int) bool {
		return proposals[i].CreatedAt.Before(proposals[j].CreatedAt)
	})
	return &ShadowSnapshot{UserID: userID, Events: views, Proposals: proposals}, nil
}

// GetProposal returns a stored proposal by ID.
func (s *ShadowCalendarService) GetProposal(ctx context.Context, userID, proposalID string) (*ShadowProposal, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("shadow calendar store not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	proposalID = strings.TrimSpace(proposalID)
	userID = strings.TrimSpace(userID)
	if proposalID == "" || userID == "" {
		return nil, nil
	}
	return s.store.GetProposal(ctx, userID, proposalID)
}

// SaveProposal persists a proposal after it has been modified.
func (s *ShadowCalendarService) SaveProposal(ctx context.Context, proposal *ShadowProposal) error {
	if s == nil || s.store == nil {
		return errors.New("shadow calendar store not configured")
	}
	if proposal == nil {
		return errors.New("proposal is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return s.store.SaveProposal(ctx, proposal)
}

func (s *ShadowCalendarService) ensureGroup(ctx context.Context, streamKey string) error {
	if err := s.redisClient.XGroupCreateMkStream(ctx, streamKey, s.groupName, "0").Err(); err != nil {
		if !strings.Contains(err.Error(), "BUSYGROUP") {
			return fmt.Errorf("shadow calendar: create group %s for %s: %w", s.groupName, streamKey, err)
		}
	}
	return nil
}

func (s *ShadowCalendarService) consumeLoop(ctx context.Context, userID, streamKey, consumerName string) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		cmd := s.redisClient.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    s.groupName,
			Consumer: consumerName,
			Streams:  []string{streamKey, ">"},
			Count:    s.batchSize,
			Block:    s.pollTimeout,
		})
		streams, err := cmd.Result()
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			if err == redis.Nil {
				continue
			}
			log.Printf("shadow calendar: read failure for %s: %v", userID, err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		for _, stream := range streams {
			for _, msg := range stream.Messages {
				if err := s.processMessage(ctx, userID, msg); err != nil {
					log.Printf("shadow calendar: process error user=%s id=%s: %v", userID, msg.ID, err)
				}
				if err := s.redisClient.XAck(ctx, streamKey, s.groupName, msg.ID).Err(); err != nil {
					log.Printf("shadow calendar: failed ack %s: %v", msg.ID, err)
				}
			}
		}
	}
}

func (s *ShadowCalendarService) processMessage(ctx context.Context, fallbackUserID string, msg redis.XMessage) error {
	delta, err := parseCalendarDelta(msg.Values)
	if err != nil {
		return err
	}
	if delta.Event == nil {
		return errors.New("shadow calendar: missing event payload")
	}
	if delta.Event.UserID == "" {
		delta.Event.UserID = fallbackUserID
	}
	if delta.Deleted {
		if err := s.store.RemoveEvent(ctx, delta.Event.UserID, delta.Event.EventID); err != nil {
			return err
		}
		return s.store.RemoveProposalsForEvent(ctx, delta.Event.UserID, delta.Event.EventID)
	}
	if err := s.store.UpsertEvent(ctx, delta.Event); err != nil {
		return err
	}
	return s.evaluateConflicts(ctx, delta.Event.UserID, delta.Event)
}

func (s *ShadowCalendarService) evaluateConflicts(ctx context.Context, userID string, event *ShadowEvent) error {
	events, err := s.store.ListEvents(ctx, userID)
	if err != nil {
		return err
	}
	for _, existing := range events {
		if existing.EventID == event.EventID {
			continue
		}
		if !eventsOverlap(event, existing) {
			continue
		}
		if err := s.ensureProposal(ctx, userID, event, existing); err != nil {
			return err
		}
	}
	return nil
}

func (s *ShadowCalendarService) ensureProposal(ctx context.Context, userID string, a, b *ShadowEvent) error {
	conflictKey := buildConflictKey(a.EventID, b.EventID)
	if proposal, _ := s.store.FindProposalByConflict(ctx, userID, conflictKey); proposal != nil {
		return nil
	}
	planDate := a.StartTime.Format("2006-01-02")
	timeBlock := fmt.Sprintf("Resolve overlap: %s (%s-%s) vs %s (%s-%s)",
		a.Summary,
		a.StartTime.Format(time.Kitchen),
		a.EndTime.Format(time.Kitchen),
		b.Summary,
		b.StartTime.Format(time.Kitchen),
		b.EndTime.Format(time.Kitchen))
	plan, err := s.planner.GenerateCalendarPlan(ctx, planDate, timeBlock, "calendar_conflict")
	if err != nil {
		return fmt.Errorf("planner run failed: %w", err)
	}
	now := time.Now().UTC()
	proposal := &ShadowProposal{
		ID:               uuid.New().String(),
		UserID:           userID,
		PrimaryEvent:     summarizeEvent(a),
		ConflictingEvent: summarizeEvent(b),
		ConflictKey:      conflictKey,
		Reason:           fmt.Sprintf("Overlap detected between %s and %s", a.Summary, b.Summary),
		Plan:             plan,
		Status:           "pending",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	return s.store.SaveProposal(ctx, proposal)
}

func summarizeEvent(evt *ShadowEvent) *ShadowEventSummary {
	return &ShadowEventSummary{
		EventID:    evt.EventID,
		CalendarID: evt.CalendarID,
		Summary:    evt.Summary,
		StartISO:   evt.StartTime.Format(time.RFC3339),
		EndISO:     evt.EndTime.Format(time.RFC3339),
		Location:   evt.Location,
	}
}

func eventsOverlap(a, b *ShadowEvent) bool {
	// Treat touching events (end == start) as needing attention only if they are not all-day
	if a.AllDay || b.AllDay {
		return !(a.EndTime.Before(b.StartTime) || b.EndTime.Before(a.StartTime))
	}
	return a.StartTime.Before(b.EndTime) && b.StartTime.Before(a.EndTime)
}

// userCalendarStream returns stream key.
func userCalendarStream(userID string) string {
	return fmt.Sprintf("user:%s:in:calendar", userID)
}

// calendarDelta is a parsed Redis stream entry.
type calendarDelta struct {
	Event   *ShadowEvent
	Deleted bool
}

func parseCalendarDelta(values map[string]interface{}) (*calendarDelta, error) {
	eventID := stringValue(values, "event_id")
	if eventID == "" {
		return nil, errors.New("missing event_id in calendar delta")
	}
	userID := stringValue(values, "user_id")
	calendarID := stringValue(values, "calendar_id")
	allDay := parseBool(stringValue(values, "all_day"))
	startISO := stringValue(values, "start_time")
	endISO := stringValue(values, "end_time")
	startTime, err := parseEventTime(startISO, allDay)
	if err != nil {
		return nil, fmt.Errorf("invalid start time: %w", err)
	}
	endTime, err := parseEventTime(endISO, allDay)
	if err != nil {
		return nil, fmt.Errorf("invalid end time: %w", err)
	}
	if !endTime.After(startTime) {
		if allDay {
			endTime = startTime.Add(24 * time.Hour)
		} else {
			endTime = startTime.Add(30 * time.Minute)
		}
	}
	sequence := parseInt(stringValue(values, "sequence"))
	updated := parseTimeOrZero(stringValue(values, "updated"))
	notified := parseTimeOrZero(stringValue(values, "notified_at"))
	status := strings.ToLower(stringValue(values, "status"))
	changeType := strings.ToLower(stringValue(values, "change_type"))
	event := &ShadowEvent{
		UserID:         userID,
		CalendarID:     calendarID,
		EventID:        eventID,
		Summary:        strings.TrimSpace(stringValue(values, "event_summary")),
		Description:    strings.TrimSpace(stringValue(values, "event_description")),
		Location:       strings.TrimSpace(stringValue(values, "event_location")),
		StartTime:      startTime,
		EndTime:        endTime,
		StartTimezone:  stringValue(values, "start_timezone"),
		EndTimezone:    stringValue(values, "end_timezone"),
		AllDay:         allDay,
		Status:         status,
		ChangeType:     changeType,
		HTMLLink:       stringValue(values, "html_link"),
		CreatorEmail:   stringValue(values, "creator_email"),
		OrganizerEmail: stringValue(values, "organizer_email"),
		Sequence:       sequence,
		Updated:        updated,
		NotifiedAt:     notified,
		RawPayload:     stringValue(values, "raw_event"),
		ChannelID:      stringValue(values, "channel_id"),
		RecordedAt:     time.Now().UTC(),
	}
	deleted := status == "cancelled" || changeType == "deleted"
	return &calendarDelta{Event: event, Deleted: deleted}, nil
}

func stringValue(values map[string]interface{}, key string) string {
	if raw, ok := values[key]; ok && raw != nil {
		return strings.TrimSpace(fmt.Sprintf("%v", raw))
	}
	return ""
}

func parseBool(value string) bool {
	b, err := strconv.ParseBool(strings.ToLower(value))
	if err != nil {
		return false
	}
	return b
}

func parseInt(value string) int {
	if value == "" {
		return 0
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return n
}

func parseTimeOrZero(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t
	}
	return time.Time{}
}

func parseEventTime(value string, allDay bool) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("empty timestamp")
	}
	if strings.Contains(value, "T") {
		return time.Parse(time.RFC3339, value)
	}
	layout := "2006-01-02"
	day, err := time.Parse(layout, value)
	if err != nil {
		return time.Time{}, err
	}
	if !allDay {
		return day, nil
	}
	return day, nil
}

func buildConflictKey(eventA, eventB string) string {
	ids := []string{eventA, eventB}
	sort.Strings(ids)
	return strings.Join(ids, "|")
}

func sanitizeUserIDs(userIDs []string) []string {
	set := make(map[string]struct{})
	for _, id := range userIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		set[id] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for id := range set {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

// redisShadowStore stores state inside Redis hashes for durability.
type redisShadowStore struct {
	client *redis.Client
}

func (s *redisShadowStore) UpsertEvent(ctx context.Context, event *ShadowEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	key := shadowEventKey(event.UserID)
	return s.client.HSet(ctx, key, event.EventID, payload).Err()
}

func (s *redisShadowStore) RemoveEvent(ctx context.Context, userID, eventID string) error {
	if userID == "" || eventID == "" {
		return nil
	}
	return s.client.HDel(ctx, shadowEventKey(userID), eventID).Err()
}

func (s *redisShadowStore) ListEvents(ctx context.Context, userID string) ([]*ShadowEvent, error) {
	if userID == "" {
		return nil, nil
	}
	entries, err := s.client.HGetAll(ctx, shadowEventKey(userID)).Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}
	events := make([]*ShadowEvent, 0, len(entries))
	for _, raw := range entries {
		var evt ShadowEvent
		if err := json.Unmarshal([]byte(raw), &evt); err != nil {
			continue
		}
		events = append(events, &evt)
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].StartTime.Before(events[j].StartTime)
	})
	return events, nil
}

func (s *redisShadowStore) SaveProposal(ctx context.Context, proposal *ShadowProposal) error {
	payload, err := json.Marshal(proposal)
	if err != nil {
		return err
	}
	if err := s.client.HSet(ctx, shadowProposalKey(proposal.UserID), proposal.ID, payload).Err(); err != nil {
		return err
	}
	return s.client.HSet(ctx, shadowConflictKey(proposal.UserID), proposal.ConflictKey, proposal.ID).Err()
}

func (s *redisShadowStore) GetProposal(ctx context.Context, userID, proposalID string) (*ShadowProposal, error) {
	if userID == "" || proposalID == "" {
		return nil, nil
	}
	raw, err := s.client.HGet(ctx, shadowProposalKey(userID), proposalID).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var proposal ShadowProposal
	if err := json.Unmarshal([]byte(raw), &proposal); err != nil {
		return nil, err
	}
	return &proposal, nil
}

func (s *redisShadowStore) ListProposals(ctx context.Context, userID string) ([]*ShadowProposal, error) {
	entries, err := s.client.HGetAll(ctx, shadowProposalKey(userID)).Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}
	proposals := make([]*ShadowProposal, 0, len(entries))
	for _, raw := range entries {
		var proposal ShadowProposal
		if err := json.Unmarshal([]byte(raw), &proposal); err != nil {
			continue
		}
		proposals = append(proposals, &proposal)
	}
	sort.SliceStable(proposals, func(i, j int) bool {
		return proposals[i].CreatedAt.Before(proposals[j].CreatedAt)
	})
	return proposals, nil
}

func (s *redisShadowStore) FindProposalByConflict(ctx context.Context, userID, conflictKey string) (*ShadowProposal, error) {
	if conflictKey == "" {
		return nil, nil
	}
	proposalID, err := s.client.HGet(ctx, shadowConflictKey(userID), conflictKey).Result()
	if err == redis.Nil || proposalID == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	raw, err := s.client.HGet(ctx, shadowProposalKey(userID), proposalID).Result()
	if err != nil {
		return nil, err
	}
	var proposal ShadowProposal
	if err := json.Unmarshal([]byte(raw), &proposal); err != nil {
		return nil, err
	}
	return &proposal, nil
}

func (s *redisShadowStore) RemoveProposalsForEvent(ctx context.Context, userID, eventID string) error {
	proposals, err := s.ListProposals(ctx, userID)
	if err != nil {
		return err
	}
	for _, proposal := range proposals {
		if proposal.PrimaryEvent.EventID != eventID && proposal.ConflictingEvent.EventID != eventID {
			continue
		}
		if err := s.client.HDel(ctx, shadowProposalKey(userID), proposal.ID).Err(); err != nil {
			return err
		}
		if err := s.client.HDel(ctx, shadowConflictKey(userID), proposal.ConflictKey).Err(); err != nil {
			return err
		}
	}
	return nil
}

func shadowEventKey(userID string) string {
	return fmt.Sprintf("shadow_calendar:%s", userID)
}

func shadowProposalKey(userID string) string {
	return fmt.Sprintf("shadow_calendar:%s:proposals", userID)
}

func shadowConflictKey(userID string) string {
	return fmt.Sprintf("shadow_calendar:%s:conflicts", userID)
}
