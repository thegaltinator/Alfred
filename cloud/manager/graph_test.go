package manager

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type appendCall struct {
	userID   string
	threadID string
	values   map[string]any
}

type stubBus struct {
	appends []appendCall
}

func (s *stubBus) AppendWithThread(ctx context.Context, userID, threadID string, values map[string]any) (string, error) {
	s.appends = append(s.appends, appendCall{
		userID:   userID,
		threadID: threadID,
		values:   values,
	})
	return "wb-append-id", nil
}

func TestProdOverrunEmitsSinglePrompt(t *testing.T) {
	bus := &stubBus{}
	graph, err := NewManagerGraph(GraphConfig{
		PlannerURL:     "http://example.com/planner/run",
		ProdControlURL: "http://example.com/prod/recompute",
		Bus:            bus,
	})
	require.NoError(t, err)

	evt := NormalizedEvent{
		WBID:     "1-0",
		UserID:   "user-1",
		ThreadID: "thread-1",
		Event: Event{
			Source: "prod",
			Kind:   "overrun",
			Payload: map[string]any{
				"block_id":       "block-1",
				"activity_label": "coding",
			},
		},
	}

	err = graph.Run(context.Background(), evt)
	require.NoError(t, err)

	require.Len(t, bus.appends, 1, "expected exactly one prompt append")
	call := bus.appends[0]
	require.Equal(t, "user-1", call.userID)
	require.Equal(t, "thread-1", call.threadID)
	require.Equal(t, "manager.prompt", call.values["type"])
	require.Equal(t, "prod", call.values["source"])
	require.Equal(t, "overrun", call.values["kind"])
	require.Equal(t, "1-0", call.values["wb_parent_id"])
	require.NotEmpty(t, call.values["content"])
	require.Contains(t, call.values["content"], "coding")
}
