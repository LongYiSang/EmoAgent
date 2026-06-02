package turn

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/storage"
	_ "modernc.org/sqlite"
)

func TestSQLiteJournalPersistsTransitionsEventsAndOutbound(t *testing.T) {
	db := openTurnTestDB(t)
	journal := NewSQLiteJournal(db)
	ctx := context.Background()

	if err := journal.StartTurn(ctx, TurnRecord{
		TurnID:         "turn-1",
		IdempotencyKey: "key-1",
		Source:         SourceWebUI,
		SourceEventID:  "request-1",
		Kind:           InboundUserMessage,
		SessionID:      "session-1",
		PersonaKey:     "default",
		State:          StateCreated,
		StartedAt:      time.Unix(1, 0).UTC(),
	}); err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	if err := journal.RecordTransition(ctx, "turn-1", StateCreated, StateNormalizing, StageMetrics{Stage: StageNormalize, DurationMS: 7}); err != nil {
		t.Fatalf("RecordTransition: %v", err)
	}
	if err := journal.RecordEvent(ctx, "turn-1", JournalEvent{
		Stage: StageEmotionLoop,
		Type:  "tool_call_end",
		Payload: map[string]any{
			"tool":            "write_file",
			"summary":         "safe summary",
			"raw_tool_output": "SECRET=value",
			"nested": map[string]any{
				"file_content": "secret file",
				"status":       "done",
			},
		},
	}); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := journal.RecordOutbound(ctx, "turn-1", OutboundEvent{
		Type:    EventStreamDelta,
		Content: "hello",
		Payload: map[string]any{"raw_prompt": "hidden", "token_count": 3},
	}); err != nil {
		t.Fatalf("RecordOutbound: %v", err)
	}
	if err := journal.CompleteTurn(ctx, "turn-1", "done", ""); err != nil {
		t.Fatalf("CompleteTurn: %v", err)
	}

	reopened := NewSQLiteJournal(db)
	snapshot, ok, err := reopened.GetTurn(ctx, "turn-1")
	if err != nil {
		t.Fatalf("GetTurn: %v", err)
	}
	if !ok {
		t.Fatal("turn not found after reopening journal")
	}
	if snapshot.Status != "done" || snapshot.State != StateNormalizing {
		t.Fatalf("snapshot status/state = %q/%q, want done/normalizing", snapshot.Status, snapshot.State)
	}
	if len(snapshot.Transitions) != 1 || snapshot.Transitions[0].Stage != StageNormalize {
		t.Fatalf("transitions = %#v, want normalize transition", snapshot.Transitions)
	}
	if len(snapshot.Events) != 1 {
		t.Fatalf("events = %#v, want one event", snapshot.Events)
	}
	payloadText := toDebugString(snapshot.Events[0].Payload)
	for _, leak := range []string{"raw_tool_output", "SECRET=value", "file_content", "secret file"} {
		if strings.Contains(payloadText, leak) {
			t.Fatalf("event payload leaks %q: %s", leak, payloadText)
		}
	}
	if snapshot.Events[0].Payload["summary"] != "safe summary" {
		t.Fatalf("event payload = %#v, want safe summary", snapshot.Events[0].Payload)
	}

	outbound, err := reopened.ListOutbound(ctx, "turn-1")
	if err != nil {
		t.Fatalf("ListOutbound: %v", err)
	}
	if len(outbound) != 1 || outbound[0].Type != EventStreamDelta {
		t.Fatalf("outbound = %#v, want persisted stream_delta summary", outbound)
	}
	if outbound[0].Content != "" {
		t.Fatalf("outbound content = %q, want no persisted stream delta body", outbound[0].Content)
	}
	if outbound[0].Payload["content_bytes"] == nil || outbound[0].Payload["content_hash"] == nil {
		t.Fatalf("outbound payload = %#v, want content_bytes/content_hash summary", outbound[0].Payload)
	}
	if _, ok := outbound[0].Payload["raw_prompt"]; ok {
		t.Fatalf("outbound payload leaks raw_prompt: %#v", outbound[0].Payload)
	}
}

func TestMultiJournalWritesToPrimaryAndSecondary(t *testing.T) {
	db := openTurnTestDB(t)
	primary := NewSQLiteJournal(db)
	secondary := NewMemoryJournal()
	journal := NewMultiJournal(primary, secondary)

	if err := journal.StartTurn(context.Background(), TurnRecord{TurnID: "turn-1", Kind: InboundUserMessage, State: StateCreated}); err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	if err := journal.RecordEvent(context.Background(), "turn-1", JournalEvent{Stage: StageNormalize, Type: "normalized", Payload: map[string]any{"ok": true}}); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := journal.CompleteTurn(context.Background(), "turn-1", "done", ""); err != nil {
		t.Fatalf("CompleteTurn: %v", err)
	}

	if _, ok, err := primary.GetTurn(context.Background(), "turn-1"); err != nil || !ok {
		t.Fatalf("primary GetTurn ok=%v err=%v", ok, err)
	}
	if _, ok := secondary.GetTurn("turn-1"); !ok {
		t.Fatal("secondary memory journal missing turn")
	}
}

func openTurnTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("foreign_keys pragma: %v", err)
	}
	if err := storage.ApplyMigrations(db); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	return db
}
