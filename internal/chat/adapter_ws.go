package chat

import (
	"strings"

	"github.com/longyisang/emoagent/internal/turn"
)

func wsMessageToInbound(msg WSMessage, sessionID, personaName string) (turn.InboundEnvelope, error) {
	switch msg.Type {
	case "approval_action":
		action := strings.ToLower(strings.TrimSpace(msg.Action))
		env := turn.InboundEnvelope{
			Kind:       turn.InboundApprovalAction,
			Source:     turn.SourceWebUI,
			SessionID:  sessionID,
			PersonaKey: personaName,
			RequestID:  msg.RequestID,
			Approval: &turn.InboundApproval{
				RequestID: strings.TrimSpace(msg.RequestID),
				Action:    action,
				OptionID:  strings.TrimSpace(msg.OptionID),
			},
		}
		env.IdempotencyKey = turn.BuildIdempotencyKey(env)
		return env, nil
	default:
		parts, err := normalizeUserParts(msg.Content, msg.Parts)
		if err != nil {
			return turn.InboundEnvelope{}, err
		}
		content := strings.TrimSpace(msg.Content)
		if len(msg.Parts) > 0 {
			content = renderUserParts(parts, "history")
		}
		env := turn.InboundEnvelope{
			Kind:       turn.InboundUserMessage,
			Source:     turn.SourceWebUI,
			SessionID:  sessionID,
			PersonaKey: personaName,
			RequestID:  strings.TrimSpace(msg.RequestID),
			Content:    content,
			UserMessage: &turn.UserMessageInput{
				Content: content,
				Parts:   parts,
			},
		}
		env.IdempotencyKey = turn.BuildIdempotencyKey(env)
		return env, nil
	}
}

func outboundEventToWSMessage(event turn.OutboundEvent) WSMessage {
	msg := WSMessage{
		Type:    event.Type,
		Content: event.Content,
		TurnID:  event.TurnID,
		Payload: clonePayload(event.Payload),
	}
	if groupID, ok := event.Payload["group_id"].(string); ok {
		msg.GroupID = groupID
	}
	if segmentID, ok := event.Payload["segment_id"].(string); ok {
		msg.SegmentID = segmentID
	}
	msg.SegmentIndex = payloadInt(event.Payload, "segment_index")
	msg.SegmentTotal = payloadInt(event.Payload, "segment_total")
	if status, ok := event.Payload["status"].(string); ok {
		msg.Status = status
	}
	if errorKind, ok := event.Payload["error_kind"].(string); ok {
		msg.ErrorKind = errorKind
	}
	if event.Tool != nil {
		msg.Tool = &ToolActivity{
			ID:                   event.Tool.ID,
			Name:                 event.Tool.Name,
			Status:               event.Tool.Status,
			DurationMS:           event.Tool.DurationMS,
			Preview:              event.Tool.Preview,
			Size:                 event.Tool.Size,
			Hash:                 event.Tool.Hash,
			IsTruncated:          event.Tool.IsTruncated,
			Origin:               event.Tool.Origin,
			RuntimeKind:          event.Tool.RuntimeKind,
			ProducerID:           event.Tool.ProducerID,
			Executor:             event.Tool.Executor,
			Integrity:            event.Tool.Integrity,
			InstructionAuthority: event.Tool.InstructionAuthority,
			Sensitivity:          event.Tool.Sensitivity,
			Redacted:             event.Tool.Redacted,
			GrantIDs:             append([]string(nil), event.Tool.GrantIDs...),
		}
	}
	if event.Reasoning != nil {
		msg.Reasoning = &ReasoningActivity{
			ID:         event.Reasoning.ID,
			Status:     event.Reasoning.Status,
			Content:    event.Reasoning.Content,
			DurationMS: event.Reasoning.DurationMS,
			Provider:   event.Reasoning.Provider,
			Model:      event.Reasoning.Model,
			Kind:       event.Reasoning.Kind,
		}
	}
	if event.Approval != nil {
		msg.Approval = event.Approval.Request
	}
	return msg
}

func wsMessageToOutboundEvent(msg WSMessage) turn.OutboundEvent {
	event := turn.OutboundEvent{
		Type:    msg.Type,
		Content: msg.Content,
		TurnID:  msg.TurnID,
		Payload: clonePayload(msg.Payload),
	}
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}
	if msg.GroupID != "" {
		event.Payload["group_id"] = msg.GroupID
	}
	if msg.SegmentID != "" {
		event.Payload["segment_id"] = msg.SegmentID
	}
	if msg.SegmentIndex != 0 {
		event.Payload["segment_index"] = msg.SegmentIndex
	}
	if msg.SegmentTotal != 0 {
		event.Payload["segment_total"] = msg.SegmentTotal
	}
	if msg.Tool != nil {
		event.Tool = &turn.ToolActivity{
			ID:                   msg.Tool.ID,
			Name:                 msg.Tool.Name,
			Status:               msg.Tool.Status,
			DurationMS:           msg.Tool.DurationMS,
			Preview:              msg.Tool.Preview,
			Size:                 msg.Tool.Size,
			Hash:                 msg.Tool.Hash,
			IsTruncated:          msg.Tool.IsTruncated,
			Origin:               msg.Tool.Origin,
			RuntimeKind:          msg.Tool.RuntimeKind,
			ProducerID:           msg.Tool.ProducerID,
			Executor:             msg.Tool.Executor,
			Integrity:            msg.Tool.Integrity,
			InstructionAuthority: msg.Tool.InstructionAuthority,
			Sensitivity:          msg.Tool.Sensitivity,
			Redacted:             msg.Tool.Redacted,
			GrantIDs:             append([]string(nil), msg.Tool.GrantIDs...),
		}
	}
	if msg.Reasoning != nil {
		event.Reasoning = &turn.ReasoningActivity{
			ID:         msg.Reasoning.ID,
			Status:     msg.Reasoning.Status,
			Content:    msg.Reasoning.Content,
			DurationMS: msg.Reasoning.DurationMS,
			Provider:   msg.Reasoning.Provider,
			Model:      msg.Reasoning.Model,
			Kind:       msg.Reasoning.Kind,
		}
	}
	if msg.Approval != nil {
		event.Approval = &turn.ApprovalActivity{Request: msg.Approval}
	}
	return event
}

func payloadInt(payload map[string]any, key string) int {
	if payload == nil {
		return 0
	}
	switch value := payload[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func clonePayload(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	clone := make(map[string]any, len(payload))
	for key, value := range payload {
		clone[key] = value
	}
	return clone
}
