package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/tool"
)

func writeTemp(t *testing.T, root, name, content string) string {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEditFile_HappyPath(t *testing.T) {
	root := t.TempDir()
	writeTemp(t, root, "f.txt", "hello world")
	_, handler := NewEditFileTool(root)
	input, _ := json.Marshal(map[string]any{
		"path":       "f.txt",
		"old_string": "world",
		"new_string": "Go",
	})
	raw, err := handler(context.Background(), input)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var out struct {
		Replacements int `json:"replacements"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Replacements != 1 {
		t.Fatalf("replacements = %d, want 1", out.Replacements)
	}
	data, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if string(data) != "hello Go" {
		t.Fatalf("content = %q, want 'hello Go'", string(data))
	}
}

func TestEditFile_ReplaceAll(t *testing.T) {
	root := t.TempDir()
	writeTemp(t, root, "f.txt", "a a a")
	_, handler := NewEditFileTool(root)
	input, _ := json.Marshal(map[string]any{
		"path":        "f.txt",
		"old_string":  "a",
		"new_string":  "b",
		"replace_all": true,
	})
	raw, err := handler(context.Background(), input)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var out struct {
		Replacements int `json:"replacements"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Replacements != 3 {
		t.Fatalf("replacements = %d, want 3", out.Replacements)
	}
}

func TestEditFile_OldStringNotFound(t *testing.T) {
	root := t.TempDir()
	writeTemp(t, root, "f.txt", "hello")
	_, handler := NewEditFileTool(root)
	input, _ := json.Marshal(map[string]any{
		"path":       "f.txt",
		"old_string": "nothere",
		"new_string": "x",
	})
	if _, err := handler(context.Background(), input); err == nil {
		t.Fatal("expected error when old_string not found")
	}
}

func TestEditFile_AmbiguousWithoutReplaceAll(t *testing.T) {
	root := t.TempDir()
	writeTemp(t, root, "f.txt", "x x x")
	_, handler := NewEditFileTool(root)
	input, _ := json.Marshal(map[string]any{
		"path":       "f.txt",
		"old_string": "x",
		"new_string": "y",
	})
	if _, err := handler(context.Background(), input); err == nil {
		t.Fatal("expected error for ambiguous match without replace_all")
	}
}

func TestEditFile_IdenticalStrings(t *testing.T) {
	root := t.TempDir()
	writeTemp(t, root, "f.txt", "hello")
	_, handler := NewEditFileTool(root)
	input, _ := json.Marshal(map[string]any{
		"path":       "f.txt",
		"old_string": "hello",
		"new_string": "hello",
	})
	if _, err := handler(context.Background(), input); err == nil {
		t.Fatal("expected error when old_string == new_string")
	}
}

func TestEditFile_RejectsEmptyOldString(t *testing.T) {
	root := t.TempDir()
	writeTemp(t, root, "f.txt", "hello")
	_, handler := NewEditFileTool(root)
	input, _ := json.Marshal(map[string]any{
		"path":       "f.txt",
		"old_string": "",
		"new_string": "x",
	})
	if _, err := handler(context.Background(), input); err == nil {
		t.Fatal("expected error when old_string is empty")
	} else if !strings.Contains(err.Error(), "old_string must not be empty") {
		t.Fatalf("error = %q, want old_string must not be empty", err.Error())
	}
}

func TestEditFile_PathEscape(t *testing.T) {
	root := t.TempDir()
	_, handler := NewEditFileTool(root)
	input, _ := json.Marshal(map[string]any{
		"path":       "../outside.txt",
		"old_string": "a",
		"new_string": "b",
	})
	if _, err := handler(context.Background(), input); err == nil {
		t.Fatal("expected error for path escape")
	}
}

func TestEditFile_ReadScopeAllStillRejectsExternalPath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	path := filepath.Join(outside, "outside.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, handler := NewEditFileTool(root)
	input, _ := json.Marshal(map[string]any{
		"path":       path,
		"old_string": "hello",
		"new_string": "bye",
	})

	if _, err := handler(tool.WithReadScope(context.Background(), tool.ReadScopeAll), input); err == nil {
		t.Fatal("edit_file should reject external paths even when read_scope=all")
	}
}

func TestEditFile_RejectsWorkspaceSymlinkToExternalDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "outside.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, handler := NewEditFileTool(root)
	input, _ := json.Marshal(map[string]any{
		"path":       filepath.Join("linked", "outside.txt"),
		"old_string": "hello",
		"new_string": "bye",
	})

	if _, err := handler(context.Background(), input); err == nil {
		t.Fatal("edit_file should reject edits through a workspace symlink to an external directory")
	}
	data, err := os.ReadFile(filepath.Join(outside, "outside.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("external file changed to %q, want hello", data)
	}
}
