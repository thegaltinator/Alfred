package manager

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"alfred-cloud/wb"
	"github.com/redis/go-redis/v9"
)

// Runtime is the long-lived Manager worker that tails the whiteboard and feeds LangGraph.
type Runtime struct {
	cfg   RuntimeConfig
	redis *redis.Client
	bus   *wb.Bus
	graph *ManagerGraph

	mu      sync.RWMutex
	lastIDs map[string]map[string]string // user -> thread -> wb id
	lastErr error
	started time.Time
}

// NewRuntimeFromEnv bootstraps the runtime using environment variables.
func NewRuntimeFromEnv(ctx context.Context) (*Runtime, error) {
	cfg := RuntimeConfigFromEnv()

	client, err := connectRedis(ctx, cfg.RedisURL)
	if err != nil {
		return nil, err
	}

	bus := wb.NewBus(client)
	graph, err := NewManagerGraph(GraphConfig{
		PlannerURL:     cfg.PlannerURL,
		ProdControlURL: cfg.ProdControlURL,
		Bus:            bus,
	})
	if err != nil {
		return nil, err
	}

	return &Runtime{
		cfg:     cfg,
		redis:   client,
		bus:     bus,
		graph:   graph,
		lastIDs: map[string]map[string]string{
			// init lazily per user
		},
		started: time.Now().UTC(),
	}, nil
}

// Run starts tailing the configured whiteboard streams until the context is canceled.
func (rt *Runtime) Run(ctx context.Context) error {
	if rt == nil || rt.redis == nil || rt.bus == nil || rt.graph == nil {
		return errors.New("manager runtime not initialized")
	}

	rt.recordError(nil)
	log.Printf("manager: LangGraph runtime ready (planner=%s prod_control=%s)", rt.cfg.PlannerURL, rt.cfg.ProdControlURL)
	log.Printf("manager: connected to Redis at %s", rt.cfg.RedisURL)

	for _, userID := range rt.cfg.Users {
		user := strings.TrimSpace(userID)
		if user == "" {
			continue
		}
		go rt.consumeUser(ctx, user)
	}

	<-ctx.Done()
	return ctx.Err()
}

// Handler returns a minimal HTTP handler exposing /healthz.
func (rt *Runtime) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", rt.healthz)
	return mux
}

// ListenAddr returns the configured HTTP listen address for the runtime.
func (rt *Runtime) ListenAddr() string {
	if rt == nil {
		return defaultManagerListenAddr
	}
	if trimmed := strings.TrimSpace(rt.cfg.ListenAddr); trimmed != "" {
		return trimmed
	}
	return defaultManagerListenAddr
}

func (rt *Runtime) consumeUser(ctx context.Context, userID string) {
	startID := rt.cfg.StartAfterID
	if startID == "" {
		startID = "$"
	}

	log.Printf("manager: watching wb stream %s from %q", wb.StreamKey(userID), startID)

	lastID := rt.cfg.StartAfterID
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		events, nextID, err := rt.bus.Tail(ctx, userID, lastID)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			rt.recordError(err)
			log.Printf("manager: tail error for %s: %v", userID, err)
			time.Sleep(350 * time.Millisecond)
			continue
		}

		rt.recordError(nil)
		if len(events) == 0 {
			continue
		}

		lastID = nextID
		for _, evt := range events {
			normalized, normErr := NormalizeWhiteboardEvent(evt)
			if normErr != nil {
				log.Printf("manager: skip wb=%s for user=%s: %v", evt.ID, userID, normErr)
				continue
			}

			if normalized.ThreadID == "" {
				log.Printf("manager: drop wb=%s for user=%s because thread_id missing (E-03 requirement)", evt.ID, userID)
				continue
			}

			rt.recordSeen(userID, normalized.ThreadID, evt.ID)
			if err := rt.graph.Run(ctx, normalized); err != nil {
				rt.recordError(err)
				log.Printf("manager: graph run failed for wb=%s user=%s: %v", evt.ID, userID, err)
			}
		}
	}
}

func (rt *Runtime) healthz(w http.ResponseWriter, r *http.Request) {
	status := rt.health()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(status)
}

func (rt *Runtime) health() map[string]any {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	payload := map[string]any{
		"ok":              rt.lastErr == nil,
		"service":         "manager-runtime",
		"users":           append([]string(nil), rt.cfg.Users...),
		"planner_url":     rt.cfg.PlannerURL,
		"prod_control":    rt.cfg.ProdControlURL,
		"redis":           rt.cfg.RedisURL,
		"last_wb_ids":     copyNested(rt.lastIDs),
		"started_at":      rt.started.Format(time.RFC3339Nano),
		"checked_at":      time.Now().UTC().Format(time.RFC3339Nano),
		"start_after_id":  rt.cfg.StartAfterID,
		"langgraph_ready": rt.graph != nil,
	}

	if rt.lastErr != nil {
		payload["last_error"] = rt.lastErr.Error()
	}

	return payload
}

func (rt *Runtime) recordSeen(userID, threadID, wbID string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if _, ok := rt.lastIDs[userID]; !ok {
		rt.lastIDs[userID] = make(map[string]string)
	}
	rt.lastIDs[userID][threadID] = wbID
}

func (rt *Runtime) recordError(err error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.lastErr = err
}

func copyNested(src map[string]map[string]string) map[string]map[string]string {
	if len(src) == 0 {
		return map[string]map[string]string{}
	}
	out := make(map[string]map[string]string, len(src))
	for user, threads := range src {
		out[user] = make(map[string]string, len(threads))
		for thread, wbID := range threads {
			out[user][thread] = wbID
		}
	}
	return out
}

func connectRedis(ctx context.Context, rawURL string) (*redis.Client, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		trimmed = defaultManagerRedisURL
	}
	if !strings.HasPrefix(trimmed, "redis://") {
		trimmed = "redis://" + trimmed
	}

	opts, err := redis.ParseURL(trimmed)
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return client, nil
}
