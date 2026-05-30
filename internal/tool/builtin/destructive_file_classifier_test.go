package builtin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileDestructiveClassifier(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "existing.md"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	spec, _ := NewWriteFileTool(root)
	if spec.DestructiveClassifier == nil {
		t.Fatal("write_file should attach a destructive classifier")
	}

	tests := []struct {
		name string
		in   map[string]any
		want bool
	}{
		{
			name: "new ordinary file is not destructive",
			in:   map[string]any{"path": "docs/new.md", "content": "hello"},
			want: false,
		},
		{
			name: "overwriting existing file is destructive",
			in:   map[string]any{"path": "docs/existing.md", "content": "new"},
			want: true,
		},
		{
			name: "env file is destructive",
			in:   map[string]any{"path": ".env", "content": "SECRET=value"},
			want: true,
		},
		{
			name: "secrets file is destructive",
			in:   map[string]any{"path": "config/secrets.json", "content": "{}"},
			want: true,
		},
		{
			name: "create ordinary directory is not destructive",
			in:   map[string]any{"path": "docs/newdir/file.md", "content": "hello", "create_dirs": true},
			want: false,
		},
		{
			name: "create workflow directory is destructive",
			in:   map[string]any{"path": ".github/workflows/test.yml", "content": "name: ci", "create_dirs": true},
			want: true,
		},
		{
			name: "create vendor directory is destructive",
			in:   map[string]any{"path": "vendor/pkg/file.txt", "content": "x", "create_dirs": true},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, _ := json.Marshal(tt.in)
			got, reason := spec.DestructiveClassifier(input)
			if got != tt.want {
				t.Fatalf("destructive = %v, want %v; reason=%q", got, tt.want, reason)
			}
			if got && reason == "" {
				t.Fatal("destructive result should include a reason")
			}
		})
	}
}

func TestEditFileDestructiveClassifier(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "ordinary.md"), []byte("alpha beta gamma delta epsilon"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "large.md"), []byte(strings.Repeat("x", 100)+"tail"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "many.md"), []byte("needle needle needle needle needle"), 0644); err != nil {
		t.Fatal(err)
	}
	spec, _ := NewEditFileTool(root)
	if spec.DestructiveClassifier == nil {
		t.Fatal("edit_file should attach a destructive classifier")
	}

	tests := []struct {
		name string
		in   map[string]any
		want bool
	}{
		{
			name: "ordinary unique replacement is not destructive",
			in:   map[string]any{"path": "docs/ordinary.md", "old_string": "beta", "new_string": "BETA"},
			want: false,
		},
		{
			name: "replace all is destructive",
			in:   map[string]any{"path": "docs/ordinary.md", "old_string": "a", "new_string": "A", "replace_all": true},
			want: true,
		},
		{
			name: "sensitive path is destructive",
			in:   map[string]any{"path": ".env", "old_string": "A", "new_string": "B"},
			want: true,
		},
		{
			name: "old string near whole file is destructive",
			in:   map[string]any{"path": "docs/large.md", "old_string": strings.Repeat("x", 30), "new_string": "short"},
			want: true,
		},
		{
			name: "too many replacements is destructive",
			in:   map[string]any{"path": "docs/many.md", "old_string": "needle", "new_string": "pin"},
			want: true,
		},
		{
			name: "empty old string is destructive",
			in:   map[string]any{"path": "docs/ordinary.md", "old_string": "", "new_string": "x"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, _ := json.Marshal(tt.in)
			got, reason := spec.DestructiveClassifier(input)
			if got != tt.want {
				t.Fatalf("destructive = %v, want %v; reason=%q", got, tt.want, reason)
			}
			if got && reason == "" {
				t.Fatal("destructive result should include a reason")
			}
		})
	}
}
