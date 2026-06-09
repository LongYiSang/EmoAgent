package agentaffect

import (
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
)

func TestClampMoodDeltaLimitsProposedDeltas(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect

	result := ClampMoodDelta(cfg, MoodVector{Arousal: 0.5, Frustration: 0.2}, MoodVector{
		Valence:     0.9,
		Arousal:     -0.9,
		Attachment:  0.9,
		Frustration: -0.9,
	}, ClampOptions{CommittedBy: "core"})

	if result.ClampedDelta.Valence != 0.15 {
		t.Fatalf("valence delta = %v, want 0.15", result.ClampedDelta.Valence)
	}
	if result.ClampedDelta.Arousal != -0.18 {
		t.Fatalf("arousal delta = %v, want -0.18", result.ClampedDelta.Arousal)
	}
	if result.ClampedDelta.Attachment != 0.08 {
		t.Fatalf("attachment delta = %v, want 0.08", result.ClampedDelta.Attachment)
	}
	if result.ClampedDelta.Frustration != -0.08 {
		t.Fatalf("frustration delta = %v, want -0.08", result.ClampedDelta.Frustration)
	}
	if len(result.Notes) == 0 {
		t.Fatal("clamp notes empty, want notes for capped deltas")
	}
}

func TestClampMoodDeltaAppliesAbsoluteAttachmentAndFrustrationBounds(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	before := MoodVector{Attachment: 0.74, Frustration: 0.34}

	result := ClampMoodDelta(cfg, before, MoodVector{
		Attachment:  0.08,
		Frustration: 0.08,
	}, ClampOptions{CommittedBy: "core"})

	if result.PredictedState.Attachment != 0.75 {
		t.Fatalf("attachment = %v, want absolute max 0.75", result.PredictedState.Attachment)
	}
	if result.ClampedDelta.Attachment != 0.01 {
		t.Fatalf("attachment committed delta = %v, want 0.01", result.ClampedDelta.Attachment)
	}
	if result.PredictedState.Frustration != 0.35 {
		t.Fatalf("frustration = %v, want absolute max 0.35", result.PredictedState.Frustration)
	}
	if result.ClampedDelta.Frustration != 0.01 {
		t.Fatalf("frustration committed delta = %v, want 0.01", result.ClampedDelta.Frustration)
	}
	if !strings.Contains(strings.Join(result.Notes, "\n"), "attachment") {
		t.Fatalf("notes = %#v, want attachment clamp note", result.Notes)
	}
}

func TestClampMoodDeltaAppliesPluginMultiplier(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	cfg.Limits.PluginDeltaMultiplier = 0.5

	result := ClampMoodDelta(cfg, MoodVector{}, MoodVector{
		Valence: 0.14,
		Warmth:  0.10,
	}, ClampOptions{CommittedBy: "plugin"})

	if result.ClampedDelta.Valence != 0.07 {
		t.Fatalf("plugin valence delta = %v, want 0.07", result.ClampedDelta.Valence)
	}
	if result.ClampedDelta.Warmth != 0.05 {
		t.Fatalf("plugin warmth delta = %v, want 0.05", result.ClampedDelta.Warmth)
	}
}
