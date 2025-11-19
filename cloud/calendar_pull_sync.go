package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"alfred-cloud/security"
	"alfred-cloud/streams"
	"alfred-cloud/subagents/productivity"
	"github.com/redis/go-redis/v9"
	calendar "google.golang.org/api/calendar/v3"
)

type CalendarPullSync struct {
	tokenStore    *security.TokenStore
	streamsHelper *streams.StreamsHelper
	heuristics    *productivity.HeuristicService
	redisClient   *redis.Client
	configuredIDs []string
	interval      time.Duration
	lookback      time.Duration
	calendarID    string
	enabled       bool
}

func NewCalendarPullSync(redisClient *redis.Client, tokenStore *security.TokenStore, streamsHelper *streams.StreamsHelper, heuristics *productivity.HeuristicService, userIDs []string, interval, lookback time.Duration, enabled bool) *CalendarPullSync {
	return &CalendarPullSync{
		tokenStore:    tokenStore,
		streamsHelper: streamsHelper,
		heuristics:    heuristics,
		redisClient:   redisClient,
		configuredIDs: userIDs,
		interval:      interval,
		lookback:      lookback,
		calendarID:    "primary",
		enabled:       enabled,
	}
}

func (p *CalendarPullSync) Start(ctx context.Context) {
	if !p.enabled {
		log.Println("Calendar pull sync disabled")
		return
	}
	if p.interval <= 0 {
		p.interval = 3 * time.Minute
	}
	if p.lookback <= 0 {
		p.lookback = 48 * time.Hour
	}

	go func() {
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		for {
			p.runOnce(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (p *CalendarPullSync) runOnce(ctx context.Context) {
	userIDs := p.configuredIDs
	if len(userIDs) == 0 {
		userIDs = p.discoverUsers(ctx)
	}
	if len(userIDs) == 0 {
		log.Println("Pull sync: no users discovered")
		return
	}
	for _, userID := range userIDs {
		p.syncUser(ctx, userID)
	}
}

func (p *CalendarPullSync) discoverUsers(ctx context.Context) []string {
	if p.redisClient == nil {
		return nil
	}
	iter := p.redisClient.Scan(ctx, 0, "oauth_token:*:calendar", 100).Iterator()
	seen := make(map[string]struct{})
	for iter.Next(ctx) {
		key := iter.Val()
		parts := strings.Split(key, ":")
		if len(parts) < 3 {
			continue
		}
		userID := parts[1]
		if userID == "" {
			continue
		}
		seen[userID] = struct{}{}
	}
	if err := iter.Err(); err != nil {
		log.Printf("Pull sync: discover users scan error: %v", err)
	}
	users := make([]string, 0, len(seen))
	for u := range seen {
		users = append(users, u)
	}
	return users
}

func (p *CalendarPullSync) syncUser(ctx context.Context, userID string) {
	googleClient := security.NewGoogleServiceClient(p.tokenStore)
	calendarService, err := googleClient.GetCalendarService(ctx, userID)
	if err != nil {
		log.Printf("Pull sync: calendar service err user=%s: %v", userID, err)
		return
	}

	events, err := p.fetchRecentEvents(ctx, calendarService, p.calendarID, time.Now().Add(-p.lookback))
	if err != nil {
		log.Printf("Pull sync: fetch recent err user=%s: %v", userID, err)
		return
	}
	if len(events) == 0 {
		return
	}

	inputKey := "user:" + userID + ":in:calendar"
	for _, changeData := range events {
		changeData["channel_id"] = "pull-sync"
		changeData["resource_state"] = "exists"
		changeData["resource_uri"] = changeData["resource_uri"]
		changeData["notified_at"] = time.Now().UTC().Format(time.RFC3339Nano)
		changeData["user_id"] = userID
		changeData["calendar_id"] = p.calendarID

		if _, err := p.streamsHelper.AppendToStream(ctx, inputKey, changeData); err != nil {
			log.Printf("Pull sync: enqueue err user=%s: %v", userID, err)
		}
		if p.heuristics != nil && changeData["change_type"] != "deleted" {
			payload, err := p.buildHeuristicPayload(changeData, userID)
			if err != nil {
				log.Printf("Pull sync: heuristic payload err user=%s event=%v: %v", userID, changeData["event_id"], err)
				continue
			}
			if stored, err := p.heuristics.UpsertEventHeuristic(ctx, payload); err != nil {
				log.Printf("Pull sync: heuristic persist err user=%s event=%s: %v", userID, payload.EventID, err)
			} else {
				log.Printf("Pull sync: stored heuristic user=%s event=%s apps=%v", userID, stored.EventID, stored.ExpectedApps)
			}
		}
	}
}

func (p *CalendarPullSync) fetchRecentEvents(ctx context.Context, calendarService *calendar.Service, calendarID string, since time.Time) ([]map[string]interface{}, error) {
	call := calendarService.Events.List(calendarID).
		ShowDeleted(true).
		SingleEvents(true).
		TimeMin(since.Format(time.RFC3339)).
		TimeMax(time.Now().Add(48 * time.Hour).Format(time.RFC3339))

	resp, err := call.Context(ctx).Do()
	if err != nil {
		return nil, err
	}

	changes := make([]map[string]interface{}, 0, len(resp.Items))
	for _, event := range resp.Items {
		changes = append(changes, normalizeEventChange(event, "", calendarID))
	}
	return changes, nil
}

func (p *CalendarPullSync) buildHeuristicPayload(change map[string]interface{}, userID string) (productivity.EventPayload, error) {
	payload := productivity.EventPayload{
		UserID:      userID,
		EventID:     fmt.Sprint(change["event_id"]),
		Title:       fmt.Sprint(change["event_summary"]),
		Description: fmt.Sprint(change["event_description"]),
	}
	if payload.EventID == "" {
		return payload, fmt.Errorf("event_id missing")
	}
	start, err := parseEventTimestamp(fmt.Sprint(change["start_time"]))
	if err != nil {
		return payload, fmt.Errorf("invalid start_time: %w", err)
	}
	payload.StartTime = start

	endRaw := fmt.Sprint(change["end_time"])
	if endRaw != "" {
		if end, err := parseEventTimestamp(endRaw); err == nil {
			payload.EndTime = end
		} else {
			return payload, fmt.Errorf("invalid end_time: %w", err)
		}
	}
	if payload.EndTime.IsZero() {
		payload.EndTime = payload.StartTime.Add(time.Hour)
	}
	return payload, nil
}
