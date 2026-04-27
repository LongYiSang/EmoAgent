package work

import (
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/runtimeenv"
)

func TestBuildWorkSystem_NoEmotionLeak(t *testing.T) {
	text := strings.ToLower(BuildWorkSystem(protocol.TaskBrief{
		Goal:            "list dependencies",
		PermissionScope: "read-only",
	}, runtimeenv.Facts{OS: "linux"}))

	for _, forbidden := range []string{"companion", "long-term memory", "recent conversation"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("system prompt leaked %q: %s", forbidden, text)
		}
	}
}

func TestBuildWorkSystem_ReadOnlyPermissionBranch(t *testing.T) {
	text := BuildWorkSystem(protocol.TaskBrief{
		Goal:            "inspect file",
		PermissionScope: "read-only",
	}, runtimeenv.Facts{OS: "linux"})

	if !strings.Contains(strings.ToLower(text), "read-only") {
		t.Fatalf("system prompt missing read-only wording: %s", text)
	}
}

func TestBuildWorkSystem_ReadOnlyDoesNotAdvertiseShellCommands(t *testing.T) {
	text := BuildWorkSystem(protocol.TaskBrief{
		Goal:            "inspect file",
		PermissionScope: "read-only",
	}, runtimeenv.Facts{
		OS:            "windows",
		WorkspaceRoot: `D:\repo`,
		PathStyle:     "windows",
		BashEnabled:   true,
		ShellDisplay:  "cmd /c",
	})

	if !strings.Contains(text, "Shell commands: unavailable in this task.") {
		t.Fatalf("read-only task should mark shell unavailable: %s", text)
	}
	if strings.Contains(text, "Shell commands: available via cmd /c") {
		t.Fatalf("read-only task should not advertise available shell: %s", text)
	}
}

func TestBuildWorkSystem_OmitsEmptyOptionalSections(t *testing.T) {
	text := BuildWorkSystem(protocol.TaskBrief{
		Goal:            "inspect file",
		PermissionScope: "read-only",
	}, runtimeenv.Facts{OS: "linux"})

	for _, header := range []string{
		"## Background",
		"## Constraints",
		"## Acceptance Criteria",
	} {
		if strings.Contains(text, header) {
			t.Fatalf("system prompt should omit empty section %q: %s", header, text)
		}
	}
}

func TestBuildWorkSystem_IncludesOptionalSections(t *testing.T) {
	text := BuildWorkSystem(protocol.TaskBrief{
		Goal:               "inspect file",
		PermissionScope:    "read-only",
		Background:         "Need to inspect go.mod",
		Constraints:        []string{"Do not read tests"},
		AcceptanceCriteria: []string{"List dependency names only"},
	}, runtimeenv.Facts{OS: "linux"})

	for _, snippet := range []string{
		"Need to inspect go.mod",
		"Do not read tests",
		"List dependency names only",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("system prompt missing %q: %s", snippet, text)
		}
	}
}

func TestBuildWorkSystem_ReinforcesExecutionOnlyBoundary(t *testing.T) {
	text := BuildWorkSystem(protocol.TaskBrief{
		Goal:               "draft an internal summary",
		PermissionScope:    "read-only",
		Background:         "If the task output itself needs a formal tone, it must be described in the task brief.",
		AcceptanceCriteria: []string{"Return a concise internal summary"},
	}, runtimeenv.Facts{OS: "linux"})

	for _, snippet := range []string{
		"You are not responsible for user-facing tone or persona-driven phrasing.",
		"If the delegated task itself requires a specific tone or format, treat that as task semantics from the goal, background, or constraints.",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("system prompt missing %q: %s", snippet, text)
		}
	}
	for _, forbidden := range []string{"## Expression Hints", "Tone:", "Directness:", "Hint:"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("system prompt should not mention removed expression hints %q: %s", forbidden, text)
		}
	}
}

func TestBuildWorkSystem_UsesFinishTaskContract(t *testing.T) {
	text := BuildWorkSystem(protocol.TaskBrief{
		Goal:            "inspect file",
		PermissionScope: "read-only",
	}, runtimeenv.Facts{OS: "linux"})

	if !strings.Contains(text, "finish_task") {
		t.Fatalf("system prompt missing finish_task contract: %s", text)
	}
	if strings.Contains(text, "Return exactly one TaskReport JSON object") {
		t.Fatalf("system prompt should not ask for final TaskReport JSON text: %s", text)
	}
	if !strings.Contains(strings.ToLower(text), "runtime metadata") {
		t.Fatalf("system prompt should describe reports as runtime metadata: %s", text)
	}
	if !strings.Contains(strings.ToLower(text), "do not write") {
		t.Fatalf("system prompt should forbid writing protocol objects to disk: %s", text)
	}
}

func TestBuildWorkSystem_IncludesEnvironmentDetails(t *testing.T) {
	text := BuildWorkSystem(protocol.TaskBrief{
		Goal:            "inspect file",
		PermissionScope: "workspace-write",
	}, runtimeenv.Facts{
		OS:            "windows",
		WorkspaceRoot: `D:\repo`,
		PathStyle:     "windows",
		BashEnabled:   true,
		ShellDisplay:  "cmd /c",
	})

	for _, snippet := range []string{
		"## Execution Environment",
		"OS: Windows",
		`Workspace root: D:\repo`,
		"Path style: windows",
		"Shell commands: available via cmd /c",
		"Do not assume Unix tools such as ls, rm, or pwd are available.",
		"Prefer dedicated file tools",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("system prompt missing %q: %s", snippet, text)
		}
	}
}

func TestBuildWorkSystem_DisabledBashMentionsUnavailableShell(t *testing.T) {
	text := BuildWorkSystem(protocol.TaskBrief{
		Goal:            "inspect file",
		PermissionScope: "workspace-write",
	}, runtimeenv.Facts{
		OS:            "linux",
		WorkspaceRoot: "/repo",
		PathStyle:     "posix",
		BashEnabled:   false,
	})

	if !strings.Contains(text, "Shell commands: unavailable in this runtime.") {
		t.Fatalf("system prompt should mention unavailable shell: %s", text)
	}
	if strings.Contains(text, "run shell commands (bash tool)") {
		t.Fatalf("permission section should not promise bash access when disabled: %s", text)
	}
}

func TestBuildWorkSystem_DistinguishesPermissionEscalationFromEmotionJudgment(t *testing.T) {
	text := BuildWorkSystem(protocol.TaskBrief{
		Goal:            "inspect file",
		PermissionScope: "workspace-write",
	}, runtimeenv.Facts{OS: "linux"})

	for _, snippet := range []string{
		"emotion_judgment: choices that require Emotion to interpret user intent, tone, preference, or relationship context",
		"Never use human_confirmation to ask for tool permission escalation.",
		"If a workspace-write task hits a destructive tool call, runtime will pause with permission_escalation_required and Emotion must ask the user instead of deciding itself.",
		"Only approved-destructive tasks may enter tool_approval.",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("system prompt missing %q: %s", snippet, text)
		}
	}
}

func TestBuildWorkSystem_IncludesP1ExecutionQualitySections(t *testing.T) {
	text := BuildWorkSystem(protocol.TaskBrief{
		Goal:               "make the README install steps clearer",
		PermissionScope:    "workspace-write",
		AcceptanceCriteria: []string{"README install steps are clearer", "Changes are verified"},
	}, runtimeenv.Facts{
		OS:            "windows",
		WorkspaceRoot: `D:\repo`,
		PathStyle:     "windows",
		BashEnabled:   true,
		ShellDisplay:  "cmd /c",
	})

	for _, snippet := range []string{
		"## Operating Loop",
		"Understand the Goal, Background, Constraints, and Acceptance Criteria before using tools.",
		"## Tool Selection Policy",
		"Use web_search to discover sources and web_fetch to read a specific source URL.",
		"File tool paths must be workspace-relative; use \".\" for the workspace root and never pass the absolute Workspace root to file tools.",
		"When creating or overwriting a file in a missing directory, prefer write_file with create_dirs=true instead of shell mkdir.",
		"## Verification",
		"After any workspace-write change, run the narrowest practical verification.",
		"## Minimal Change Policy",
		"Preserve unrelated user changes and do not perform opportunistic refactors.",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("system prompt missing %q: %s", snippet, text)
		}
	}
}
