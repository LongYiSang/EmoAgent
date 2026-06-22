package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/resource"
	"github.com/longyisang/emoagent/internal/tool"
)

func TestReadFileAllScopeUsesHostResourceBrokerWhenConfigured(t *testing.T) {
	workspace := t.TempDir()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("broker note"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.txt")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	broker, err := resource.NewBroker(config.HostResourcesConfig{
		Enabled: true,
		Roots: []config.HostResourceRoot{{
			ID:        "documents",
			Path:      root,
			Access:    "read",
			Recursive: true,
		}},
		MaxReadBytes:     1024,
		MaxSearchResults: 100,
		ProtectedPolicy:  "default",
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}

	_, handler := NewReadFileToolWithBroker(workspace, broker)
	ctx := tool.WithReadScope(context.Background(), tool.ReadScopeAll)
	raw, err := handler(ctx, json.RawMessage(`{"path":"@documents/note.txt"}`))
	if err != nil {
		t.Fatalf("read_file broker read: %v", err)
	}
	var out struct {
		Path      string `json:"path"`
		PathScope string `json:"path_scope"`
		Content   string `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Path != "@documents/note.txt" || out.PathScope != "external" || out.Content != "broker note" {
		t.Fatalf("broker output = %#v", out)
	}

	input, _ := json.Marshal(map[string]string{"path": outsideFile})
	if _, err := handler(ctx, input); err == nil {
		t.Fatal("read_file should reject unconfigured external path in broker mode")
	}
}

func TestListDirAllScopeUsesHostResourceBrokerWhenConfigured(t *testing.T) {
	workspace := t.TempDir()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	broker, err := resource.NewBroker(config.HostResourcesConfig{
		Enabled: true,
		Roots: []config.HostResourceRoot{{
			ID:        "documents",
			Path:      root,
			Access:    "read",
			Recursive: true,
		}},
		MaxReadBytes:     1024,
		MaxSearchResults: 100,
		ProtectedPolicy:  "default",
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}

	_, handler := NewListDirToolWithBroker(workspace, broker)
	raw, err := handler(tool.WithReadScope(context.Background(), tool.ReadScopeAll), json.RawMessage(`{"path":"@documents"}`))
	if err != nil {
		t.Fatalf("list_dir broker list: %v", err)
	}
	var out struct {
		Path      string     `json:"path"`
		PathScope string     `json:"path_scope"`
		Entries   []dirEntry `json:"entries"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Path != "@documents" || out.PathScope != "external" || len(out.Entries) != 1 || out.Entries[0].Name != "a.txt" {
		t.Fatalf("broker list output = %#v", out)
	}
}
