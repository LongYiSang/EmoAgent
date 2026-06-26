package agentaffect

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

func TestApplyStateDecayUsesProfileBaselineHalfLife(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	cfg.State.DecayHalfLifeSeconds = 100
	baseline := MoodVector{Warmth: 0.2, Energy: 0.4}
	stored := MoodSnapshot{
		Vector:    MoodVector{Warmth: 1.0, Energy: 0.8},
		UpdatedAt: time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC),
	}

	oneHalfLife := applyStateDecay(cfg, stored, baseline, stored.UpdatedAt.Add(100*time.Second))
	twoHalfLives := applyStateDecay(cfg, stored, baseline, stored.UpdatedAt.Add(200*time.Second))

	assertNear(t, oneHalfLife.Vector.Warmth, 0.6)
	assertNear(t, oneHalfLife.Vector.Energy, 0.6)
	assertNear(t, twoHalfLives.Vector.Warmth, 0.4)
	assertNear(t, twoHalfLives.Vector.Energy, 0.5)
}

func TestUpdateCauseTraceMergesPrunesAndBoundsItems(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	cfg.State.CauseStackMaxItems = 5
	cfg.State.CauseHalfLifeSeconds = 3600
	cfg.State.CauseMinWeight = 0.05
	now := time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC)
	old := []CauseContributor{
		{Kind: "same", Summary: "old same", Weight: 0.4, Confidence: 0.5, OccurredAt: now.Add(-time.Minute)},
		{Kind: "low", Summary: "drops", Weight: 0.01, OccurredAt: now.Add(-time.Minute)},
		{Kind: "a", Summary: "a", Weight: 0.7, OccurredAt: now.Add(-2 * time.Minute)},
		{Kind: "b", Summary: "b", Weight: 0.6, OccurredAt: now.Add(-3 * time.Minute)},
		{Kind: "c", Summary: "c", Weight: 0.5, OccurredAt: now.Add(-4 * time.Minute)},
		{Kind: "d", Summary: "d", Weight: 0.3, OccurredAt: now.Add(-5 * time.Minute)},
	}
	result := LLMEvaluationResult{
		HasAppraisal: true,
		Appraisal:    AffectAppraisal{EventSignificance: 0.9},
		Cause:        AffectCauseProposal{Code: "same", Summary: strings.Repeat("new ", 80), VisibleSummary: strings.Repeat("visible ", 80)},
		Confidence:   0.8,
	}

	next := updateCauseTrace(cfg, old, result, MoodVector{Warmth: 0.12}, now)

	if len(next) != 5 {
		t.Fatalf("trace length = %d, want 5", len(next))
	}
	if next[0].Kind != "same" {
		t.Fatalf("top cause = %#v, want merged same", next[0])
	}
	if next[0].Weight <= 0.9 || next[0].Weight > 1 {
		t.Fatalf("merged weight = %v, want high bounded weight", next[0].Weight)
	}
	if len([]rune(next[0].Summary)) > 120 {
		t.Fatalf("summary too long: %d", len([]rune(next[0].Summary)))
	}
	for _, item := range next {
		if item.Kind == "low" {
			t.Fatalf("low weight item survived: %#v", next)
		}
		if strings.Contains(item.Summary, "用户原文") {
			t.Fatalf("trace leaked raw text: %#v", item)
		}
	}
}

func assertNear(t *testing.T, got float64, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.000001 {
		t.Fatalf("got %v, want %v", got, want)
	}
}
