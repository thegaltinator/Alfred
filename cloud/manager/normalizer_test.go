package manager

import (
	"testing"

	"alfred-cloud/wb"
	"github.com/stretchr/testify/require"
)

func TestNormalizeWhiteboardEventMappings(t *testing.T) {
	tests := []struct {
		name        string
		input       wb.Event
		wantSource  string
		wantKind    string
		wantThread  string
		wantUser    string
		wantPayload map[string]any
	}{
		{
			name: "calendar proposed",
			input: wb.Event{
				ID:       "1-0",
				UserID:   "user-a",
				ThreadID: "thread-a",
				Values: map[string]any{
					"type":     "calendar.plan.proposed",
					"delta_id": "delta-123",
					"summary":  "Move standup",
					"impact":   "conflicts with interview",
				},
			},
			wantSource: "calendar",
			wantKind:   "plan.proposed",
			wantThread: "thread-a",
			wantUser:   "user-a",
			wantPayload: map[string]any{
				"delta_id": "delta-123",
				"summary":  "Move standup",
				"impact":   "conflicts with interview",
			},
		},
		{
			name: "calendar new version",
			input: wb.Event{
				ID:       "2-0",
				UserID:   "user-b",
				ThreadID: "thread-b",
				Values: map[string]any{
					"type":    "calendar.plan.new_version",
					"plan_id": "plan-9",
					"version": "4",
				},
			},
			wantSource: "calendar",
			wantKind:   "plan.new_version",
			wantThread: "thread-b",
			wantUser:   "user-b",
			wantPayload: map[string]any{
				"plan_id": "plan-9",
				"version": "4",
			},
		},
		{
			name: "prod underrun",
			input: wb.Event{
				ID:       "3-0",
				UserID:   "user-c",
				ThreadID: "thread-c",
				Values: map[string]any{
					"type":           "prod.underrun",
					"block_id":       "block-1",
					"activity_label": "coding",
				},
			},
			wantSource: "prod",
			wantKind:   "underrun",
			wantThread: "thread-c",
			wantUser:   "user-c",
			wantPayload: map[string]any{
				"block_id":       "block-1",
				"activity_label": "coding",
			},
		},
		{
			name: "prod overrun",
			input: wb.Event{
				ID:       "4-0",
				UserID:   "user-d",
				ThreadID: "thread-d",
				Values: map[string]any{
					"type":           "prod.overrun",
					"block_id":       "block-2",
					"activity_label": "email",
				},
			},
			wantSource: "prod",
			wantKind:   "overrun",
			wantThread: "thread-d",
			wantUser:   "user-d",
			wantPayload: map[string]any{
				"block_id":       "block-2",
				"activity_label": "email",
			},
		},
		{
			name: "prod nudge",
			input: wb.Event{
				ID:       "5-0",
				UserID:   "user-e",
				ThreadID: "thread-e",
				Values: map[string]any{
					"type":           "prod.nudge",
					"block_id":       "block-3",
					"activity_label": "planning",
				},
			},
			wantSource: "prod",
			wantKind:   "nudge",
			wantThread: "thread-e",
			wantUser:   "user-e",
			wantPayload: map[string]any{
				"block_id":       "block-3",
				"activity_label": "planning",
			},
		},
		{
			name: "email reply needed",
			input: wb.Event{
				ID:       "6-0",
				UserID:   "user-f",
				ThreadID: "thread-f",
				Values: map[string]any{
					"type":       "email.reply_needed",
					"message_id": "msg-1",
					"sender":     "sender@example.com",
					"summary":    "Need confirmation for 3pm",
					"draft":      "Yes, 3pm works.",
				},
			},
			wantSource: "email",
			wantKind:   "reply_needed",
			wantThread: "thread-f",
			wantUser:   "user-f",
			wantPayload: map[string]any{
				"message_id": "msg-1",
				"sender":     "sender@example.com",
				"summary":    "Need confirmation for 3pm",
				"draft":      "Yes, 3pm works.",
			},
		},
		{
			name: "manager user action with kind fallback",
			input: wb.Event{
				ID: "7-0",
				Values: map[string]any{
					"kind":      "manager.user_action",
					"user_id":   "user-g",
					"thread_id": "thread-g",
					"action_id": "action-1",
					"choice":    "update_plan",
					"metadata": map[string]any{
						"prompt_id": "prompt-1",
					},
				},
			},
			wantSource: "manager",
			wantKind:   "user_action",
			wantThread: "thread-g",
			wantUser:   "user-g",
			wantPayload: map[string]any{
				"action_id": "action-1",
				"choice":    "update_plan",
				"thread_id": "thread-g",
				"metadata": map[string]any{
					"prompt_id": "prompt-1",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized, err := NormalizeWhiteboardEvent(tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.wantSource, normalized.Event.Source)
			require.Equal(t, tt.wantKind, normalized.Event.Kind)
			require.Equal(t, tt.wantThread, normalized.ThreadID)
			require.Equal(t, tt.wantUser, normalized.UserID)
			require.Equal(t, tt.wantPayload, normalized.Event.Payload)
			require.Equal(t, tt.input.ID, normalized.WBID)
		})
	}
}

func TestNormalizeWhiteboardEventRejectsUnknown(t *testing.T) {
	_, err := NormalizeWhiteboardEvent(wb.Event{
		ID:     "999-0",
		UserID: "user-x",
		Values: map[string]any{
			"type": "unknown.event",
		},
	})
	require.Error(t, err)
}
