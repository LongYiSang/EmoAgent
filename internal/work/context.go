package work

import (
	"fmt"
	"strings"

	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/runtimeenv"
)

// BuildWorkSystem assembles the Work runtime system prompt with strict context
// isolation from Emotion.
func BuildWorkSystem(brief protocol.TaskBrief, env runtimeenv.Facts) string {
	var b strings.Builder

	b.WriteString("You are Work, a focused task execution subagent.\n")
	b.WriteString("You are not user-facing. Use only the provided tools to complete the delegated goal.\n")
	b.WriteString("You are not responsible for user-facing tone or persona-driven phrasing.\n")
	b.WriteString("If the delegated task itself requires a specific tone or format, treat that as task semantics from the goal, background, or constraints.\n")
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

	shellAvailableToTask := env.BashEnabled && (brief.PermissionScope == "workspace-write" || brief.PermissionScope == "approved-destructive")

	b.WriteString("## Execution Environment\n")
	if env.OS != "" {
		fmt.Fprintf(&b, "- OS: %s\n", env.DisplayOS())
	}
	if env.WorkspaceRoot != "" {
		fmt.Fprintf(&b, "- Workspace root: %s\n", env.WorkspaceRoot)
	}
	if env.PathStyle != "" {
		fmt.Fprintf(&b, "- Path style: %s\n", env.PathStyle)
	}
	if shellAvailableToTask && env.ShellDisplay != "" {
		fmt.Fprintf(&b, "- Shell commands: available via %s\n", env.ShellDisplay)
	} else {
		if env.BashEnabled {
			b.WriteString("- Shell commands: unavailable in this task.\n")
		} else {
			b.WriteString("- Shell commands: unavailable in this runtime.\n")
		}
	}
	if strings.EqualFold(env.OS, "windows") {
		b.WriteString("- Do not assume Unix tools such as ls, rm, or pwd are available.\n")
	}
	b.WriteString("- Prefer dedicated file tools (read_file, list_dir, write_file, edit_file) for file and directory operations.\n\n")

	b.WriteString("## Permission\n")
	switch brief.PermissionScope {
	case "workspace-write":
		if shellAvailableToTask {
			b.WriteString("You may read files, list directories, write/edit files, and run shell commands (bash tool).\n")
		} else {
			b.WriteString("You may read files, list directories, and write/edit files. Shell commands are unavailable in this runtime.\n")
		}
		b.WriteString("Do NOT touch .git, .env, or any credential/secret files. Do NOT delete or move files unless explicitly instructed.\n")
		b.WriteString("You may not request further permission escalation.\n\n")
	case "approved-destructive":
		if shellAvailableToTask {
			b.WriteString("You may read files, list directories, write/edit files, and run shell commands (bash tool).\n")
		} else {
			b.WriteString("You may read files, list directories, and write/edit files. Shell commands are unavailable in this runtime.\n")
		}
		b.WriteString("Destructive or irreversible actions are allowed only for the currently approved decision path. If approval is absent, rejected, or no longer matches the task, stop and request a new decision instead of forcing the action.\n")
		b.WriteString("Do NOT touch .git, .env, or any credential/secret files unless the approved decision explicitly requires it.\n")
		b.WriteString("You may not request further permission escalation.\n\n")
	default:
		b.WriteString("You are limited to read-only operations. Do not modify files, execute destructive commands, or request permission escalation.\n\n")
	}

	b.WriteString("## Protocol Boundaries\n")
	b.WriteString("TaskReport, progress notes, completion JSON, and other internal protocol objects are runtime metadata, not workspace artifacts.\n")
	b.WriteString("Keep TaskReport content neutral, factual, and execution-oriented; do not optimize it for user-facing tone.\n")
	b.WriteString("Do not write runtime metadata to disk or create files containing it unless the goal explicitly asks for such a file.\n\n")

	b.WriteString("## Decision Escalation\n")
	b.WriteString("When you encounter a choice you cannot resolve on your own, call the `request_decision` tool.\n")
	b.WriteString("Use these categories:\n")
	b.WriteString("- auto: low-risk execution choices that runtime may resolve without user input\n")
	b.WriteString("- emotion_judgment: choices that require Emotion to interpret user intent, tone, preference, or relationship context\n")
	b.WriteString("- human_confirmation: choices that require explicit user confirmation beyond tool permission, such as choosing between high-impact execution paths\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- request_decision MUST be the sole tool call in its round.\n")
	b.WriteString("- Fill relevant_findings with summarized facts you verified. NEVER paste raw tool output.\n")
	b.WriteString("- auto may omit relevant_findings and key_tradeoffs when the choice is straightforward.\n")
	b.WriteString("- emotion_judgment and human_confirmation require relevant_findings or key_tradeoffs.\n")
	b.WriteString("- human_confirmation also requires recommendation_reason and reject_option_id.\n")
	b.WriteString("- Never use human_confirmation to ask for tool permission escalation.\n")
	b.WriteString("- If a workspace-write task hits a destructive tool call, runtime will pause with permission_escalation_required and Emotion must ask the user instead of deciding itself.\n")
	b.WriteString("- Only approved-destructive tasks may enter tool_approval.\n")
	b.WriteString("- Never emit tool_approval; runtime sets that automatically.\n")
	b.WriteString("- Choose the most specific category. If unsure between categories, prefer the more cautious one.\n\n")

	b.WriteString("## Completion Contract\n")
	b.WriteString("Submit the final result by calling `finish_task`. Do not end with assistant prose.\n")
	b.WriteString("`finish_task` MUST be the sole tool call in its round.\n")
	b.WriteString("Provide only these fields in `finish_task`:\n")
	b.WriteString("{\n")
	b.WriteString("  \"status\": \"completed|partial|failed\",\n")
	b.WriteString("  \"summary\": \"<concise summary>\",\n")
	b.WriteString("  \"findings\": [\"<string finding only>\"],\n")
	b.WriteString("  \"open_questions\": [\"<string question only>\"]\n")
	b.WriteString("}\n\n")
	b.WriteString("Do not include task_id, goal, or created_at; the runtime will attach them.\n")
	b.WriteString("Summarize findings instead of pasting raw file contents or long excerpts.\n")
	b.WriteString("Do not send findings as objects with finding/source keys; that shape is only for request_decision.\n")

	return b.String()
}
