package productivity

import (
	"context"
	"errors"
	"strings"
	"time"
)

// DecisionType enumerates productivity outcomes.
type DecisionType string

const (
	DecisionUnderrun  DecisionType = "underrun"
	DecisionOverrun   DecisionType = "overrun"
	DecisionAllowlist DecisionType = "allowlist"
	DecisionNudge     DecisionType = "nudge"
)

// Heartbeat is the normalized foreground signal from the client.
type Heartbeat struct {
	UserID      string
	BundleID    string
	WindowTitle string
	URL         string
	ActivityID  string
	Timestamp   time.Time
}

// Decision captures a single classification outcome.
type Decision struct {
	Kind         DecisionType `json:"kind"`
	UserID       string       `json:"user_id"`
	EventID      string       `json:"event_id,omitempty"`
	Observed     string       `json:"observed"`
	ExpectedApps []string     `json:"expected_apps,omitempty"`
	StartedAt    time.Time    `json:"started_at"`
	DecidedAt    time.Time    `json:"decided_at"`
}

// Classifier evaluates heartbeats against expected apps and records decisions.
type Classifier struct {
	heuristics  *HeuristicService
	gracePeriod time.Duration
	now         func() time.Time

	decisions map[string][]Decision
	state     map[string]*classifierState
}

type classifierState struct {
	eventID          string
	lastMatch        time.Time
	mismatchStart    time.Time
	decisionRecorded bool
	lastDecisionKind DecisionType
	lastObserved     string
	negativeCache    map[string]struct{}
}

// ClassifierOption configures optional classifier settings.
type ClassifierOption func(*Classifier)

// WithGracePeriod overrides the default 120s mismatch window.
func WithGracePeriod(d time.Duration) ClassifierOption {
	return func(c *Classifier) {
		if d > 0 {
			c.gracePeriod = d
		}
	}
}

// WithClock overrides the time source (useful for tests).
func WithClock(clock func() time.Time) ClassifierOption {
	return func(c *Classifier) {
		if clock != nil {
			c.now = clock
		}
	}
}

// NewClassifier constructs a classifier tied to the heuristic service.
func NewClassifier(heuristics *HeuristicService, opts ...ClassifierOption) (*Classifier, error) {
	if heuristics == nil {
		return nil, errors.New("heuristic service is required")
	}
	c := &Classifier{
		heuristics:  heuristics,
		gracePeriod: 2 * time.Minute,
		now:         time.Now,
		decisions:   make(map[string][]Decision),
		state:       make(map[string]*classifierState),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// ProcessHeartbeat ingests a single heartbeat and returns a decision
// when the foreground has been outside the expected set for the full grace period.
func (c *Classifier) ProcessHeartbeat(ctx context.Context, hb Heartbeat) (*Decision, error) {
	if c == nil {
		return nil, errors.New("classifier not initialized")
	}
	if hb.UserID == "" {
		return nil, errors.New("user_id is required")
	}

	ts := hb.Timestamp
	if ts.IsZero() {
		ts = c.now()
	}

	heuristic, err := c.heuristics.ActiveHeuristic(ctx, hb.UserID, ts)
	if err != nil {
		return nil, err
	}

	state := c.stateForUser(hb.UserID)
	if heuristic == nil || len(heuristic.ExpectedApps) == 0 {
		state.resetForEvent("")
		return nil, nil
	}
	state.resetForEvent(heuristic.EventID)

	foreground := hb.foregroundKey()
	if foreground == "" {
		return nil, nil
	}

	if ForegroundMatches(heuristic, foreground) {
		state.lastMatch = ts
		state.mismatchStart = time.Time{}
		state.decisionRecorded = false
		state.lastObserved = foreground
		return nil, nil
	}

	// Mismatch detected. Check if we should verify with Nano.
	// If this foreground is not in our negative cache (meaning we haven't asked Nano yet for this event), ask now.
	if state.negativeCache == nil {
		state.negativeCache = make(map[string]struct{})
	}
	if _, knownBad := state.negativeCache[foreground]; !knownBad {
		isMatch, err := c.heuristics.ClassifyMismatch(ctx, heuristic, foreground)
		if err != nil {
			// If the check fails, we don't update cache, so we'll retry next time.
			// We return the error so the caller knows something is wrong.
			return nil, err
		}
		if isMatch {
			// Nano said it's a match, and the heuristic has been updated.
			// Treat this as a match.
			state.lastMatch = ts
			state.mismatchStart = time.Time{}
			state.decisionRecorded = false
			state.lastObserved = foreground
			return nil, nil
		}
		// Nano said no. Cache it so we don't ask again for this event.
		state.negativeCache[foreground] = struct{}{}
	}

	if state.mismatchStart.IsZero() {
		state.mismatchStart = ts
		state.lastObserved = foreground
		return nil, nil
	}

	elapsed := ts.Sub(state.mismatchStart)
	if elapsed < c.gracePeriod {
		state.lastObserved = foreground
		return nil, nil
	}
	if state.decisionRecorded {
		return nil, nil
	}

	kind := c.classifyDecision(heuristic, state, foreground, ts)
	decision := Decision{
		Kind:         kind,
		UserID:       hb.UserID,
		EventID:      heuristic.EventID,
		Observed:     foreground,
		ExpectedApps: append([]string(nil), heuristic.ExpectedApps...),
		StartedAt:    state.mismatchStart,
		DecidedAt:    ts,
	}

	state.decisionRecorded = true
	state.lastDecisionKind = kind
	c.decisions[hb.UserID] = append(c.decisions[hb.UserID], decision)

	return &decision, nil
}

// Decisions returns recorded decisions for a user (copy).
func (c *Classifier) Decisions(userID string) []Decision {
	if c == nil {
		return nil
	}
	decisions := c.decisions[userID]
	out := make([]Decision, len(decisions))
	copy(out, decisions)
	return out
}

func (c *Classifier) stateForUser(userID string) *classifierState {
	if st, ok := c.state[userID]; ok {
		return st
	}
	st := &classifierState{}
	c.state[userID] = st
	return st
}

func (st *classifierState) resetForEvent(eventID string) {
	if st.eventID != eventID {
		st.eventID = eventID
		st.lastMatch = time.Time{}
		st.mismatchStart = time.Time{}
		st.decisionRecorded = false
		st.lastDecisionKind = ""
		st.negativeCache = make(map[string]struct{})
	}
}

func (hb Heartbeat) foregroundKey() string {
	var parts []string
	for _, v := range []string{hb.BundleID, hb.WindowTitle, hb.URL} {
		v = strings.TrimSpace(v)
		if v != "" {
			parts = append(parts, v)
		}
	}
	return strings.ToLower(strings.Join(parts, " | "))
}

func (c *Classifier) classifyDecision(heuristic *EventHeuristic, st *classifierState, observed string, ts time.Time) DecisionType {
	if heuristic != nil && !heuristic.EndTime.IsZero() && ts.After(heuristic.EndTime) {
		return DecisionOverrun
	}
	if st.lastMatch.IsZero() {
		return DecisionUnderrun
	}
	if st.lastDecisionKind == DecisionNudge && st.lastObserved == observed {
		return DecisionAllowlist
	}
	return DecisionNudge
}
