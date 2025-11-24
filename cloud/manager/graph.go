package manager

import (
	"context"
	"fmt"
	"log"
	"strings"
)

type whiteboardAppender interface {
	AppendWithThread(ctx context.Context, userID, threadID string, values map[string]any) (string, error)
}

// GraphConfig configures the Manager LangGraph runtime.
type GraphConfig struct {
	PlannerURL     string
	ProdControlURL string
	Bus            whiteboardAppender
}

// ManagerGraph is the LangGraph runtime placeholder; nodes are added in later tasks.
type ManagerGraph struct {
	config GraphConfig
	bus    whiteboardAppender
}

// NewManagerGraph constructs a ManagerGraph with the provided configuration.
func NewManagerGraph(cfg GraphConfig) (*ManagerGraph, error) {
	if strings.TrimSpace(cfg.PlannerURL) == "" {
		return nil, fmt.Errorf("planner URL is required")
	}
	if cfg.Bus == nil {
		return nil, fmt.Errorf("whiteboard bus is required")
	}
	return &ManagerGraph{
		config: cfg,
		bus:    cfg.Bus,
	}, nil
}

// Run feeds a normalized whiteboard event through the LangGraph.
func (g *ManagerGraph) Run(ctx context.Context, evt NormalizedEvent) error {
	if g == nil || g.bus == nil {
		return fmt.Errorf("manager graph not initialized")
	}
	return g.ingestWB(ctx, evt)
}

func (g *ManagerGraph) ingestWB(ctx context.Context, evt NormalizedEvent) error {
	log.Printf("manager graph node=ingest_wb wb=%s user=%s thread=%s type=%s.%s", evt.WBID, evt.UserID, evt.ThreadID, evt.Event.Source, evt.Event.Kind)
	return g.router(ctx, evt)
}

func (g *ManagerGraph) router(ctx context.Context, evt NormalizedEvent) error {
	switch strings.ToLower(strings.TrimSpace(evt.Event.Source)) {
	case "calendar":
		log.Printf("manager graph node=router next=calendar_branch wb=%s", evt.WBID)
		return g.calendarBranch(ctx, evt)
	case "prod":
		log.Printf("manager graph node=router next=prod_branch wb=%s", evt.WBID)
		return g.prodBranch(ctx, evt)
	case "email":
		log.Printf("manager graph node=router next=email_branch wb=%s", evt.WBID)
		return g.emailBranch(ctx, evt)
	case "manager":
		if strings.ToLower(strings.TrimSpace(evt.Event.Kind)) == "user_action" {
			log.Printf("manager graph node=router next=user_action_branch wb=%s", evt.WBID)
			return g.userActionBranch(ctx, evt)
		}
	}
	log.Printf("manager graph node=router drop wb=%s type=%s.%s", evt.WBID, evt.Event.Source, evt.Event.Kind)
	return nil
}

func (g *ManagerGraph) calendarBranch(ctx context.Context, evt NormalizedEvent) error {
	log.Printf("manager graph node=calendar_branch wb=%s kind=%s", evt.WBID, evt.Event.Kind)
	return nil
}

func (g *ManagerGraph) prodBranch(ctx context.Context, evt NormalizedEvent) error {
	log.Printf("manager graph node=prod_branch wb=%s kind=%s", evt.WBID, evt.Event.Kind)
	prompt := g.maybePromptUser(evt)
	if prompt == "" {
		log.Printf("manager graph node=prod_branch wb=%s decision=no_prompt", evt.WBID)
		return nil
	}
	return g.emitPrompt(ctx, evt, prompt)
}

func (g *ManagerGraph) emailBranch(ctx context.Context, evt NormalizedEvent) error {
	log.Printf("manager graph node=email_branch wb=%s kind=%s", evt.WBID, evt.Event.Kind)
	return nil
}

func (g *ManagerGraph) userActionBranch(ctx context.Context, evt NormalizedEvent) error {
	log.Printf("manager graph node=user_action_branch wb=%s kind=%s", evt.WBID, evt.Event.Kind)
	return nil
}

func (g *ManagerGraph) plannerCall(ctx context.Context, evt NormalizedEvent) error {
	log.Printf("manager graph node=planner_call wb=%s", evt.WBID)
	return nil
}

func (g *ManagerGraph) prodRecalcSignal(ctx context.Context, evt NormalizedEvent) error {
	log.Printf("manager graph node=prod_recalc_signal wb=%s", evt.WBID)
	return nil
}

func (g *ManagerGraph) maybePromptUser(evt NormalizedEvent) string {
	source := strings.ToLower(strings.TrimSpace(evt.Event.Source))
	if source != "prod" {
		return ""
	}

	kind := strings.ToLower(strings.TrimSpace(evt.Event.Kind))
	activity := strings.TrimSpace(stringFromPayload(evt.Event.Payload, "activity_label"))
	if activity == "" {
		activity = "this block"
	}

	switch kind {
	case "overrun":
		return fmt.Sprintf("You are still in %s. Do you want to refocus?", activity)
	case "underrun":
		return fmt.Sprintf("You seem behind on %s. Want to adjust?", activity)
	case "nudge":
		return fmt.Sprintf("Time to get back to %s?", activity)
	default:
		return ""
	}
}

func (g *ManagerGraph) emitPrompt(ctx context.Context, evt NormalizedEvent, prompt string) error {
	values := map[string]any{
		"type":         "manager.prompt",
		"source":       evt.Event.Source,
		"kind":         evt.Event.Kind,
		"content":      prompt,
		"prompt":       prompt,
		"wb_parent_id": evt.WBID,
	}

	id, err := g.bus.AppendWithThread(ctx, evt.UserID, evt.ThreadID, values)
	if err != nil {
		return fmt.Errorf("emit_prompt failed for wb=%s: %w", evt.WBID, err)
	}

	log.Printf("manager graph node=emit_prompt wb=%s prompt_id=%s user=%s thread=%s", evt.WBID, id, evt.UserID, evt.ThreadID)
	return nil
}

func stringFromPayload(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	switch val := payload[key].(type) {
	case string:
		return val
	case fmt.Stringer:
		return val.String()
	case []byte:
		return string(val)
	default:
		return ""
	}
}
