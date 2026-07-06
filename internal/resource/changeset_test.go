package resource

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	_ "modernc.org/sqlite"
)

func newChangeSetTestManager(t *testing.T, root string, store ChangeSetStore) *ChangeSetManager {
	t.Helper()
	broker, err := NewBroker(config.HostResourcesConfig{
		Enabled:          true,
		Roots:            []config.HostResourceRoot{{ID: "documents", Path: root, Access: "read", Recursive: true}},
		MaxReadBytes:     1024 * 1024,
		MaxSearchResults: 100,
		ProtectedPolicy:  "default",
	})
	if err != nil {
		t.Fatalf("NewBroker: %v", err)
	}
	manager, err := NewChangeSetManager(broker, store, ChangeSetManagerOptions{
		StagingDir:    filepath.Join(t.TempDir(), "staging"),
		QuarantineDir: filepath.Join(t.TempDir(), "quarantine"),
		MaxBytes:      1024 * 1024,
	})
	if err != nil {
		t.Fatalf("NewChangeSetManager: %v", err)
	}
	return manager
}

func TestChangeSetOverwritePreviewThenApply(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := newChangeSetTestManager(t, root, nil)

	cs, err := manager.PrepareChange(context.Background(), ChangeSetRequest{
		Operation: ChangeOpOverwriteFile,
		Path:      "@documents/note.txt",
		Content:   "new\n",
	})
	if err != nil {
		t.Fatalf("PrepareChange: %v", err)
	}
	if cs.Status != ChangeSetStatusApprovalPending || cs.PlanHash == "" || cs.BaselineHash == "" || cs.ContentHash == "" {
		t.Fatalf("changeset = %#v", cs)
	}
	if !strings.Contains(cs.Preview.Diff, "-old") || !strings.Contains(cs.Preview.Diff, "+new") {
		t.Fatalf("diff = %q", cs.Preview.Diff)
	}
	if got := readText(t, target); got != "old\n" {
		t.Fatalf("file changed before apply: %q", got)
	}

	applied, err := manager.ApplyChange(context.Background(), cs.ID, cs.PlanHash)
	if err != nil {
		t.Fatalf("ApplyChange: %v", err)
	}
	if applied.Status != ChangeSetStatusApplied {
		t.Fatalf("status = %q, want applied", applied.Status)
	}
	if got := readText(t, target); got != "new\n" {
		t.Fatalf("file = %q, want new", got)
	}
}

func TestChangeSetOverwriteDetectsBaselineConflict(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := newChangeSetTestManager(t, root, nil)
	cs, err := manager.PrepareChange(context.Background(), ChangeSetRequest{
		Operation: ChangeOpOverwriteFile,
		Path:      "@documents/note.txt",
		Content:   "new",
	})
	if err != nil {
		t.Fatalf("PrepareChange: %v", err)
	}
	if err := os.WriteFile(target, []byte("external"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := manager.ApplyChange(context.Background(), cs.ID, cs.PlanHash)
	if err != nil {
		t.Fatalf("ApplyChange conflict should return changeset, got error: %v", err)
	}
	if got.Status != ChangeSetStatusConflict || !strings.Contains(got.ErrorMessage, "baseline") {
		t.Fatalf("changeset = %#v, want baseline conflict", got)
	}
	if text := readText(t, target); text != "external" {
		t.Fatalf("file = %q, want external", text)
	}
}

func TestChangeSetStagedContentTamperConflictsAndDoesNotApply(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := newChangeSetTestManager(t, root, nil)
	cs, err := manager.PrepareChange(context.Background(), ChangeSetRequest{
		Operation: ChangeOpOverwriteFile,
		Path:      "@documents/note.txt",
		Content:   "new",
	})
	if err != nil {
		t.Fatalf("PrepareChange: %v", err)
	}
	if err := os.WriteFile(cs.StagingPath, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := manager.ApplyChange(context.Background(), cs.ID, cs.PlanHash)
	if err != nil {
		t.Fatalf("ApplyChange tamper should return conflict changeset, got error: %v", err)
	}
	if got.Status != ChangeSetStatusConflict || !strings.Contains(got.ErrorMessage, "staged content hash mismatch") {
		t.Fatalf("changeset = %#v, want staged content conflict", got)
	}
	if text := readText(t, target); text != "old" {
		t.Fatalf("file = %q, want old", text)
	}
}

func TestChangeSetMoveRenamesWithinRoot(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(source, []byte("move me"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := newChangeSetTestManager(t, root, nil)
	cs, err := manager.PrepareChange(context.Background(), ChangeSetRequest{
		Operation:  ChangeOpMove,
		Path:       "@documents/source.txt",
		TargetPath: "@documents/target.txt",
	})
	if err != nil {
		t.Fatalf("PrepareChange: %v", err)
	}
	applied, err := manager.ApplyChange(context.Background(), cs.ID, cs.PlanHash)
	if err != nil {
		t.Fatalf("ApplyChange: %v", err)
	}
	if applied.Status != ChangeSetStatusApplied {
		t.Fatalf("status = %q, want applied", applied.Status)
	}
	if _, err := os.Lstat(source); !os.IsNotExist(err) {
		t.Fatalf("source exists after move: %v", err)
	}
	if got := readText(t, target); got != "move me" {
		t.Fatalf("target = %q, want moved content", got)
	}
}

func TestChangeSetDeleteQuarantineAndRestore(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("delete me"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := newChangeSetTestManager(t, root, nil)
	cs, err := manager.PrepareChange(context.Background(), ChangeSetRequest{
		Operation: ChangeOpDelete,
		Path:      "@documents/note.txt",
	})
	if err != nil {
		t.Fatalf("PrepareChange: %v", err)
	}
	applied, err := manager.ApplyChange(context.Background(), cs.ID, cs.PlanHash)
	if err != nil {
		t.Fatalf("ApplyChange: %v", err)
	}
	if applied.Status != ChangeSetStatusApplied {
		t.Fatalf("status = %q, want applied", applied.Status)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("target exists after delete/quarantine: %v", err)
	}
	if _, err := os.Lstat(applied.QuarantinePath); err != nil {
		t.Fatalf("quarantine missing: %v", err)
	}
	restored, err := manager.RestoreQuarantine(context.Background(), cs.ID, cs.PlanHash)
	if err != nil {
		t.Fatalf("RestoreQuarantine: %v", err)
	}
	if restored.Status != ChangeSetStatusRestored || readText(t, target) != "delete me" {
		t.Fatalf("restored = %#v file=%q", restored, readText(t, target))
	}
}

func TestChangeSetPermanentDeleteRequiresExplicitApplyMode(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("delete me"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := newChangeSetTestManager(t, root, nil)
	cs, err := manager.PrepareChange(context.Background(), ChangeSetRequest{
		Operation:       ChangeOpDelete,
		Path:            "@documents/note.txt",
		PermanentDelete: true,
	})
	if err != nil {
		t.Fatalf("PrepareChange: %v", err)
	}
	if !cs.PermanentDelete || cs.QuarantinePath != "" {
		t.Fatalf("changeset = %#v, want permanent delete plan without quarantine path", cs)
	}

	if _, err := manager.ApplyChange(context.Background(), cs.ID, cs.PlanHash); err == nil {
		t.Fatal("ApplyChange without explicit permanent delete mode should fail closed")
	}
	if got := readText(t, target); got != "delete me" {
		t.Fatalf("file changed after rejected apply: %q", got)
	}

	applied, err := manager.ApplyChangeWithOptions(context.Background(), cs.ID, cs.PlanHash, ChangeApplyOptions{
		DeleteMode: DeleteModePermanent,
	})
	if err != nil {
		t.Fatalf("ApplyChangeWithOptions: %v", err)
	}
	if applied.Status != ChangeSetStatusApplied {
		t.Fatalf("status = %q, want applied", applied.Status)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("target exists after permanent delete: %v", err)
	}
}

func TestChangeSetRecursiveDeleteQuarantinesTreeWithPlanStats(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "tree")
	if err := os.MkdirAll(filepath.Join(targetDir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "a.txt"), []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "sub", "b.txt"), []byte("bbbb"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := newChangeSetTestManager(t, root, nil)

	cs, err := manager.PrepareChange(context.Background(), ChangeSetRequest{
		Operation: ChangeOpDelete,
		Path:      "@documents/tree",
		Recursive: true,
	})
	if err != nil {
		t.Fatalf("PrepareChange: %v", err)
	}
	if !cs.Recursive {
		t.Fatalf("Recursive = false, want true")
	}
	if cs.Preview.AffectedFiles != 4 {
		t.Fatalf("AffectedFiles = %d, want 4 entries including directories", cs.Preview.AffectedFiles)
	}
	if cs.Preview.Bytes != 7 {
		t.Fatalf("Bytes = %d, want 7", cs.Preview.Bytes)
	}
	if len(cs.Preview.Ops) != 4 {
		t.Fatalf("Ops = %#v, want one op per affected tree entry", cs.Preview.Ops)
	}

	if _, err := manager.ApplyChange(context.Background(), cs.ID, cs.PlanHash); err == nil {
		t.Fatal("ApplyChange without explicit recursive approval should fail closed")
	}
	applied, err := manager.ApplyChangeWithOptions(context.Background(), cs.ID, cs.PlanHash, ChangeApplyOptions{
		DeleteMode: DeleteModeQuarantine,
		Recursive:  true,
	})
	if err != nil {
		t.Fatalf("ApplyChangeWithOptions: %v", err)
	}
	if applied.Status != ChangeSetStatusApplied {
		t.Fatalf("applied = %#v, want applied status", applied)
	}
	if _, err := os.Lstat(targetDir); !os.IsNotExist(err) {
		t.Fatalf("target tree exists after quarantine delete: %v", err)
	}
	if readText(t, filepath.Join(applied.QuarantinePath, "sub", "b.txt")) != "bbbb" {
		t.Fatalf("quarantine tree missing nested file")
	}
	restored, err := manager.RestoreQuarantine(context.Background(), cs.ID, cs.PlanHash)
	if err != nil {
		t.Fatalf("RestoreQuarantine: %v", err)
	}
	if restored.Status != ChangeSetStatusRestored || readText(t, filepath.Join(targetDir, "a.txt")) != "aaa" {
		t.Fatalf("restored = %#v", restored)
	}
}

func TestChangeSetRmdirRejectsNonEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "tree")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "a.txt"), []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := newChangeSetTestManager(t, root, nil)

	if _, err := manager.PrepareChange(context.Background(), ChangeSetRequest{
		Operation: ChangeOpRmdir,
		Path:      "@documents/tree",
	}); err == nil {
		t.Fatal("rmdir should reject non-empty directories")
	}
}

func TestChangeSetRmdirQuarantinesEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "empty")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manager := newChangeSetTestManager(t, root, nil)
	cs, err := manager.PrepareChange(context.Background(), ChangeSetRequest{
		Operation: ChangeOpRmdir,
		Path:      "@documents/empty",
	})
	if err != nil {
		t.Fatalf("PrepareChange: %v", err)
	}
	applied, err := manager.ApplyChange(context.Background(), cs.ID, cs.PlanHash)
	if err != nil {
		t.Fatalf("ApplyChange: %v", err)
	}
	if applied.Status != ChangeSetStatusApplied {
		t.Fatalf("status = %q, want applied", applied.Status)
	}
	if _, err := os.Lstat(targetDir); !os.IsNotExist(err) {
		t.Fatalf("target dir exists after rmdir quarantine: %v", err)
	}
	if info, err := os.Lstat(applied.QuarantinePath); err != nil || !info.IsDir() {
		t.Fatalf("quarantine dir missing: info=%#v err=%v", info, err)
	}
}

func TestChangeSetCreateRejectsSymlinkParentEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	manager := newChangeSetTestManager(t, root, nil)
	if _, err := manager.PrepareChange(context.Background(), ChangeSetRequest{
		Operation: ChangeOpCreateFile,
		Path:      "@documents/link/new.txt",
		Content:   "escape",
	}); err == nil {
		t.Fatal("PrepareChange should reject create through symlink parent escaping root")
	}
	if _, err := os.Lstat(filepath.Join(outside, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside target was created or inaccessible: %v", err)
	}
}

func TestChangeSetOverwriteRejectsSymlinkTargetReplacement(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(target, []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	manager := newChangeSetTestManager(t, root, nil)
	if _, err := manager.PrepareChange(context.Background(), ChangeSetRequest{
		Operation: ChangeOpOverwriteFile,
		Path:      "@documents/link.txt",
		Content:   "replace",
	}); err == nil {
		t.Fatal("PrepareChange should reject overwriting through a symlink path")
	}
	if got := readText(t, target); got != "target" {
		t.Fatalf("symlink target was modified: %q", got)
	}
}

func TestSQLiteChangeSetStoreListsApprovalPendingAfterRestart(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	createTestChangeSetTables(t, sqlDB)

	store := NewSQLiteChangeSetStore(sqlDB)
	manager := newChangeSetTestManager(t, root, store)
	cs, err := manager.PrepareChange(context.Background(), ChangeSetRequest{
		Operation: ChangeOpOverwriteFile,
		Path:      "@documents/note.txt",
		Content:   "new",
	})
	if err != nil {
		t.Fatalf("PrepareChange: %v", err)
	}

	restarted := newChangeSetTestManager(t, root, store)
	pending, err := restarted.ListChanges(context.Background(), []ChangeSetStatus{ChangeSetStatusApprovalPending})
	if err != nil {
		t.Fatalf("ListChanges: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != cs.ID || pending[0].PlanHash != cs.PlanHash {
		t.Fatalf("pending = %#v, want restarted manager to list prepared changeset", pending)
	}
}

func TestSQLiteChangeSetStoreDoesNotPersistDiffBody(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "note.txt")
	if err := os.WriteFile(target, []byte("secret old"), 0o644); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	createTestChangeSetTables(t, sqlDB)
	manager := newChangeSetTestManager(t, root, NewSQLiteChangeSetStore(sqlDB))
	cs, err := manager.PrepareChange(context.Background(), ChangeSetRequest{
		Operation: ChangeOpOverwriteFile,
		Path:      "@documents/note.txt",
		Content:   "secret new",
	})
	if err != nil {
		t.Fatalf("PrepareChange: %v", err)
	}
	if !strings.Contains(cs.Preview.Diff, "secret") {
		t.Fatalf("expected in-memory preview diff for approval, got %q", cs.Preview.Diff)
	}
	var previewJSON string
	if err := sqlDB.QueryRow(`SELECT preview_json FROM host_resource_changesets WHERE id = ?`, cs.ID).Scan(&previewJSON); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(previewJSON, "secret old") || strings.Contains(previewJSON, "secret new") {
		t.Fatalf("preview_json persisted diff body: %s", previewJSON)
	}
}

func TestSQLiteChangeSetStoreSaveRollsBackWhenOpsFail(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	createTestChangeSetTables(t, sqlDB)
	if _, err := sqlDB.Exec(`
CREATE TRIGGER fail_changeset_ops_insert
BEFORE INSERT ON host_resource_change_ops
BEGIN
	SELECT RAISE(ABORT, 'ops insert failed');
END;`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	store := NewSQLiteChangeSetStore(sqlDB)
	cs := testPersistentChangeSet("cs-save")
	if err := store.Save(context.Background(), cs); err == nil {
		t.Fatal("Save succeeded, want ops insert failure")
	}
	var count int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM host_resource_changesets WHERE id = ?`, cs.ID).Scan(&count); err != nil {
		t.Fatalf("count changesets: %v", err)
	}
	if count != 0 {
		t.Fatalf("changeset rows after failed Save = %d, want 0", count)
	}
}

func TestSQLiteChangeSetStoreUpdateRollsBackWhenOpsFail(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	createTestChangeSetTables(t, sqlDB)
	store := NewSQLiteChangeSetStore(sqlDB)
	cs := testPersistentChangeSet("cs-update")
	if err := store.Save(context.Background(), cs); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := sqlDB.Exec(`
CREATE TRIGGER fail_changeset_ops_update_insert
BEFORE INSERT ON host_resource_change_ops
BEGIN
	SELECT RAISE(ABORT, 'ops update insert failed');
END;`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	next := cs
	next.Status = ChangeSetStatusApplied
	next.ErrorMessage = "should roll back"
	next.Preview.Ops = append(next.Preview.Ops, ChangeOp{
		Index:      1,
		Operation:  ChangeOpOverwriteFile,
		SourcePath: "@documents/other.txt",
		TargetPath: "@documents/other.txt",
		Bytes:      5,
	})
	if err := store.Update(context.Background(), next); err == nil {
		t.Fatal("Update succeeded, want ops insert failure")
	}
	got, ok, err := store.Get(context.Background(), cs.ID)
	if err != nil || !ok {
		t.Fatalf("Get after failed Update = %#v %v %v", got, ok, err)
	}
	if got.Status != ChangeSetStatusApprovalPending || got.ErrorMessage != "" {
		t.Fatalf("changeset after failed Update = %#v, want original status and error", got)
	}
	var opCount int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM host_resource_change_ops WHERE changeset_id = ?`, cs.ID).Scan(&opCount); err != nil {
		t.Fatalf("count ops: %v", err)
	}
	if opCount != 1 {
		t.Fatalf("op count after failed Update = %d, want original 1", opCount)
	}
}

func TestSQLiteChangeSetStoreUpdateMissingWrapsSentinel(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	createTestChangeSetTables(t, sqlDB)

	err = NewSQLiteChangeSetStore(sqlDB).Update(context.Background(), testPersistentChangeSet("missing"))
	if !errors.Is(err, ErrChangeSetNotFound) {
		t.Fatalf("Update missing error = %v, want ErrChangeSetNotFound", err)
	}
}

func createTestChangeSetTables(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	if _, err := sqlDB.Exec(`
CREATE TABLE host_resource_changesets (
    id TEXT PRIMARY KEY,
    principal_kind TEXT NOT NULL DEFAULT '',
    principal_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    operation TEXT NOT NULL,
    source_ref_json TEXT NOT NULL DEFAULT '{}',
    target_ref_json TEXT NOT NULL DEFAULT '{}',
    target_display_path TEXT NOT NULL DEFAULT '',
    baseline_hash TEXT NOT NULL DEFAULT '',
    baseline_file_id TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    plan_hash TEXT NOT NULL,
    staging_path TEXT NOT NULL DEFAULT '',
    quarantine_path TEXT NOT NULL DEFAULT '',
    preview_json TEXT NOT NULL DEFAULT '{}',
    permanent_delete INTEGER NOT NULL DEFAULT 0,
    recursive INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    applied_at TEXT
);
CREATE TABLE host_resource_change_ops (
    id TEXT PRIMARY KEY,
    changeset_id TEXT NOT NULL,
    op_index INTEGER NOT NULL,
    operation TEXT NOT NULL,
    source_display_path TEXT NOT NULL DEFAULT '',
    target_display_path TEXT NOT NULL DEFAULT '',
    source_hash TEXT NOT NULL DEFAULT '',
    target_hash TEXT NOT NULL DEFAULT '',
    bytes INTEGER NOT NULL DEFAULT 0,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL
);`); err != nil {
		t.Fatal(err)
	}
}

func testPersistentChangeSet(id string) ChangeSet {
	now := time.Unix(500, 0).UTC()
	return ChangeSet{
		ID:        id,
		Principal: PrincipalRef{Kind: PrincipalWorkTask, ID: "task-1"},
		Status:    ChangeSetStatusApprovalPending,
		Operation: ChangeOpOverwriteFile,
		Source: ResourceRef{
			ID:          "local:source",
			Provider:    "host",
			DisplayPath: "@documents/note.txt",
		},
		Target: ResourceRef{
			ID:          "local:target",
			Provider:    "host",
			DisplayPath: "@documents/note.txt",
		},
		TargetDisplayPath: "@documents/note.txt",
		BaselineHash:      "sha256:old",
		BaselineFileID:    "file-old",
		ContentHash:       "sha256:new",
		PlanHash:          "sha256:plan",
		Preview: ChangePreview{
			Summary: "overwrite @documents/note.txt",
			Ops: []ChangeOp{{
				Index:      0,
				Operation:  ChangeOpOverwriteFile,
				SourcePath: "@documents/note.txt",
				TargetPath: "@documents/note.txt",
				SourceHash: "sha256:old",
				TargetHash: "sha256:new",
				Bytes:      3,
			}},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestCopyFileVerifyRemoveFallbackCopiesThenDeletesSource(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.txt")
	target := filepath.Join(root, "target.txt")
	data := []byte("copy fallback")
	if err := os.WriteFile(source, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyFileVerifyRemove(source, target, "sha256:"+sha256Hex(data)); err != nil {
		t.Fatalf("copyFileVerifyRemove: %v", err)
	}
	if _, err := os.Lstat(source); !os.IsNotExist(err) {
		t.Fatalf("source exists after fallback move: %v", err)
	}
	if got := readText(t, target); got != "copy fallback" {
		t.Fatalf("target = %q, want copied content", got)
	}
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
