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
	return testDBWithTimezone(t, "Asia/Shanghai")
}

func testDBWithTimezone(t *testing.T, timezone string) *DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := OpenWithOptions(path, logger, StorageOptions{Timezone: timezone})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenAndMigrate(t *testing.T) {
	db := testDB(t)

	// Verify tables exist by querying them.
	tables := []string{
		"sessions",
		"messages",
		"personas",
		"config_runtime",
		"runtime_settings",
		"llm_providers",
		"agent_configs",
		"schema_version",
		"pending_decisions",
		"archived_decisions",
		"memory_chat_links",
		"memory_segments",
		"memory_extraction_jobs",
		"turns",
		"turn_events",
		"turn_outbound_events",
		"turn_idempotency",
		"agent_affect_profiles",
		"agent_affect_states",
		"agent_affect_evaluations",
		"agent_affect_events",
		"agent_affect_plugin_writes",
		"agent_affect_jobs",
		"agent_affect_job_batches",
		"plugin_installations",
		"plugin_enabled_state",
		"plugin_runtime_records",
		"plugin_access_events",
		"plugin_provider_usage",
		"plugin_kv",
	}
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

	var latestVersion int
	if err := db.SqlDB().QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&latestVersion); err != nil {
		t.Fatalf("read latest schema_version: %v", err)
	}
	if latestVersion != 24 {
		t.Fatalf("latest schema_version = %d, want 24", latestVersion)
	}
}

func TestPluginRuntimeStorageCRUD(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	installation := PluginInstallation{
		PluginID:        "com.example.echo",
		Version:         "0.1.0",
		Name:            "Echo",
		ManifestJSON:    `{"id":"com.example.echo"}`,
		SourceType:      "local_dir",
		SourceRef:       "fixture",
		PackageDigest:   "sha256:package",
		ManifestDigest:  "sha256:manifest",
		SignatureStatus: "unsigned_dev",
		StorePath:       "data/plugins/store/com.example.echo/0.1.0",
	}
	if err := db.UpsertPluginInstallation(ctx, installation); err != nil {
		t.Fatalf("UpsertPluginInstallation: %v", err)
	}
	gotInstallation, err := db.GetPluginInstallation(ctx, "com.example.echo")
	if err != nil {
		t.Fatalf("GetPluginInstallation: %v", err)
	}
	if gotInstallation == nil || gotInstallation.PluginID != installation.PluginID || gotInstallation.SignatureStatus != "unsigned_dev" {
		t.Fatalf("installation = %#v", gotInstallation)
	}

	if err := db.SetPluginEnabled(ctx, "com.example.echo", "0.1.0", true, `{"tier":"runtime_safe"}`); err != nil {
		t.Fatalf("SetPluginEnabled: %v", err)
	}
	state, err := db.GetPluginEnabledState(ctx, "com.example.echo")
	if err != nil {
		t.Fatalf("GetPluginEnabledState: %v", err)
	}
	if state == nil || !state.Enabled || state.UserGrantJSON != `{"tier":"runtime_safe"}` {
		t.Fatalf("enabled state = %#v", state)
	}

	pid := 1234
	if err := db.UpsertPluginRuntimeRecord(ctx, PluginRuntimeRecord{
		PluginID:     "com.example.echo",
		Version:      "0.1.0",
		RuntimeKind:  "python_process",
		Status:       "running",
		PID:          &pid,
		RestartCount: 1,
	}); err != nil {
		t.Fatalf("UpsertPluginRuntimeRecord: %v", err)
	}
	runtimeRecord, err := db.GetPluginRuntimeRecord(ctx, "com.example.echo")
	if err != nil {
		t.Fatalf("GetPluginRuntimeRecord: %v", err)
	}
	if runtimeRecord == nil || runtimeRecord.PID == nil || *runtimeRecord.PID != 1234 || runtimeRecord.Status != "running" {
		t.Fatalf("runtime record = %#v", runtimeRecord)
	}

	if err := db.RecordPluginAccessEvent(ctx, PluginAccessEvent{
		PluginID:       "com.example.echo",
		AccessKind:     "facade.call",
		Capability:     "plugin.kv",
		Status:         "allowed",
		RequestSummary: "plugin.kv.set",
	}); err != nil {
		t.Fatalf("RecordPluginAccessEvent: %v", err)
	}
	events, err := db.ListPluginAccessEvents(ctx, "com.example.echo", 10)
	if err != nil {
		t.Fatalf("ListPluginAccessEvents: %v", err)
	}
	if len(events) != 1 || events[0].Capability != "plugin.kv" {
		t.Fatalf("events = %#v", events)
	}

	if err := db.RecordPluginProviderUsage(ctx, PluginProviderUsage{
		PluginID:        "com.example.echo",
		ProviderID:      "fake",
		Model:           "fake-model",
		Purpose:         "test",
		EstimatedTokens: 4,
		Status:          "success",
	}); err != nil {
		t.Fatalf("RecordPluginProviderUsage: %v", err)
	}
	usages, err := db.ListPluginProviderUsage(ctx, "com.example.echo", 10)
	if err != nil {
		t.Fatalf("ListPluginProviderUsage: %v", err)
	}
	if len(usages) != 1 || usages[0].ProviderID != "fake" {
		t.Fatalf("usages = %#v", usages)
	}

	if err := db.PluginKVSet(ctx, "com.example.echo", "seen", `{"count":1}`); err != nil {
		t.Fatalf("PluginKVSet: %v", err)
	}
	value, ok, err := db.PluginKVGet(ctx, "com.example.echo", "seen")
	if err != nil {
		t.Fatalf("PluginKVGet: %v", err)
	}
	if !ok || value != `{"count":1}` {
		t.Fatalf("PluginKVGet = %q/%v", value, ok)
	}
}

func TestApplyMigrationsRepairsDriftedPluginRuntimeSchema(t *testing.T) {
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
INSERT INTO schema_version (version) VALUES (24);
CREATE TABLE plugin_access_events (
    id TEXT PRIMARY KEY,
    plugin_id TEXT NOT NULL,
    access_kind TEXT NOT NULL,
    status TEXT NOT NULL
);
`)
	if err != nil {
		t.Fatalf("seed drifted plugin schema: %v", err)
	}

	if err := ApplyMigrations(sqlDB); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	columns, err := tableColumns(sqlDB, "plugin_access_events")
	if err != nil {
		t.Fatalf("tableColumns: %v", err)
	}
	for _, required := range []string{"capability", "request_summary", "input_hash", "output_hash", "duration_ms", "created_at"} {
		if !columns[required] {
			t.Fatalf("plugin_access_events missing repaired column %q", required)
		}
	}
}

func TestOpenAndMigrate_CreatesAgentAffectMoodOwnerColumns(t *testing.T) {
	db := testDB(t)

	for _, table := range []string{"agent_affect_states", "agent_affect_evaluations", "agent_affect_events"} {
		required := []string{
			"mood_description",
			"mood_reason",
			"prompt_mood_text",
			"mood_owner_scope",
			"mood_owner_id",
		}
		if table != "agent_affect_states" {
			required = append(required, "batch_id")
		}
		assertTableColumns(t, db, table, required)
	}
}

func TestOpenAndMigrate_CreatesAgentAffectJobQueueSchema(t *testing.T) {
	db := testDB(t)

	assertTableColumns(t, db, "agent_affect_jobs", []string{
		"seq", "id", "persona_id", "session_id", "turn_id",
		"mood_owner_scope", "mood_owner_id", "job_type", "batchable", "barrier_kind",
		"status", "priority", "run_after", "attempts", "max_attempts",
		"claimed_by", "claimed_until", "trigger_json", "input_mode",
		"user_text", "assistant_text", "input_summary", "memory_prompt_block",
		"base_state_id", "base_state_updated_at", "batch_id",
		"result_evaluation_id", "result_event_id", "error_message",
		"created_at", "started_at", "finished_at",
	})
	assertTableColumns(t, db, "agent_affect_job_batches", []string{
		"id", "persona_id", "mood_owner_scope", "mood_owner_id",
		"job_type", "status", "job_count", "first_job_seq", "last_job_seq",
		"job_ids_json", "session_ids_json", "turn_ids_json",
		"batch_input_summary", "context_window_snapshot_json",
		"evaluation_id", "affect_event_id", "error_message",
		"claimed_by", "started_at", "finished_at",
	})
}

func TestRuntimeSettingsCRUD(t *testing.T) {
	db := testDB(t)

	if err := db.UpsertRuntimeSetting("memory.sidecar", "managed", `{"enabled":true}`, "ui"); err != nil {
		t.Fatalf("UpsertRuntimeSetting: %v", err)
	}
	if err := db.UpsertRuntimeSetting("memory.sidecar", "managed", `{"enabled":false}`, "test"); err != nil {
		t.Fatalf("UpsertRuntimeSetting update: %v", err)
	}

	setting, ok, err := db.GetRuntimeSetting("memory.sidecar", "managed")
	if err != nil {
		t.Fatalf("GetRuntimeSetting: %v", err)
	}
	if !ok {
		t.Fatal("GetRuntimeSetting ok = false, want true")
	}
	if setting.Namespace != "memory.sidecar" || setting.Key != "managed" || setting.ValueJSON != `{"enabled":false}` || setting.Source != "test" {
		t.Fatalf("setting = %#v", setting)
	}
	if setting.UpdatedAt == "" {
		t.Fatal("UpdatedAt is empty")
	}

	settings, err := db.ListRuntimeSettings()
	if err != nil {
		t.Fatalf("ListRuntimeSettings: %v", err)
	}
	if len(settings) != 1 || settings[0].Namespace != "memory.sidecar" || settings[0].Key != "managed" {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestOpenAndMigrate_CreatesTurnRuntimeSchema(t *testing.T) {
	db := testDB(t)

	assertTableColumns(t, db, "turns", []string{
		"id", "idempotency_key", "source", "source_event_id", "kind",
		"session_id", "persona_key", "state", "status", "error_kind",
		"error_message", "started_at", "updated_at", "completed_at",
	})
	assertTableColumns(t, db, "turn_events", []string{
		"id", "turn_id", "seq", "stage", "event_type", "payload_json", "created_at",
	})
	assertTableColumns(t, db, "turn_outbound_events", []string{
		"id", "turn_id", "seq", "event_type", "payload_json", "delivery_status", "created_at", "delivered_at",
	})
	assertTableColumns(t, db, "turn_idempotency", []string{
		"idempotency_key", "turn_id", "status", "created_at", "updated_at",
	})

	for _, indexName := range []string{
		"idx_turns_session_started",
		"idx_turns_status_updated",
		"idx_turn_events_turn_seq",
		"idx_turn_outbound_turn_seq",
	} {
		var name string
		if err := db.SqlDB().QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name=?", indexName).Scan(&name); err != nil {
			t.Fatalf("turn runtime index %q not found: %v", indexName, err)
		}
	}

	var idempotencyPK string
	if err := db.SqlDB().QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='turn_idempotency' AND sql IS NULL").Scan(&idempotencyPK); err != nil {
		t.Fatalf("turn_idempotency primary key index not found: %v", err)
	}
}

func TestOpenAndMigrate_CreatesMemoryExtractionJobSchema(t *testing.T) {
	db := testDB(t)

	requiredJobColumns := []string{
		"id", "persona_id", "chat_session_id", "segment_id", "memory_session_id",
		"trigger", "scope", "mode", "requested_by", "priority", "force",
		"episode_ids_json", "since_at", "until_at", "episode_limit",
		"status", "attempts", "max_attempts", "run_after", "claimed_by", "claimed_until",
		"request_json", "result_json", "mirror_sync_result_json",
		"error_code", "error_message", "dedupe_key",
		"created_at", "updated_at", "started_at", "finished_at",
	}
	assertTableColumns(t, db, "memory_extraction_jobs", requiredJobColumns)
	assertTableColumns(t, db, "memory_segments", []string{
		"last_extracted_until_at",
		"last_extracted_user_episode_id",
		"last_extracted_assistant_episode_id",
		"last_extraction_job_id",
		"last_extraction_error_code",
		"last_extraction_error_message",
		"extraction_attempt_count",
	})

	for _, indexName := range []string{
		"idx_memory_extraction_jobs_claim",
		"idx_memory_extraction_jobs_segment",
		"idx_memory_extraction_jobs_chat_session",
		"idx_memory_extraction_jobs_dedupe_pending",
	} {
		var name string
		if err := db.SqlDB().QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name=?", indexName).Scan(&name); err != nil {
			t.Fatalf("index %q not found: %v", indexName, err)
		}
	}
}

func assertTableColumns(t *testing.T, db *DB, table string, required []string) {
	t.Helper()

	rows, err := db.SqlDB().Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s): %v", table, err)
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
			t.Fatalf("Scan(table_info %s): %v", table, err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err(%s): %v", table, err)
	}
	for _, name := range required {
		if !columns[name] {
			t.Fatalf("%s missing column %q", table, name)
		}
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

func approvalRequestColumns(t *testing.T, db *sql.DB) map[string]bool {
	t.Helper()

	rows, err := db.Query("PRAGMA table_info(approval_requests)")
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
	return columns
}

func TestOpenAndMigrate_CreatesApprovalRequestsTableAndColumns(t *testing.T) {
	db := testDB(t)

	var name string
	if err := db.SqlDB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='approval_requests'").Scan(&name); err != nil {
		t.Fatalf("table %q not found: %v", "approval_requests", err)
	}

	columns := approvalRequestColumns(t, db.SqlDB())
	for _, required := range []string{
		"id", "session_id", "task_id", "category", "risk_level", "goal_summary", "question",
		"options_json", "recommended_option", "recommendation_reason", "reject_option_id",
		"status", "selected_option_id", "actor_channel", "actor_ref", "expires_at",
		"decided_at", "consumed_at", "created_at", "updated_at",
		"approval_kind", "tool_name", "normalized_input_hash", "path_digest", "input_preview",
	} {
		if !columns[required] {
			t.Fatalf("approval_requests missing column %q", required)
		}
	}

	var indexName string
	if err := db.SqlDB().QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name='idx_approval_requests_kind_binding'").Scan(&indexName); err != nil {
		t.Fatalf("approval_requests missing kind binding index: %v", err)
	}
}

func TestApplyMigrationsRepairsDriftedApprovalRequestSchema(t *testing.T) {
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
INSERT INTO schema_version (version)
VALUES (1), (2), (3), (4), (5), (6), (7), (8), (9), (10), (11), (12), (13), (14), (15), (16), (17);

CREATE TABLE approval_requests (
    id                    TEXT PRIMARY KEY,
    session_id            TEXT NOT NULL,
    task_id               TEXT NOT NULL,
    category              TEXT NOT NULL,
    risk_level            TEXT NOT NULL,
    goal_summary          TEXT NOT NULL,
    question              TEXT NOT NULL,
    options_json          TEXT NOT NULL,
    recommended_option    TEXT NOT NULL DEFAULT '',
    recommendation_reason TEXT NOT NULL DEFAULT '',
    reject_option_id      TEXT NOT NULL,
    status                TEXT NOT NULL,
    selected_option_id    TEXT NOT NULL DEFAULT '',
    actor_channel         TEXT NOT NULL DEFAULT '',
    actor_ref             TEXT NOT NULL DEFAULT '',
    expires_at            TEXT NOT NULL,
    decided_at            TEXT,
    consumed_at           TEXT,
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL
);
`)
	if err != nil {
		t.Fatalf("seed drifted approval_requests schema: %v", err)
	}

	if err := ApplyMigrations(sqlDB); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}

	columns := approvalRequestColumns(t, sqlDB)
	for _, required := range []string{
		"approval_kind", "tool_name", "normalized_input_hash", "path_digest", "input_preview",
	} {
		if !columns[required] {
			t.Fatalf("approval_requests missing repaired column %q", required)
		}
	}

	for _, required := range []string{
		"idx_approval_requests_binding", "idx_approval_requests_kind_binding",
	} {
		var indexName string
		if err := sqlDB.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name=?", required).Scan(&indexName); err != nil {
			t.Fatalf("approval_requests missing repaired index %q: %v", required, err)
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
		Capabilities:   []string{"chat", "embedding"},
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
	if got := strings.Join(providers[0].Capabilities, ","); got != "chat,embedding" {
		t.Fatalf("provider capabilities = %#v, want chat,embedding", providers[0].Capabilities)
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
