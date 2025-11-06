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
