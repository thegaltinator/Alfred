package manager

import (
	"fmt"
	"log"
	"strings"

	"alfred-cloud/wb"
)

// NormalizedEvent is the Manager-ready form of a whiteboard entry.
type NormalizedEvent struct {
	WBID     string
	UserID   string
	ThreadID string
	Event    Event
}

// NormalizeWhiteboardEvent maps a raw whiteboard event into a Manager event shape.
func NormalizeWhiteboardEvent(evt wb.Event) (NormalizedEvent, error) {
	eventType := detectEventType(evt.Values)
	if eventType == "" {
		return NormalizedEvent{}, fmt.Errorf("whiteboard event %s missing type/kind", evt.ID)
	}

	threadID := pickThreadID(evt)
	payload := make(map[string]any)
	switch eventType {
	case "calendar.plan.proposed":
		deltaID, err := requiredString(evt.Values, "delta_id")
		if err != nil {
			return NormalizedEvent{}, err
		}
		summary, err := requiredString(evt.Values, "summary")
		if err != nil {
			return NormalizedEvent{}, err
		}
		impact, err := requiredString(evt.Values, "impact")
		if err != nil {
			return NormalizedEvent{}, err
		}
		payload["delta_id"] = deltaID
		payload["summary"] = summary
		payload["impact"] = impact
	case "calendar.plan.new_version":
		planID, err := requiredString(evt.Values, "plan_id")
		if err != nil {
			return NormalizedEvent{}, err
		}
		version, err := requiredString(evt.Values, "version")
		if err != nil {
			return NormalizedEvent{}, err
		}
		payload["plan_id"] = planID
		payload["version"] = version
	case "prod.underrun", "prod.overrun", "prod.nudge":
		blockID, err := requiredString(evt.Values, "block_id")
		if err != nil {
			return NormalizedEvent{}, err
		}
		activity, err := requiredString(evt.Values, "activity_label")
		if err != nil {
			return NormalizedEvent{}, err
		}
		payload["block_id"] = blockID
		payload["activity_label"] = activity
	case "email.reply_needed":
		messageID, err := requiredString(evt.Values, "message_id")
		if err != nil {
			return NormalizedEvent{}, err
		}
		sender, err := requiredString(evt.Values, "sender")
		if err != nil {
			return NormalizedEvent{}, err
		}
		summary, err := requiredString(evt.Values, "summary")
		if err != nil {
			return NormalizedEvent{}, err
		}
		draft, err := requiredString(evt.Values, "draft")
		if err != nil {
			return NormalizedEvent{}, err
		}
		payload["message_id"] = messageID
		payload["sender"] = sender
		payload["summary"] = summary
		payload["draft"] = draft
	case "manager.user_action":
		actionID, err := requiredString(evt.Values, "action_id")
		if err != nil {
			return NormalizedEvent{}, err
		}
		choice, err := requiredString(evt.Values, "choice")
		if err != nil {
			return NormalizedEvent{}, err
		}
		payload["action_id"] = actionID
		payload["choice"] = choice
		if threadID != "" {
			payload["thread_id"] = threadID
		}
		if meta, ok := evt.Values["metadata"].(map[string]any); ok && len(meta) > 0 {
			payload["metadata"] = meta
		}
	default:
		return NormalizedEvent{}, fmt.Errorf("unsupported whiteboard event type %s", eventType)
	}

	userID := pickUserID(evt)
	source, kind := splitEventType(eventType)

	normalized := NormalizedEvent{
		WBID:     evt.ID,
		UserID:   userID,
		ThreadID: threadID,
		Event: Event{
			Source:  source,
			Kind:    kind,
			Payload: payload,
		},
	}

	log.Printf("manager normalized: user=%s thread=%s wb=%s type=%s.%s payload=%v", userID, threadID, evt.ID, source, kind, payload)
	return normalized, nil
}

func detectEventType(values map[string]any) string {
	if len(values) == 0 {
		return ""
	}

	for _, key := range []string{"type", "kind", "event_type"} {
		if raw, ok := values[key]; ok {
			if s := stringVal(raw); s != "" {
				return strings.ToLower(s)
			}
		}
	}
	return ""
}

func requiredString(values map[string]any, key string) (string, error) {
	if len(values) == 0 {
		return "", fmt.Errorf("%s is required", key)
	}
	if s := stringVal(values[key]); s != "" {
		return s, nil
	}
	return "", fmt.Errorf("%s is required", key)
}

func pickUserID(evt wb.Event) string {
	if trimmed := strings.TrimSpace(evt.UserID); trimmed != "" {
		return trimmed
	}
	return stringVal(evt.Values["user_id"])
}

func pickThreadID(evt wb.Event) string {
	if trimmed := strings.TrimSpace(evt.ThreadID); trimmed != "" {
		return trimmed
	}
	return stringVal(evt.Values["thread_id"])
}

func splitEventType(eventType string) (string, string) {
	eventType = strings.TrimSpace(strings.ToLower(eventType))
	parts := strings.SplitN(eventType, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return eventType, ""
}

func stringVal(v any) string {
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val)
	case fmt.Stringer:
		return strings.TrimSpace(val.String())
	case []byte:
		return strings.TrimSpace(string(val))
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return strings.TrimSpace(fmt.Sprint(val))
	default:
		return ""
	}
}
