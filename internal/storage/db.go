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
	"time"

	"github.com/longyisang/emoagent/internal/config"
	_ "modernc.org/sqlite"
)

// DB wraps a sql.DB with helper methods.
type DB struct {
	db     *sql.DB
	logger *slog.Logger
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

// LLMProfileRecord is the DB representation of an LLM profile.
type LLMProfileRecord struct {
	Name                string
	Provider            string
	BaseURL             string
	Model               string
	SummaryModel        string
	MaxTokens           int
	Temperature         float64
	APIKeyEnv           string
	InputBudgetTokens   sql.NullInt64
	SoftCompactRatio    sql.NullFloat64
	HardCompactRatio    sql.NullFloat64
	ReserveOutputTokens sql.NullInt64
	CreatedAt           string
	UpdatedAt           string
}

// Open creates or opens a SQLite database, sets pragmas, and runs migrations.
func Open(path string, logger *slog.Logger) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
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
	return &DB{db: db, logger: logger}, nil
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
	_, err := d.db.Exec(`
		INSERT INTO config_runtime (key, value, updated_at) VALUES (?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')
	`, key, value)
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

// UpsertLLMProfile inserts or updates an LLM profile in the database.
func (d *DB) UpsertLLMProfile(profile config.LLMProfile) error {
	inputBudgetTokens, softCompactRatio, hardCompactRatio, reserveOutputTokens, err := profileBudgetArgs(profile)
	if err != nil {
		return err
	}
	_, execErr := d.db.Exec(`
		INSERT INTO llm_profiles (
			name, provider, base_url, model, summary_model, max_tokens, temperature, api_key_env,
			input_budget_tokens, soft_compact_ratio, hard_compact_ratio, reserve_output_tokens,
			created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
		ON CONFLICT(name) DO UPDATE SET
			provider = excluded.provider,
			base_url = excluded.base_url,
			model = excluded.model,
			summary_model = excluded.summary_model,
			max_tokens = excluded.max_tokens,
			temperature = excluded.temperature,
			api_key_env = excluded.api_key_env,
			input_budget_tokens = excluded.input_budget_tokens,
			soft_compact_ratio = excluded.soft_compact_ratio,
			hard_compact_ratio = excluded.hard_compact_ratio,
			reserve_output_tokens = excluded.reserve_output_tokens,
			updated_at = datetime('now')
	`, profile.Name, profile.Provider, profile.BaseURL, profile.Model, profile.SummaryModel, profile.MaxTokens, profile.Temperature, profile.APIKeyEnv, inputBudgetTokens, softCompactRatio, hardCompactRatio, reserveOutputTokens)
	return execErr
}

// GetLLMProfile returns a profile by name, or nil when it does not exist.
func (d *DB) GetLLMProfile(ctx context.Context, name string) (*LLMProfileRecord, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT name, provider, base_url, model, COALESCE(summary_model, ''), max_tokens, temperature, COALESCE(api_key_env, ''),
		       input_budget_tokens, soft_compact_ratio, hard_compact_ratio, reserve_output_tokens, created_at, updated_at
		FROM llm_profiles
		WHERE name = ?
	`, name)

	var record LLMProfileRecord
	if err := row.Scan(&record.Name, &record.Provider, &record.BaseURL, &record.Model, &record.SummaryModel, &record.MaxTokens, &record.Temperature, &record.APIKeyEnv, &record.InputBudgetTokens, &record.SoftCompactRatio, &record.HardCompactRatio, &record.ReserveOutputTokens, &record.CreatedAt, &record.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// ListLLMProfiles returns all LLM profiles ordered by name.
func (d *DB) ListLLMProfiles() ([]LLMProfileRecord, error) {
	rows, err := d.db.Query(`
		SELECT name, provider, base_url, model, COALESCE(summary_model, ''), max_tokens, temperature, COALESCE(api_key_env, ''),
		       input_budget_tokens, soft_compact_ratio, hard_compact_ratio, reserve_output_tokens, created_at, updated_at
		FROM llm_profiles
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []LLMProfileRecord
	for rows.Next() {
		var record LLMProfileRecord
		if err := rows.Scan(&record.Name, &record.Provider, &record.BaseURL, &record.Model, &record.SummaryModel, &record.MaxTokens, &record.Temperature, &record.APIKeyEnv, &record.InputBudgetTokens, &record.SoftCompactRatio, &record.HardCompactRatio, &record.ReserveOutputTokens, &record.CreatedAt, &record.UpdatedAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// DeleteLLMProfile deletes a profile by name.
func (d *DB) DeleteLLMProfile(name string) error {
	_, err := d.db.Exec("DELETE FROM llm_profiles WHERE name = ?", name)
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
	_, err := d.db.Exec(`
		INSERT INTO personas (key, name, description, system_prompt, tone, quirks, greeting, work_progress_phrases, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET
			name = excluded.name,
			description = excluded.description,
			system_prompt = excluded.system_prompt,
			tone = excluded.tone,
			quirks = excluded.quirks,
			greeting = excluded.greeting,
			work_progress_phrases = excluded.work_progress_phrases,
			updated_at = datetime('now')
	`, key, name, description, systemPrompt, tone, string(quirksJSON), greeting, progressValue)
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

	now := nowUTC()
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
	`, id, sessionID, role, content, metadataJSON, nowUTC())
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
	`, nowUTC(), id)
	return err
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
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

func profileBudgetArgs(profile config.LLMProfile) (any, any, any, any, error) {
	if profile.InputBudgetTokens != nil && *profile.InputBudgetTokens <= 0 {
		return nil, nil, nil, nil, fmt.Errorf("input_budget_tokens must be > 0")
	}
	if profile.SoftCompactRatio != nil && (*profile.SoftCompactRatio <= 0 || *profile.SoftCompactRatio >= 1) {
		return nil, nil, nil, nil, fmt.Errorf("soft_compact_ratio must be between 0 and 1")
	}
	if profile.HardCompactRatio != nil && (*profile.HardCompactRatio <= 0 || *profile.HardCompactRatio >= 1) {
		return nil, nil, nil, nil, fmt.Errorf("hard_compact_ratio must be between 0 and 1")
	}
	if profile.ReserveOutputTokens != nil && *profile.ReserveOutputTokens <= 0 {
		return nil, nil, nil, nil, fmt.Errorf("reserve_output_tokens must be > 0")
	}
	if profile.SoftCompactRatio != nil && profile.HardCompactRatio != nil && *profile.SoftCompactRatio >= *profile.HardCompactRatio {
		return nil, nil, nil, nil, fmt.Errorf("soft_compact_ratio must be < hard_compact_ratio")
	}
	return nullableInt(profile.InputBudgetTokens), nullableFloat(profile.SoftCompactRatio), nullableFloat(profile.HardCompactRatio), nullableInt(profile.ReserveOutputTokens), nil
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}
