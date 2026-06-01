package chat

import (
	"testing"

	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/turn"
)

func TestWSMessageToInboundMessageUsesRequestID(t *testing.T) {
	env := wsMessageToInbound(WSMessage{
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
	env := wsMessageToInbound(WSMessage{
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
