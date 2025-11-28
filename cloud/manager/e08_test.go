package manager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestE08Checkpointing validates checkpoint contents and replay safety.
func TestE08Checkpointing(t *testing.T) {
	bus := &capturingBus{}
	store := NewInMemoryCheckpointStore()

	plannerCalls := 0
	prodCalls := 0

	plannerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plannerCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer plannerSrv.Close()

	prodSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prodCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer prodSrv.Close()

	graph, err := NewManagerGraph(GraphConfig{
		PlannerURL:     plannerSrv.URL,
		ProdControlURL: prodSrv.URL,
		Bus:            bus,
	})
	require.NoError(t, err)

	process := func(evt NormalizedEvent) {
		cp := store.Get(evt.UserID, evt.ThreadID)
		if shouldSkipID(evt.WBID, cp.LastWBID) {
			return
		}
		res, err := graph.Run(context.Background(), evt)
		require.NoError(t, err)
		cp.LastWBID = evt.WBID
		if res.PromptID != "" {
			cp.PendingPromptID = res.PromptID
		}
		if res.LastPlanID != "" {
			cp.LastPlanID = res.LastPlanID
		}
		if res.LastPlanVersion != "" {
			cp.LastPlanVersion = res.LastPlanVersion
		}
		if len(res.SideEffects) > 0 {
			cp.SideEffects = dedupeStrings(append(cp.SideEffects, res.SideEffects...))
		}
		store.Save(evt.UserID, evt.ThreadID, cp)
	}

	// 1) prod.overrun
	prodEvt := NormalizedEvent{
		WBID:     "1-0",
		UserID:   "user-e08",
		ThreadID: "thread-e08",
		Event: Event{
			Source: "prod",
			Kind:   "overrun",
			Payload: map[string]any{
				"block_id":       "block-1",
				"activity_label": "coding",
			},
		},
	}
	process(prodEvt)

	// 2) calendar.plan.proposed
	calendarEvt := NormalizedEvent{
		WBID:     "2-0",
		UserID:   "user-e08",
		ThreadID: "thread-e08",
		Event: Event{
			Source: "calendar",
			Kind:   "plan.proposed",
			Payload: map[string]any{
				"delta_id": "delta-123",
				"summary":  "Move standup",
				"impact":   "conflicts with interview",
			},
		},
	}
	process(calendarEvt)

	// Check checkpoint contents
	cp := store.Get("user-e08", "thread-e08")
	require.Equal(t, "2-0", cp.LastWBID)
	require.Equal(t, "delta-123", cp.LastPlanID)
	require.NotEmpty(t, cp.PendingPromptID, "prompt id should be tracked")
	require.Len(t, cp.SideEffects, 2, "planner_call and prod_recalc_signal should be recorded")

	// Capture counts to verify no replay side effects
	prevPlanner := plannerCalls
	prevProd := prodCalls
	prevPrompts := len(bus.appends)

	// Replay same events (simulate restart + replay)
	process(prodEvt)
	process(calendarEvt)

	// No additional calls or prompts
	require.Equal(t, prevPlanner, plannerCalls, "planner should not be re-called on replay")
	require.Equal(t, prevProd, prodCalls, "prod recompute should not be re-called on replay")
	require.Equal(t, prevPrompts, len(bus.appends), "no duplicate prompts on replay")
}
