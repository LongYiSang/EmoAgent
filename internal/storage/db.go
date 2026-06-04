package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	_ "modernc.org/sqlite"
)

// DB wraps a sql.DB with helper methods.
type DB struct {
	db     *sql.DB
	logger *slog.Logger
	loc    *time.Location
}

type StorageOptions struct {
	Timezone string
}

// SessionRecord is the DB representation of a conversation session.
type SessionRecord struct {
	ID        string
	Persona   string
	Title     string
	CreatedAt string
	UpdatedAt string
	Metadata  string
}

// SessionSummary is the DB representation of a session list item.
type SessionSummary struct {
	ID           string
	Persona      string
	Title        string
	MessageCount int
	LastMessage  string
	CreatedAt    string
	UpdatedAt    string
}

// MessageRecord is the DB representation of a chat message.
type MessageRecord struct {
	ID        string
	SessionID string
	Role      string
	Content   string
	CreatedAt string
	Metadata  string
}

var (
	ErrProviderInUse                 = errors.New("llm provider is referenced by an agent config")
	ErrCannotDeleteActiveAgentConfig = errors.New("cannot delete the active agent config")
	ErrCannotDeleteLastAgentConfig   = errors.New("cannot delete the last agent config")
)

type LLMProviderRecord struct {
	config.LLMProvider
	ModelsCacheJSON      string
	ModelsCacheUpdatedAt sql.NullString
	CreatedAt            string
	UpdatedAt            string
}

type RuntimeSetting struct {
	Namespace string `json:"namespace"`
	Key       string `json:"key"`
	ValueJSON string `json:"value_json"`
	Source    string `json:"source"`
	UpdatedBy string `json:"updated_by"`
	UpdatedAt string `json:"updated_at"`
}

// Open creates or opens a SQLite database, sets pragmas, and runs migrations.
func Open(path string, logger *slog.Logger) (*DB, error) {
	return OpenWithOptions(path, logger, StorageOptions{Timezone: "Asia/Shanghai"})
}

func OpenWithOptions(path string, logger *slog.Logger, opts StorageOptions) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}
	loc, err := loadLocation(opts.Timezone)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// Set pragmas for performance and correctness.
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("set pragma %q: %w", p, err)
		}
	}

	if err := ApplyMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrations: %w", err)
	}

	logger.Info("database opened", "path", path)
	return &DB{db: db, logger: logger, loc: loc}, nil
}

func loadLocation(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("load timezone %q: %w", name, err)
	}
	return loc, nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// SqlDB returns the underlying *sql.DB for direct queries.
func (d *DB) SqlDB() *sql.DB {
	return d.db
}

// GetRuntimeConfig returns a single runtime config value.
func (d *DB) GetRuntimeConfig(key string) (string, bool, error) {
	var value string
	err := d.db.QueryRow("SELECT value FROM config_runtime WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// SetRuntimeConfig upserts a runtime config key-value pair.
func (d *DB) SetRuntimeConfig(key, value string) error {
	now := d.nowText()
	_, err := d.db.Exec(`
		INSERT INTO config_runtime (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, key, value, now)
	return err
}

// GetAllRuntimeConfig returns all runtime config as a map.
func (d *DB) GetAllRuntimeConfig() (map[string]string, error) {
	rows, err := d.db.Query("SELECT key, value FROM config_runtime")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

func (d *DB) UpsertRuntimeSetting(namespace, key, valueJSON, source string) error {
	if !json.Valid([]byte(valueJSON)) {
		return fmt.Errorf("value_json must be valid JSON")
	}
	if source == "" {
		source = "ui"
	}
	now := d.nowText()
	_, err := d.db.Exec(`
		INSERT INTO runtime_settings (namespace, key, value_json, source, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(namespace, key) DO UPDATE SET
			value_json = excluded.value_json,
			source = excluded.source,
			updated_at = excluded.updated_at
	`, namespace, key, valueJSON, source, now)
	return err
}

func (d *DB) GetRuntimeSetting(namespace, key string) (RuntimeSetting, bool, error) {
	row := d.db.QueryRow(`
		SELECT namespace, key, value_json, source, updated_by, updated_at
		FROM runtime_settings
		WHERE namespace = ? AND key = ?
	`, namespace, key)
	setting, err := scanRuntimeSetting(row)
	if err == sql.ErrNoRows {
		return RuntimeSetting{}, false, nil
	}
	if err != nil {
		return RuntimeSetting{}, false, err
	}
	return setting, true, nil
}

func (d *DB) ListRuntimeSettings() ([]RuntimeSetting, error) {
	rows, err := d.db.Query(`
		SELECT namespace, key, value_json, source, updated_by, updated_at
		FROM runtime_settings
		ORDER BY namespace, key
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var settings []RuntimeSetting
	for rows.Next() {
		setting, err := scanRuntimeSetting(rows)
		if err != nil {
			return nil, err
		}
		settings = append(settings, setting)
	}
	return settings, rows.Err()
}

func (d *DB) SetActiveAgentConfig(id string) error {
	return d.SetRuntimeConfig("agent.active_config", id)
}

func scanRuntimeSetting(row scanner) (RuntimeSetting, error) {
	var setting RuntimeSetting
	err := row.Scan(
		&setting.Namespace,
		&setting.Key,
		&setting.ValueJSON,
		&setting.Source,
		&setting.UpdatedBy,
		&setting.UpdatedAt,
	)
	return setting, err
}

func (d *DB) GetActiveAgentConfig() (string, bool, error) {
	return d.GetRuntimeConfig("agent.active_config")
}

func (d *DB) UpsertLLMProvider(provider config.LLMProvider) error {
	discovery := provider.ModelDiscovery
	if discovery == "" {
		discovery = "manual"
	}
	capabilities, err := encodeProviderCapabilities(provider.Capabilities)
	if err != nil {
		return err
	}
	now := d.nowText()
	_, err = d.db.Exec(`
		INSERT INTO llm_providers (
			id, name, preset_id, protocol, base_url, api_key_env, model_discovery, enabled, capabilities_json, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			preset_id = excluded.preset_id,
			protocol = excluded.protocol,
			base_url = excluded.base_url,
			api_key_env = excluded.api_key_env,
			model_discovery = excluded.model_discovery,
			enabled = excluded.enabled,
			capabilities_json = excluded.capabilities_json,
			updated_at = excluded.updated_at
	`, provider.ID, provider.Name, provider.PresetID, provider.Protocol, provider.BaseURL, provider.APIKeyEnv, discovery, boolInt(provider.Enabled), capabilities, now)
	return err
}

func (d *DB) GetLLMProvider(ctx context.Context, id string) (*LLMProviderRecord, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT id, name, preset_id, protocol, base_url, api_key_env, model_discovery, enabled,
		       capabilities_json, models_cache_json, models_cache_updated_at, created_at, updated_at
		FROM llm_providers
		WHERE id = ?
	`, id)
	record, err := scanLLMProvider(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (d *DB) ListLLMProviders() ([]LLMProviderRecord, error) {
	rows, err := d.db.Query(`
		SELECT id, name, preset_id, protocol, base_url, api_key_env, model_discovery, enabled,
		       capabilities_json, models_cache_json, models_cache_updated_at, created_at, updated_at
		FROM llm_providers
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []LLMProviderRecord
	for rows.Next() {
		record, err := scanLLMProvider(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (d *DB) DeleteLLMProvider(id string) error {
	var refs int
	if err := d.db.QueryRow(`
		SELECT COUNT(*) FROM agent_configs
		WHERE emotion_main_provider_id = ?
		   OR emotion_summary_provider_id = ?
		   OR work_main_provider_id = ?
		   OR work_summary_provider_id = ?
	`, id, id, id, id).Scan(&refs); err != nil {
		return err
	}
	if refs > 0 {
		return ErrProviderInUse
	}
	_, err := d.db.Exec("DELETE FROM llm_providers WHERE id = ?", id)
	return err
}

func (d *DB) UpdateProviderModelsCache(id, modelsJSON, updatedAt string) error {
	now := d.nowText()
	_, err := d.db.Exec(`
		UPDATE llm_providers
		SET models_cache_json = ?, models_cache_updated_at = ?, updated_at = ?
		WHERE id = ?
	`, modelsJSON, updatedAt, now, id)
	return err
}

func (d *DB) UpsertAgentConfig(agent config.AgentConfig) error {
	emotionMainParams, err := encodeJSONObject(agent.Emotion.Main.Params)
	if err != nil {
		return fmt.Errorf("emotion.main.params: %w", err)
	}
	emotionSummaryParams, err := encodeJSONObject(agent.Emotion.Summary.Params)
	if err != nil {
		return fmt.Errorf("emotion.summary.params: %w", err)
	}
	workMainParams, err := encodeJSONObject(agent.Work.Main.Params)
	if err != nil {
		return fmt.Errorf("work.main.params: %w", err)
	}
	workSummaryParams, err := encodeJSONObject(agent.Work.Summary.Params)
	if err != nil {
		return fmt.Errorf("work.summary.params: %w", err)
	}
	contextOverrides, err := encodeJSONObject(agent.ContextOverrides)
	if err != nil {
		return fmt.Errorf("context_overrides: %w", err)
	}
	now := d.nowText()

	_, err = d.db.Exec(`
		INSERT INTO agent_configs (
			id, name, persona_key,
			emotion_main_provider_id, emotion_main_model, emotion_main_params_json,
			emotion_summary_provider_id, emotion_summary_model, emotion_summary_params_json,
			work_main_provider_id, work_main_model, work_main_params_json,
			work_summary_provider_id, work_summary_model, work_summary_params_json,
			context_overrides_json, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			persona_key = excluded.persona_key,
			emotion_main_provider_id = excluded.emotion_main_provider_id,
			emotion_main_model = excluded.emotion_main_model,
			emotion_main_params_json = excluded.emotion_main_params_json,
			emotion_summary_provider_id = excluded.emotion_summary_provider_id,
			emotion_summary_model = excluded.emotion_summary_model,
			emotion_summary_params_json = excluded.emotion_summary_params_json,
			work_main_provider_id = excluded.work_main_provider_id,
			work_main_model = excluded.work_main_model,
			work_main_params_json = excluded.work_main_params_json,
			work_summary_provider_id = excluded.work_summary_provider_id,
			work_summary_model = excluded.work_summary_model,
			work_summary_params_json = excluded.work_summary_params_json,
			context_overrides_json = excluded.context_overrides_json,
			updated_at = excluded.updated_at
	`, agent.ID, agent.Name, agent.PersonaKey,
		agent.Emotion.Main.ProviderID, agent.Emotion.Main.Model, emotionMainParams,
		agent.Emotion.Summary.ProviderID, agent.Emotion.Summary.Model, emotionSummaryParams,
		agent.Work.Main.ProviderID, agent.Work.Main.Model, workMainParams,
		agent.Work.Summary.ProviderID, agent.Work.Summary.Model, workSummaryParams,
		contextOverrides, now)
	return err
}

func (d *DB) GetAgentConfig(ctx context.Context, id string) (*config.AgentConfig, error) {
	row := d.db.QueryRowContext(ctx, agentConfigSelectSQL()+" WHERE id = ?", id)
	agent, err := scanAgentConfig(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &agent, nil
}

func (d *DB) ListAgentConfigs() ([]config.AgentConfig, error) {
	rows, err := d.db.Query(agentConfigSelectSQL() + " ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []config.AgentConfig
	for rows.Next() {
		agent, err := scanAgentConfig(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}

func (d *DB) DeleteAgentConfig(id string) error {
	active, found, err := d.GetActiveAgentConfig()
	if err != nil {
		return err
	}
	if found && active == id {
		return ErrCannotDeleteActiveAgentConfig
	}
	var count int
	if err := d.db.QueryRow("SELECT COUNT(*) FROM agent_configs").Scan(&count); err != nil {
		return err
	}
	if count <= 1 {
		return ErrCannotDeleteLastAgentConfig
	}
	_, err = d.db.Exec("DELETE FROM agent_configs WHERE id = ?", id)
	return err
}

// PersonaRecord is the DB representation of a persona.
type PersonaRecord struct {
	Key                 string
	Name                string
	Description         string
	SystemPrompt        string
	Tone                string
	Quirks              string // JSON array
	Greeting            string
	WorkProgressPhrases string // JSON object
}

// UpsertPersona inserts or updates a persona in the database.
func (d *DB) UpsertPersona(
	key, name, description, systemPrompt, tone string,
	quirks []string,
	greeting string,
	workProgressPhrases map[string][]string,
) error {
	if key == "" {
		return errors.New("persona key is required")
	}
	if name == "" {
		name = key
	}

	quirksJSON, _ := json.Marshal(quirks)
	progressJSON, _ := json.Marshal(workProgressPhrases)
	progressValue := string(progressJSON)
	if progressValue == "" || progressValue == "null" {
		progressValue = "{}"
	}
	now := d.nowText()
	_, err := d.db.Exec(`
		INSERT INTO personas (key, name, description, system_prompt, tone, quirks, greeting, work_progress_phrases, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			name = excluded.name,
			description = excluded.description,
			system_prompt = excluded.system_prompt,
			tone = excluded.tone,
			quirks = excluded.quirks,
			greeting = excluded.greeting,
			work_progress_phrases = excluded.work_progress_phrases,
			updated_at = excluded.updated_at
	`, key, name, description, systemPrompt, tone, string(quirksJSON), greeting, progressValue, now)
	return err
}

// ListPersonas returns all persona keys from the database.
func (d *DB) ListPersonas() ([]string, error) {
	rows, err := d.db.Query("SELECT key FROM personas ORDER BY key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// GetPersona returns a persona by key, or nil when it does not exist.
func (d *DB) GetPersona(ctx context.Context, key string) (*PersonaRecord, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT key, COALESCE(name, ''), COALESCE(description, ''), COALESCE(system_prompt, ''), COALESCE(tone, ''), COALESCE(quirks, ''), COALESCE(greeting, ''), COALESCE(work_progress_phrases, '{}')
		FROM personas
		WHERE key = ?
	`, key)

	var record PersonaRecord
	if err := row.Scan(&record.Key, &record.Name, &record.Description, &record.SystemPrompt, &record.Tone, &record.Quirks, &record.Greeting, &record.WorkProgressPhrases); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// DeletePersona deletes a persona by key.
func (d *DB) DeletePersona(ctx context.Context, key string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM personas WHERE key = ?", key)
	return err
}

// CreateSession inserts a new session row.
func (d *DB) CreateSession(ctx context.Context, id, persona string) error {
	if id == "" {
		return errors.New("session id is required")
	}
	if persona == "" {
		persona = "default"
	}

	now := d.nowText()
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO sessions (id, persona, created_at, updated_at)
		VALUES (?, ?, ?, ?)
	`, id, persona, now, now)
	return err
}

// GetSession returns a session by id, or nil when it does not exist.
func (d *DB) GetSession(ctx context.Context, id string) (*SessionRecord, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT id, persona, title, created_at, updated_at, COALESCE(metadata, '')
		FROM sessions
		WHERE id = ?
	`, id)

	var record SessionRecord
	if err := row.Scan(&record.ID, &record.Persona, &record.Title, &record.CreatedAt, &record.UpdatedAt, &record.Metadata); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// ListSessions returns non-empty sessions ordered by recent activity.
func (d *DB) ListSessions(ctx context.Context, persona string, limit int) ([]SessionSummary, error) {
	if limit <= 0 {
		return []SessionSummary{}, nil
	}

	rows, err := d.db.QueryContext(ctx, `
		SELECT s.id, s.persona, s.title, s.created_at, s.updated_at,
		       (SELECT COUNT(*) FROM messages WHERE session_id = s.id) AS message_count,
		       COALESCE(
		         (SELECT SUBSTR(content, 1, 100)
		            FROM messages
		           WHERE session_id = s.id
		           ORDER BY created_at DESC, rowid DESC
		           LIMIT 1),
		         ''
		       ) AS last_message
		FROM sessions s
		WHERE (? = '' OR s.persona = ?)
		  AND EXISTS (SELECT 1 FROM messages m WHERE m.session_id = s.id)
		ORDER BY s.updated_at DESC, s.created_at DESC, s.id DESC
		LIMIT ?
	`, persona, persona, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []SessionSummary
	for rows.Next() {
		var summary SessionSummary
		if err := rows.Scan(&summary.ID, &summary.Persona, &summary.Title, &summary.CreatedAt, &summary.UpdatedAt, &summary.MessageCount, &summary.LastMessage); err != nil {
			return nil, err
		}
		sessions = append(sessions, summary)
	}
	return sessions, rows.Err()
}

// GetLatestSession returns the most recent non-empty session for a persona.
func (d *DB) GetLatestSession(ctx context.Context, persona string) (*SessionSummary, error) {
	sessions, err := d.ListSessions(ctx, persona, 1)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, nil
	}
	return &sessions[0], nil
}

// DeleteSession removes a session and all of its messages.
func (d *DB) DeleteSession(ctx context.Context, id string) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateSessionTitle sets the title for a session.
func (d *DB) UpdateSessionTitle(ctx context.Context, id, title string) error {
	_, err := d.db.ExecContext(ctx, `UPDATE sessions SET title = ? WHERE id = ?`, title, id)
	return err
}

// UpdateSessionMetadata replaces the serialized session metadata payload.
func (d *DB) UpdateSessionMetadata(ctx context.Context, id, metadata string) error {
	_, err := d.db.ExecContext(ctx, `UPDATE sessions SET metadata = ? WHERE id = ?`, metadata, id)
	return err
}

// AddMessage inserts a new chat message for a session.
func (d *DB) AddMessage(ctx context.Context, id, sessionID, role, content string) error {
	return d.AddMessageWithMetadata(ctx, id, sessionID, role, content, nil)
}

// AddMessageWithMetadata inserts a new visible chat message and stores serialized metadata when provided.
func (d *DB) AddMessageWithMetadata(ctx context.Context, id, sessionID, role, content string, metadata any) error {
	if id == "" {
		return errors.New("message id is required")
	}
	if sessionID == "" {
		return errors.New("session id is required")
	}
	if err := validateMessageRole(role); err != nil {
		return err
	}
	if content == "" {
		return errors.New("message content is required")
	}

	metadataJSON, err := encodeMetadata(metadata)
	if err != nil {
		return err
	}

	_, err = d.db.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, sessionID, role, content, metadataJSON, d.nowText())
	return err
}

// GetRecentMessages returns the latest N messages in ascending time order.
func (d *DB) GetRecentMessages(ctx context.Context, sessionID string, limit int) ([]MessageRecord, error) {
	if limit <= 0 {
		return []MessageRecord{}, nil
	}

	rows, err := d.db.QueryContext(ctx, `
		SELECT id, session_id, role, content, created_at, COALESCE(metadata, '')
		FROM messages
		WHERE session_id = ?
		ORDER BY created_at DESC, rowid DESC
		LIMIT ?
	`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []MessageRecord
	for rows.Next() {
		var record MessageRecord
		if err := rows.Scan(&record.ID, &record.SessionID, &record.Role, &record.Content, &record.CreatedAt, &record.Metadata); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	reverseMessages(records)
	return records, nil
}

// GetAllMessages returns all messages for a session in ascending time order.
func (d *DB) GetAllMessages(ctx context.Context, sessionID string) ([]MessageRecord, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, session_id, role, content, created_at, COALESCE(metadata, '')
		FROM messages
		WHERE session_id = ?
		ORDER BY created_at ASC, rowid ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []MessageRecord
	for rows.Next() {
		var record MessageRecord
		if err := rows.Scan(&record.ID, &record.SessionID, &record.Role, &record.Content, &record.CreatedAt, &record.Metadata); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// UpdateSessionTimestamp updates the session's updated_at column.
func (d *DB) UpdateSessionTimestamp(ctx context.Context, id string) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE sessions
		SET updated_at = ?
		WHERE id = ?
	`, d.nowText(), id)
	return err
}

func (d *DB) nowText() string {
	return d.formatTime(time.Now())
}

func (d *DB) formatTime(t time.Time) string {
	if t.IsZero() {
		t = time.Now()
	}
	loc := d.loc
	if loc == nil {
		loc = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return t.In(loc).Format(time.RFC3339Nano)
}

func validateMessageRole(role string) error {
	switch role {
	case "user", "assistant":
		return nil
	default:
		return fmt.Errorf("unsupported message role: %s", role)
	}
}

func reverseMessages(records []MessageRecord) {
	for left, right := 0, len(records)-1; left < right; left, right = left+1, right-1 {
		records[left], records[right] = records[right], records[left]
	}
}

func encodeMetadata(metadata any) (string, error) {
	switch value := metadata.(type) {
	case nil:
		return "", nil
	case string:
		return value, nil
	case []byte:
		return string(value), nil
	default:
		payload, err := json.Marshal(metadata)
		if err != nil {
			return "", fmt.Errorf("marshal metadata: %w", err)
		}
		return string(payload), nil
	}
}

type scanner interface {
	Scan(dest ...any) error
}

func scanLLMProvider(row scanner) (LLMProviderRecord, error) {
	var record LLMProviderRecord
	var enabled int
	var capabilitiesJSON string
	if err := row.Scan(
		&record.ID,
		&record.Name,
		&record.PresetID,
		&record.Protocol,
		&record.BaseURL,
		&record.APIKeyEnv,
		&record.ModelDiscovery,
		&enabled,
		&capabilitiesJSON,
		&record.ModelsCacheJSON,
		&record.ModelsCacheUpdatedAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return LLMProviderRecord{}, err
	}
	record.Enabled = enabled != 0
	if err := json.Unmarshal([]byte(capabilitiesJSON), &record.Capabilities); err != nil {
		return LLMProviderRecord{}, err
	}
	record.Capabilities = config.NormalizeProviderCapabilities(record.Capabilities)
	return record, nil
}

func encodeProviderCapabilities(capabilities []string) (string, error) {
	data, err := json.Marshal(config.NormalizeProviderCapabilities(capabilities))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func agentConfigSelectSQL() string {
	return `
		SELECT id, name, persona_key,
		       emotion_main_provider_id, emotion_main_model, emotion_main_params_json,
		       emotion_summary_provider_id, emotion_summary_model, emotion_summary_params_json,
		       work_main_provider_id, work_main_model, work_main_params_json,
		       work_summary_provider_id, work_summary_model, work_summary_params_json,
		       context_overrides_json
		FROM agent_configs`
}

func scanAgentConfig(row scanner) (config.AgentConfig, error) {
	var agent config.AgentConfig
	var emotionMainParams, emotionSummaryParams, workMainParams, workSummaryParams, contextOverrides string
	if err := row.Scan(
		&agent.ID,
		&agent.Name,
		&agent.PersonaKey,
		&agent.Emotion.Main.ProviderID,
		&agent.Emotion.Main.Model,
		&emotionMainParams,
		&agent.Emotion.Summary.ProviderID,
		&agent.Emotion.Summary.Model,
		&emotionSummaryParams,
		&agent.Work.Main.ProviderID,
		&agent.Work.Main.Model,
		&workMainParams,
		&agent.Work.Summary.ProviderID,
		&agent.Work.Summary.Model,
		&workSummaryParams,
		&contextOverrides,
	); err != nil {
		return config.AgentConfig{}, err
	}
	if err := decodeJSONObject(emotionMainParams, &agent.Emotion.Main.Params); err != nil {
		return config.AgentConfig{}, fmt.Errorf("emotion_main_params_json: %w", err)
	}
	if err := decodeJSONObject(emotionSummaryParams, &agent.Emotion.Summary.Params); err != nil {
		return config.AgentConfig{}, fmt.Errorf("emotion_summary_params_json: %w", err)
	}
	if err := decodeJSONObject(workMainParams, &agent.Work.Main.Params); err != nil {
		return config.AgentConfig{}, fmt.Errorf("work_main_params_json: %w", err)
	}
	if err := decodeJSONObject(workSummaryParams, &agent.Work.Summary.Params); err != nil {
		return config.AgentConfig{}, fmt.Errorf("work_summary_params_json: %w", err)
	}
	if err := decodeJSONObject(contextOverrides, &agent.ContextOverrides); err != nil {
		return config.AgentConfig{}, fmt.Errorf("context_overrides_json: %w", err)
	}
	if agent.ContextOverrides == nil {
		agent.ContextOverrides = map[string]any{}
	}
	return agent, nil
}

func encodeJSONObject(value any) (string, error) {
	if value == nil {
		return "{}", nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if !json.Valid(payload) || len(payload) == 0 || payload[0] != '{' {
		return "", fmt.Errorf("must be a JSON object")
	}
	return string(payload), nil
}

func decodeJSONObject(raw string, target any) error {
	if raw == "" {
		raw = "{}"
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), target)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
