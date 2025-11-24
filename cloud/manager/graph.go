package manager

import (
	"context"
	"fmt"
	"log"
	"strings"
)

// GraphConfig configures the Manager LangGraph runtime.
type GraphConfig struct {
	PlannerURL     string
	ProdControlURL string
}

// ManagerGraph is a placeholder LangGraph runtime; nodes are added in later tasks.
type ManagerGraph struct {
	config GraphConfig
}

// NewManagerGraph constructs a ManagerGraph with the provided configuration.
func NewManagerGraph(cfg GraphConfig) (*ManagerGraph, error) {
	if strings.TrimSpace(cfg.PlannerURL) == "" {
		return nil, fmt.Errorf("planner URL is required")
	}
	return &ManagerGraph{config: cfg}, nil
}

// Run feeds a normalized whiteboard event through the (future) LangGraph.
func (g *ManagerGraph) Run(ctx context.Context, evt NormalizedEvent) error {
	if g == nil {
		return fmt.Errorf("manager graph not initialized")
	}
	// Placeholder: later tasks will route to real nodes.
	log.Printf("manager graph received wb=%s user=%s thread=%s type=%s.%s", evt.WBID, evt.UserID, evt.ThreadID, evt.Event.Source, evt.Event.Kind)
	return nil
}
