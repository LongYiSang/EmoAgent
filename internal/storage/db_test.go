package storage

import (
	"context"
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
	tables := []string{"sessions", "messages", "personas", "config_runtime", "llm_profiles", "schema_version"}
	for _, table := range tables {
		var name string
		err := db.SqlDB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}

	rows, err := db.SqlDB().Query("PRAGMA table_info(personas)")
	if err != nil {
		t.Fatalf("PRAGMA table_info(personas): %v", err)
	}
	defer rows.Close()

	var (
		hasKeyColumn  bool
		hasNameColumn bool
		keyIsPK       bool
	)
	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal interface{}
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			t.Fatalf("Scan(table_info): %v", err)
		}
		switch name {
		case "key":
			hasKeyColumn = true
			keyIsPK = pk == 1
		case "name":
			hasNameColumn = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err(): %v", err)
	}
	if !hasKeyColumn {
		t.Fatal("personas table missing key column")
	}
	if !hasNameColumn {
		t.Fatal("personas table missing name column")
	}
	if !keyIsPK {
		t.Fatal("personas.key should be the primary key")
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

	err := db.UpsertPersona("test", "Display Name", "desc", "prompt", "warm", []string{"quirk1"}, "hello")
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

func TestGetAndDeletePersona(t *testing.T) {
	db := testDB(t)

	if err := db.UpsertPersona("test", "Display Name", "desc", "prompt", "warm", []string{"quirk1"}, "hello"); err != nil {
		t.Fatalf("UpsertPersona: %v", err)
	}

	record, err := db.GetPersona(context.Background(), "test")
	if err != nil {
		t.Fatalf("GetPersona: %v", err)
	}
	if record == nil {
		t.Fatal("GetPersona returned nil")
	}
	if record.Key != "test" {
		t.Fatalf("record.Key = %q, want test", record.Key)
	}
	if record.Name != "Display Name" {
		t.Fatalf("record.Name = %q, want Display Name", record.Name)
	}

	if err := db.DeletePersona(context.Background(), "test"); err != nil {
		t.Fatalf("DeletePersona: %v", err)
	}

	record, err = db.GetPersona(context.Background(), "test")
	if err != nil {
		t.Fatalf("GetPersona(after delete): %v", err)
	}
	if record != nil {
		t.Fatalf("GetPersona(after delete) = %#v, want nil", record)
	}
}

func TestLLMProfileCRUD(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	if got, err := db.GetLLMProfile(ctx, "default"); err != nil {
		t.Fatalf("GetLLMProfile missing: %v", err)
	} else if got != nil {
		t.Fatalf("GetLLMProfile missing = %#v, want nil", got)
	}

	if err := db.UpsertLLMProfile("default", "openai", "https://api.openai.com", "gpt-4o", "gpt-4o-mini", 4096, 0.7, "OPENAI_API_KEY"); err != nil {
		t.Fatalf("UpsertLLMProfile: %v", err)
	}

	profile, err := db.GetLLMProfile(ctx, "default")
	if err != nil {
		t.Fatalf("GetLLMProfile: %v", err)
	}
	if profile == nil {
		t.Fatal("GetLLMProfile returned nil")
	}
	if profile.Name != "default" || profile.APIKeyEnv != "OPENAI_API_KEY" {
		t.Fatalf("GetLLMProfile = %#v, want name default and APIKeyEnv OPENAI_API_KEY", profile)
	}

	profiles, err := db.ListLLMProfiles()
	if err != nil {
		t.Fatalf("ListLLMProfiles: %v", err)
	}
	if len(profiles) != 1 || profiles[0].Name != "default" {
		t.Fatalf("ListLLMProfiles = %v, want [default]", profiles)
	}

	if err := db.UpsertLLMProfile("default", "openai", "https://api.openai.com", "gpt-4o", "gpt-4.1", 2048, 0.2, "MOONSHOT_API_KEY"); err != nil {
		t.Fatalf("UpsertLLMProfile update: %v", err)
	}

	profile, err = db.GetLLMProfile(ctx, "default")
	if err != nil {
		t.Fatalf("GetLLMProfile after update: %v", err)
	}
	if profile == nil || profile.SummaryModel != "gpt-4.1" || profile.MaxTokens != 2048 || profile.Temperature != 0.2 || profile.APIKeyEnv != "MOONSHOT_API_KEY" {
		t.Fatalf("updated profile = %#v", profile)
	}

	if err := db.DeleteLLMProfile("default"); err != nil {
		t.Fatalf("DeleteLLMProfile: %v", err)
	}

	profile, err = db.GetLLMProfile(ctx, "default")
	if err != nil {
		t.Fatalf("GetLLMProfile after delete: %v", err)
	}
	if profile != nil {
		t.Fatalf("GetLLMProfile after delete = %#v, want nil", profile)
	}
}
