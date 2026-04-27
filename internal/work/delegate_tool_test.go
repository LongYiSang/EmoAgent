package work

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/progress"
	"github.com/longyisang/emoagent/internal/tool"
)

func TestDelegateTool_SchemaStaysValidatorCompatible(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{
		responses: []*llm.ChatResponse{textResp(`{"status":"completed","summary":"ok"}`)},
	})

	spec, _ := NewDelegateTool(runtime, nil, t.TempDir(), testLogger())
	if spec.Scope != tool.ScopeEmotion {
		t.Fatalf("Scope = %q, want %q", spec.Scope, tool.ScopeEmotion)
	}
	if spec.Permission != tool.PermReadOnly {
		t.Fatalf("Permission = %q, want %q", spec.Permission, tool.PermReadOnly)
	}

	var schema map[string]any
	if err := json.Unmarshal(spec.Parameters, &schema); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	props := schema["properties"].(map[string]any)
	if len(props) != 5 {
		t.Fatalf("schema properties = %#v, want goal/background/constraints/acceptance_criteria/permission_scope", props)
	}
	for _, name := range []string{"goal", "background", "constraints", "acceptance_criteria", "permission_scope"} {
		if _, ok := props[name]; !ok {
			t.Fatalf("schema missing %q: %#v", name, props)
		}
	}

	permissionScope := props["permission_scope"].(map[string]any)
	enum := permissionScope["enum"].([]any)
	if len(enum) != 3 || enum[0] != "read-only" || enum[1] != "workspace-write" || enum[2] != "approved-destructive" {
		t.Fatalf("permission_scope enum = %#v, want [read-only workspace-write approved-destructive]", enum)
	}
	acceptanceCriteria := props["acceptance_criteria"].(map[string]any)
	if acceptanceCriteria["minItems"] != float64(1) {
		t.Fatalf("acceptance_criteria minItems = %#v, want 1", acceptanceCriteria["minItems"])
	}
	required := schema["required"].([]any)
	if !containsAnyString(required, "acceptance_criteria") {
		t.Fatalf("schema required = %#v, want acceptance_criteria", required)
	}
}

func TestDelegateTool_SchemaAcceptsFullBriefInput(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{
		responses: []*llm.ChatResponse{textResp(`{"status":"completed","summary":"ok"}`)},
	})

	spec, _ := NewDelegateTool(runtime, nil, t.TempDir(), testLogger())
	input := json.RawMessage(`{
		"goal":"inspect config",
		"background":"need a concise summary",
		"constraints":["do not change files","prefer primary config"],
		"acceptance_criteria":["list active ports","mention profile source"],
		"permission_scope":"read-only"
	}`)

	if err := (tool.MinimalSchemaValidator{}).Validate(spec.Parameters, input); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestDelegateTool_DescriptionIncludesPermissionGuidance(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{
		responses: []*llm.ChatResponse{textResp(`{"status":"completed","summary":"ok"}`)},
	})

	spec, _ := NewDelegateTool(runtime, nil, t.TempDir(), testLogger())
	for _, snippet := range []string{
		"acceptance_criteria must contain at least one observable success condition",
		"Give Work an outcome, not a script",
		"use approved-destructive when the goal includes delete/remove/move/rename/overwrite",
		"use workspace-write for non-destructive writes/edits",
	} {
		if !strings.Contains(spec.Description, snippet) {
			t.Fatalf("description missing %q: %s", snippet, spec.Description)
		}
	}
}

func TestDelegateTool_SchemaRejectsRemovedExpressionBrief(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{
		responses: []*llm.ChatResponse{textResp(`{"status":"completed","summary":"ok"}`)},
	})

	spec, _ := NewDelegateTool(runtime, nil, t.TempDir(), testLogger())
	input := json.RawMessage(`{
		"goal":"inspect config",
		"acceptance_criteria":["confirm config facts"],
		"permission_scope":"read-only",
		"expression_brief":{"tone":"calm"}
	}`)

	if err := (tool.MinimalSchemaValidator{}).Validate(spec.Parameters, input); err == nil {
		t.Fatal("Validate should reject removed expression_brief field")
	}
}

func TestDelegateTool_HandlerAcceptsApprovedDestructiveScope(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{
		responses: []*llm.ChatResponse{textResp(`{"status":"completed","summary":"ok"}`)},
	})

	_, handler := NewDelegateTool(runtime, nil, t.TempDir(), testLogger())
	input, err := json.Marshal(map[string]any{
		"goal":                "edit config",
		"acceptance_criteria": []string{"Config edit is complete"},
		"permission_scope":    "approved-destructive",
	})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	raw, err := handler(context.Background(), input)
	if err != nil {
		t.Fatalf("handler should accept approved-destructive permission, got: %v", err)
	}

	var report map[string]any
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if report["status"] != "completed" {
		t.Fatalf("status = %#v, want completed", report["status"])
	}
}

func TestDelegateTool_HandlerRejectsRemovedExpressionBriefField(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{
		responses: []*llm.ChatResponse{textResp(`{"status":"completed","summary":"ok"}`)},
	})

	_, handler := NewDelegateTool(runtime, nil, t.TempDir(), testLogger())
	input := json.RawMessage(`{
		"goal":"inspect config",
		"acceptance_criteria":["confirm config facts"],
		"permission_scope":"read-only",
		"expression_brief":{"tone":"calm"}
	}`)

	if _, err := handler(context.Background(), input); err == nil {
		t.Fatal("handler should reject removed expression_brief field")
	}
}

func TestDelegateTool_HappyPathWritesJournalAndReturnsReport(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{
		responses: []*llm.ChatResponse{textResp(`{"status":"completed","summary":"done"}`)},
	})
	root := t.TempDir()
	_, handler := NewDelegateTool(runtime, nil, root, testLogger())
	input, err := json.Marshal(map[string]any{
		"goal":                "inspect file",
		"background":          "look at go.mod",
		"acceptance_criteria": []string{"Report whether go.mod was inspected"},
		"permission_scope":    "read-only",
	})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	raw, err := handler(context.Background(), input)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var report map[string]any
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if report["status"] != "completed" {
		t.Fatalf("status = %#v, want completed", report["status"])
	}

	var found string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".jsonl") {
			found = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir returned error: %v", err)
	}
	if found == "" {
		t.Fatal("expected a journal file to be written")
	}

	data, err := os.ReadFile(found)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	text := string(data)
	for _, snippet := range []string{`"kind":"task_start"`, `"kind":"task_end"`} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("journal missing %s: %s", snippet, text)
		}
	}
}

func TestDelegateTool_EmitsProgressEndOnReport(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{
		responses: []*llm.ChatResponse{textResp(`{"status":"completed","summary":"ok"}`)},
	})
	_, handler := NewDelegateTool(runtime, nil, t.TempDir(), testLogger())

	var events []progress.Event
	ctx := progress.WithCallback(context.Background(), func(event progress.Event) {
		events = append(events, event)
	})

	input := json.RawMessage(`{"goal":"inspect config","acceptance_criteria":["report active config"],"permission_scope":"read-only"}`)
	if _, err := handler(ctx, input); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !hasProgressKind(events, progress.KindEnd) {
		t.Fatalf("events = %#v, want end event", events)
	}
}

func TestDelegateTool_EmitsProgressPausedOnPause(t *testing.T) {
	packetJSON := `{
		"category":"auto",
		"goal_summary":"need a technical decision",
		"question":"pick one",
		"why_blocked":"blocked",
		"options":[{"id":"a","summary":"A"},{"id":"b","summary":"B"}],
		"suggests_user_input":false
	}`
	runtime := newTestRuntime(t, &scriptedLLM{
		responses: []*llm.ChatResponse{toolUseResp("call-1", "request_decision", packetJSON)},
	})
	_, handler := NewDelegateTool(runtime, nil, t.TempDir(), testLogger())

	var events []progress.Event
	ctx := progress.WithCallback(context.Background(), func(event progress.Event) {
		events = append(events, event)
	})

	input := json.RawMessage(`{"goal":"inspect config","acceptance_criteria":["pause with a decision packet"],"permission_scope":"read-only"}`)
	if _, err := handler(ctx, input); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !hasProgressKind(events, progress.KindPaused) {
		t.Fatalf("events = %#v, want paused event", events)
	}
}

func TestDelegateTool_PausedJournalUsesDerivedRiskLevel(t *testing.T) {
	packetJSON := `{
		"category":"auto",
		"goal_summary":"need a technical decision",
		"question":"pick one",
		"why_blocked":"blocked",
		"options":[{"id":"a","summary":"A"},{"id":"b","summary":"B"}],
		"suggests_user_input":false
	}`
	runtime := newTestRuntime(t, &scriptedLLM{
		responses: []*llm.ChatResponse{toolUseResp("call-1", "request_decision", packetJSON)},
	})
	root := t.TempDir()
	_, handler := NewDelegateTool(runtime, nil, root, testLogger())

	input := json.RawMessage(`{"goal":"inspect config","acceptance_criteria":["pause with a decision packet"],"permission_scope":"read-only"}`)
	if _, err := handler(context.Background(), input); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".jsonl") {
			found = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir returned error: %v", err)
	}
	if found == "" {
		t.Fatal("expected a journal file to be written")
	}

	data, err := os.ReadFile(found)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	var pausedLine string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"kind":"task_paused"`) {
			pausedLine = line
			break
		}
	}
	if pausedLine == "" {
		t.Fatalf("journal = %s, want task_paused line", data)
	}
	if !strings.Contains(pausedLine, `"risk":"low"`) {
		t.Fatalf("task_paused line = %s, want derived low risk", pausedLine)
	}
}

func TestDelegateTool_HandlerRejectsMissingAcceptanceCriteria(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{
		responses: []*llm.ChatResponse{textResp(`{"status":"completed","summary":"ok"}`)},
	})

	_, handler := NewDelegateTool(runtime, nil, t.TempDir(), testLogger())
	input := json.RawMessage(`{"goal":"inspect config","permission_scope":"read-only"}`)

	if _, err := handler(context.Background(), input); err == nil {
		t.Fatal("handler should reject missing acceptance_criteria")
	}
}

func containsAnyString(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasProgressKind(events []progress.Event, target progress.EventKind) bool {
	for _, event := range events {
		if event.Kind == target {
			return true
		}
	}
	return false
}
