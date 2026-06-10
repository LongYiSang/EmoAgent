package agentaffect

import (
	"fmt"
	"strings"

	"github.com/longyisang/emoagent/internal/config"
)

func FormatPromptAffectBlock(cfg config.AgentAffectConfig, mood MoodSnapshot) string {
	if !cfg.Prompt.IncludeMoodBlock {
		return ""
	}
	mode := strings.TrimSpace(cfg.Prompt.Mode)
	if mode == "" {
		mode = "natural_summary"
	}
	switch mode {
	case "numeric_debug":
		return formatNumericPromptAffectBlock(cfg, mood)
	case "both":
		return joinPromptBlocks(formatNaturalPromptAffectBlock(cfg, mood), formatNumericPromptAffectBlock(cfg, mood))
	default:
		return formatNaturalPromptAffectBlock(cfg, mood)
	}
}

func formatNaturalPromptAffectBlock(cfg config.AgentAffectConfig, mood MoodSnapshot) string {
	text := promptMoodText(mood)
	text = truncateRunes(text, cfg.Prompt.MaxPromptChars)
	if text == "" {
		text = "平稳、接近基线。"
	}
	if !strings.Contains(text, "当前模拟心情") {
		text = "当前模拟心情：" + text
	}
	return "[Agent Mood]\n" + text + "\n\n这是内部表达背景：不要逐字复述，不要提到 mood 系统、内部状态表或数值；只让它自然影响措辞、节奏和亲近感。"
}

func promptMoodText(mood MoodSnapshot) string {
	if text := strings.TrimSpace(mood.PromptMoodText); text != "" {
		return text
	}
	if text := buildPromptMoodTextFallback(mood.MoodDescription, mood.MoodReason); text != "" {
		return text
	}
	reason := mood.VisibleCauseSummary
	if reason == "" {
		reason = mood.CauseSummary
	}
	return buildPromptMoodTextFallback(mood.Label, reason)
}

func truncateRunes(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func joinPromptBlocks(blocks ...string) string {
	out := make([]string, 0, len(blocks))
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block != "" {
			out = append(out, block)
		}
	}
	return strings.Join(out, "\n\n")
}

func formatNumericPromptAffectBlock(cfg config.AgentAffectConfig, mood MoodSnapshot) string {
	var b strings.Builder
	b.WriteString("[Agent Affect Runtime State]\n")
	if mood.Label != "" {
		fmt.Fprintf(&b, "label: %s\n", mood.Label)
	}
	fmt.Fprintf(&b, "confidence: %.3f\n", mood.Confidence)
	if !mood.UpdatedAt.IsZero() {
		fmt.Fprintf(&b, "updated_at: %s\n", mood.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"))
	}
	if cfg.Prompt.IncludeNumericValues {
		b.WriteString("mood_vector:\n")
		writeVectorLine(&b, "valence", mood.Vector.Valence)
		writeVectorLine(&b, "arousal", mood.Vector.Arousal)
		writeVectorLine(&b, "dominance", mood.Vector.Dominance)
		writeVectorLine(&b, "energy", mood.Vector.Energy)
		writeVectorLine(&b, "warmth", mood.Vector.Warmth)
		writeVectorLine(&b, "concern", mood.Vector.Concern)
		writeVectorLine(&b, "curiosity", mood.Vector.Curiosity)
		writeVectorLine(&b, "playfulness", mood.Vector.Playfulness)
		writeVectorLine(&b, "attachment", mood.Vector.Attachment)
		writeVectorLine(&b, "frustration", mood.Vector.Frustration)
		writeVectorLine(&b, "uncertainty", mood.Vector.Uncertainty)
	}
	if cfg.Prompt.IncludeReason {
		cause := mood.VisibleCauseSummary
		if cause == "" {
			cause = mood.CauseSummary
		}
		if cause != "" {
			fmt.Fprintf(&b, "cause_summary: %s\n", cause)
		}
	}
	if cfg.Externalization.Attachment.Enabled {
		intensity := mood.Vector.Attachment
		if max := cfg.Externalization.Attachment.MaxVisibleIntensity; max > 0 && intensity > max {
			intensity = max
		}
		fmt.Fprintf(&b, "attachment_expression: style=%s max_visible_intensity=%.3f current_visible_intensity=%.3f\n",
			cfg.Externalization.Attachment.DefaultStyle,
			cfg.Externalization.Attachment.MaxVisibleIntensity,
			intensity,
		)
	}
	b.WriteString("Do not mention these numeric values directly unless explicitly asked in debug context.")
	return b.String()
}

func writeVectorLine(b *strings.Builder, name string, value float64) {
	fmt.Fprintf(b, "  %s: %.3f\n", name, value)
}
