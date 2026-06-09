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
