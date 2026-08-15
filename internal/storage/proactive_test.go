package storage

import (
	"context"
	"testing"
	"time"
)

func insertTestCandidate(t *testing.T, db *DB, id, personaKey string, createdAt, expiresAt time.Time) {
	t.Helper()
	err := db.InsertProactiveCandidate(context.Background(), ProactiveCandidateRecord{
		ID:           id,
		PersonaKey:   personaKey,
		EventType:    "stuck",
		Summary:      "反复跑 go test，终端持续报错",
		ObservedFrom: db.formatTime(createdAt.Add(-30 * time.Minute)),
		ObservedTo:   db.formatTime(createdAt),
		Importance:   0.7,
		CreatedAt:    db.formatTime(createdAt),
		ExpiresAt:    db.formatTime(expiresAt),
	})
	if err != nil {
		t.Fatalf("InsertProactiveCandidate(%s): %v", id, err)
	}
}

func insertTestDecision(t *testing.T, db *DB, id, personaKey, originKey, decision string, createdAt time.Time) {
	t.Helper()
	err := db.InsertProactiveDecision(context.Background(), ProactiveDecisionRecord{
		ID:         id,
		PersonaKey: personaKey,
		OriginKey:  originKey,
		Decision:   decision,
		Reason:     "test",
		CreatedAt:  db.formatTime(createdAt),
	})
	if err != nil {
		t.Fatalf("InsertProactiveDecision(%s): %v", id, err)
	}
}

func TestProactiveCandidateLifecycle(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	now := time.Now()

	insertTestCandidate(t, db, "cand-1", "default", now, now.Add(6*time.Hour))
	insertTestCandidate(t, db, "cand-2", "default", now, now.Add(6*time.Hour))

	pending, err := db.ListProactiveCandidates(ctx, ProactiveCandidateFilter{
		PersonaKey: "default",
		Statuses:   []string{ProactiveCandidateStatusPending},
	})
	if err != nil {
		t.Fatalf("ListProactiveCandidates: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("pending candidates = %d, want 2", len(pending))
	}

	// A skip keeps the candidate visible and bumps skip_count so the gate can
	// see it has already declined this one.
	if err := db.MarkProactiveCandidates(ctx, []string{"cand-1"}, ProactiveCandidateStatusSkipped, "dec-1"); err != nil {
		t.Fatalf("MarkProactiveCandidates: %v", err)
	}
	skipped, err := db.ListProactiveCandidates(ctx, ProactiveCandidateFilter{
		PersonaKey: "default",
		Statuses:   []string{ProactiveCandidateStatusSkipped},
	})
	if err != nil {
		t.Fatalf("ListProactiveCandidates(skipped): %v", err)
	}
	if len(skipped) != 1 || skipped[0].SkipCount != 1 || skipped[0].LastDecisionID != "dec-1" {
		t.Fatalf("skipped = %#v, want one record with skip_count 1 and decision dec-1", skipped)
	}

	if err := db.MarkProactiveCandidates(ctx, []string{"cand-1"}, ProactiveCandidateStatusSkipped, "dec-2"); err != nil {
		t.Fatalf("MarkProactiveCandidates second skip: %v", err)
	}
	skipped, _ = db.ListProactiveCandidates(ctx, ProactiveCandidateFilter{
		PersonaKey: "default",
		Statuses:   []string{ProactiveCandidateStatusSkipped},
	})
	if len(skipped) != 1 || skipped[0].SkipCount != 2 {
		t.Fatalf("skip_count = %d, want 2", skipped[0].SkipCount)
	}
}

func TestProactiveCandidateBackpressureCount(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	now := time.Now()

	for _, id := range []string{"c1", "c2", "c3"} {
		insertTestCandidate(t, db, id, "default", now, now.Add(6*time.Hour))
	}
	insertTestCandidate(t, db, "other-persona", "neko", now, now.Add(6*time.Hour))

	count, err := db.CountProactiveCandidates(ctx, "default", []string{
		ProactiveCandidateStatusPending, ProactiveCandidateStatusSkipped,
	})
	if err != nil {
		t.Fatalf("CountProactiveCandidates: %v", err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3 (persona scoped)", count)
	}
}

func TestProactiveCandidateExpiry(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	now := time.Now()

	insertTestCandidate(t, db, "fresh", "default", now, now.Add(6*time.Hour))
	insertTestCandidate(t, db, "stale", "default", now.Add(-8*time.Hour), now.Add(-2*time.Hour))

	expired, err := db.ExpireProactiveCandidates(ctx, now)
	if err != nil {
		t.Fatalf("ExpireProactiveCandidates: %v", err)
	}
	if expired != 1 {
		t.Fatalf("expired = %d, want 1", expired)
	}

	remaining, _ := db.ListProactiveCandidates(ctx, ProactiveCandidateFilter{
		PersonaKey: "default",
		Statuses:   []string{ProactiveCandidateStatusPending},
	})
	if len(remaining) != 1 || remaining[0].ID != "fresh" {
		t.Fatalf("remaining = %#v, want only 'fresh'", remaining)
	}
}

func TestProactiveSpeakQuotaCount(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	now := time.Now()

	insertTestDecision(t, db, "d1", "default", "qq:1", ProactiveDecisionSpeak, now.Add(-2*time.Hour))
	insertTestDecision(t, db, "d2", "default", "qq:1", ProactiveDecisionSpeak, now.Add(-30*time.Hour))
	insertTestDecision(t, db, "d3", "default", "", ProactiveDecisionSkip, now.Add(-1*time.Hour))

	count, err := db.CountProactiveSpeakSince(ctx, "default", now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("CountProactiveSpeakSince: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1 (skips and out-of-window speaks excluded)", count)
	}
}

func TestProactiveReplyBackfillAndIgnoredStreak(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	now := time.Now()

	insertTestDecision(t, db, "d1", "default", "qq:1", ProactiveDecisionSpeak, now.Add(-3*time.Hour))
	insertTestDecision(t, db, "d2", "default", "qq:1", ProactiveDecisionSpeak, now.Add(-2*time.Hour))
	insertTestDecision(t, db, "d3", "default", "qq:1", ProactiveDecisionSpeak, now.Add(-10*time.Minute))

	ignored, err := db.CountProactiveConsecutiveIgnored(ctx, "default", 10)
	if err != nil {
		t.Fatalf("CountProactiveConsecutiveIgnored: %v", err)
	}
	if ignored != 3 {
		t.Fatalf("ignored = %d, want 3", ignored)
	}

	// A user message inside the attribution window attributes to the newest
	// unanswered delivered message only.
	ok, err := db.BackfillProactiveUserReplied(ctx, "qq:1", now, 30*time.Minute)
	if err != nil {
		t.Fatalf("BackfillProactiveUserReplied: %v", err)
	}
	if !ok {
		t.Fatal("BackfillProactiveUserReplied = false, want true")
	}

	ignored, _ = db.CountProactiveConsecutiveIgnored(ctx, "default", 10)
	if ignored != 0 {
		t.Fatalf("ignored after reply = %d, want 0 (streak broken by newest)", ignored)
	}

	decisions, err := db.ListProactiveDecisions(ctx, "default", 10)
	if err != nil {
		t.Fatalf("ListProactiveDecisions: %v", err)
	}
	if len(decisions) != 3 || decisions[0].ID != "d3" || decisions[0].UserRepliedAt == "" {
		t.Fatalf("decisions[0] = %#v, want d3 with user_replied_at set", decisions[0])
	}
	if decisions[1].UserRepliedAt != "" {
		t.Fatalf("decisions[1].UserRepliedAt = %q, want empty (only newest attributed)", decisions[1].UserRepliedAt)
	}
}

func TestProactiveReplyBackfillOutsideWindow(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	now := time.Now()

	insertTestDecision(t, db, "d1", "default", "qq:1", ProactiveDecisionSpeak, now.Add(-3*time.Hour))

	ok, err := db.BackfillProactiveUserReplied(ctx, "qq:1", now, 30*time.Minute)
	if err != nil {
		t.Fatalf("BackfillProactiveUserReplied: %v", err)
	}
	if ok {
		t.Fatal("BackfillProactiveUserReplied = true, want false (outside attribution window)")
	}
}

func TestProactiveDecisionOutcomeUpdate(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	insertTestDecision(t, db, "d1", "default", "qq:1", ProactiveDecisionSpeak, time.Now())
	if err := db.UpdateProactiveDecisionOutcome(ctx, "d1", "turn-1", true); err != nil {
		t.Fatalf("UpdateProactiveDecisionOutcome: %v", err)
	}

	decisions, _ := db.ListProactiveDecisions(ctx, "default", 10)
	if len(decisions) != 1 || decisions[0].TurnID != "turn-1" || !decisions[0].SilencedByEmotion {
		t.Fatalf("decision = %#v, want turn-1 and silenced", decisions[0])
	}

	// A silenced message was never delivered, so it must not count as ignored.
	ignored, _ := db.CountProactiveConsecutiveIgnored(ctx, "default", 10)
	if ignored != 0 {
		t.Fatalf("ignored = %d, want 0 (silenced messages are not deliveries)", ignored)
	}
}

func TestProactiveDecisionRejectsUnknownVerdict(t *testing.T) {
	db := testDB(t)
	err := db.InsertProactiveDecision(context.Background(), ProactiveDecisionRecord{
		ID:         "d-bad",
		PersonaKey: "default",
		Decision:   "maybe",
	})
	if err == nil {
		t.Fatal("InsertProactiveDecision accepted unknown verdict, want error")
	}
}
