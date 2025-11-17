package streams

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client holds the initialized Redis client once Init succeeds.
var Client *redis.Client

const (
	defaultRedisURL = "redis://localhost:6379"
	healthStream    = "health:user:dev:test"
	healthGroup     = "cloud-bootstrap"
	healthConsumer  = "bootstrap-check"
)

// Init connects to Redis using REDIS_URL (falls back to localhost) and verifies
// XADD/XREADGROUP support so downstream code can rely on streams APIs.
func Init(ctx context.Context) (*redis.Client, error) {
	client, err := newClientFromEnv()
	if err != nil {
		return nil, err
	}

	if err := verifyStreamOps(ctx, client); err != nil {
		client.Close()
		return nil, err
	}

	Client = client
	return Client, nil
}

func newClientFromEnv() (*redis.Client, error) {
	url := strings.TrimSpace(os.Getenv("REDIS_URL"))
	if url == "" {
		url = defaultRedisURL
	}

	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("redis: invalid REDIS_URL %q: %w", url, err)
	}

	return redis.NewClient(opts), nil
}

func verifyStreamOps(ctx context.Context, client *redis.Client) error {
	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis: ping failed: %w", err)
	}

	// Ensure the health check stream + group exist.
	if err := client.XGroupCreateMkStream(ctx, healthStream, healthGroup, "$").Err(); err != nil {
		if !strings.Contains(err.Error(), "BUSYGROUP") {
			return fmt.Errorf("redis: create stream group: %w", err)
		}
	}

	// Write a test entry to confirm XADD.
	msgID, err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: healthStream,
		Values: map[string]any{
			"msg": "redis-online-check",
			"ts":  time.Now().UTC().Format(time.RFC3339Nano),
		},
	}).Result()
	if err != nil {
		return fmt.Errorf("redis: XADD failed: %w", err)
	}

	// Consume the entry via XREADGROUP to verify consumer groups work.
	readRes, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    healthGroup,
		Consumer: healthConsumer,
		Streams:  []string{healthStream, ">"},
		Count:    1,
		Block:    time.Second,
	}).Result()
	if err != nil {
		return fmt.Errorf("redis: XREADGROUP failed: %w", err)
	}
	if len(readRes) == 0 || len(readRes[0].Messages) == 0 {
		return fmt.Errorf("redis: XREADGROUP returned no messages for %s", msgID)
	}

	// Acknowledge to keep the stream clean for future checks.
	entryID := readRes[0].Messages[0].ID
	if err := client.XAck(ctx, healthStream, healthGroup, entryID).Err(); err != nil {
		return fmt.Errorf("redis: XACK failed for %s: %w", entryID, err)
	}

	return nil
}

// StreamsHelper provides helper methods for working with Redis streams
type StreamsHelper struct {
	client *redis.Client
}

// NewStreamsHelper creates a new streams helper
func NewStreamsHelper(client *redis.Client) *StreamsHelper {
	return &StreamsHelper{
		client: client,
	}
}

// AppendToStream appends data to a Redis stream
func (sh *StreamsHelper) AppendToStream(ctx context.Context, streamKey string, data map[string]interface{}) (string, error) {
	return sh.client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: data,
	}).Result()
}

// ReadFromStream reads from a Redis stream
func (sh *StreamsHelper) ReadFromStream(ctx context.Context, streamKey string, lastID string, count int64) ([]redis.XStream, error) {
	return sh.client.XRead(ctx, &redis.XReadArgs{
		Streams: []string{streamKey, lastID},
		Count:   count,
		Block:   time.Second,
	}).Result()
}

// ReadFromGroup reads from a consumer group in a Redis stream
func (sh *StreamsHelper) ReadFromGroup(ctx context.Context, streamKey, group, consumer string, count int64) ([]redis.XStream, error) {
	return sh.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    group,
		Consumer: consumer,
		Streams:  []string{streamKey, ">"},
		Count:    count,
		Block:    time.Second,
	}).Result()
}

// CreateConsumerGroup creates a consumer group for a stream
func (sh *StreamsHelper) CreateConsumerGroup(ctx context.Context, streamKey, group string) error {
	return sh.client.XGroupCreateMkStream(ctx, streamKey, group, "$").Err()
}

// AcknowledgeMessage acknowledges a message in a consumer group
func (sh *StreamsHelper) AcknowledgeMessage(ctx context.Context, streamKey, group string, messageIDs ...string) (int64, error) {
	return sh.client.XAck(ctx, streamKey, group, messageIDs...).Result()
}

// GetStreamLength returns the length of a stream
func (sh *StreamsHelper) GetStreamLength(ctx context.Context, streamKey string) (int64, error) {
	return sh.client.XLen(ctx, streamKey).Result()
}

// TrimStream trims a stream to a maximum length
func (sh *StreamsHelper) TrimStream(ctx context.Context, streamKey string, maxLength int64) error {
	return sh.client.XTrimMaxLen(ctx, streamKey, maxLength).Err()
}
