package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB wraps a sql.DB with helper methods.
type DB struct {
	db     *sql.DB
	logger *slog.Logger
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
