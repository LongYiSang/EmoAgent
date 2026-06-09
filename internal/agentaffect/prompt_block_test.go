package agentaffect

import (
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

func TestFormatPromptAffectBlockIncludesMoodCauseAndAttachmentExpression(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	snapshot := MoodSnapshot{
		StateID:      "state-1",
		PersonaID:    "default",
		SessionID:    "session-1",
		Label:        "attentive",
		Confidence:   0.8,
		CauseSummary: "User shared a stressful milestone.",
		Vector: MoodVector{
			Valence:    0.1,
			Warmth:     0.7,
			Attachment: 0.62,
		},
		UpdatedAt: time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC),
	}

	block := FormatPromptAffectBlock(cfg, snapshot)

	for _, want := range []string{
		"[Agent Affect Runtime State]",
		"mood_vector:",
		"valence: 0.100",
		"attachment: 0.620",
		"cause_summary: User shared a stressful milestone.",
		"attachment_expression:",
		"gentle_explicit",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("prompt block missing %q:\n%s", want, block)
		}
	}
}

func TestFormatPromptAffectBlockOmitsRawInput(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	snapshot := MoodSnapshot{
		PersonaID:    "default",
		SessionID:    "session-1",
		CauseSummary: "Visible summary only.",
		Vector:       MoodVector{Warmth: 0.5},
		UpdatedAt:    time.Now(),
	}

	block := FormatPromptAffectBlock(cfg, snapshot)
	if strings.Contains(block, "raw input") {
		t.Fatalf("prompt block should not include raw input: %s", block)
	}
}
