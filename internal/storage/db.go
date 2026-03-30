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
	CreatedAt string
	UpdatedAt string
	Metadata  string
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

// PersonaRecord is the DB representation of a persona.
type PersonaRecord struct {
	Name         string
	Description  string
	SystemPrompt string
	Tone         string
	Quirks       string // JSON array
	Greeting     string
}

// UpsertPersona inserts or updates a persona in the database.
func (d *DB) UpsertPersona(name, description, systemPrompt, tone string, quirks []string, greeting string) error {
	quirksJSON, _ := json.Marshal(quirks)
	_, err := d.db.Exec(`
		INSERT INTO personas (name, description, system_prompt, tone, quirks, greeting, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(name) DO UPDATE SET
			description = excluded.description,
			system_prompt = excluded.system_prompt,
			tone = excluded.tone,
			quirks = excluded.quirks,
			greeting = excluded.greeting,
			updated_at = datetime('now')
	`, name, description, systemPrompt, tone, string(quirksJSON), greeting)
	return err
}

// ListPersonas returns all persona names from the database.
func (d *DB) ListPersonas() ([]string, error) {
	rows, err := d.db.Query("SELECT name FROM personas ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
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
		SELECT id, persona, created_at, updated_at, COALESCE(metadata, '')
		FROM sessions
		WHERE id = ?
	`, id)

	var record SessionRecord
	if err := row.Scan(&record.ID, &record.Persona, &record.CreatedAt, &record.UpdatedAt, &record.Metadata); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// AddMessage inserts a new chat message for a session.
func (d *DB) AddMessage(ctx context.Context, id, sessionID, role, content string) error {
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

	_, err := d.db.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, id, sessionID, role, content, nowUTC())
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
