package proactive

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
)

// The runner is tested against a real SQLite database rather than a fake store:
// most of its behaviour lives in the queries (skip streaks, quotas, target
// ordering), and a hand-written fake would only test itself.

type fakePresence struct{ active bool }

func (p fakePresence) HasActiveRun(string) bool { return p.active }

type fakeQuiet struct {
	until time.Time
	set   bool
}

func (q fakeQuiet) QuietUntil(context.Context) (time.Time, bool) { return q.until, q.set }

func runnerTestDB(t *testing.T) *storage.DB {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := storage.Open(filepath.Join(t.TempDir(), "proactive.db"), logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// seedReachableUser creates a persona bound to a QQ private origin with real
// conversation history — the minimum state in which a target is resolvable.
func seedReachableUser(t *testing.T, db *storage.DB, lastUserMessageAt time.Time) string {
	t.Helper()
	ctx := context.Background()
	sessionID := "session-1"

	if err := db.CreateSession(ctx, sessionID, "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := db.UpsertConversationOrigin(ctx, storage.ConversationOriginRecord{
		ID:                     "origin-1",
		OriginKey:              "onebot:private:123",
		SourceType:             "platform",
		AdapterInstanceID:      "onebot-1",
		PlatformID:             "onebot_v11",
		ChannelType:            "private",
		ExternalConversationID: "123",
		ExternalActorID:        "123",
		MetadataJSON:           "{}",
	}); err != nil {
		t.Fatalf("UpsertConversationOrigin: %v", err)
	}
	if err := db.UpsertConversationBinding(ctx, storage.ConversationBindingRecord{
		ID:               "binding-1",
		OriginKey:        "onebot:private:123",
		PersonaKey:       "default",
		CurrentSessionID: sessionID,
		VariablesJSON:    "{}",
	}); err != nil {
		t.Fatalf("UpsertConversationBinding: %v", err)
	}
	if err := db.AddMessage(ctx, "msg-1", sessionID, "user", "在忙啥"); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	return sessionID
}

func seedCandidate(t *testing.T, db *storage.DB, id string) {
	t.Helper()
	now := time.Now()
	err := db.InsertProactiveCandidate(context.Background(), storage.ProactiveCandidateRecord{
		ID:           id,
		PersonaKey:   "default",
		EventType:    "stuck",
		Summary:      "反复跑 go test，终端持续报错",
		ObservedFrom: now.Add(-40 * time.Minute).Format(time.RFC3339Nano),
		ObservedTo:   now.Format(time.RFC3339Nano),
		Importance:   0.7,
		ExpiresAt:    now.Add(6 * time.Hour).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("InsertProactiveCandidate(%s): %v", id, err)
	}
}

func speakingGate(t *testing.T) *Gate {
	t.Helper()
	return NewGate(&stubClient{resp: &llm.ChatResponse{
		Content: `{"decision":"speak","reason":"卡了很久","urgency":0.7,"hint":"他在调测试"}`,
	}}, "test-model", llm.RequestParams{}, config.DefaultProactiveConfig().Gate, nil)
}

func enabledRunnerConfig() config.ProactiveConfig {
	cfg := config.DefaultProactiveConfig()
	cfg.Enabled = true
	cfg.QuietHours = nil
	return cfg
}

func TestRunnerSpeaksWhenEverythingAligns(t *testing.T) {
	db := runnerTestDB(t)
	seedReachableUser(t, db, time.Now().Add(-time.Hour))
	seedCandidate(t, db, "cand-1")

	runner := NewRunner(db, speakingGate(t), fakePresence{}, fakeQuiet{}, nil, nil)
	eval, err := runner.Evaluate(context.Background(), enabledRunnerConfig(), "default")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Decision.Verdict != VerdictSpeak {
		t.Fatalf("verdict = %q (%s), want speak", eval.Decision.Verdict, eval.Decision.Reason)
	}
	if !eval.HasTarget || eval.Target.Origin.OriginKey != "onebot:private:123" {
		t.Fatalf("target = %#v, want the seeded QQ origin", eval.Target)
	}
	if eval.Decision.Hint != "他在调测试" {
		t.Fatalf("hint = %q", eval.Decision.Hint)
	}
}

// Every refusal must be recorded. With the agent silent most of the time, an
// unrecorded skip is indistinguishable from a broken pipeline.
func TestRunnerRecordsEverySkip(t *testing.T) {
	db := runnerTestDB(t)
	seedReachableUser(t, db, time.Now().Add(-time.Hour))
	seedCandidate(t, db, "cand-1")

	cfg := enabledRunnerConfig()
	runner := NewRunner(db, speakingGate(t), fakePresence{active: true}, fakeQuiet{}, nil, nil)

	eval, err := runner.Evaluate(context.Background(), cfg, "default")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Decision.Verdict != VerdictSkip || eval.Decision.Reason != ReasonUserPresent {
		t.Fatalf("decision = %#v, want skip/user_present", eval.Decision)
	}

	decisions, err := db.ListProactiveDecisions(context.Background(), "default", 10)
	if err != nil {
		t.Fatalf("ListProactiveDecisions: %v", err)
	}
	if len(decisions) != 1 || decisions[0].Reason != ReasonUserPresent {
		t.Fatalf("decisions = %#v, want one user_present skip", decisions)
	}
}

// A skipped candidate stays in play with a bumped skip_count, so the gate can
// see it has already declined this activity and reason about escalation.
func TestRunnerSkipKeepsCandidateAndBumpsSkipCount(t *testing.T) {
	db := runnerTestDB(t)
	seedReachableUser(t, db, time.Now().Add(-time.Hour))
	seedCandidate(t, db, "cand-1")

	runner := NewRunner(db, speakingGate(t), fakePresence{active: true}, fakeQuiet{}, nil, nil)
	if _, err := runner.Evaluate(context.Background(), enabledRunnerConfig(), "default"); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	candidates, err := db.ListProactiveCandidates(context.Background(), storage.ProactiveCandidateFilter{
		PersonaKey: "default",
		Statuses:   []string{storage.ProactiveCandidateStatusSkipped},
	})
	if err != nil {
		t.Fatalf("ListProactiveCandidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].SkipCount != 1 {
		t.Fatalf("candidates = %#v, want one with skip_count 1", candidates)
	}

	// Second pass sees the same candidate again, now with skip_count 1.
	if _, err := runner.Evaluate(context.Background(), enabledRunnerConfig(), "default"); err != nil {
		t.Fatalf("second Evaluate: %v", err)
	}
	candidates, _ = db.ListProactiveCandidates(context.Background(), storage.ProactiveCandidateFilter{
		PersonaKey: "default",
		Statuses:   []string{storage.ProactiveCandidateStatusSkipped},
	})
	if len(candidates) != 1 || candidates[0].SkipCount != 2 {
		t.Fatalf("candidates = %#v, want skip_count 2 after a second refusal", candidates)
	}
}

func TestRunnerSkipsWithoutCandidates(t *testing.T) {
	db := runnerTestDB(t)
	seedReachableUser(t, db, time.Now().Add(-time.Hour))

	runner := NewRunner(db, speakingGate(t), fakePresence{}, fakeQuiet{}, nil, nil)
	eval, err := runner.Evaluate(context.Background(), enabledRunnerConfig(), "default")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Decision.Reason != ReasonNoCandidates {
		t.Fatalf("reason = %q, want no_candidates", eval.Decision.Reason)
	}
}

// A user who has never talked to the bot on any channel is unreachable, and the
// gate must not even be consulted.
func TestRunnerSkipsWhenNoTargetExists(t *testing.T) {
	db := runnerTestDB(t)
	seedCandidate(t, db, "cand-1")

	runner := NewRunner(db, speakingGate(t), fakePresence{}, fakeQuiet{}, nil, nil)
	eval, err := runner.Evaluate(context.Background(), enabledRunnerConfig(), "default")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Decision.Reason != ReasonNoTarget {
		t.Fatalf("reason = %q, want no_target", eval.Decision.Reason)
	}
	if eval.Decision.GateConsulted {
		t.Fatal("gate was consulted despite there being nowhere to deliver")
	}
}

// The daily cap must short-circuit before the model is ever called: a cap a
// model can argue past is not a cap.
func TestRunnerDailyCapShortCircuitsBeforeGate(t *testing.T) {
	db := runnerTestDB(t)
	seedReachableUser(t, db, time.Now().Add(-time.Hour))
	seedCandidate(t, db, "cand-1")
	ctx := context.Background()

	for i, id := range []string{"d1", "d2"} {
		if err := db.InsertProactiveDecision(ctx, storage.ProactiveDecisionRecord{
			ID:         id,
			PersonaKey: "default",
			OriginKey:  "onebot:private:123",
			Decision:   storage.ProactiveDecisionSpeak,
			CreatedAt:  time.Now().Add(-time.Duration(i+1) * time.Hour).Format(time.RFC3339Nano),
		}); err != nil {
			t.Fatalf("InsertProactiveDecision: %v", err)
		}
	}

	cfg := enabledRunnerConfig()
	cfg.Cooldown.MaxPerDay = 2

	client := &stubClient{resp: &llm.ChatResponse{Content: `{"decision":"speak","reason":"r","urgency":1,"hint":"h"}`}}
	gate := NewGate(client, "test-model", llm.RequestParams{}, cfg.Gate, nil)
	runner := NewRunner(db, gate, fakePresence{}, fakeQuiet{}, nil, nil)

	eval, err := runner.Evaluate(ctx, cfg, "default")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Decision.Reason != ReasonDailyCapReached {
		t.Fatalf("reason = %q, want daily_cap_reached", eval.Decision.Reason)
	}
	if client.lastReq.Model != "" {
		t.Fatal("gate model was called despite the daily cap being reached")
	}
}

func TestRunnerHonoursQuietCommand(t *testing.T) {
	db := runnerTestDB(t)
	seedReachableUser(t, db, time.Now().Add(-time.Hour))
	seedCandidate(t, db, "cand-1")

	runner := NewRunner(db, speakingGate(t), fakePresence{},
		fakeQuiet{until: time.Now().Add(2 * time.Hour), set: true}, nil, nil)
	eval, err := runner.Evaluate(context.Background(), enabledRunnerConfig(), "default")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Decision.Reason != ReasonQuietCommand {
		t.Fatalf("reason = %q, want quiet_command", eval.Decision.Reason)
	}
}

// Expired candidates must not keep provoking decisions.
func TestRunnerExpiresStaleCandidates(t *testing.T) {
	db := runnerTestDB(t)
	seedReachableUser(t, db, time.Now().Add(-time.Hour))
	ctx := context.Background()

	past := time.Now().Add(-8 * time.Hour)
	if err := db.InsertProactiveCandidate(ctx, storage.ProactiveCandidateRecord{
		ID:           "stale",
		PersonaKey:   "default",
		EventType:    "stuck",
		Summary:      "很久以前的事",
		ObservedFrom: past.Format(time.RFC3339Nano),
		ObservedTo:   past.Format(time.RFC3339Nano),
		CreatedAt:    past.Format(time.RFC3339Nano),
		ExpiresAt:    time.Now().Add(-2 * time.Hour).Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("InsertProactiveCandidate: %v", err)
	}

	runner := NewRunner(db, speakingGate(t), fakePresence{}, fakeQuiet{}, nil, nil)
	eval, err := runner.Evaluate(ctx, enabledRunnerConfig(), "default")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Decision.Reason != ReasonNoCandidates {
		t.Fatalf("reason = %q, want no_candidates after expiry", eval.Decision.Reason)
	}
}

func TestRunnerDisabledConfigNeverSpeaks(t *testing.T) {
	db := runnerTestDB(t)
	seedReachableUser(t, db, time.Now().Add(-time.Hour))
	seedCandidate(t, db, "cand-1")

	runner := NewRunner(db, speakingGate(t), fakePresence{}, fakeQuiet{}, nil, nil)
	eval, err := runner.Evaluate(context.Background(), config.DefaultProactiveConfig(), "default")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Decision.Verdict != VerdictSkip || eval.Decision.Reason != ReasonDisabled {
		t.Fatalf("decision = %#v, want skip/disabled on default config", eval.Decision)
	}
}
