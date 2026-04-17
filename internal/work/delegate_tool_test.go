package work

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/tool"
)

func TestDelegateTool_SchemaStaysValidatorCompatible(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{
		responses: []*llm.ChatResponse{textResp(`{"status":"completed","summary":"ok"}`)},
	})

	spec, _ := NewDelegateTool(runtime, t.TempDir(), testLogger())
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
	if len(props) != 3 {
		t.Fatalf("schema properties = %#v, want only goal/background/permission_scope", props)
	}
	for _, name := range []string{"goal", "background", "permission_scope"} {
		if _, ok := props[name]; !ok {
			t.Fatalf("schema missing %q: %#v", name, props)
		}
	}
}

func TestDelegateTool_HandlerRejectsNonReadOnly(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{
		responses: []*llm.ChatResponse{textResp(`{"status":"completed","summary":"ok"}`)},
	})

	_, handler := NewDelegateTool(runtime, t.TempDir(), testLogger())
	input, err := json.Marshal(map[string]any{
		"goal":             "edit config",
		"permission_scope": "workspace-write",
	})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	if _, err := handler(context.Background(), input); err == nil {
		t.Fatal("handler should reject non-read-only permission")
	}
}

func TestDelegateTool_HappyPathWritesJournalAndReturnsReport(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{
		responses: []*llm.ChatResponse{textResp(`{"status":"completed","summary":"done"}`)},
	})
	root := t.TempDir()
	_, handler := NewDelegateTool(runtime, root, testLogger())
	input, err := json.Marshal(map[string]any{
		"goal":             "inspect file",
		"background":       "look at go.mod",
		"permission_scope": "read-only",
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
