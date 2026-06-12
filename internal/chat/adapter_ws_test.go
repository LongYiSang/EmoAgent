package chat

import (
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/turn"
)

func mustWSMessageToInbound(t *testing.T, msg WSMessage, sessionID, personaName string) turn.InboundEnvelope {
	t.Helper()
	env, err := wsMessageToInbound(msg, sessionID, personaName)
	if err != nil {
		t.Fatalf("wsMessageToInbound: %v", err)
	}
	return env
}

func TestWSMessageToInboundMessageUsesRequestID(t *testing.T) {
	env := mustWSMessageToInbound(t, WSMessage{
		Type:      "message",
		Content:   " hello ",
		RequestID: "request-1",
	}, "session-1", "default")

	if env.Kind != turn.InboundUserMessage {
		t.Fatalf("kind = %q, want user_message", env.Kind)
	}
	if env.UserMessage == nil || env.UserMessage.Content != "hello" {
		t.Fatalf("user message = %#v, want trimmed content", env.UserMessage)
	}
	if env.IdempotencyKey != "webui:session-1:user_message:request-1" {
		t.Fatalf("idempotency key = %q", env.IdempotencyKey)
	}
}

func TestWSMessageToInboundApprovalNormalizesAction(t *testing.T) {
	env := mustWSMessageToInbound(t, WSMessage{
		Type:      "approval_action",
		RequestID: "approval-1",
		Action:    " APPROVE ",
		OptionID:  "delete",
	}, "session-1", "default")

	if env.Kind != turn.InboundApprovalAction {
		t.Fatalf("kind = %q, want approval_action", env.Kind)
	}
	if env.Approval == nil || env.Approval.Action != "approve" {
		t.Fatalf("approval = %#v, want approve", env.Approval)
	}
	if env.IdempotencyKey != "webui:session-1:approval_action:approval-1:approve:delete" {
		t.Fatalf("idempotency key = %q", env.IdempotencyKey)
	}
}

func TestWSMessageToInboundRejectsUnsupportedUserParts(t *testing.T) {
	_, err := wsMessageToInbound(WSMessage{
		Type:    "message",
		Content: "hello",
		Parts: []llm.ContentBlock{
			{Type: string(llm.PartText), Text: "hello"},
			{Type: string(llm.PartToolUse), ID: "tool-call-1", Name: "spoofed_tool"},
		},
	}, "session-1", "default")
	if err == nil || !strings.Contains(err.Error(), "unsupported user content part type") {
		t.Fatalf("err = %v, want unsupported user content part type", err)
	}
}

func TestOutboundEventToWSMessagePreservesExistingTypes(t *testing.T) {
	approval := &protocol.ApprovalRequest{ID: "approval-1", TaskID: "task-1"}
	msg := outboundEventToWSMessage(turn.OutboundEvent{
		Type:    turn.EventApprovalRequired,
		Content: "ignored",
		Approval: &turn.ApprovalActivity{
			Request: approval,
		},
	})

	if msg.Type != "approval_required" || msg.Approval == nil || msg.Approval.ID != "approval-1" {
		t.Fatalf("message = %#v, want approval_required with request", msg)
	}
}

func TestOutboundEventToWSMessagePreservesTurnStatusPayload(t *testing.T) {
	msg := outboundEventToWSMessage(turn.OutboundEvent{
		TurnID: "turn-1",
		Type:   turn.EventTurnStatus,
		Payload: map[string]any{
			"status":     "previous_failed",
			"error_kind": "llm_failed",
		},
	})

	if msg.Type != "turn_status" || msg.TurnID != "turn-1" || msg.Status != "previous_failed" || msg.ErrorKind != "llm_failed" {
		t.Fatalf("message = %#v, want turn status fields", msg)
	}
}

func TestOutboundEventToWSMessagePreservesReplaySummaryPayload(t *testing.T) {
	msg := outboundEventToWSMessage(turn.OutboundEvent{
		Type: turn.EventStreamDelta,
		Payload: map[string]any{
			"content_bytes": int64(12),
			"content_hash":  "sha256:abc",
		},
	})

	if msg.Payload["content_bytes"] == nil || msg.Payload["content_hash"] != "sha256:abc" {
		t.Fatalf("payload = %#v, want replay summary", msg.Payload)
	}
}
