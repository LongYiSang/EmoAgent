package proactive

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/storage"
)

func seedCandidateWithSummary(t *testing.T, db *storage.DB, id, summary string, at time.Time, status string) {
	t.Helper()
	err := db.InsertProactiveCandidate(context.Background(), storage.ProactiveCandidateRecord{
		ID:           id,
		PersonaKey:   "default",
		EventType:    "stuck",
		Summary:      summary,
		ObservedFrom: at.Add(-30 * time.Minute).Format(time.RFC3339Nano),
		ObservedTo:   at.Format(time.RFC3339Nano),
		Status:       status,
		CreatedAt:    at.Format(time.RFC3339Nano),
		ExpiresAt:    at.Add(6 * time.Hour).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("InsertProactiveCandidate(%s): %v", id, err)
	}
}

// The injector is what makes this feature worth having even when the gate never
// lets the agent speak: it did not interrupt, but it knows what you were doing.
func TestInjectorRendersSkippedCandidates(t *testing.T) {
	db := runnerTestDB(t)
	now := time.Now()
	seedCandidateWithSummary(t, db, "c1", "反复跑 go test，终端持续报错", now, storage.ProactiveCandidateStatusSkipped)

	block := NewInjector(db).Block(context.Background(), config.DefaultProactiveConfig(), "default")
	if !strings.Contains(block, "反复跑 go test") {
		t.Fatalf("block = %q, want the candidate summary", block)
	}
	// It must read as background knowledge, not as an instruction to bring it up.
	if !strings.Contains(block, "不必主动提起") {
		t.Fatalf("block = %q, want the do-not-volunteer framing", block)
	}
}

func TestInjectorIncludesPendingAndSkipped(t *testing.T) {
	db := runnerTestDB(t)
	now := time.Now()
	seedCandidateWithSummary(t, db, "c1", "写代码", now, storage.ProactiveCandidateStatusPending)
	seedCandidateWithSummary(t, db, "c2", "看视频", now, storage.ProactiveCandidateStatusSkipped)

	block := NewInjector(db).Block(context.Background(), config.DefaultProactiveConfig(), "default")
	if !strings.Contains(block, "写代码") || !strings.Contains(block, "看视频") {
		t.Fatalf("block = %q, want both candidates", block)
	}
}

// Consumed candidates already produced a message; repeating them as ambient
// context would make the agent bring up the same thing twice.
func TestInjectorExcludesConsumedCandidates(t *testing.T) {
	db := runnerTestDB(t)
	now := time.Now()
	seedCandidateWithSummary(t, db, "c1", "已经说过的事", now, storage.ProactiveCandidateStatusConsumed)

	block := NewInjector(db).Block(context.Background(), config.DefaultProactiveConfig(), "default")
	if block != "" {
		t.Fatalf("block = %q, want empty when only consumed candidates exist", block)
	}
}

func TestInjectorExcludesCandidatesOlderThanTTL(t *testing.T) {
	db := runnerTestDB(t)
	old := time.Now().Add(-10 * time.Hour)
	seedCandidateWithSummary(t, db, "c1", "很久以前的事", old, storage.ProactiveCandidateStatusSkipped)

	cfg := config.DefaultProactiveConfig()
	cfg.Candidates.TTLHours = 6
	block := NewInjector(db).Block(context.Background(), cfg, "default")
	if block != "" {
		t.Fatalf("block = %q, want empty past the TTL", block)
	}
}

func TestInjectorReturnsEmptyWithoutCandidates(t *testing.T) {
	db := runnerTestDB(t)
	if block := NewInjector(db).Block(context.Background(), config.DefaultProactiveConfig(), "default"); block != "" {
		t.Fatalf("block = %q, want empty", block)
	}
}

// The block competes with memory and history for the context window.
func TestInjectorRespectsCharBudget(t *testing.T) {
	db := runnerTestDB(t)
	now := time.Now()
	long := strings.Repeat("很长的活动摘要", 40)
	for _, id := range []string{"c1", "c2", "c3", "c4", "c5"} {
		seedCandidateWithSummary(t, db, id, long, now, storage.ProactiveCandidateStatusSkipped)
	}

	injector := NewInjector(db)
	injector.SetBudget(400, 8)
	block := injector.Block(context.Background(), config.DefaultProactiveConfig(), "default")

	if block == "" {
		t.Fatal("block = empty, want a truncated block")
	}
	if len(block) > 500 {
		t.Fatalf("block is %d bytes, want it truncated near the 400 budget", len(block))
	}
}

func TestInjectorIsPersonaScoped(t *testing.T) {
	db := runnerTestDB(t)
	now := time.Now()
	seedCandidateWithSummary(t, db, "c1", "default 的活动", now, storage.ProactiveCandidateStatusSkipped)

	block := NewInjector(db).Block(context.Background(), config.DefaultProactiveConfig(), "neko")
	if block != "" {
		t.Fatalf("block = %q, want empty for a different persona", block)
	}
}
