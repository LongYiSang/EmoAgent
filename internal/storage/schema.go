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
    key           TEXT PRIMARY KEY,
    name          TEXT NOT NULL DEFAULT '',
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
-- Personas schema was squashed into migration v1.
-- Legacy upgrades from the old name-primary-key schema are intentionally unsupported.
SELECT 1;
`,
	},
	{
		Version: 4,
		SQL:     `ALTER TABLE sessions ADD COLUMN title TEXT NOT NULL DEFAULT '';`,
	},
	{
		Version: 5,
		SQL: `
ALTER TABLE llm_profiles ADD COLUMN input_budget_tokens INTEGER;
ALTER TABLE llm_profiles ADD COLUMN soft_compact_ratio REAL;
ALTER TABLE llm_profiles ADD COLUMN hard_compact_ratio REAL;
ALTER TABLE llm_profiles ADD COLUMN reserve_output_tokens INTEGER;
`,
	},
	{
		Version: 6,
		SQL:     `ALTER TABLE personas ADD COLUMN work_progress_phrases TEXT NOT NULL DEFAULT '{}';`,
	},
	{
		Version: 7,
		SQL: `
CREATE TABLE IF NOT EXISTS pending_decisions (
    session_id        TEXT NOT NULL,
    task_id           TEXT NOT NULL,
    status            TEXT NOT NULL,
    fail_closed       INTEGER NOT NULL DEFAULT 0,
    category          TEXT NOT NULL,
    risk_level        TEXT NOT NULL,
    summary_json      TEXT NOT NULL,
    resume_blob_json  TEXT,
    report_json       TEXT,
    resolved_decision TEXT,
    resolved_reason   TEXT,
    created_at        TEXT NOT NULL,
    status_entered_at TEXT NOT NULL,
    soft_expires_at   TEXT,
    hard_expires_at   TEXT,
    archive_after     TEXT,
    claim_id          TEXT,
    claim_expires_at  TEXT,
    updated_at        TEXT NOT NULL,
    PRIMARY KEY (session_id, task_id)
);

CREATE INDEX IF NOT EXISTS idx_pending_decisions_session_status
    ON pending_decisions(session_id, status);

CREATE INDEX IF NOT EXISTS idx_pending_decisions_claim
    ON pending_decisions(claim_expires_at)
    WHERE claim_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_pending_decisions_soft_expire
    ON pending_decisions(soft_expires_at)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_pending_decisions_hard_expire
    ON pending_decisions(hard_expires_at)
    WHERE status IN ('pending', 'stale');

CREATE INDEX IF NOT EXISTS idx_pending_decisions_archive_after
    ON pending_decisions(archive_after)
    WHERE status IN ('expired_open', 'auto_rejected', 'resolved');

CREATE TABLE IF NOT EXISTS archived_decisions (
    session_id        TEXT NOT NULL,
    task_id           TEXT NOT NULL,
    final_status      TEXT NOT NULL,
    fail_closed       INTEGER NOT NULL DEFAULT 0,
    category          TEXT NOT NULL,
    risk_level        TEXT NOT NULL,
    summary_json      TEXT NOT NULL,
    report_json       TEXT,
    resolved_decision TEXT,
    resolved_reason   TEXT,
    created_at        TEXT NOT NULL,
    status_entered_at TEXT NOT NULL,
    archived_at       TEXT NOT NULL,
    PRIMARY KEY (session_id, task_id)
);

CREATE INDEX IF NOT EXISTS idx_archived_decisions_status
    ON archived_decisions(final_status, archived_at);
`,
	},
	{
		Version: 8,
		SQL:     `ALTER TABLE llm_profiles ADD COLUMN summary_temperature REAL;`,
	},
	{
		Version: 9,
		SQL: `
ALTER TABLE pending_decisions ADD COLUMN approval_request_id TEXT;
ALTER TABLE archived_decisions ADD COLUMN approval_request_id TEXT;

CREATE TABLE IF NOT EXISTS approval_requests (
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

CREATE INDEX IF NOT EXISTS idx_approval_requests_session_status
    ON approval_requests(session_id, status);

CREATE INDEX IF NOT EXISTS idx_approval_requests_task_created
    ON approval_requests(task_id, created_at);

CREATE INDEX IF NOT EXISTS idx_approval_requests_expires_at
    ON approval_requests(expires_at);
`,
	},
	{
		Version: 10,
		SQL:     `ALTER TABLE llm_profiles ADD COLUMN summary_max_tokens INTEGER;`,
	},
	{
		Version: 11,
		SQL: `
DROP TABLE IF EXISTS llm_profiles;

DELETE FROM config_runtime
WHERE key LIKE 'llm.%'
   OR key = 'personas.default';

CREATE TABLE IF NOT EXISTS llm_providers (
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

CREATE TABLE IF NOT EXISTS agent_configs (
    id                          TEXT PRIMARY KEY,
    name                        TEXT NOT NULL,
    persona_key                 TEXT NOT NULL,
    emotion_main_provider_id    TEXT NOT NULL REFERENCES llm_providers(id),
    emotion_main_model          TEXT NOT NULL,
    emotion_main_params_json    TEXT NOT NULL DEFAULT '{}',
    emotion_summary_provider_id TEXT NOT NULL REFERENCES llm_providers(id),
    emotion_summary_model       TEXT NOT NULL,
    emotion_summary_params_json TEXT NOT NULL DEFAULT '{}',
    work_main_provider_id       TEXT NOT NULL REFERENCES llm_providers(id),
    work_main_model             TEXT NOT NULL,
    work_main_params_json       TEXT NOT NULL DEFAULT '{}',
    work_summary_provider_id    TEXT NOT NULL REFERENCES llm_providers(id),
    work_summary_model          TEXT NOT NULL,
    work_summary_params_json    TEXT NOT NULL DEFAULT '{}',
    context_overrides_json      TEXT NOT NULL DEFAULT '{}',
    created_at                  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at                  TEXT NOT NULL DEFAULT (datetime('now'))
);
`,
	},
	{
		Version: 12,
		SQL: `
ALTER TABLE llm_providers ADD COLUMN preset_id TEXT NOT NULL DEFAULT '';

UPDATE llm_providers
SET preset_id = id
WHERE id IN (
    'openai', 'moonshot', 'deepseek', 'anthropic', 'gemini',
    'qwen_dashscope_cn', 'qwen_dashscope_intl', 'xai', 'groq',
    'mistral', 'openrouter', 'custom_openai_compatible'
);
`,
	},
	{
		Version: 13,
		SQL: `
CREATE TABLE IF NOT EXISTS memory_chat_links (
    chat_session_id          TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    persona_id               TEXT NOT NULL,
    current_memory_session_id TEXT,
    created_at               TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at               TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS memory_segments (
    id                         TEXT PRIMARY KEY,
    chat_session_id            TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    memory_session_id          TEXT NOT NULL,
    segment_index              INTEGER NOT NULL CHECK (segment_index >= 1),
    started_at                 TEXT NOT NULL,
    last_activity_at           TEXT NOT NULL,
    finalized_at               TEXT,
    finalize_reason            TEXT,
    summary                    TEXT,
    last_user_episode_id       TEXT,
    last_assistant_episode_id  TEXT,
    last_extracted_at          TEXT,
    extraction_status          TEXT,
    UNIQUE(chat_session_id, segment_index),
    UNIQUE(memory_session_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_memory_segments_active
    ON memory_segments(chat_session_id)
    WHERE finalized_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_memory_segments_chat
    ON memory_segments(chat_session_id, segment_index);
`,
	},
	{
		Version: 14,
		SQL: `
CREATE TABLE IF NOT EXISTS approval_requests (
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

ALTER TABLE approval_requests ADD COLUMN tool_name TEXT NOT NULL DEFAULT '';
ALTER TABLE approval_requests ADD COLUMN normalized_input_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE approval_requests ADD COLUMN path_digest TEXT NOT NULL DEFAULT '';
ALTER TABLE approval_requests ADD COLUMN input_preview TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_approval_requests_binding
    ON approval_requests(session_id, task_id, tool_name, normalized_input_hash, path_digest);
`,
	},
	{
		Version: 15,
		SQL: `
ALTER TABLE approval_requests ADD COLUMN approval_kind TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_approval_requests_kind_binding
    ON approval_requests(session_id, task_id, approval_kind, tool_name, normalized_input_hash, path_digest);
`,
	},
	{
		Version: 16,
		SQL: `
ALTER TABLE memory_segments ADD COLUMN last_extracted_until_at TEXT;
ALTER TABLE memory_segments ADD COLUMN last_extracted_user_episode_id TEXT;
ALTER TABLE memory_segments ADD COLUMN last_extracted_assistant_episode_id TEXT;
ALTER TABLE memory_segments ADD COLUMN last_extraction_job_id TEXT;
ALTER TABLE memory_segments ADD COLUMN last_extraction_error_code TEXT;
ALTER TABLE memory_segments ADD COLUMN last_extraction_error_message TEXT;
ALTER TABLE memory_segments ADD COLUMN extraction_attempt_count INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS memory_extraction_jobs (
    id                         TEXT PRIMARY KEY,
    persona_id                 TEXT NOT NULL,
    chat_session_id             TEXT,
    segment_id                  TEXT,
    memory_session_id           TEXT,
    trigger                     TEXT NOT NULL,
    scope                       TEXT NOT NULL DEFAULT 'segment',
    mode                        TEXT NOT NULL DEFAULT 'apply',
    requested_by                TEXT NOT NULL DEFAULT 'system',
    priority                    INTEGER NOT NULL DEFAULT 100,
    force                       INTEGER NOT NULL DEFAULT 0,
    episode_ids_json            TEXT NOT NULL DEFAULT '[]',
    since_at                    TEXT,
    until_at                    TEXT,
    episode_limit               INTEGER NOT NULL DEFAULT 50,
    status                      TEXT NOT NULL DEFAULT 'pending',
    attempts                    INTEGER NOT NULL DEFAULT 0,
    max_attempts                INTEGER NOT NULL DEFAULT 3,
    run_after                   TEXT NOT NULL,
    claimed_by                  TEXT,
    claimed_until               TEXT,
    request_json                TEXT,
    result_json                 TEXT,
    mirror_sync_result_json      TEXT,
    error_code                  TEXT,
    error_message               TEXT,
    dedupe_key                  TEXT NOT NULL,
    created_at                  TEXT NOT NULL,
    updated_at                  TEXT NOT NULL,
    started_at                  TEXT,
    finished_at                 TEXT
);

CREATE INDEX IF NOT EXISTS idx_memory_extraction_jobs_claim
    ON memory_extraction_jobs(status, run_after, priority, created_at);

CREATE INDEX IF NOT EXISTS idx_memory_extraction_jobs_segment
    ON memory_extraction_jobs(segment_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_memory_extraction_jobs_chat_session
    ON memory_extraction_jobs(chat_session_id, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_memory_extraction_jobs_dedupe_pending
    ON memory_extraction_jobs(dedupe_key)
    WHERE status IN ('pending', 'running');
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
