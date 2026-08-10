package storage

import (
	"context"
	"encoding/json"
	"testing"
)

// messages and turns carry a foreign key to sessions, so scene tests need a
// real session row before they can seed anything.
func seedSession(ctx context.Context, t *testing.T, db *DB, sessionID string) {
	t.Helper()
	if _, err := db.db.ExecContext(ctx,
		`INSERT INTO sessions (id, persona, created_at, updated_at, title)
		 VALUES (?, 'default', '2026-08-10T03:59:00Z', '2026-08-10T03:59:00Z', 'test')`,
		sessionID); err != nil {
		t.Fatalf("seed session %s: %v", sessionID, err)
	}
}

func TestBadSampleInsertAndReadBack(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	inserted, err := db.InsertBadSample(ctx, BadSampleRecord{
		Reason:       "又忘了我讨厌香菜",
		SessionID:    "session-1",
		OriginKey:    "webui:local:main",
		PersonaKey:   "default",
		TargetTurnID: "turn-1",
		ContextJSON:  `{"context_schema_version":1,"messages":[]}`,
	})
	if err != nil {
		t.Fatalf("InsertBadSample: %v", err)
	}
	if inserted.ID == "" {
		t.Fatal("InsertBadSample did not assign an ID")
	}
	if inserted.CreatedAt == "" {
		t.Fatal("InsertBadSample did not assign CreatedAt")
	}
	if inserted.ContextSchemaVersion != 1 {
		t.Fatalf("ContextSchemaVersion = %d, want 1", inserted.ContextSchemaVersion)
	}

	items, err := db.ListBadSamples(ctx, 10)
	if err != nil {
		t.Fatalf("ListBadSamples: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	got := items[0]
	if got.Reason != "又忘了我讨厌香菜" {
		t.Fatalf("Reason = %q", got.Reason)
	}
	if got.SessionID != "session-1" || got.OriginKey != "webui:local:main" || got.PersonaKey != "default" {
		t.Fatalf("identity columns round-tripped wrong: %#v", got)
	}
	if got.TargetTurnID != "turn-1" {
		t.Fatalf("TargetTurnID = %q", got.TargetTurnID)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(got.ContextJSON), &decoded); err != nil {
		t.Fatalf("ContextJSON is not valid JSON: %v", err)
	}
}

func TestBadSampleCount(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	count, err := db.CountBadSamples(ctx)
	if err != nil {
		t.Fatalf("CountBadSamples empty: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}

	for i := 0; i < 3; i++ {
		if _, err := db.InsertBadSample(ctx, BadSampleRecord{Reason: "r", ContextJSON: "{}"}); err != nil {
			t.Fatalf("InsertBadSample %d: %v", i, err)
		}
	}
	count, err = db.CountBadSamples(ctx)
	if err != nil {
		t.Fatalf("CountBadSamples: %v", err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
}

// Every scene reader must tolerate an empty database: a young session may have
// no snapshots, no affect state and no turns, and that must never block capture.
func TestBadSampleReadersTolerateMissingData(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	messages, err := db.RecentSessionMessages(ctx, "nope", 12)
	if err != nil {
		t.Fatalf("RecentSessionMessages: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("messages = %d, want 0", len(messages))
	}

	snapshots, err := db.RecentPromptSnapshots(ctx, "nope", 3)
	if err != nil {
		t.Fatalf("RecentPromptSnapshots: %v", err)
	}
	if len(snapshots) != 0 {
		t.Fatalf("snapshots = %d, want 0", len(snapshots))
	}

	affect, err := db.SessionAffectSnapshot(ctx, "nope", 3)
	if err != nil {
		t.Fatalf("SessionAffectSnapshot: %v", err)
	}
	if affect.State != nil {
		t.Fatalf("affect.State = %#v, want nil", affect.State)
	}
	if len(affect.RecentEvaluations) != 0 {
		t.Fatalf("evaluations = %d, want 0", len(affect.RecentEvaluations))
	}

	turns, err := db.RecentTurns(ctx, "nope", 3)
	if err != nil {
		t.Fatalf("RecentTurns: %v", err)
	}
	if len(turns) != 0 {
		t.Fatalf("turns = %d, want 0", len(turns))
	}

	turnID, err := db.LatestCompletedTurnID(ctx, "nope")
	if err != nil {
		t.Fatalf("LatestCompletedTurnID: %v", err)
	}
	if turnID != "" {
		t.Fatalf("turnID = %q, want empty", turnID)
	}
}

func TestBadSampleRecentSessionMessagesReturnsOldestFirst(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	seedSession(ctx, t, db, "s1")
	for _, row := range []struct{ id, role, content, created string }{
		{"m1", "user", "第一条", "2026-08-10T04:00:00Z"},
		{"m2", "assistant", "第二条", "2026-08-10T04:00:01Z"},
		{"m3", "user", "第三条", "2026-08-10T04:00:02Z"},
	} {
		if _, err := db.db.ExecContext(ctx,
			`INSERT INTO messages (id, session_id, role, content, created_at, metadata) VALUES (?, ?, ?, ?, ?, ?)`,
			row.id, "s1", row.role, row.content, row.created, `{"kind":"dialogue_user"}`); err != nil {
			t.Fatalf("seed message %s: %v", row.id, err)
		}
	}

	// Limit smaller than the row count must keep the NEWEST rows, then present
	// them oldest-first so the frozen scene reads as a conversation.
	got, err := db.RecentSessionMessages(ctx, "s1", 2)
	if err != nil {
		t.Fatalf("RecentSessionMessages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Content != "第二条" || got[1].Content != "第三条" {
		t.Fatalf("order wrong: %q then %q", got[0].Content, got[1].Content)
	}
	if got[0].Metadata == "" {
		t.Fatal("metadata was not carried through; it holds the injected memory block")
	}
}

// The target turn must be a successful user turn, while the frozen turns block
// must include failures. Guarding both in one test keeps the distinction from
// being "simplified" away later.
func TestBadSampleTurnSelectionDistinguishesFailures(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	seedSession(ctx, t, db, "s1")
	for _, row := range []struct{ id, kind, state, status, started, completed string }{
		{"t1", "user_message", "done", "done", "2026-08-10T04:00:00Z", "2026-08-10T04:00:05Z"},
		{"t2", "user_message", "failed", "failed", "2026-08-10T04:01:00Z", "2026-08-10T04:01:02Z"},
	} {
		if _, err := db.db.ExecContext(ctx,
			`INSERT INTO turns (id, idempotency_key, source, source_event_id, kind, session_id, persona_key,
			                    state, status, error_kind, error_message, started_at, updated_at, completed_at)
			 VALUES (?, ?, 'webui', ?, ?, 's1', 'default', ?, ?, '', '', ?, ?, ?)`,
			row.id, row.id+"-key", row.id+"-event", row.kind, row.state, row.status,
			row.started, row.completed, row.completed); err != nil {
			t.Fatalf("seed turn %s: %v", row.id, err)
		}
	}

	turnID, err := db.LatestCompletedTurnID(ctx, "s1")
	if err != nil {
		t.Fatalf("LatestCompletedTurnID: %v", err)
	}
	if turnID != "t1" {
		t.Fatalf("target turn = %q, want t1 (the failed t2 must not be targeted)", turnID)
	}

	turns, err := db.RecentTurns(ctx, "s1", 5)
	if err != nil {
		t.Fatalf("RecentTurns: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("frozen turns = %d, want 2 including the failed one", len(turns))
	}
	var sawFailed bool
	for _, turn := range turns {
		if turn.Status == "failed" {
			sawFailed = true
		}
	}
	if !sawFailed {
		t.Fatal("failed turn was filtered out; it is exactly what attribution needs")
	}
}
