package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFile_HappyPath(t *testing.T) {
	root := t.TempDir()
	_, handler := NewWriteFileTool(root)
	input, _ := json.Marshal(map[string]any{"path": "hello.txt", "content": "world"})
	raw, err := handler(context.Background(), input)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var out struct {
		Path         string `json:"path"`
		BytesWritten int    `json:"bytes_written"`
		Existed      bool   `json:"existed"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.BytesWritten != 5 {
		t.Fatalf("bytes_written = %d, want 5", out.BytesWritten)
	}
	if out.Existed {
		t.Fatal("existed should be false for new file")
	}
	data, _ := os.ReadFile(filepath.Join(root, "hello.txt"))
	if string(data) != "world" {
		t.Fatalf("content = %q, want world", string(data))
	}
}

func TestWriteFile_Overwrite(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "f.txt")
	if err := os.WriteFile(p, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	_, handler := NewWriteFileTool(root)
	input, _ := json.Marshal(map[string]any{"path": "f.txt", "content": "new"})
	raw, err := handler(context.Background(), input)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var out struct {
		Existed bool `json:"existed"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Existed {
		t.Fatal("existed should be true")
	}
	data, _ := os.ReadFile(p)
	if string(data) != "new" {
		t.Fatalf("content = %q, want new", string(data))
	}
}

func TestWriteFile_CreateDirs(t *testing.T) {
	root := t.TempDir()
	_, handler := NewWriteFileTool(root)
	input, _ := json.Marshal(map[string]any{
		"path":        "deep/nested/f.txt",
		"content":     "hi",
		"create_dirs": true,
	})
	if _, err := handler(context.Background(), input); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "deep/nested/f.txt")); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestWriteFile_PathEscape(t *testing.T) {
	root := t.TempDir()
	_, handler := NewWriteFileTool(root)
	for _, bad := range []string{"../escape.txt", "/etc/passwd"} {
		input, _ := json.Marshal(map[string]any{"path": bad, "content": "x"})
		if _, err := handler(context.Background(), input); err == nil {
			t.Fatalf("expected error for path %q", bad)
		}
	}
}

func TestWriteFile_SizeCap(t *testing.T) {
	root := t.TempDir()
	_, handler := NewWriteFileTool(root)
	big := strings.Repeat("x", writeFileMaxBytes+1)
	input, _ := json.Marshal(map[string]any{"path": "big.txt", "content": big})
	if _, err := handler(context.Background(), input); err == nil {
		t.Fatal("expected error for oversized content")
	}
}
