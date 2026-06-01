package turn

import "testing"

func TestBuildIdempotencyKeyUsesRequestIDForWebUIMessage(t *testing.T) {
	env := InboundEnvelope{
		Source:    SourceWebUI,
		Kind:      InboundUserMessage,
		SessionID: "session-1",
		RequestID: "request-1",
	}

	got := BuildIdempotencyKey(env)

	if got != "webui:session-1:user_message:request-1" {
		t.Fatalf("key = %q, want webui:session-1:user_message:request-1", got)
	}
}

func TestBuildIdempotencyKeyGeneratesEphemeralKeyWithoutRequestID(t *testing.T) {
	env := InboundEnvelope{
		Source:    SourceWebUI,
		Kind:      InboundUserMessage,
		SessionID: "session-1",
	}

	first := BuildIdempotencyKey(env)
	second := BuildIdempotencyKey(env)

	if first == "" || second == "" {
		t.Fatalf("keys must not be empty: %q %q", first, second)
	}
	if first == second {
		t.Fatalf("keys = %q and %q, want distinct ephemeral keys", first, second)
	}
	if first == "webui:session-1:user_message:" || second == "webui:session-1:user_message:" {
		t.Fatalf("key used empty request_id form: %q %q", first, second)
	}
}

func TestBuildIdempotencyKeyDistinguishesApprovalActionOption(t *testing.T) {
	env := InboundEnvelope{
		Source:    SourceWebUI,
		Kind:      InboundApprovalAction,
		SessionID: "session-1",
		Approval: &InboundApproval{
			RequestID: "approval-1",
			Action:    "approve",
			OptionID:  "delete",
		},
	}

	got := BuildIdempotencyKey(env)

	if got != "webui:session-1:approval_action:approval-1:approve:delete" {
		t.Fatalf("key = %q, want approval key", got)
	}
}

func TestMemoryIdempotencyStoreSkipsCompletedDuplicate(t *testing.T) {
	store := NewMemoryIdempotencyStore()
	key := "webui:session-1:user_message:request-1"

	first, err := store.Begin(key, "turn-1")
	if err != nil {
		t.Fatalf("Begin first: %v", err)
	}
	if first.Duplicate {
		t.Fatal("first begin marked duplicate")
	}
	if err := store.Complete(key, "done"); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	duplicate, err := store.Begin(key, "turn-2")
	if err != nil {
		t.Fatalf("Begin duplicate: %v", err)
	}
	if !duplicate.Duplicate || duplicate.Status != "done" || duplicate.TurnID != "turn-1" {
		t.Fatalf("duplicate result = %#v, want completed turn-1", duplicate)
	}
}
