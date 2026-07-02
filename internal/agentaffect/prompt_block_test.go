package agentaffect

import (
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

func TestFormatPromptAffectBlockNaturalSummaryOmitsNumericVectorByDefault(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	snapshot := MoodSnapshot{
		StateID:             "state-1",
		PersonaID:           "default",
		SessionID:           "session-1",
		Label:               "attentive",
		Confidence:          0.8,
		MoodDescription:     "温和、专注",
		MoodReason:          "用户分享了一个有压力的节点",
		PromptMoodText:      "温和专注，带一点关切；表达上轻柔、少打扰。",
		VisibleCauseSummary: "用户分享了一个有压力的节点。",
		Vector: MoodVector{
			Valence:    0.1,
			Warmth:     0.7,
			Attachment: 0.62,
		},
		UpdatedAt: time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC),
	}

	block := FormatPromptAffectBlock(cfg, snapshot)

	for _, want := range []string{
		"[Agent Mood]",
		"当前模拟心情：温和专注，带一点关切；表达上轻柔、少打扰。",
		"这是内部表达背景",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("prompt block missing %q:\n%s", want, block)
		}
	}
	for _, forbidden := range []string{"mood_vector", "valence", "attachment_expression"} {
		if strings.Contains(block, forbidden) {
			t.Fatalf("prompt block should not include %q by default:\n%s", forbidden, block)
		}
	}
}

func TestFormatPromptAffectBlockNumericDebugIncludesVector(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	cfg.Prompt.Mode = "numeric_debug"
	cfg.Prompt.IncludeNumericValues = true
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
		"label: attentive",
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

func TestPromptMoodTextStripsLegacyLabelPrefix(t *testing.T) {
	got := promptMoodText(MoodSnapshot{
		Label:          "playful_caring_weather_sleep_reminder",
		PromptMoodText: "playful_caring_weather_sleep_reminder；轻松亲近，略带关切；表达上轻柔、少打扰。",
	})
	if got != "轻松亲近，略带关切；表达上轻柔、少打扰。" {
		t.Fatalf("prompt mood text = %q", got)
	}
}

func TestPromptMoodTextFallsBackToVisibleCauseWithoutLabel(t *testing.T) {
	got := promptMoodText(MoodSnapshot{
		Label:               "steady",
		VisibleCauseSummary: "No meaningful affective change.",
	})
	if got != "No meaningful affective change." {
		t.Fatalf("prompt mood text = %q", got)
	}
}

func TestFormatPromptAffectBlockUsesDefaultWhenOnlyLabelAvailable(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	block := FormatPromptAffectBlock(cfg, MoodSnapshot{Label: "steady"})

	if !strings.Contains(block, "当前模拟心情：平稳、接近基线。") {
		t.Fatalf("prompt block missing default baseline text:\n%s", block)
	}
	if strings.Contains(block, "当前模拟心情：steady") {
		t.Fatalf("prompt block leaked label:\n%s", block)
	}
}
