package storage

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func testDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := Open(path, logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenAndMigrate(t *testing.T) {
	db := testDB(t)

	// Verify tables exist by querying them.
	tables := []string{"sessions", "messages", "personas", "config_runtime", "schema_version"}
	for _, table := range tables {
		var name string
		err := db.SqlDB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestRuntimeConfig(t *testing.T) {
	db := testDB(t)

	// Initially empty.
	_, found, err := db.GetRuntimeConfig("test.key")
	if err != nil {
		t.Fatalf("GetRuntimeConfig: %v", err)
	}
	if found {
		t.Error("expected key not found")
	}

	// Set and get.
	if err := db.SetRuntimeConfig("test.key", "hello"); err != nil {
		t.Fatalf("SetRuntimeConfig: %v", err)
	}
	val, found, err := db.GetRuntimeConfig("test.key")
	if err != nil {
		t.Fatalf("GetRuntimeConfig: %v", err)
	}
	if !found || val != "hello" {
		t.Errorf("got %q (found=%v), want hello", val, found)
	}

	// Upsert.
	if err := db.SetRuntimeConfig("test.key", "world"); err != nil {
		t.Fatalf("SetRuntimeConfig upsert: %v", err)
	}
	val, _, _ = db.GetRuntimeConfig("test.key")
	if val != "world" {
		t.Errorf("after upsert got %q, want world", val)
	}

	// GetAll.
	db.SetRuntimeConfig("another", "value")
	all, err := db.GetAllRuntimeConfig()
	if err != nil {
		t.Fatalf("GetAllRuntimeConfig: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("GetAll returned %d items, want 2", len(all))
	}
}

func TestUpsertPersona(t *testing.T) {
	db := testDB(t)

	err := db.UpsertPersona("test", "desc", "prompt", "warm", []string{"quirk1"}, "hello")
	if err != nil {
		t.Fatalf("UpsertPersona: %v", err)
	}

	names, err := db.ListPersonas()
	if err != nil {
		t.Fatalf("ListPersonas: %v", err)
	}
	if len(names) != 1 || names[0] != "test" {
		t.Errorf("ListPersonas = %v, want [test]", names)
	}
}
