package agentaffect

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/storage"
	_ "modernc.org/sqlite"
)

type fakeEvaluator struct {
	result LLMEvaluationResult
	err    error
}

func (f fakeEvaluator) Evaluate(context.Context, LLMEvaluationRequest) (LLMEvaluationResult, error) {
	return f.result, f.err
}

func newTestRuntime(t *testing.T, cfg config.AgentAffectConfig, evaluator Evaluator) (*Runtime, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.ApplyMigrations(db); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	store := NewSQLiteStore(db)
	rt := NewRuntime(RuntimeOptions{
		Config:    cfg,
		Store:     store,
		Evaluator: evaluator,
		Logger:    slog.Default(),
		Now:       func() time.Time { return time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC) },
	})
	return rt, db
}

func TestSubmitMoodImpactWithFakeEvaluatorWritesEvaluationStateAndEvent(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	cfg.Enabled = true
	rt, db := newTestRuntime(t, cfg, fakeEvaluator{result: LLMEvaluationResult{
		Delta:               MoodVector{Valence: 0.4, Attachment: 0.2},
		Label:               "warmer",
		CauseSummary:        "User expressed appreciation.",
		VisibleCauseSummary: "User expressed appreciation.",
		Confidence:          0.9,
		Status:              EvaluationStatusPreview,
	}})

	resp, err := rt.SubmitMoodImpact(context.Background(), SubmitMoodImpactRequest{
		PersonaID:  "default",
		SessionID:  "session-1",
		TurnID:     "turn-1",
		Trigger:    TriggerDescriptor{TriggerType: "user_message", SourceKind: "turn", SourceRefType: "episode", SourceRefID: "episode-1"},
		Input:      MoodImpactInput{Mode: "raw", Text: "thanks for helping"},
		CommitMode: CommitModeCommitIfAllowed,
	})
	if err != nil {
		t.Fatalf("SubmitMoodImpact: %v", err)
	}
	if resp.Mood.StateID == "" || resp.EvaluationID == "" || resp.EventID == "" {
		t.Fatalf("missing ids in response: %#v", resp)
	}
	if resp.Mood.Vector.Valence != 0.15 {
		t.Fatalf("valence = %v, want clamped 0.15", resp.Mood.Vector.Valence)
	}
	if resp.Mood.Vector.Attachment != 0.08 {
		t.Fatalf("attachment = %v, want clamped 0.08", resp.Mood.Vector.Attachment)
	}
	for table, want := range map[string]int{
		"agent_affect_profiles":    1,
		"agent_affect_states":      1,
		"agent_affect_evaluations": 1,
		"agent_affect_events":      1,
	} {
		var got int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s count = %d, want %d", table, got, want)
		}
	}
}

func TestEvaluateMoodImpactPreviewDoesNotCommitState(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	cfg.Enabled = true
	rt, db := newTestRuntime(t, cfg, fakeEvaluator{result: LLMEvaluationResult{
		Delta:        MoodVector{Warmth: 0.1},
		CauseSummary: "Preview only.",
		Confidence:   0.7,
		Status:       EvaluationStatusPreview,
	}})

	resp, err := rt.EvaluateMoodImpact(context.Background(), EvaluateMoodImpactRequest{
		PersonaID: "default",
		SessionID: "session-1",
		Trigger:   TriggerDescriptor{TriggerType: "debug"},
		Input:     MoodImpactInput{Mode: "summary", Summary: "preview"},
	})
	if err != nil {
		t.Fatalf("EvaluateMoodImpact: %v", err)
	}
	if resp.EvaluationID == "" {
		t.Fatalf("missing evaluation id: %#v", resp)
	}
	var states int
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_states").Scan(&states); err != nil {
		t.Fatalf("count states: %v", err)
	}
	if states != 0 {
		t.Fatalf("states count = %d, want 0 for preview", states)
	}
}

func TestDisabledAgentAffectReturnsNoChangeWithoutWrites(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	cfg.Enabled = false
	rt, db := newTestRuntime(t, cfg, fakeEvaluator{result: LLMEvaluationResult{
		Delta: MoodVector{Valence: 0.5},
	}})

	resp, err := rt.SubmitMoodImpact(context.Background(), SubmitMoodImpactRequest{
		PersonaID:  "default",
		SessionID:  "session-1",
		Trigger:    TriggerDescriptor{TriggerType: "user_message"},
		Input:      MoodImpactInput{Mode: "raw", Text: "hello"},
		CommitMode: CommitModeCommitIfAllowed,
	})
	if err != nil {
		t.Fatalf("SubmitMoodImpact disabled: %v", err)
	}
	if !resp.NoChange || resp.Mood.Label != "baseline" {
		t.Fatalf("disabled response = %#v, want no-change baseline", resp)
	}
	var states int
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_states").Scan(&states); err != nil {
		t.Fatalf("count states: %v", err)
	}
	if states != 0 {
		t.Fatalf("states count = %d, want 0 when disabled", states)
	}
}

func TestApplyMoodDeltaRejectsInvalidCommittedByWithoutWrites(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	cfg.Enabled = true
	rt, db := newTestRuntime(t, cfg, fakeEvaluator{})

	_, err := rt.ApplyMoodDelta(context.Background(), ApplyMoodDeltaRequest{
		PersonaID:   "default",
		SessionID:   "session-1",
		Trigger:     TriggerDescriptor{TriggerType: "debug"},
		Delta:       MoodVector{Valence: 0.9},
		CommittedBy: "smoke_test",
	})
	if err == nil {
		t.Fatal("ApplyMoodDelta error = nil, want invalid committed_by error")
	}
	for table, want := range map[string]int{
		"agent_affect_states": 0,
		"agent_affect_events": 0,
	} {
		var got int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Fatalf("%s count = %d, want %d", table, got, want)
		}
	}
}

func TestPluginAPISubmitAuditsPluginWrite(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	cfg.Enabled = true
	rt, db := newTestRuntime(t, cfg, fakeEvaluator{result: LLMEvaluationResult{
		Delta:        MoodVector{Warmth: 0.1},
		CauseSummary: "Plugin submitted event.",
		Confidence:   0.7,
		Status:       EvaluationStatusPreview,
	}})
	api := NewPluginAPI(rt, NewSQLiteStore(db))

	resp, err := api.SubmitMoodImpact(context.Background(), "com.example.affect", SubmitMoodImpactRequest{
		PersonaID:  "default",
		SessionID:  "session-1",
		Trigger:    TriggerDescriptor{TriggerType: "plugin_signal"},
		Input:      MoodImpactInput{Mode: "summary", Summary: "plugin signal"},
		CommitMode: CommitModeCommitIfAllowed,
	})
	if err != nil {
		t.Fatalf("SubmitMoodImpact: %v", err)
	}
	if resp.EventID == "" {
		t.Fatalf("missing event id: %#v", resp)
	}
	var pluginID, capability string
	if err := db.QueryRow("SELECT plugin_id, capability FROM agent_affect_plugin_writes").Scan(&pluginID, &capability); err != nil {
		t.Fatalf("read plugin write: %v", err)
	}
	if pluginID != "com.example.affect" || capability != "agent_affect.submit" {
		t.Fatalf("plugin write = %q/%q", pluginID, capability)
	}
}
