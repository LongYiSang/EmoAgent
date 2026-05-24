package memoryhost

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/storage"
	_ "modernc.org/sqlite"
)

func TestOpenWithOptionsCreatesMemoryDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")

	host, err := OpenWithOptions(context.Background(), memorycore.Options{
		DBPath:      dbPath,
		AutoMigrate: true,
		EnableFTS:   true,
	}, testMemoryLogger())
	if err != nil {
		t.Fatalf("OpenWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })

	if host.DBPath != dbPath {
		t.Fatalf("Host.DBPath = %q, want %q", host.DBPath, dbPath)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("memory db was not created: %v", err)
	}
}

func TestOpenWithOptionsRequiresAutoMigrate(t *testing.T) {
	_, err := OpenWithOptions(context.Background(), memorycore.Options{
		DBPath: filepath.Join(t.TempDir(), "memory.db"),
	}, testMemoryLogger())
	if err == nil {
		t.Fatal("OpenWithOptions succeeded with AutoMigrate=false, want error")
	}
	if !strings.Contains(err.Error(), "AutoMigrate") {
		t.Fatalf("error = %q, want AutoMigrate", err.Error())
	}
}

func TestOpenFromConfigCreatesMemoryDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	configPath := writeMemoryCoreConfig(t, dir, true, true, dbPath)

	host, err := OpenFromConfig(context.Background(), configPath, testMemoryLogger())
	if err != nil {
		t.Fatalf("OpenFromConfig: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })

	if host.Source != configPath {
		t.Fatalf("Host.Source = %q, want %q", host.Source, configPath)
	}
	if host.DBPath != filepath.ToSlash(dbPath) {
		t.Fatalf("Host.DBPath = %q, want %q", host.DBPath, filepath.ToSlash(dbPath))
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("memory db was not created: %v", err)
	}
}

func TestOpenFromConfigRejectsMissingConfig(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yaml")

	_, err := OpenFromConfig(context.Background(), missing, testMemoryLogger())
	if err == nil {
		t.Fatal("OpenFromConfig succeeded with missing config, want error")
	}
	if !strings.Contains(err.Error(), "load memorycore config") {
		t.Fatalf("error = %q, want load memorycore config", err.Error())
	}
}

func TestOpenFromConfigRequiresEnabledConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := writeMemoryCoreConfig(t, dir, false, true, filepath.Join(dir, "memory.db"))

	_, err := OpenFromConfig(context.Background(), configPath, testMemoryLogger())
	if err == nil {
		t.Fatal("OpenFromConfig succeeded with enabled=false, want error")
	}
	if !strings.Contains(err.Error(), "enabled must be true") {
		t.Fatalf("error = %q, want enabled must be true", err.Error())
	}
}

func TestOpenFromConfigRequiresAutoMigrate(t *testing.T) {
	dir := t.TempDir()
	configPath := writeMemoryCoreConfig(t, dir, true, false, filepath.Join(dir, "memory.db"))

	_, err := OpenFromConfig(context.Background(), configPath, testMemoryLogger())
	if err == nil {
		t.Fatal("OpenFromConfig succeeded with auto_migrate=false, want error")
	}
	if !strings.Contains(err.Error(), "core.auto_migrate must be true") {
		t.Fatalf("error = %q, want core.auto_migrate must be true", err.Error())
	}
}

func TestBridgeEnsureAppendAndFinalizeSegment(t *testing.T) {
	ctx := context.Background()
	logger := testMemoryLogger()
	chatDB := openBridgeChatDB(t, logger)
	if err := chatDB.CreateSession(ctx, "chat-1", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	memoryDBPath := filepath.Join(t.TempDir(), "memory.db")
	host, err := OpenWithOptions(ctx, memorycore.Options{
		DBPath:      memoryDBPath,
		PersonaID:   "default",
		AutoMigrate: true,
		EnableFTS:   true,
	}, logger)
	if err != nil {
		t.Fatalf("OpenWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })

	bridge := NewBridge(host, chatDB, logger)
	segment, err := bridge.EnsureSegment(ctx, "chat-1", "default")
	if err != nil {
		t.Fatalf("EnsureSegment: %v", err)
	}
	if segment.SegmentID == "" || segment.MemorySessionID == "" {
		t.Fatalf("segment = %#v, want ids", segment)
	}

	userEpisodeID, err := bridge.AppendUserEpisode(ctx, segment.SegmentID, "msg-user", "hello")
	if err != nil {
		t.Fatalf("AppendUserEpisode: %v", err)
	}
	assistantEpisodeID, err := bridge.AppendAssistantEpisode(ctx, segment.SegmentID, "msg-assistant", "hi")
	if err != nil {
		t.Fatalf("AppendAssistantEpisode: %v", err)
	}
	if userEpisodeID == "" || assistantEpisodeID == "" || userEpisodeID == assistantEpisodeID {
		t.Fatalf("episode ids = %q/%q, want distinct non-empty ids", userEpisodeID, assistantEpisodeID)
	}

	stored, err := chatDB.GetMemorySegment(ctx, segment.SegmentID)
	if err != nil {
		t.Fatalf("GetMemorySegment: %v", err)
	}
	if stored.LastUserEpisodeID != userEpisodeID || stored.LastAssistantEpisodeID != assistantEpisodeID {
		t.Fatalf("stored episode ids = %#v, want %q/%q", stored, userEpisodeID, assistantEpisodeID)
	}

	requireMemoryEpisodeCount(t, memoryDBPath, segment.MemorySessionID, "user", 1)
	requireMemoryEpisodeCount(t, memoryDBPath, segment.MemorySessionID, "assistant", 1)

	if err := bridge.FinalizeSegment(ctx, segment.SegmentID, "manual_debug", "summary text"); err != nil {
		t.Fatalf("FinalizeSegment: %v", err)
	}
	finalized, err := chatDB.GetMemorySegment(ctx, segment.SegmentID)
	if err != nil {
		t.Fatalf("GetMemorySegment(finalized): %v", err)
	}
	if finalized.FinalizedAt == "" || finalized.FinalizeReason != "manual_debug" || finalized.Summary != "summary text" {
		t.Fatalf("finalized segment = %#v, want finalized manual_debug summary", finalized)
	}
	requireMemorySessionEnded(t, memoryDBPath, segment.MemorySessionID, "summary text")
}

func TestBridgeRolloverFinalizesCurrentSegmentAndStartsNext(t *testing.T) {
	ctx := context.Background()
	logger := testMemoryLogger()
	chatDB := openBridgeChatDB(t, logger)
	if err := chatDB.CreateSession(ctx, "chat-rollover", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	host, err := OpenWithOptions(ctx, memorycore.Options{
		DBPath:      filepath.Join(t.TempDir(), "memory.db"),
		PersonaID:   "default",
		AutoMigrate: true,
		EnableFTS:   true,
	}, logger)
	if err != nil {
		t.Fatalf("OpenWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = host.Close() })

	bridge := NewBridge(host, chatDB, logger)
	first, err := bridge.EnsureSegment(ctx, "chat-rollover", "default")
	if err != nil {
		t.Fatalf("EnsureSegment(first): %v", err)
	}
	second, err := bridge.RolloverSegment(ctx, "chat-rollover", "default", "session_resume")
	if err != nil {
		t.Fatalf("RolloverSegment: %v", err)
	}
	if second.SegmentID == first.SegmentID || second.MemorySessionID == first.MemorySessionID {
		t.Fatalf("second segment = %#v, first = %#v; want new segment and memory session", second, first)
	}

	firstStored, err := chatDB.GetMemorySegment(ctx, first.SegmentID)
	if err != nil {
		t.Fatalf("GetMemorySegment(first): %v", err)
	}
	if firstStored.FinalizedAt == "" || firstStored.FinalizeReason != "session_resume" {
		t.Fatalf("first segment = %#v, want finalized session_resume", firstStored)
	}
	secondStored, err := chatDB.GetMemorySegment(ctx, second.SegmentID)
	if err != nil {
		t.Fatalf("GetMemorySegment(second): %v", err)
	}
	if secondStored.SegmentIndex != 2 || secondStored.FinalizedAt != "" {
		t.Fatalf("second segment = %#v, want active index 2", secondStored)
	}
	link, err := chatDB.GetMemoryChatLink(ctx, "chat-rollover")
	if err != nil {
		t.Fatalf("GetMemoryChatLink: %v", err)
	}
	if link == nil || link.CurrentMemorySessionID != second.MemorySessionID {
		t.Fatalf("link = %#v, want current %q", link, second.MemorySessionID)
	}
}

func writeMemoryCoreConfig(t *testing.T, dir string, enabled bool, autoMigrate bool, dbPath string) string {
	t.Helper()

	configPath := filepath.Join(dir, "memorycore.yaml")
	body := "schema_version: memorycore.config.v0.2\n" +
		"enabled: " + boolYAML(enabled) + "\n" +
		"core:\n" +
		"  db_path: " + filepath.ToSlash(dbPath) + "\n" +
		"  persona_id: default\n" +
		"  auto_migrate: " + boolYAML(autoMigrate) + "\n" +
		"  enable_fts: true\n"
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile memorycore config: %v", err)
	}
	return configPath
}

func boolYAML(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func testMemoryLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func openBridgeChatDB(t *testing.T, logger *slog.Logger) *storage.DB {
	t.Helper()

	db, err := storage.Open(filepath.Join(t.TempDir(), "chat.db"), logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func requireMemoryEpisodeCount(t *testing.T, dbPath string, sessionID string, role string, want int) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open(memory): %v", err)
	}
	defer db.Close()

	var got int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM episodes
		WHERE session_id = ? AND role = ?
	`, sessionID, role).Scan(&got); err != nil {
		t.Fatalf("count memory episodes: %v", err)
	}
	if got != want {
		t.Fatalf("episode count for role %s = %d, want %d", role, got, want)
	}
}

func requireMemorySessionEnded(t *testing.T, dbPath string, sessionID string, wantSummary string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open(memory): %v", err)
	}
	defer db.Close()

	var endedAt string
	var summary string
	if err := db.QueryRow(`
		SELECT COALESCE(ended_at, ''), COALESCE(summary, '')
		FROM sessions
		WHERE id = ?
	`, sessionID).Scan(&endedAt, &summary); err != nil {
		t.Fatalf("read memory session: %v", err)
	}
	if endedAt == "" || summary != wantSummary {
		t.Fatalf("memory session ended_at/summary = %q/%q, want ended and %q", endedAt, summary, wantSummary)
	}
}
