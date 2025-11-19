package productivity

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"alfred-cloud/streams"
	"github.com/redis/go-redis/v9"
)

const (
	ConsumerGroup   = "productivity-subagent"
	ConsumerName    = "worker-1"
	StreamKeyFormat = "user:%s:in:prod"
	WhiteboardFormat = "user:%s:wb"
)

type ProductivityConsumer struct {
	client      *redis.Client
	classifier  *Classifier
	heuristics  *HeuristicService
	streams     *streams.StreamsHelper
	userIDs     []string
	stopChan    chan struct{}
}

func NewProductivityConsumer(client *redis.Client, classifier *Classifier, heuristics *HeuristicService, userIDs []string) *ProductivityConsumer {
	return &ProductivityConsumer{
		client:     client,
		classifier: classifier,
		heuristics: heuristics,
		streams:    streams.NewStreamsHelper(client),
		userIDs:    userIDs,
		stopChan:   make(chan struct{}),
	}
}

func (c *ProductivityConsumer) Start(ctx context.Context) error {
	log.Printf("Starting productivity consumer for users: %v", c.userIDs)

	// Ensure groups exist
	streamKeys := make([]string, 0, len(c.userIDs))
	args := make([]string, 0, len(c.userIDs)*2)

	for _, userID := range c.userIDs {
		key := fmt.Sprintf(StreamKeyFormat, userID)
		streamKeys = append(streamKeys, key)
		
		// Create group (ignore if exists)
		err := c.client.XGroupCreateMkStream(ctx, key, ConsumerGroup, "$").Err()
		if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
			log.Printf("Failed to create group for %s: %v", key, err)
		}
		args = append(args, key)
	}
	// Append ">" for each stream
	for range c.userIDs {
		args = append(args, ">")
	}

	ticker := time.NewTicker(100 * time.Millisecond) // Small delay to prevent tight loop if empty
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Read from all streams
			res, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
				Group:    ConsumerGroup,
				Consumer: ConsumerName,
				Streams:  args,
				Count:    10,
				Block:    2 * time.Second,
			}).Result()

			if err != nil && err != redis.Nil {
				if !strings.Contains(err.Error(), "timeout") {
					log.Printf("Error reading streams: %v", err)
				}
				continue
			}

			for _, stream := range res {
				userID := extractUserID(stream.Stream)
				for _, msg := range stream.Messages {
					if err := c.processMessage(ctx, userID, msg.Values); err != nil {
						log.Printf("Error processing message %s: %v", msg.ID, err)
						// We still ack? Or retry? For now ack to avoid stuck queue
					}
					c.client.XAck(ctx, stream.Stream, ConsumerGroup, msg.ID)
				}
			}
		}
	}
}

func (c *ProductivityConsumer) Stop() {
	close(c.stopChan)
}

func (c *ProductivityConsumer) processMessage(ctx context.Context, userID string, values map[string]interface{}) error {
	// Detect message type
	// Activity Update: has "event_id"
	if _, ok := values["event_id"]; ok {
		return c.handleActivityUpdate(ctx, userID, values)
	}
	
	// Heartbeat: has "bundle_id" (or we assume it's heartbeat)
	return c.handleHeartbeat(ctx, userID, values)
}

func (c *ProductivityConsumer) handleActivityUpdate(ctx context.Context, userID string, values map[string]interface{}) error {
	// Parse payload
	payload := EventPayload{
		UserID: userID,
	}
	
	if v, ok := values["event_id"].(string); ok { payload.EventID = v }
	if v, ok := values["title"].(string); ok { payload.Title = v }
	if v, ok := values["description"].(string); ok { payload.Description = v }
	
	// Parse times
	if v, ok := values["start_time"].(string); ok {
		t, _ := time.Parse(time.RFC3339, v)
		payload.StartTime = t
	}
	if v, ok := values["end_time"].(string); ok {
		t, _ := time.Parse(time.RFC3339, v)
		payload.EndTime = t
	}

	log.Printf("Processing activity update for %s: %s", userID, payload.Title)
	_, err := c.heuristics.UpsertEventHeuristic(ctx, payload)
	return err
}

func (c *ProductivityConsumer) handleHeartbeat(ctx context.Context, userID string, values map[string]interface{}) error {
	hb := Heartbeat{
		UserID: userID,
		Timestamp: time.Now(), // Default
	}

	if v, ok := values["bundle_id"].(string); ok { hb.BundleID = v }
	if v, ok := values["window_title"].(string); ok { hb.WindowTitle = v }
	if v, ok := values["url"].(string); ok { hb.URL = v }
	if v, ok := values["activity_id"].(string); ok { hb.ActivityID = v }
	
	if v, ok := values["ts"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			hb.Timestamp = t
		}
	}

	decision, err := c.classifier.ProcessHeartbeat(ctx, hb)
	if err != nil {
		return fmt.Errorf("classifier process: %w", err)
	}

	if decision != nil {
		// Emit decision to whiteboard
		return c.emitDecision(ctx, userID, decision)
	}

	return nil
}

func (c *ProductivityConsumer) emitDecision(ctx context.Context, userID string, decision *Decision) error {
	log.Printf("Emitting decision for %s: %s (%s)", userID, decision.Kind, decision.Observed)

	wbKey := fmt.Sprintf(WhiteboardFormat, userID)

	// Whiteboard only gets the core decision: underrun or overrun
	msg := map[string]interface{}{
		"decision": string(decision.Kind),
		"ts":       time.Now().UTC().Format(time.RFC3339),
	}

	_, err := c.client.XAdd(ctx, &redis.XAddArgs{
		Stream: wbKey,
		Values: msg,
	}).Result()

	return err
}

func extractUserID(streamKey string) string {
	// user:{id}:in:prod
	parts := strings.Split(streamKey, ":")
	if len(parts) >= 2 {
		return parts[1]
	}
	return "unknown"
}

