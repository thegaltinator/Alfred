package wb

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	streamKeyFormat   = "user:%s:wb"
	defaultBlock      = 5 * time.Second
	defaultBatchCount = 50
)

// Event is the typed form of a whiteboard stream entry.
// Match client's expected JSON structure with capitalized field names
type Event struct {
	ID       string         `json:"ID"`
	Stream   string         `json:"Stream"`
	UserID   string         `json:"UserID"`
	ThreadID string         `json:"ThreadID"`
	Values   map[string]any `json:"Values"`
}

// Bus provides typed helpers for the per-user whiteboard stream.
type Bus struct {
	client *redis.Client
}

// NewBus creates a new whiteboard bus for the given redis client.
func NewBus(client *redis.Client) *Bus {
	return &Bus{client: client}
}

// StreamKey returns the canonical whiteboard stream key for a user.
func StreamKey(userID string) string {
	if strings.TrimSpace(userID) == "" {
		userID = "test-user"
	}
	return fmt.Sprintf(streamKeyFormat, userID)
}

// Append writes a payload to the user's whiteboard stream, attaching a ts if missing.
func (b *Bus) Append(ctx context.Context, userID string, values map[string]any) (string, error) {
	return b.AppendWithThread(ctx, userID, "", values)
}

// AppendWithThread writes a payload to the user's whiteboard stream with thread_id.
func (b *Bus) AppendWithThread(ctx context.Context, userID, threadID string, values map[string]any) (string, error) {
	if b == nil || b.client == nil {
		return "", fmt.Errorf("whiteboard bus not configured")
	}

	if values == nil {
		values = make(map[string]any)
	}
	if _, ok := values["ts"]; !ok {
		values["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if threadID != "" {
		values["thread_id"] = threadID
	}

	return b.client.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamKey(userID),
		Values: values,
	}).Result()
}

// Tail blocks for new events after afterID and returns them with the latest ID observed.
func (b *Bus) Tail(ctx context.Context, userID, afterID string) ([]Event, string, error) {
	if b == nil || b.client == nil {
		return nil, afterID, fmt.Errorf("whiteboard bus not configured")
	}

	if strings.TrimSpace(afterID) == "" {
		afterID = "$"
	}

	res, err := b.client.XRead(ctx, &redis.XReadArgs{
		Streams: []string{StreamKey(userID), afterID},
		Count:   defaultBatchCount,
		Block:   defaultBlock,
	}).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, afterID, nil
		}
		return nil, afterID, err
	}

	events := make([]Event, 0)
	nextID := afterID

	for _, stream := range res {
		for _, msg := range stream.Messages {
			values := make(map[string]any, len(msg.Values))
			for k, v := range msg.Values {
				values[k] = v
			}
			threadID := stringVal(values["thread_id"])
			events = append(events, Event{
				ID:       msg.ID,
				Stream:   stream.Stream,
				UserID:   userIDFromStream(stream.Stream),
				ThreadID: threadID,
				Values:   values,
			})
			nextID = msg.ID
		}
	}

	return events, nextID, nil
}

func stringVal(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case []byte:
		return string(val)
	default:
		return ""
	}
}

func userIDFromStream(stream string) string {
	parts := strings.Split(stream, ":")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}
