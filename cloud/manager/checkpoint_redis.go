package manager

import (
	"context"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCheckpointStore persists checkpoints to Redis for replay safety.
type RedisCheckpointStore struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

// NewRedisCheckpointStore creates a Redis-backed checkpoint store.
func NewRedisCheckpointStore(client *redis.Client) *RedisCheckpointStore {
	return &RedisCheckpointStore{
		client: client,
		prefix: "manager:ckpt",
		ttl:    7 * 24 * time.Hour,
	}
}

func (s *RedisCheckpointStore) hashKey(userID, threadID string) string {
	return strings.Join([]string{s.prefix, "hash", strings.TrimSpace(userID), strings.TrimSpace(threadID)}, ":")
}

func (s *RedisCheckpointStore) sideEffectsKey(userID, threadID string) string {
	return strings.Join([]string{s.prefix, "side_effects", strings.TrimSpace(userID), strings.TrimSpace(threadID)}, ":")
}

// Get returns the checkpoint for a user/thread, falling back to zero value if not found.
func (s *RedisCheckpointStore) Get(userID, threadID string) Checkpoint {
	if s == nil || s.client == nil {
		return Checkpoint{}
	}
	ctx := context.Background()
	hash := s.hashKey(userID, threadID)

	fields, err := s.client.HGetAll(ctx, hash).Result()
	if err != nil || len(fields) == 0 {
		return Checkpoint{}
	}

	cp := Checkpoint{
		LastWBID:        fields["last_wb_id"],
		LastPlanID:      fields["last_plan_id"],
		LastPlanVersion: fields["last_plan_version"],
		PendingPromptID: fields["pending_prompt_id"],
	}

	// Side effects stored in a set to dedupe.
	if members, err := s.client.SMembers(ctx, s.sideEffectsKey(userID, threadID)).Result(); err == nil {
		cp.SideEffects = dedupeStrings(members)
	}
	return cp
}

// Save upserts the checkpoint for a user/thread.
func (s *RedisCheckpointStore) Save(userID, threadID string, cp Checkpoint) {
	if s == nil || s.client == nil {
		return
	}
	ctx := context.Background()
	hash := s.hashKey(userID, threadID)

	payload := map[string]string{
		"last_wb_id":        cp.LastWBID,
		"last_plan_id":      cp.LastPlanID,
		"last_plan_version": cp.LastPlanVersion,
		"pending_prompt_id": cp.PendingPromptID,
	}
	_ = s.client.HSet(ctx, hash, payload).Err()
	if s.ttl > 0 {
		_ = s.client.Expire(ctx, hash, s.ttl).Err()
		_ = s.client.Expire(ctx, s.sideEffectsKey(userID, threadID), s.ttl).Err()
	}

	if len(cp.SideEffects) > 0 {
		members := make([]any, 0, len(cp.SideEffects))
		for _, key := range cp.SideEffects {
			members = append(members, key)
		}
		_ = s.client.SAdd(ctx, s.sideEffectsKey(userID, threadID), members...).Err()
	}
}
