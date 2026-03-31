package storage

import (
	"database/sql"
	"fmt"
)

// Migration represents a single schema migration.
type Migration struct {
	Version int
	SQL     string
}

var migrations = []Migration{
	{
		Version: 1,
		SQL: `
CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,
    persona    TEXT NOT NULL DEFAULT 'default',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    metadata   TEXT
);

CREATE TABLE IF NOT EXISTS messages (
    id         TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id),
    role       TEXT NOT NULL,
    content    TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    metadata   TEXT
);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, created_at);

CREATE TABLE IF NOT EXISTS personas (
    name          TEXT PRIMARY KEY,
    description   TEXT,
    system_prompt TEXT,
    tone          TEXT,
    quirks        TEXT,
    greeting      TEXT,
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS config_runtime (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS schema_version (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);
`,
	},
	{
		Version: 2,
		SQL: `
CREATE TABLE IF NOT EXISTS llm_profiles (
    name          TEXT PRIMARY KEY,
    provider      TEXT NOT NULL,
    base_url      TEXT NOT NULL,
    model         TEXT NOT NULL,
    summary_model TEXT NOT NULL DEFAULT '',
    max_tokens    INTEGER NOT NULL DEFAULT 4096,
    temperature   REAL NOT NULL DEFAULT 0.7,
    api_key_env   TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
`,
	},
	{
		Version: 3,
		SQL: `
DROP TABLE IF EXISTS personas;

CREATE TABLE IF NOT EXISTS personas (
    key           TEXT PRIMARY KEY,
    name          TEXT NOT NULL DEFAULT '',
    description   TEXT,
    system_prompt TEXT,
    tone          TEXT,
    quirks        TEXT,
    greeting      TEXT,
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
`,
	},
}

// ApplyMigrations runs any pending migrations inside transactions.
func ApplyMigrations(db *sql.DB) error {
	// Ensure schema_version table exists (bootstrap).
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
		version    INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	var current int
	row := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version")
	if err := row.Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	for _, m := range migrations {
		if m.Version <= current {
			continue
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", m.Version, err)
		}

		if _, err := tx.Exec(m.SQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", m.Version, err)
		}

		if _, err := tx.Exec("INSERT INTO schema_version (version) VALUES (?)", m.Version); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", m.Version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.Version, err)
		}
	}

	return nil
}
