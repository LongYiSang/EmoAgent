package storage

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
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
	tables := []string{"sessions", "messages", "personas", "config_runtime", "llm_profiles", "schema_version", "pending_decisions", "archived_decisions"}
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

func TestLLMProfileCRUD(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	if got, err := db.GetLLMProfile(ctx, "default"); err != nil {
		t.Fatalf("GetLLMProfile missing: %v", err)
	} else if got != nil {
		t.Fatalf("GetLLMProfile missing = %#v, want nil", got)
	}

	if err := db.UpsertLLMProfile(config.LLMProfile{
		Name:               "default",
		Provider:           "openai",
		BaseURL:            "https://api.openai.com",
		Model:              "gpt-4o",
		SummaryModel:       "gpt-4o-mini",
		SummaryTemperature: floatPtr(0.15),
		MaxTokens:          4096,
		Temperature:        0.7,
		APIKeyEnv:          "OPENAI_API_KEY",
	}); err != nil {
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
	if !profile.SummaryTemperature.Valid || profile.SummaryTemperature.Float64 != 0.15 {
		t.Fatalf("SummaryTemperature = %#v, want 0.15", profile.SummaryTemperature)
	}

	profiles, err := db.ListLLMProfiles()
	if err != nil {
		t.Fatalf("ListLLMProfiles: %v", err)
	}
	if len(profiles) != 1 || profiles[0].Name != "default" {
		t.Fatalf("ListLLMProfiles = %v, want [default]", profiles)
	}

	if err := db.UpsertLLMProfile(config.LLMProfile{
		Name:         "default",
		Provider:     "openai",
		BaseURL:      "https://api.openai.com",
		Model:        "gpt-4o",
		SummaryModel: "gpt-4.1",
		MaxTokens:    2048,
		Temperature:  0.2,
		APIKeyEnv:    "MOONSHOT_API_KEY",
	}); err != nil {
		t.Fatalf("UpsertLLMProfile update: %v", err)
	}

	profile, err = db.GetLLMProfile(ctx, "default")
	if err != nil {
		t.Fatalf("GetLLMProfile after update: %v", err)
	}
	if profile == nil || profile.SummaryModel != "gpt-4.1" || profile.MaxTokens != 2048 || profile.Temperature != 0.2 || profile.APIKeyEnv != "MOONSHOT_API_KEY" {
		t.Fatalf("updated profile = %#v", profile)
	}
	if profile.SummaryTemperature.Valid {
		t.Fatalf("SummaryTemperature = %#v, want NULL after clearing", profile.SummaryTemperature)
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

func TestLLMProfileBudgetOverridesRoundTrip(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	inputBudget := 12000
	softRatio := 0.65
	hardRatio := 0.88
	reserve := 2048
	if err := db.UpsertLLMProfile(config.LLMProfile{
		Name:                "profile-with-overrides",
		Provider:            "openai",
		BaseURL:             "https://api.openai.com",
		Model:               "gpt-4o",
		SummaryModel:        "gpt-4o-mini",
		MaxTokens:           4096,
		Temperature:         0.7,
		APIKeyEnv:           "OPENAI_API_KEY",
		InputBudgetTokens:   &inputBudget,
		SoftCompactRatio:    &softRatio,
		HardCompactRatio:    &hardRatio,
		ReserveOutputTokens: &reserve,
	}); err != nil {
		t.Fatalf("UpsertLLMProfile: %v", err)
	}

	record, err := db.GetLLMProfile(ctx, "profile-with-overrides")
	if err != nil {
		t.Fatalf("GetLLMProfile: %v", err)
	}
	if record == nil {
		t.Fatal("GetLLMProfile returned nil")
	}
	if !record.InputBudgetTokens.Valid || int(record.InputBudgetTokens.Int64) != 12000 {
		t.Fatalf("InputBudgetTokens = %#v, want 12000", record.InputBudgetTokens)
	}
	if !record.SoftCompactRatio.Valid || record.SoftCompactRatio.Float64 != 0.65 {
		t.Fatalf("SoftCompactRatio = %#v, want 0.65", record.SoftCompactRatio)
	}
	if !record.HardCompactRatio.Valid || record.HardCompactRatio.Float64 != 0.88 {
		t.Fatalf("HardCompactRatio = %#v, want 0.88", record.HardCompactRatio)
	}
	if !record.ReserveOutputTokens.Valid || int(record.ReserveOutputTokens.Int64) != 2048 {
		t.Fatalf("ReserveOutputTokens = %#v, want 2048", record.ReserveOutputTokens)
	}
}

func floatPtr(v float64) *float64 { return &v }

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
