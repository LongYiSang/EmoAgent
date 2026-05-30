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

func TestResolveReadPath_WorkspaceScope(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := tool.WithReadScope(context.Background(), tool.ReadScopeWorkspace)

	got, err := resolveReadPath(ctx, root, "README.md")
	if err != nil {
		t.Fatalf("resolveReadPath returned error: %v", err)
	}
	if !got.InWorkspace || got.External {
		t.Fatalf("scope flags = in_workspace:%v external:%v, want workspace", got.InWorkspace, got.External)
	}
	if got.DisplayPath != "README.md" || got.WorkspaceRelative != "README.md" {
		t.Fatalf("paths = display:%q rel:%q, want README.md", got.DisplayPath, got.WorkspaceRelative)
	}
}

func TestResolveReadPath_WorkspaceScopeRejectsAbsoluteAndEscape(t *testing.T) {
	root := t.TempDir()
	ctx := tool.WithReadScope(context.Background(), tool.ReadScopeWorkspace)

	for _, raw := range []string{filepath.Join(root, "README.md"), filepath.Join("..", "outside.txt")} {
		if _, err := resolveReadPath(ctx, root, raw); err == nil {
			t.Fatalf("resolveReadPath should reject %q in workspace scope", raw)
		}
	}
}

func TestResolveReadPath_WorkspaceScopeRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	ctx := tool.WithReadScope(context.Background(), tool.ReadScopeWorkspace)
	if _, err := resolveReadPath(ctx, root, "linked.txt"); err == nil {
		t.Fatal("resolveReadPath should reject a workspace symlink that resolves outside")
	}
}

func TestResolveReadPath_AllScopeAllowsAbsoluteAndEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	absolute := filepath.Join(outside, "outside.txt")
	if err := os.WriteFile(absolute, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	relativeOutside := filepath.Join(filepath.Base(outside), "relative.txt")
	if err := os.WriteFile(filepath.Join(filepath.Dir(root), relativeOutside), []byte("relative"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := tool.WithReadScope(context.Background(), tool.ReadScopeAll)

	got, err := resolveReadPath(ctx, root, absolute)
	if err != nil {
		t.Fatalf("resolve absolute returned error: %v", err)
	}
	if !got.External || got.InWorkspace {
		t.Fatalf("absolute scope flags = in_workspace:%v external:%v, want external", got.InWorkspace, got.External)
	}
	if got.DisplayPath != filepath.ToSlash(filepath.Clean(absolute)) {
		t.Fatalf("DisplayPath = %q, want absolute external path", got.DisplayPath)
	}

	escapeArg := filepath.Join("..", relativeOutside)
	got, err = resolveReadPath(ctx, root, escapeArg)
	if err != nil {
		t.Fatalf("resolve escape returned error: %v", err)
	}
	if !got.External {
		t.Fatalf("escape path should be external: %#v", got)
	}
}

func TestResolveReadPath_AllScopeKeepsWorkspaceAndDoesNotExpandTilde(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "~"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "~", ".config"), []byte("local tilde"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := tool.WithReadScope(context.Background(), tool.ReadScopeAll)

	got, err := resolveReadPath(ctx, root, "~/.config")
	if err != nil {
		t.Fatalf("resolveReadPath returned error: %v", err)
	}
	if !got.InWorkspace || got.External {
		t.Fatalf("tilde path should remain workspace-relative, got %#v", got)
	}
	if !strings.Contains(filepath.ToSlash(got.FullPath), "/~/.config") {
		t.Fatalf("FullPath = %q, want literal ~/ segment", got.FullPath)
	}
}

func TestSensitiveReadClassifier(t *testing.T) {
	root := t.TempDir()
	ctx := tool.WithReadScope(context.Background(), tool.ReadScopeAll)

	tests := []struct {
		name string
		path string
	}{
		{name: "env", path: ".env"},
		{name: "ssh segment", path: filepath.Join(".ssh", "config")},
		{name: "private key", path: "id_rsa"},
		{name: "credentials", path: "credentials.json"},
		{name: "pem suffix", path: "cert.pem"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, _ := NewReadFileTool(root)
			if spec.ApprovalClassifier == nil {
				t.Fatal("read_file should attach an ApprovalClassifier")
			}
			req, ok := spec.ApprovalClassifier(ctx, mustJSON(t, map[string]any{"path": tt.path}))
			if !ok {
				t.Fatalf("path %q should require approval", tt.path)
			}
			if req.Kind != tool.ApprovalKindSensitiveRead {
				t.Fatalf("ApprovalKind = %q, want %q", req.Kind, tool.ApprovalKindSensitiveRead)
			}
		})
	}
}

func TestSensitiveReadClassifier_ExternalOrdinaryReadDoesNotRequireApproval(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	ordinary := filepath.Join(outside, "readme.txt")
	if err := os.WriteFile(ordinary, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	spec, _ := NewReadFileTool(root)
	req, ok := spec.ApprovalClassifier(tool.WithReadScope(context.Background(), tool.ReadScopeAll), mustJSON(t, map[string]any{"path": ordinary}))
	if ok {
		t.Fatalf("ordinary external read should not require approval, got %#v", req)
	}
}

func TestSensitiveReadClassifier_ListDirRecursiveExternalRequiresApproval(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	ctx := tool.WithReadScope(context.Background(), tool.ReadScopeAll)

	spec, _ := NewListDirTool(root)
	req, ok := spec.ApprovalClassifier(ctx, mustJSON(t, map[string]any{"path": outside, "recursive": true}))
	if !ok {
		t.Fatal("recursive external list_dir should require approval")
	}
	if req.Kind != tool.ApprovalKindSensitiveRead || req.Reason != "external_recursive_list" {
		t.Fatalf("approval requirement = %#v, want sensitive_read external_recursive_list", req)
	}

	req, ok = spec.ApprovalClassifier(ctx, mustJSON(t, map[string]any{"path": outside, "recursive": false}))
	if ok {
		t.Fatalf("non-recursive ordinary external list_dir should not require approval, got %#v", req)
	}
}

func TestSensitiveReadClassifier_UsesRawSensitivePathBeforeSymlinkResolution(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".ssh"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "ordinary.txt")
	if err := os.WriteFile(target, []byte("ordinary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, ".ssh", "config")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	spec, _ := NewReadFileTool(root)
	req, ok := spec.ApprovalClassifier(tool.WithReadScope(context.Background(), tool.ReadScopeAll), mustJSON(t, map[string]any{"path": filepath.Join(".ssh", "config")}))
	if !ok {
		t.Fatal("raw sensitive path should require approval even when symlink resolves to ordinary path")
	}
	if req.Kind != tool.ApprovalKindSensitiveRead {
		t.Fatalf("ApprovalKind = %q, want %q", req.Kind, tool.ApprovalKindSensitiveRead)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return raw
}
