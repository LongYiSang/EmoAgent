package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/resource"
)

func newHostChangeSetToolTestManager(t *testing.T, root string) *resource.ChangeSetManager {
	t.Helper()
	broker, err := resource.NewBroker(config.HostResourcesConfig{
		Enabled: true,
		Roots: []config.HostResourceRoot{{
			ID:        "documents",
			Path:      root,
			Access:    "read",
			Recursive: true,
		}},
		MaxReadBytes:     1024 * 1024,
		MaxSearchResults: 100,
		ProtectedPolicy:  "default",
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}
	manager, err := resource.NewChangeSetManager(broker, nil, resource.ChangeSetManagerOptions{
		StagingDir:    filepath.Join(t.TempDir(), "staging"),
		QuarantineDir: filepath.Join(t.TempDir(), "quarantine"),
		MaxBytes:      1024 * 1024,
	})
	if err != nil {
		t.Fatalf("NewChangeSetManager: %v", err)
	}
	return manager
}

func TestHostApplyChangeRequiresExactResourceBindingFields(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := newHostChangeSetToolTestManager(t, root)
	cs, err := manager.PrepareChange(context.Background(), resource.ChangeSetRequest{
		Operation: resource.ChangeOpOverwriteFile,
		Path:      "@documents/note.txt",
		Content:   "new",
	})
	if err != nil {
		t.Fatalf("PrepareChange: %v", err)
	}
	_, handler := NewHostApplyChangeTool(manager)

	if _, err := handler(context.Background(), mustJSON(t, map[string]any{
		"changeset_id": cs.ID,
		"plan_hash":    cs.PlanHash,
	})); err == nil || !strings.Contains(err.Error(), "resource_id") {
		t.Fatalf("handler without resource binding err = %v, want resource_id mismatch", err)
	}
	if got := readHostToolText(t, target); got != "old" {
		t.Fatalf("file changed after rejected apply: %q", got)
	}

	base := map[string]any{
		"changeset_id":        cs.ID,
		"plan_hash":           cs.PlanHash,
		"resource_id":         cs.Source.ID,
		"canonical_path_hash": cs.Source.CanonicalPathHash,
		"baseline_hash":       cs.BaselineHash,
		"baseline_file_id":    cs.BaselineFileID,
	}
	for _, tc := range []struct {
		name    string
		field   string
		value   string
		wantErr string
	}{
		{name: "resource_id", field: "resource_id", value: "local:wrong", wantErr: "resource_id"},
		{name: "canonical_path_hash", field: "canonical_path_hash", value: "sha256:wrong-path", wantErr: "canonical_path_hash"},
		{name: "baseline_hash", field: "baseline_hash", value: "sha256:wrong", wantErr: "baseline_hash"},
		{name: "baseline_file_id", field: "baseline_file_id", value: "file-id-wrong", wantErr: "baseline_file_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := map[string]any{}
			for key, value := range base {
				input[key] = value
			}
			input[tc.field] = tc.value
			if _, err := handler(context.Background(), mustJSON(t, input)); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("handler with mutated %s err = %v, want %s mismatch", tc.field, err, tc.wantErr)
			}
			if got := readHostToolText(t, target); got != "old" {
				t.Fatalf("file changed after rejected apply: %q", got)
			}
		})
	}

	raw, err := handler(context.Background(), mustJSON(t, map[string]any{
		"changeset_id":        cs.ID,
		"plan_hash":           cs.PlanHash,
		"resource_id":         cs.Source.ID,
		"canonical_path_hash": cs.Source.CanonicalPathHash,
		"baseline_hash":       cs.BaselineHash,
		"baseline_file_id":    cs.BaselineFileID,
	}))
	if err != nil {
		t.Fatalf("handler exact binding: %v", err)
	}
	var applied resource.ChangeSet
	if err := json.Unmarshal(raw, &applied); err != nil {
		t.Fatalf("unmarshal applied: %v", err)
	}
	if applied.Status != resource.ChangeSetStatusApplied || readHostToolText(t, target) != "new" {
		t.Fatalf("applied = %#v file=%q", applied, readHostToolText(t, target))
	}
}

func TestHostApplyChangeRequiresExplicitPermanentDeleteMode(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("delete me"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := newHostChangeSetToolTestManager(t, root)
	cs, err := manager.PrepareChange(context.Background(), resource.ChangeSetRequest{
		Operation:       resource.ChangeOpDelete,
		Path:            "@documents/note.txt",
		PermanentDelete: true,
	})
	if err != nil {
		t.Fatalf("PrepareChange: %v", err)
	}
	_, handler := NewHostApplyChangeTool(manager)
	base := map[string]any{
		"changeset_id":        cs.ID,
		"plan_hash":           cs.PlanHash,
		"resource_id":         cs.Source.ID,
		"canonical_path_hash": cs.Source.CanonicalPathHash,
		"baseline_hash":       cs.BaselineHash,
		"baseline_file_id":    cs.BaselineFileID,
	}
	if _, err := handler(context.Background(), mustJSON(t, base)); err == nil || !strings.Contains(err.Error(), "delete_mode=permanent") {
		t.Fatalf("handler without permanent mode err = %v, want explicit permanent mode", err)
	}
	base["delete_mode"] = resource.DeleteModePermanent
	if _, err := handler(context.Background(), mustJSON(t, base)); err != nil {
		t.Fatalf("handler with permanent mode: %v", err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("target exists after permanent delete: %v", err)
	}
}

func TestHostApplyChangeDoesNotReportConflictAsSuccess(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := newHostChangeSetToolTestManager(t, root)
	cs, err := manager.PrepareChange(context.Background(), resource.ChangeSetRequest{
		Operation: resource.ChangeOpOverwriteFile,
		Path:      "@documents/note.txt",
		Content:   "new",
	})
	if err != nil {
		t.Fatalf("PrepareChange: %v", err)
	}
	if err := os.WriteFile(target, []byte("external"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, handler := NewHostApplyChangeTool(manager)
	_, err = handler(context.Background(), mustJSON(t, map[string]any{
		"changeset_id":        cs.ID,
		"plan_hash":           cs.PlanHash,
		"resource_id":         cs.Source.ID,
		"canonical_path_hash": cs.Source.CanonicalPathHash,
		"baseline_hash":       cs.BaselineHash,
		"baseline_file_id":    cs.BaselineFileID,
	}))
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("handler err = %v, want conflict error", err)
	}
	if got := readHostToolText(t, target); got != "external" {
		t.Fatalf("file = %q, want external", got)
	}
}

func TestHostApplyChangeRequiresExplicitRecursiveApproval(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "tree")
	if err := os.MkdirAll(filepath.Join(targetDir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "sub", "a.txt"), []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := newHostChangeSetToolTestManager(t, root)
	cs, err := manager.PrepareChange(context.Background(), resource.ChangeSetRequest{
		Operation: resource.ChangeOpDelete,
		Path:      "@documents/tree",
		Recursive: true,
	})
	if err != nil {
		t.Fatalf("PrepareChange: %v", err)
	}
	_, handler := NewHostApplyChangeTool(manager)
	base := map[string]any{
		"changeset_id":        cs.ID,
		"plan_hash":           cs.PlanHash,
		"resource_id":         cs.Source.ID,
		"canonical_path_hash": cs.Source.CanonicalPathHash,
		"baseline_hash":       cs.BaselineHash,
		"baseline_file_id":    cs.BaselineFileID,
		"delete_mode":         resource.DeleteModeQuarantine,
	}
	if _, err := handler(context.Background(), mustJSON(t, base)); err == nil || !strings.Contains(err.Error(), "recursive approval") {
		t.Fatalf("handler without recursive approval err = %v, want explicit recursive approval", err)
	}
	base["recursive"] = true
	raw, err := handler(context.Background(), mustJSON(t, base))
	if err != nil {
		t.Fatalf("handler with recursive approval: %v", err)
	}
	var applied resource.ChangeSet
	if err := json.Unmarshal(raw, &applied); err != nil {
		t.Fatalf("unmarshal applied changeset: %v", err)
	}
	if applied.Status != resource.ChangeSetStatusApplied {
		t.Fatalf("applied = %#v, want applied", applied)
	}
	if _, err := os.Lstat(targetDir); !os.IsNotExist(err) {
		t.Fatalf("target tree exists after recursive delete: %v; applied=%#v", err, applied)
	}
}

func readHostToolText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
