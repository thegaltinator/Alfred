package manager

import (
	"context"
	"strconv"
	"strings"
	"sync"
)

// Checkpoint captures per-(user, thread) progress and idempotency state.
type Checkpoint struct {
	LastWBID        string
	LastPlanID      string
	LastPlanVersion string
	PendingPromptID string
	SideEffects     []string
}

// CheckpointStore persists checkpoints.
type CheckpointStore interface {
	Get(userID, threadID string) Checkpoint
	Save(userID, threadID string, cp Checkpoint)
}

type checkpointKey struct{}

// InMemoryCheckpointStore is a simple thread-safe store for checkpoints.
type InMemoryCheckpointStore struct {
	mu    sync.RWMutex
	store map[string]map[string]Checkpoint // user -> thread -> checkpoint
}

// NewInMemoryCheckpointStore creates an empty checkpoint store.
func NewInMemoryCheckpointStore() *InMemoryCheckpointStore {
	return &InMemoryCheckpointStore{
		store: make(map[string]map[string]Checkpoint),
	}
}

// Get returns the checkpoint for a user/thread (zero value if missing).
func (s *InMemoryCheckpointStore) Get(userID, threadID string) Checkpoint {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if threads, ok := s.store[userID]; ok {
		if cp, ok := threads[threadID]; ok {
			return cp
		}
	}
	return Checkpoint{}
}

// Save upserts the checkpoint for a user/thread.
func (s *InMemoryCheckpointStore) Save(userID, threadID string, cp Checkpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.store[userID]; !ok {
		s.store[userID] = make(map[string]Checkpoint)
	}
	s.store[userID][threadID] = cp
}

// WithCheckpoint attaches a checkpoint to the context for idempotency guards.
func WithCheckpoint(ctx context.Context, cp Checkpoint) context.Context {
	return context.WithValue(ctx, checkpointKey{}, cp)
}

// checkpointFromContext fetches a checkpoint from the context if present.
func checkpointFromContext(ctx context.Context) (Checkpoint, bool) {
	if ctx == nil {
		return Checkpoint{}, false
	}
	cp, ok := ctx.Value(checkpointKey{}).(Checkpoint)
	return cp, ok
}

// shouldSkipID returns true if id <= lastProcessed (Redis stream ordering).
func shouldSkipID(id, lastProcessed string) bool {
	if strings.TrimSpace(lastProcessed) == "" {
		return false
	}
	curTs, curSeq := splitStreamID(id)
	lastTs, lastSeq := splitStreamID(lastProcessed)
	if curTs < lastTs {
		return true
	}
	if curTs == lastTs && curSeq <= lastSeq {
		return true
	}
	return false
}

func splitStreamID(id string) (int64, int64) {
	parts := strings.Split(id, "-")
	if len(parts) != 2 {
		return 0, 0
	}
	ts, _ := strconv.ParseInt(parts[0], 10, 64)
	seq, _ := strconv.ParseInt(parts[1], 10, 64)
	return ts, seq
}

// dedupeStrings preserves order and drops duplicates.
func dedupeStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// sideEffectRecorded reports whether the idempotency key already exists in the checkpoint log.
func sideEffectRecorded(cp Checkpoint, key string) bool {
	for _, existing := range cp.SideEffects {
		if existing == key {
			return true
		}
	}
	return false
}
