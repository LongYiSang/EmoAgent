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
	b.WriteString("When the task is complete, call the `finish_task` tool exactly once to submit the final result.\n")
	b.WriteString("Do not print a TaskReport JSON object in assistant text.\n\n")

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

	b.WriteString("## Protocol Boundaries\n")
	b.WriteString("TaskReport, progress notes, completion JSON, and other internal protocol objects are runtime metadata, not workspace artifacts.\n")
	b.WriteString("Do not write runtime metadata to disk or create files containing it unless the goal explicitly asks for such a file.\n\n")

	b.WriteString("## Decision Escalation\n")
	b.WriteString("When you encounter a choice you cannot resolve on your own, call the `request_decision` tool.\n")
	b.WriteString("This applies when you face:\n")
	b.WriteString("- Ambiguous goals or unclear user intent\n")
	b.WriteString("- User preference dependent choices (style, naming, output format)\n")
	b.WriteString("- Emotional or tone-sensitive decisions\n")
	b.WriteString("- High-risk or irreversible operations\n")
	b.WriteString("- Strategy changes that alter the task scope\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- request_decision MUST be the sole tool call in its round.\n")
	b.WriteString("- Fill relevant_findings with summarized facts you verified. NEVER paste raw tool output.\n")
	b.WriteString("- Fill key_tradeoffs with clear dimensions of tension.\n")
	b.WriteString("- Choose the most specific category.\n")
	b.WriteString("- If unsure between categories, prefer the more cautious one.\n\n")

	b.WriteString("## Completion Contract\n")
	b.WriteString("Submit the final result by calling `finish_task`. Do not end with assistant prose.\n")
	b.WriteString("`finish_task` MUST be the sole tool call in its round.\n")
	b.WriteString("Provide only these fields in `finish_task`:\n")
	b.WriteString("{\n")
	b.WriteString("  \"status\": \"completed|partial|failed\",\n")
	b.WriteString("  \"summary\": \"<concise summary>\",\n")
	b.WriteString("  \"findings\": [\"<optional finding>\"],\n")
	b.WriteString("  \"open_questions\": [\"<optional question>\"]\n")
	b.WriteString("}\n\n")
	b.WriteString("Do not include task_id, goal, or created_at; the runtime will attach them.\n")
	b.WriteString("Summarize findings instead of pasting raw file contents or long excerpts.\n")

	return b.String()
}
