package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/longyisang/emoagent/internal/tool"
)

func TestListDir_HappyPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0755); err != nil {
		t.Fatal(err)
	}

	_, handler := NewListDirTool(root)
	raw, err := handler(context.Background(), json.RawMessage(`{"path":"."}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var out struct {
		Path      string     `json:"path"`
		PathScope string     `json:"path_scope"`
		Entries   []dirEntry `json:"entries"`
		Truncated bool       `json:"truncated"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(out.Entries))
	}
	if out.PathScope != "workspace" {
		t.Fatalf("path_scope = %q, want workspace", out.PathScope)
	}
	if out.Truncated {
		t.Fatal("truncated should be false")
	}
}

func TestListDir_Recursive(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "deep.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	_, handler := NewListDirTool(root)
	raw, err := handler(context.Background(), json.RawMessage(`{"path":".","recursive":true}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var out struct {
		Entries []dirEntry
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Entries) < 2 {
		t.Fatalf("want at least 2 entries (sub + deep.txt), got %d", len(out.Entries))
	}
}

func TestListDir_PathEscape(t *testing.T) {
	root := t.TempDir()
	_, handler := NewListDirTool(root)
	for _, bad := range []string{"../", "/etc", "sub/../../.."} {
		input, _ := json.Marshal(map[string]string{"path": bad})
		_, err := handler(context.Background(), input)
		if err == nil {
			t.Fatalf("expected error for path %q", bad)
		}
	}
}

func TestListDir_MaxEntriesTruncation(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		name := filepath.Join(root, "f"+string(rune('a'+i))+".txt")
		if err := os.WriteFile(name, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	_, handler := NewListDirTool(root)
	raw, err := handler(context.Background(), json.RawMessage(`{"path":".","max_entries":2}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var out struct {
		Entries   []dirEntry
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(out.Entries))
	}
	if !out.Truncated {
		t.Fatal("truncated should be true")
	}
}

func TestListDir_NotADirectory(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	_, handler := NewListDirTool(root)
	_, err := handler(context.Background(), json.RawMessage(`{"path":"file.txt"}`))
	if err == nil {
		t.Fatal("expected error for file path")
	}
}

func TestListDir_AllScopeListsExternalDirectory(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "outside.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, handler := NewListDirTool(root)
	input, _ := json.Marshal(map[string]any{"path": outside})
	raw, err := handler(tool.WithReadScope(context.Background(), tool.ReadScopeAll), input)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var out struct {
		Path      string     `json:"path"`
		PathScope string     `json:"path_scope"`
		Entries   []dirEntry `json:"entries"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.PathScope != "external" {
		t.Fatalf("path_scope = %q, want external", out.PathScope)
	}
	if out.Path != filepath.ToSlash(filepath.Clean(outside)) {
		t.Fatalf("path = %q, want external directory display path", out.Path)
	}
	if len(out.Entries) != 1 || out.Entries[0].Name != "outside.txt" {
		t.Fatalf("entries = %#v, want outside.txt", out.Entries)
	}
}
