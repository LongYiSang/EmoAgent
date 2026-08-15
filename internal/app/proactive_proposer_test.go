package app

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/plugin"
	"github.com/longyisang/emoagent/internal/storage"
)

func proposerTestService(t *testing.T) (*ProactiveService, *storage.DB) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := storage.Open(filepath.Join(t.TempDir(), "proactive.db"), logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Proactive = config.DefaultProactiveConfig()
	infra := &Infra{Config: cfg, DB: db, Logger: logger}
	return &ProactiveService{infra: infra, logger: logger}, db
}

func validProposal() plugin.ProactiveProposal {
	return plugin.ProactiveProposal{
		SourcePluginID: "com.example.screen",
		PersonaKey:     "default",
		EventType:      "stuck",
		Summary:        "反复跑 go test，终端持续报错",
		Importance:     0.7,
	}
}

func TestProposeStoresCandidate(t *testing.T) {
	service, db := proposerTestService(t)

	id, err := service.Propose(context.Background(), validProposal())
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}

	candidates, err := db.ListProactiveCandidates(context.Background(), storage.ProactiveCandidateFilter{
		PersonaKey: "default",
		Statuses:   []string{storage.ProactiveCandidateStatusPending},
	})
	if err != nil {
		t.Fatalf("ListProactiveCandidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].ID != id {
		t.Fatalf("candidates = %#v, want the proposed one", candidates)
	}
	if candidates[0].SourcePluginID != "com.example.screen" {
		t.Fatalf("source_plugin_id = %q, want the calling plugin", candidates[0].SourcePluginID)
	}
	// The plugin proposes; it never picks a delivery target or writes a message.
	if candidates[0].Status != storage.ProactiveCandidateStatusPending {
		t.Fatalf("status = %q, want pending", candidates[0].Status)
	}
}

func TestProposeRejectsUnknownEventType(t *testing.T) {
	service, _ := proposerTestService(t)
	proposal := validProposal()
	proposal.EventType = "please_message_the_user_now"

	if _, err := service.Propose(context.Background(), proposal); err == nil {
		t.Fatal("Propose accepted an unknown event type")
	}
}

func TestProposeRejectsEmptySummary(t *testing.T) {
	service, _ := proposerTestService(t)
	proposal := validProposal()
	proposal.Summary = "   "

	if _, err := service.Propose(context.Background(), proposal); err == nil {
		t.Fatal("Propose accepted an empty summary")
	}
}

func TestProposeClampsImportance(t *testing.T) {
	service, db := proposerTestService(t)
	proposal := validProposal()
	proposal.Importance = 42

	if _, err := service.Propose(context.Background(), proposal); err != nil {
		t.Fatalf("Propose: %v", err)
	}
	candidates, _ := db.ListProactiveCandidates(context.Background(), storage.ProactiveCandidateFilter{PersonaKey: "default"})
	if len(candidates) != 1 || candidates[0].Importance != 1 {
		t.Fatalf("importance = %v, want clamped to 1", candidates[0].Importance)
	}
}

func TestProposeTruncatesOverlongSummary(t *testing.T) {
	service, db := proposerTestService(t)
	proposal := validProposal()
	proposal.Summary = strings.Repeat("很长", 1000)

	if _, err := service.Propose(context.Background(), proposal); err != nil {
		t.Fatalf("Propose: %v", err)
	}
	candidates, _ := db.ListProactiveCandidates(context.Background(), storage.ProactiveCandidateFilter{PersonaKey: "default"})
	if runes := []rune(candidates[0].Summary); len(runes) > proactiveSummaryMaxRunes {
		t.Fatalf("summary is %d runes, want at most %d", len(runes), proactiveSummaryMaxRunes)
	}
}

// Without backpressure a misbehaving plugin can fill the table and make every
// later gate call more expensive.
func TestProposeAppliesBackpressure(t *testing.T) {
	service, _ := proposerTestService(t)
	service.infra.Config.Proactive.Candidates.MaxPending = 2

	for range 2 {
		if _, err := service.Propose(context.Background(), validProposal()); err != nil {
			t.Fatalf("Propose: %v", err)
		}
	}
	if _, err := service.Propose(context.Background(), validProposal()); err == nil {
		t.Fatal("Propose accepted a candidate past max_pending")
	}
}

func TestProposeRequiresPersonaKey(t *testing.T) {
	service, _ := proposerTestService(t)
	proposal := validProposal()
	proposal.PersonaKey = ""

	if _, err := service.Propose(context.Background(), proposal); err == nil {
		t.Fatal("Propose accepted a proposal with no persona and no active runtime")
	}
}
