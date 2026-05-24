package memoryhost

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
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
