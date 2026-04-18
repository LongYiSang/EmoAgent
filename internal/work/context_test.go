package work

import (
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/protocol"
)

func TestBuildWorkSystem_NoEmotionLeak(t *testing.T) {
	text := strings.ToLower(BuildWorkSystem(protocol.TaskBrief{
		Goal:            "list dependencies",
		PermissionScope: "read-only",
	}))

	for _, forbidden := range []string{"persona", "companion", "relationship"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("system prompt leaked %q: %s", forbidden, text)
		}
	}
}

func TestBuildWorkSystem_ReadOnlyPermissionBranch(t *testing.T) {
	text := BuildWorkSystem(protocol.TaskBrief{
		Goal:            "inspect file",
		PermissionScope: "read-only",
	})

	if !strings.Contains(strings.ToLower(text), "read-only") {
		t.Fatalf("system prompt missing read-only wording: %s", text)
	}
}

func TestBuildWorkSystem_OmitsEmptyOptionalSections(t *testing.T) {
	text := BuildWorkSystem(protocol.TaskBrief{
		Goal:            "inspect file",
		PermissionScope: "read-only",
	})

	for _, header := range []string{
		"## Background",
		"## Constraints",
		"## Acceptance Criteria",
		"## Expression Hints",
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
		ExpressionBrief: &protocol.ExpressionBrief{
			Tone:                "neutral",
			Directness:          "high",
			UserPreferenceHints: []string{"be concise"},
		},
	})

	for _, snippet := range []string{
		"Need to inspect go.mod",
		"Do not read tests",
		"List dependency names only",
		"neutral",
		"be concise",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("system prompt missing %q: %s", snippet, text)
		}
	}
}

func TestBuildWorkSystem_UsesFinishTaskContract(t *testing.T) {
	text := BuildWorkSystem(protocol.TaskBrief{
		Goal:            "inspect file",
		PermissionScope: "read-only",
	})

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
