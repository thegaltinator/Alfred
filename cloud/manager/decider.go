package manager

import (
	"context"
	"errors"
	"strings"
	"sync"
)

// Action describes the manager's next step.
type Action string

const (
	ActionAskUser Action = "ask_user"
	ActionRoute   Action = "route"
	ActionNoop    Action = "noop"
)

// Event is a normalized subagent output.
type Event struct {
	Source  string         `json:"source"`
	Kind    string         `json:"kind"`
	Payload map[string]any `json:"payload,omitempty"`
}

// Decision is the manager's response to a subagent output.
type Decision struct {
	Action  Action `json:"action"`
	Prompt  string `json:"prompt,omitempty"`
	RouteTo string `json:"route_to,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

var (
	llmOnce   sync.Once
	llmClient decisioner
	llmErr    error
)

type decisioner interface {
	Decide(context.Context, Event) (Decision, error)
}

type decisionerFunc func(context.Context, Event) (Decision, error)

func (f decisionerFunc) Decide(ctx context.Context, evt Event) (Decision, error) {
	return f(ctx, evt)
}

// Decide maps a subagent output to the manager's next action using the LLM.
func Decide(evt Event) (Decision, error) {
	return DecideWithContext(context.Background(), evt)
}

// DecideWithContext maps a subagent output to the manager's next action using the LLM.
func DecideWithContext(ctx context.Context, evt Event) (Decision, error) {
	client := getManagerService()
	if client != nil {
		return client.Decide(ctx, evt)
	}

	return Decision{}, errors.New("manager LLM not configured")
}

func getManagerService() decisioner {
	if llmClient != nil {
		return llmClient
	}
	llmOnce.Do(func() {
		svc, err := NewServiceFromEnv()
		if err != nil {
			llmErr = err
			llmClient = nil
			return
		}
		llmClient = svc
		llmErr = nil
		if llmErr != nil {
			llmClient = nil
		}
	})
	return llmClient
}

// resetLLMClientForTest resets the cached LLM client (test-only).
func resetLLMClientForTest() {
	llmOnce = sync.Once{}
	llmClient = nil
	llmErr = nil
}

// ResetLLMClientForTest resets the cached LLM client (exported for tests in other packages).
func ResetLLMClientForTest() {
	resetLLMClientForTest()
}

// SetLLMClientForTestFunc injects a test double for the LLM client.
func SetLLMClientForTestFunc(fn func(context.Context, Event) (Decision, error)) {
	resetLLMClientForTest()
	if fn == nil {
		return
	}
	llmClient = decisionerFunc(fn)
}

func normalize(source, kind string) (string, string) {
	src := strings.ToLower(strings.TrimSpace(source))
	k := strings.ToLower(strings.TrimSpace(kind))

	if src == "" && strings.Contains(k, ".") {
		parts := strings.SplitN(k, ".", 2)
		src = parts[0]
		k = parts[1]
	}

	return src, k
}

// setLLMClientForTest overrides the LLM client (test-only).
func setLLMClientForTest(client decisioner) {
	llmClient = client
}
