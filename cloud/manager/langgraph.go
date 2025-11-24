package manager

import (
	"context"
	"errors"
)

// Orchestrator is a thin manager wrapper that consumes subagent outputs and
// delegates the decision to the LLM client.
type Orchestrator struct {
	decider decisioner
}

// OrchestratorInput represents a normalized whiteboard event for the manager.
type OrchestratorInput struct {
	UserID   string
	ThreadID string
	Source   string
	Kind     string
	Payload  map[string]any
}

// OrchestratorOutcome is the manager's decision annotated with user context.
type OrchestratorOutcome struct {
	UserID   string
	ThreadID string
	Decision Decision
}

// NewOrchestrator constructs an orchestrator bound to the configured LLM client.
func NewOrchestrator() (*Orchestrator, error) {
	client := getManagerService()
	if client == nil {
		return nil, errors.New("manager LLM not configured")
	}
	return &Orchestrator{decider: client}, nil
}

// Handle takes a subagent/whiteboard event and returns the LLM's decision.
func (o *Orchestrator) Handle(ctx context.Context, in OrchestratorInput) (OrchestratorOutcome, error) {
	if o == nil || o.decider == nil {
		return OrchestratorOutcome{}, errors.New("manager orchestrator not initialized")
	}

	decision, err := o.decider.Decide(ctx, Event{
		Source:  in.Source,
		Kind:    in.Kind,
		Payload: in.Payload,
	})
	if err != nil {
		return OrchestratorOutcome{}, err
	}

	return OrchestratorOutcome{
		UserID:   in.UserID,
		ThreadID: in.ThreadID,
		Decision: decision,
	}, nil
}
