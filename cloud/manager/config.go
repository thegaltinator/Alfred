package manager

import (
	"os"
	"strings"
)

const (
	defaultManagerRedisURL        = "redis://localhost:6379"
	defaultManagerListenAddr      = ":8090"
	defaultManagerPlannerURL      = "http://localhost:8080/planner/run"
	defaultManagerProdControlURL  = "http://localhost:8080/prod/control/recompute"
	defaultManagerWhiteboardStart = "" // empty -> tail only new entries
)

// RuntimeConfig holds the minimal settings needed to bootstrap the Manager runtime.
type RuntimeConfig struct {
	Users          []string
	RedisURL       string
	PlannerURL     string
	ProdControlURL string
	ListenAddr     string
	StartAfterID   string
}

// RuntimeConfigFromEnv builds a RuntimeConfig using environment variables with safe defaults.
func RuntimeConfigFromEnv() RuntimeConfig {
	return RuntimeConfig{
		Users:          parseUserList(os.Getenv("MANAGER_USERS"), "test-user"),
		RedisURL:       pickEnv("REDIS_URL", defaultManagerRedisURL),
		PlannerURL:     pickEnv("MANAGER_PLANNER_URL", defaultManagerPlannerURL),
		ProdControlURL: pickEnv("MANAGER_PROD_CONTROL_URL", defaultManagerProdControlURL),
		ListenAddr:     pickEnv("MANAGER_LISTEN_ADDR", defaultManagerListenAddr),
		StartAfterID:   pickEnv("MANAGER_WB_AFTER", defaultManagerWhiteboardStart),
	}
}

func parseUserList(raw string, fallback string) []string {
	if strings.TrimSpace(raw) == "" {
		raw = fallback
	}
	parts := strings.Split(raw, ",")
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		user := strings.TrimSpace(part)
		if user == "" {
			continue
		}
		if _, exists := seen[user]; exists {
			continue
		}
		seen[user] = struct{}{}
		out = append(out, user)
	}
	return out
}

func pickEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
