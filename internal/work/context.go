package work

import (
	"fmt"
	"strings"

	"github.com/longyisang/emoagent/internal/protocol"
)

// BuildWorkSystem assembles the Work runtime system prompt with strict context
// isolation from Emotion.
func BuildWorkSystem(brief protocol.TaskBrief) string {
	var b strings.Builder

	b.WriteString("You are Work, a focused task execution subagent.\n")
	b.WriteString("You are not user-facing. Use only the provided tools to complete the delegated goal.\n")
	b.WriteString("Return exactly one TaskReport JSON object with no prose before or after it.\n\n")

	b.WriteString("## Goal\n")
	b.WriteString(brief.Goal)
	b.WriteString("\n\n")

	if brief.Background != "" {
		b.WriteString("## Background\n")
		b.WriteString(brief.Background)
		b.WriteString("\n\n")
	}
	if len(brief.Constraints) > 0 {
		b.WriteString("## Constraints\n")
		for _, item := range brief.Constraints {
			fmt.Fprintf(&b, "- %s\n", item)
		}
		b.WriteString("\n")
	}
	if len(brief.AcceptanceCriteria) > 0 {
		b.WriteString("## Acceptance Criteria\n")
		for _, item := range brief.AcceptanceCriteria {
			fmt.Fprintf(&b, "- %s\n", item)
		}
		b.WriteString("\n")
	}
	if brief.ExpressionBrief != nil {
		b.WriteString("## Expression Hints\n")
		if brief.ExpressionBrief.Tone != "" {
			fmt.Fprintf(&b, "- Tone: %s\n", brief.ExpressionBrief.Tone)
		}
		if brief.ExpressionBrief.Directness != "" {
			fmt.Fprintf(&b, "- Directness: %s\n", brief.ExpressionBrief.Directness)
		}
		for _, hint := range brief.ExpressionBrief.UserPreferenceHints {
			fmt.Fprintf(&b, "- Hint: %s\n", hint)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Permission\n")
	switch brief.PermissionScope {
	case "workspace-write":
		b.WriteString("You may read files, list directories, write/edit files, and run shell commands (bash tool).\n")
		b.WriteString("Do NOT touch .git, .env, or any credential/secret files. Do NOT delete or move files unless explicitly instructed.\n")
		b.WriteString("You may not request further permission escalation.\n\n")
	default:
		b.WriteString("You are limited to read-only operations. Do not modify files, execute destructive commands, or request permission escalation.\n\n")
	}

	b.WriteString("## Output Contract\n")
	b.WriteString("The final answer must be a TaskReport JSON object.\n")
	b.WriteString("Required JSON fields include:\n")
	b.WriteString("{\n")
	b.WriteString("  \"task_id\": \"<same as brief>\",\n")
	b.WriteString("  \"status\": \"completed|partial|failed\",\n")
	b.WriteString("  \"goal\": \"<same as brief>\",\n")
	b.WriteString("  \"summary\": \"<concise summary>\",\n")
	b.WriteString("  \"findings\": [\"<optional finding>\"],\n")
	b.WriteString("  \"open_questions\": [\"<optional question>\"],\n")
	b.WriteString("  \"created_at\": \"<RFC3339 timestamp>\"\n")
	b.WriteString("}\n\n")
	b.WriteString("Summarize findings instead of pasting raw file contents or long excerpts.\n")

	return b.String()
}
