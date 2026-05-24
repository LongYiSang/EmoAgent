package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
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
	tables := []string{"sessions", "messages", "personas", "config_runtime", "llm_providers", "agent_configs", "schema_version", "pending_decisions", "archived_decisions", "memory_chat_links", "memory_segments"}
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
		hasKeyColumn          bool
		hasNameColumn         bool
		hasWorkProgressColumn bool
		keyIsPK               bool
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
		case "work_progress_phrases":
			hasWorkProgressColumn = true
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
	if !hasWorkProgressColumn {
		t.Fatal("personas table missing work_progress_phrases column")
	}
	if !keyIsPK {
		t.Fatal("personas.key should be the primary key")
	}
}

func TestOpenAndMigrate_CreatesPendingDecisionTables(t *testing.T) {
	db := testDB(t)

	for _, table := range []string{"pending_decisions", "archived_decisions"} {
		var name string
		err := db.SqlDB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Fatalf("table %q not found: %v", table, err)
		}
	}
}

func TestOpenAndMigrate_CreatesApprovalRequestsTableAndColumns(t *testing.T) {
	db := testDB(t)

	var name string
	if err := db.SqlDB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='approval_requests'").Scan(&name); err != nil {
		t.Fatalf("table %q not found: %v", "approval_requests", err)
	}

	rows, err := db.SqlDB().Query("PRAGMA table_info(approval_requests)")
	if err != nil {
		t.Fatalf("PRAGMA table_info(approval_requests): %v", err)
	}
	defer rows.Close()

	columns := map[string]bool{}
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
			t.Fatalf("Scan(table_info approval_requests): %v", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err(): %v", err)
	}

	for _, required := range []string{
		"id", "session_id", "task_id", "category", "risk_level", "goal_summary", "question",
		"options_json", "recommended_option", "recommendation_reason", "reject_option_id",
		"status", "selected_option_id", "actor_channel", "actor_ref", "expires_at",
		"decided_at", "consumed_at", "created_at", "updated_at",
	} {
		if !columns[required] {
			t.Fatalf("approval_requests missing column %q", required)
		}
	}
}

func TestOpenAndMigrate_AddsApprovalRequestIDColumnsToDecisionTables(t *testing.T) {
	db := testDB(t)

	for _, table := range []string{"pending_decisions", "archived_decisions"} {
		rows, err := db.SqlDB().Query("PRAGMA table_info(" + table + ")")
		if err != nil {
			t.Fatalf("PRAGMA table_info(%s): %v", table, err)
		}

		found := false
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
				rows.Close()
				t.Fatalf("Scan(table_info %s): %v", table, err)
			}
			if name == "approval_request_id" {
				found = true
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatalf("rows.Err(%s): %v", table, err)
		}
		rows.Close()

		if !found {
			t.Fatalf("%s missing column approval_request_id", table)
		}
	}
}

func TestMigrationsDoNotDropPersonasTable(t *testing.T) {
	for _, m := range migrations {
		if strings.Contains(strings.ToUpper(m.SQL), "DROP TABLE IF EXISTS PERSONAS") {
			t.Fatalf("migration %d should not drop personas table", m.Version)
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

func TestAgentConfigMigrationDropsProfilesAndRuntimeKeys(t *testing.T) {
	db := testDB(t)

	tables := []string{"llm_providers", "agent_configs"}
	for _, table := range tables {
		var name string
		err := db.SqlDB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Fatalf("expected table %s: %v", table, err)
		}
	}
	var oldCount int
	if err := db.SqlDB().QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='llm_profiles'").Scan(&oldCount); err != nil {
		t.Fatalf("count old table: %v", err)
	}
	if oldCount != 0 {
		t.Fatalf("llm_profiles table exists after migration")
	}
}

func TestProviderPresetMigrationBackfillsExactPresetIDsOnly(t *testing.T) {
	dir := t.TempDir()
	sqlDB, err := sql.Open("sqlite", filepath.Join(dir, "migration.db"))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer sqlDB.Close()

	_, err = sqlDB.Exec(`
CREATE TABLE schema_version (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO schema_version (version) VALUES (11);
CREATE TABLE llm_providers (
    id                      TEXT PRIMARY KEY,
    name                    TEXT NOT NULL,
    protocol                TEXT NOT NULL,
    base_url                TEXT NOT NULL,
    api_key_env             TEXT NOT NULL,
    model_discovery         TEXT NOT NULL DEFAULT 'manual',
    enabled                 INTEGER NOT NULL DEFAULT 1,
    models_cache_json       TEXT NOT NULL DEFAULT '[]',
    models_cache_updated_at TEXT,
    created_at              TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at              TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO llm_providers (id, name, protocol, base_url, api_key_env, model_discovery, enabled)
VALUES
    ('moonshot', 'Moonshot', 'openai_compatible', 'https://api.moonshot.cn', 'MOONSHOT_API_KEY', 'openai_models', 1),
    ('local-kimi', 'Local Kimi', 'openai_compatible', 'https://api.example.test', 'LOCAL_KIMI_KEY', 'manual', 1);
`)
	if err != nil {
		t.Fatalf("seed v11 schema: %v", err)
	}
	if err := ApplyMigrations(sqlDB); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	var moonshotPreset, localPreset string
	if err := sqlDB.QueryRow("SELECT preset_id FROM llm_providers WHERE id = 'moonshot'").Scan(&moonshotPreset); err != nil {
		t.Fatalf("query moonshot preset: %v", err)
	}
	if err := sqlDB.QueryRow("SELECT preset_id FROM llm_providers WHERE id = 'local-kimi'").Scan(&localPreset); err != nil {
		t.Fatalf("query local preset: %v", err)
	}
	if moonshotPreset != "moonshot" {
		t.Fatalf("moonshot preset_id = %q, want moonshot", moonshotPreset)
	}
	if localPreset != "" {
		t.Fatalf("local-kimi preset_id = %q, want empty custom behavior", localPreset)
	}
}

func TestProviderAndAgentConfigCRUD(t *testing.T) {
	db := testDB(t)

	provider := config.LLMProvider{
		ID:             "moonshot",
		Name:           "Moonshot",
		PresetID:       "moonshot",
		Protocol:       "openai_compatible",
		BaseURL:        "https://api.moonshot.cn",
		APIKeyEnv:      "MOONSHOT_API_KEY",
		ModelDiscovery: "openai_models",
		Enabled:        true,
	}
	if err := db.UpsertLLMProvider(provider); err != nil {
		t.Fatalf("UpsertLLMProvider: %v", err)
	}
	providers, err := db.ListLLMProviders()
	if err != nil {
		t.Fatalf("ListLLMProviders: %v", err)
	}
	if len(providers) != 1 || providers[0].ID != "moonshot" {
		t.Fatalf("providers = %#v, want moonshot", providers)
	}
	if providers[0].PresetID != "moonshot" {
		t.Fatalf("provider preset_id = %q, want moonshot", providers[0].PresetID)
	}

	temperature := 0.1
	stream := false
	agent := config.AgentConfig{
		ID:         "default",
		Name:       "Default",
		PersonaKey: "default",
		Emotion: config.AgentModelGroup{
			Main:    config.ModelBinding{ProviderID: "moonshot", Model: "kimi-k2.6", Params: llmParams(8192, nil, nil)},
			Summary: config.ModelBinding{ProviderID: "moonshot", Model: "kimi-k2.6", Params: llmParams(4096, &temperature, &stream)},
		},
		Work: config.AgentModelGroup{
			Main:    config.ModelBinding{ProviderID: "moonshot", Model: "kimi-k2.6", Params: llmParams(4096, nil, nil)},
			Summary: config.ModelBinding{ProviderID: "moonshot", Model: "kimi-k2.6", Params: llmParams(2048, &temperature, &stream)},
		},
		ContextOverrides: map[string]any{"input_budget_tokens": float64(12000)},
	}
	if err := db.UpsertAgentConfig(agent); err != nil {
		t.Fatalf("UpsertAgentConfig: %v", err)
	}
	if err := db.SetActiveAgentConfig("default"); err != nil {
		t.Fatalf("SetActiveAgentConfig: %v", err)
	}
	active, found, err := db.GetActiveAgentConfig()
	if err != nil {
		t.Fatalf("GetActiveAgentConfig: %v", err)
	}
	if !found || active != "default" {
		t.Fatalf("active = %q/%v, want default/true", active, found)
	}
	got, err := db.GetAgentConfig(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetAgentConfig: %v", err)
	}
	if got == nil || got.Emotion.Summary.Params.Temperature == nil || *got.Emotion.Summary.Params.Temperature != 0.1 {
		t.Fatalf("agent config round trip = %#v", got)
	}
	if err := db.DeleteLLMProvider("moonshot"); !errors.Is(err, ErrProviderInUse) {
		t.Fatalf("DeleteLLMProvider referenced error = %v, want ErrProviderInUse", err)
	}
	if err := db.DeleteAgentConfig("default"); !errors.Is(err, ErrCannotDeleteActiveAgentConfig) {
		t.Fatalf("DeleteAgentConfig active error = %v, want ErrCannotDeleteActiveAgentConfig", err)
	}
}

func llmParams(maxTokens int, temperature *float64, stream *bool) llm.RequestParams {
	return llm.RequestParams{
		MaxTokens:   maxTokens,
		Temperature: temperature,
		Stream:      stream,
	}
}

func TestUpsertPersona(t *testing.T) {
	db := testDB(t)

	err := db.UpsertPersona("test", "Display Name", "desc", "prompt", "warm", []string{"quirk1"}, "hello", nil)
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

	if err := db.UpsertPersona("test", "Display Name", "desc", "prompt", "warm", []string{"quirk1"}, "hello", nil); err != nil {
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
	if record.WorkProgressPhrases != "{}" {
		t.Fatalf("record.WorkProgressPhrases = %q, want {}", record.WorkProgressPhrases)
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

func TestUpsertPersonaWorkProgressPhrasesRoundTrip(t *testing.T) {
	db := testDB(t)

	phrases := map[string][]string{
		"read_file": {"看看文件"},
		"_default":  {"处理中"},
	}
	if err := db.UpsertPersona("test", "Display Name", "desc", "prompt", "warm", []string{"quirk1"}, "hello", phrases); err != nil {
		t.Fatalf("UpsertPersona: %v", err)
	}

	record, err := db.GetPersona(context.Background(), "test")
	if err != nil {
		t.Fatalf("GetPersona: %v", err)
	}
	if record == nil {
		t.Fatal("GetPersona returned nil")
	}

	var decoded map[string][]string
	if err := json.Unmarshal([]byte(record.WorkProgressPhrases), &decoded); err != nil {
		t.Fatalf("Unmarshal(WorkProgressPhrases): %v", err)
	}
	if len(decoded["read_file"]) != 1 || decoded["read_file"][0] != "看看文件" {
		t.Fatalf("decoded = %#v, want read_file phrase", decoded)
	}
}

func TestAddMessageWithMetadataStoresVisibleMessageMetadata(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	if err := db.CreateSession(ctx, "session-1", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	metadata := map[string]any{
		"kind":           "dialogue_user",
		"source":         "user",
		"token_estimate": 123,
	}
	if err := db.AddMessageWithMetadata(ctx, "msg-1", "session-1", "user", "hello", metadata); err != nil {
		t.Fatalf("AddMessageWithMetadata: %v", err)
	}

	messages, err := db.GetAllMessages(ctx, "session-1")
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	if messages[0].Metadata == "" {
		t.Fatal("Metadata is empty, want stored JSON")
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(messages[0].Metadata), &got); err != nil {
		t.Fatalf("Unmarshal(metadata): %v", err)
	}
	if got["kind"] != "dialogue_user" {
		t.Fatalf("kind = %#v, want dialogue_user", got["kind"])
	}
	if got["source"] != "user" {
		t.Fatalf("source = %#v, want user", got["source"])
	}
	if got["token_estimate"] != float64(123) {
		t.Fatalf("token_estimate = %#v, want 123", got["token_estimate"])
	}
}
