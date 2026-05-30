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

func callReadFile(t *testing.T, root, pathArg string) (json.RawMessage, error) {
	t.Helper()

	return callReadFileWithContext(t, context.Background(), root, pathArg)
}

func callReadFileWithContext(t *testing.T, ctx context.Context, root, pathArg string) (json.RawMessage, error) {
	t.Helper()

	_, handler := NewReadFileTool(root)
	input, err := json.Marshal(map[string]string{"path": pathArg})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	return handler(ctx, input)
}

func TestReadFile_RejectsAbsolutePath(t *testing.T) {
	root := t.TempDir()

	if _, err := callReadFile(t, root, filepath.Join(root, "hello.txt")); err == nil {
		t.Fatal("read_file should reject absolute paths")
	}
}

func TestReadFile_RejectsParentTraversal(t *testing.T) {
	root := t.TempDir()

	if _, err := callReadFile(t, root, filepath.Join("..", "outside.txt")); err == nil {
		t.Fatal("read_file should reject parent traversal")
	}
}

func TestReadFile_RejectsDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "subdir"), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	if _, err := callReadFile(t, root, "subdir"); err == nil {
		t.Fatal("read_file should reject directories")
	}
}

func TestReadFile_RejectsMissingFile(t *testing.T) {
	root := t.TempDir()

	if _, err := callReadFile(t, root, "missing.txt"); err == nil {
		t.Fatal("read_file should reject missing files")
	}
}

func TestReadFile_RejectsLargeFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "big.txt")
	content := strings.Repeat("a", readFileMaxBytes+1)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if _, err := callReadFile(t, root, "big.txt"); err == nil {
		t.Fatal("read_file should reject files larger than 1 MiB")
	}
}

func TestReadFile_RejectsNonUTF8(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "bin.dat")
	if err := os.WriteFile(path, []byte{0xff, 0xfe, 0xfd, 0xfc}, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if _, err := callReadFile(t, root, "bin.dat"); err == nil {
		t.Fatal("read_file should reject non-UTF-8 files")
	}
}

func TestReadFile_HappyPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "hello.txt")
	if err := os.WriteFile(path, []byte("hello, world"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	raw, err := callReadFile(t, root, "hello.txt")
	if err != nil {
		t.Fatalf("read_file returned error: %v", err)
	}

	var out struct {
		Path      string `json:"path"`
		PathScope string `json:"path_scope"`
		Content   string `json:"content"`
		Size      int    `json:"size"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if out.Path != "hello.txt" {
		t.Fatalf("Path = %q, want hello.txt", out.Path)
	}
	if out.PathScope != "workspace" {
		t.Fatalf("PathScope = %q, want workspace", out.PathScope)
	}
	if out.Content != "hello, world" {
		t.Fatalf("Content = %q, want hello, world", out.Content)
	}
	if out.Size != len("hello, world") {
		t.Fatalf("Size = %d, want %d", out.Size, len("hello, world"))
	}
}

func TestReadFile_AllScopeReadsExternalAbsolutePath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	path := filepath.Join(outside, "note.txt")
	if err := os.WriteFile(path, []byte("external note"), 0o644); err != nil {
		t.Fatal(err)
	}

	raw, err := callReadFileWithContext(t, tool.WithReadScope(context.Background(), tool.ReadScopeAll), root, path)
	if err != nil {
		t.Fatalf("read_file returned error: %v", err)
	}

	var out struct {
		Path      string `json:"path"`
		PathScope string `json:"path_scope"`
		Content   string `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if out.PathScope != "external" {
		t.Fatalf("PathScope = %q, want external", out.PathScope)
	}
	if out.Path != filepath.ToSlash(filepath.Clean(path)) {
		t.Fatalf("Path = %q, want external display path", out.Path)
	}
	if out.Content != "external note" {
		t.Fatalf("Content = %q, want external note", out.Content)
	}
}
