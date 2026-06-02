package turn

import (
	"context"
	"testing"
)

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

func TestSQLiteIdempotencyStorePersistsAndClaimsAtomically(t *testing.T) {
	db := openTurnTestDB(t)
	journal := NewSQLiteJournal(db)
	if err := journal.StartTurn(context.Background(), TurnRecord{
		TurnID:         "turn-1",
		IdempotencyKey: "webui:session-1:user_message:request-1",
		Kind:           InboundUserMessage,
		SessionID:      "session-1",
		State:          StateCreated,
	}); err != nil {
		t.Fatalf("StartTurn: %v", err)
	}

	store := NewSQLiteIdempotencyStore(db)
	first, err := store.Begin("webui:session-1:user_message:request-1", "turn-1")
	if err != nil {
		t.Fatalf("Begin first: %v", err)
	}
	if first.Duplicate || first.Status != "running" || first.TurnID != "turn-1" {
		t.Fatalf("first result = %#v, want new running turn-1", first)
	}

	duplicate, err := store.Begin("webui:session-1:user_message:request-1", "turn-2")
	if err != nil {
		t.Fatalf("Begin duplicate: %v", err)
	}
	if !duplicate.Duplicate || duplicate.Status != "running" || duplicate.TurnID != "turn-1" {
		t.Fatalf("running duplicate = %#v, want existing running turn-1", duplicate)
	}

	if err := store.Complete("webui:session-1:user_message:request-1", "approval_wait"); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	reopened := NewSQLiteIdempotencyStore(db)
	afterRestart, err := reopened.Begin("webui:session-1:user_message:request-1", "turn-3")
	if err != nil {
		t.Fatalf("Begin after restart: %v", err)
	}
	if !afterRestart.Duplicate || afterRestart.Status != "approval_wait" || afterRestart.TurnID != "turn-1" {
		t.Fatalf("after restart = %#v, want persisted approval_wait turn-1", afterRestart)
	}
}
