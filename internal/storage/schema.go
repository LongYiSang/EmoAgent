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
	{
		Version: 17,
		SQL: `
CREATE TABLE IF NOT EXISTS turns (
    id              TEXT PRIMARY KEY,
    idempotency_key TEXT UNIQUE,
    source          TEXT NOT NULL DEFAULT '',
    source_event_id TEXT NOT NULL DEFAULT '',
    kind            TEXT NOT NULL,
    session_id      TEXT NOT NULL DEFAULT '',
    persona_key     TEXT NOT NULL DEFAULT '',
    state           TEXT NOT NULL,
    status          TEXT NOT NULL,
    error_kind      TEXT NOT NULL DEFAULT '',
    error_message   TEXT NOT NULL DEFAULT '',
    started_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    completed_at    TEXT
);

CREATE INDEX IF NOT EXISTS idx_turns_session_started
    ON turns(session_id, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_turns_status_updated
    ON turns(status, updated_at DESC);

CREATE TABLE IF NOT EXISTS turn_events (
    id           TEXT PRIMARY KEY,
    turn_id      TEXT NOT NULL REFERENCES turns(id) ON DELETE CASCADE,
    seq          INTEGER NOT NULL,
    stage        TEXT NOT NULL,
    event_type   TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '{}',
    created_at   TEXT NOT NULL,
    UNIQUE(turn_id, seq)
);

CREATE INDEX IF NOT EXISTS idx_turn_events_turn_seq
    ON turn_events(turn_id, seq);

CREATE TABLE IF NOT EXISTS turn_outbound_events (
    id              TEXT PRIMARY KEY,
    turn_id          TEXT NOT NULL REFERENCES turns(id) ON DELETE CASCADE,
    seq              INTEGER NOT NULL,
    event_type       TEXT NOT NULL,
    payload_json     TEXT NOT NULL DEFAULT '{}',
    delivery_status  TEXT NOT NULL DEFAULT 'pending',
    created_at       TEXT NOT NULL,
    delivered_at     TEXT,
    UNIQUE(turn_id, seq)
);

CREATE INDEX IF NOT EXISTS idx_turn_outbound_turn_seq
    ON turn_outbound_events(turn_id, seq);

CREATE TABLE IF NOT EXISTS turn_idempotency (
    idempotency_key TEXT PRIMARY KEY,
    turn_id         TEXT NOT NULL REFERENCES turns(id) ON DELETE CASCADE,
    status          TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);
`,
	},
	{
		Version: 18,
		SQL: `
CREATE TABLE IF NOT EXISTS runtime_settings (
    namespace  TEXT NOT NULL,
    key        TEXT NOT NULL,
    value_json TEXT NOT NULL,
    source     TEXT NOT NULL DEFAULT 'ui',
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY(namespace, key)
);
`,
	},
	{
		Version: 19,
		SQL:     `SELECT 1;`,
	},
	{
		Version: 20,
		SQL: `
CREATE TABLE IF NOT EXISTS agent_affect_profiles (
    id TEXT PRIMARY KEY,
    persona_id TEXT NOT NULL,
    profile_name TEXT NOT NULL DEFAULT 'default',

    baseline_valence REAL NOT NULL DEFAULT 0.0 CHECK (baseline_valence >= -1.0 AND baseline_valence <= 1.0),
    baseline_arousal REAL NOT NULL DEFAULT 0.2 CHECK (baseline_arousal >= 0.0 AND baseline_arousal <= 1.0),
    baseline_dominance REAL NOT NULL DEFAULT 0.0 CHECK (baseline_dominance >= -1.0 AND baseline_dominance <= 1.0),
    baseline_energy REAL NOT NULL DEFAULT 0.5 CHECK (baseline_energy >= 0.0 AND baseline_energy <= 1.0),
    baseline_warmth REAL NOT NULL DEFAULT 0.6 CHECK (baseline_warmth >= 0.0 AND baseline_warmth <= 1.0),
    baseline_concern REAL NOT NULL DEFAULT 0.3 CHECK (baseline_concern >= 0.0 AND baseline_concern <= 1.0),
    baseline_curiosity REAL NOT NULL DEFAULT 0.3 CHECK (baseline_curiosity >= 0.0 AND baseline_curiosity <= 1.0),
    baseline_playfulness REAL NOT NULL DEFAULT 0.2 CHECK (baseline_playfulness >= 0.0 AND baseline_playfulness <= 1.0),
    baseline_attachment REAL NOT NULL DEFAULT 0.0 CHECK (baseline_attachment >= 0.0 AND baseline_attachment <= 1.0),
    baseline_frustration REAL NOT NULL DEFAULT 0.0 CHECK (baseline_frustration >= 0.0 AND baseline_frustration <= 1.0),
    baseline_uncertainty REAL NOT NULL DEFAULT 0.1 CHECK (baseline_uncertainty >= 0.0 AND baseline_uncertainty <= 1.0),

    dimension_config_json TEXT NOT NULL DEFAULT '{}',
    externalization_config_json TEXT NOT NULL DEFAULT '{}',
    llm_config_json TEXT NOT NULL DEFAULT '{}',
    context_policy_json TEXT NOT NULL DEFAULT '{}',
    clamp_policy_json TEXT NOT NULL DEFAULT '{}',

    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT,
    UNIQUE(persona_id, profile_name)
);

CREATE TABLE IF NOT EXISTS agent_affect_states (
    id TEXT PRIMARY KEY,
    persona_id TEXT NOT NULL,
    session_id TEXT,
    profile_id TEXT,

    valence REAL NOT NULL DEFAULT 0.0 CHECK (valence >= -1.0 AND valence <= 1.0),
    arousal REAL NOT NULL DEFAULT 0.2 CHECK (arousal >= 0.0 AND arousal <= 1.0),
    dominance REAL NOT NULL DEFAULT 0.0 CHECK (dominance >= -1.0 AND dominance <= 1.0),
    energy REAL NOT NULL DEFAULT 0.5 CHECK (energy >= 0.0 AND energy <= 1.0),
    warmth REAL NOT NULL DEFAULT 0.0 CHECK (warmth >= 0.0 AND warmth <= 1.0),
    concern REAL NOT NULL DEFAULT 0.0 CHECK (concern >= 0.0 AND concern <= 1.0),
    curiosity REAL NOT NULL DEFAULT 0.0 CHECK (curiosity >= 0.0 AND curiosity <= 1.0),
    playfulness REAL NOT NULL DEFAULT 0.0 CHECK (playfulness >= 0.0 AND playfulness <= 1.0),
    attachment REAL NOT NULL DEFAULT 0.0 CHECK (attachment >= 0.0 AND attachment <= 1.0),
    frustration REAL NOT NULL DEFAULT 0.0 CHECK (frustration >= 0.0 AND frustration <= 1.0),
    uncertainty REAL NOT NULL DEFAULT 0.0 CHECK (uncertainty >= 0.0 AND uncertainty <= 1.0),

    label TEXT,
    confidence REAL NOT NULL DEFAULT 0.5 CHECK (confidence >= 0.0 AND confidence <= 1.0),
    state_vector_json TEXT NOT NULL DEFAULT '{}',
    cause_summary TEXT NOT NULL DEFAULT '',
    visible_cause_summary TEXT NOT NULL DEFAULT '',
    cause_stack_json TEXT NOT NULL DEFAULT '[]',
    last_evaluation_id TEXT,

    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at TEXT,
    visibility_status TEXT NOT NULL DEFAULT 'visible' CHECK (visibility_status IN ('visible','hidden','purged')),
    searchable INTEGER NOT NULL DEFAULT 0 CHECK (searchable IN (0,1))
);

CREATE TABLE IF NOT EXISTS agent_affect_evaluations (
    id TEXT PRIMARY KEY,
    persona_id TEXT NOT NULL,
    session_id TEXT,
    turn_id TEXT,

    trigger_type TEXT NOT NULL,
    custom_type TEXT,
    custom_type_desc TEXT,
    source_kind TEXT NOT NULL DEFAULT '',
    source_ref_type TEXT,
    source_ref_id TEXT,
    source_ref_hash TEXT,
    plugin_id TEXT,

    input_mode TEXT NOT NULL DEFAULT 'raw' CHECK (input_mode IN ('raw','summary','mixed','none')),
    input_text TEXT,
    input_summary TEXT,
    context_window_policy_json TEXT NOT NULL DEFAULT '{}',
    context_window_snapshot_json TEXT,

    before_state_id TEXT,
    before_state_json TEXT NOT NULL DEFAULT '{}',

    llm_provider TEXT,
    llm_model TEXT,
    llm_thinking_enabled INTEGER NOT NULL DEFAULT 0 CHECK (llm_thinking_enabled IN (0,1)),
    prompt_version TEXT NOT NULL DEFAULT 'agent_affect_v2.prompt.v1',
    prompt_hash TEXT NOT NULL DEFAULT '',
    prompt_snapshot TEXT,
    response_json TEXT,

    proposed_delta_json TEXT NOT NULL DEFAULT '{}',
    clamped_delta_json TEXT NOT NULL DEFAULT '{}',
    predicted_state_json TEXT NOT NULL DEFAULT '{}',

    cause_summary TEXT NOT NULL DEFAULT '',
    visible_cause_summary TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL DEFAULT 0.5 CHECK (confidence >= 0.0 AND confidence <= 1.0),
    clamp_notes_json TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL DEFAULT 'preview' CHECK (status IN ('preview','committed','rejected','failed')),

    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    visibility_status TEXT NOT NULL DEFAULT 'visible' CHECK (visibility_status IN ('visible','hidden','purged')),
    searchable INTEGER NOT NULL DEFAULT 0 CHECK (searchable IN (0,1))
);

CREATE TABLE IF NOT EXISTS agent_affect_events (
    id TEXT PRIMARY KEY,
    persona_id TEXT NOT NULL,
    session_id TEXT,
    turn_id TEXT,

    evaluation_id TEXT,
    trigger_type TEXT NOT NULL,
    custom_type TEXT,
    plugin_id TEXT,

    before_state_id TEXT,
    after_state_id TEXT,

    proposed_delta_json TEXT NOT NULL DEFAULT '{}',
    clamped_delta_json TEXT NOT NULL DEFAULT '{}',
    committed_delta_json TEXT NOT NULL DEFAULT '{}',

    label_before TEXT,
    label_after TEXT,
    cause_summary TEXT NOT NULL DEFAULT '',
    significance REAL NOT NULL DEFAULT 0.5 CHECK (significance >= 0.0 AND significance <= 1.0),
    confidence REAL NOT NULL DEFAULT 0.5 CHECK (confidence >= 0.0 AND confidence <= 1.0),
    committed_by TEXT NOT NULL DEFAULT 'core' CHECK (committed_by IN ('core','plugin','user_debug','system')),

    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    visibility_status TEXT NOT NULL DEFAULT 'visible' CHECK (visibility_status IN ('visible','hidden','purged')),
    searchable INTEGER NOT NULL DEFAULT 0 CHECK (searchable IN (0,1))
);

CREATE TABLE IF NOT EXISTS agent_affect_plugin_writes (
    id TEXT PRIMARY KEY,
    persona_id TEXT NOT NULL,
    session_id TEXT,
    turn_id TEXT,

    plugin_id TEXT NOT NULL,
    capability TEXT NOT NULL,
    request_kind TEXT NOT NULL CHECK (request_kind IN ('submit','write_delta','write_target','configure')),
    request_json TEXT NOT NULL DEFAULT '{}',

    accepted INTEGER NOT NULL DEFAULT 0 CHECK (accepted IN (0,1)),
    rejection_reason TEXT,
    clamp_notes_json TEXT NOT NULL DEFAULT '[]',

    evaluation_id TEXT,
    affect_event_id TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_agent_affect_profiles_persona
    ON agent_affect_profiles(persona_id, profile_name);
CREATE INDEX IF NOT EXISTS idx_agent_affect_states_current
    ON agent_affect_states(persona_id, session_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_affect_evaluations_session
    ON agent_affect_evaluations(persona_id, session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_affect_evaluations_trigger
    ON agent_affect_evaluations(persona_id, trigger_type, custom_type, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_affect_events_session
    ON agent_affect_events(persona_id, session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_affect_plugin_writes_plugin
    ON agent_affect_plugin_writes(plugin_id, created_at DESC);
`,
	},
	{
		Version: 21,
		SQL: `
ALTER TABLE agent_affect_states ADD COLUMN mood_description TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_affect_states ADD COLUMN mood_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_affect_states ADD COLUMN prompt_mood_text TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_affect_states ADD COLUMN mood_owner_scope TEXT NOT NULL DEFAULT 'session';
ALTER TABLE agent_affect_states ADD COLUMN mood_owner_id TEXT NOT NULL DEFAULT '';

ALTER TABLE agent_affect_evaluations ADD COLUMN mood_description TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_affect_evaluations ADD COLUMN mood_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_affect_evaluations ADD COLUMN prompt_mood_text TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_affect_evaluations ADD COLUMN mood_owner_scope TEXT NOT NULL DEFAULT 'session';
ALTER TABLE agent_affect_evaluations ADD COLUMN mood_owner_id TEXT NOT NULL DEFAULT '';

ALTER TABLE agent_affect_events ADD COLUMN mood_description TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_affect_events ADD COLUMN mood_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_affect_events ADD COLUMN prompt_mood_text TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_affect_events ADD COLUMN mood_owner_scope TEXT NOT NULL DEFAULT 'session';
ALTER TABLE agent_affect_events ADD COLUMN mood_owner_id TEXT NOT NULL DEFAULT '';

UPDATE agent_affect_states
SET mood_owner_scope = 'session',
    mood_owner_id = 'session:' || COALESCE(session_id, '')
WHERE mood_owner_id = '';

UPDATE agent_affect_evaluations
SET mood_owner_scope = 'session',
    mood_owner_id = 'session:' || COALESCE(session_id, '')
WHERE mood_owner_id = '';

UPDATE agent_affect_events
SET mood_owner_scope = 'session',
    mood_owner_id = 'session:' || COALESCE(session_id, '')
WHERE mood_owner_id = '';

CREATE INDEX IF NOT EXISTS idx_agent_affect_states_owner_current
    ON agent_affect_states(persona_id, mood_owner_scope, mood_owner_id, updated_at DESC);
`,
	},
	{
		Version: 22,
		SQL: `
CREATE TABLE IF NOT EXISTS agent_affect_jobs (
    seq INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,

    persona_id TEXT NOT NULL,
    session_id TEXT,
    turn_id TEXT,

    mood_owner_scope TEXT NOT NULL,
    mood_owner_id TEXT NOT NULL,

    job_type TEXT NOT NULL DEFAULT 'turn_evaluate'
        CHECK (job_type IN ('turn_evaluate','plugin_evaluate','manual_evaluate','barrier')),
    batchable INTEGER NOT NULL DEFAULT 1 CHECK (batchable IN (0,1)),
    barrier_kind TEXT NOT NULL DEFAULT '',

    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','running','done','failed','superseded')),
    priority INTEGER NOT NULL DEFAULT 100,
    run_after TEXT NOT NULL,

    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    claimed_by TEXT,
    claimed_until TEXT,

    trigger_json TEXT NOT NULL DEFAULT '{}',
    input_mode TEXT NOT NULL DEFAULT 'mixed'
        CHECK (input_mode IN ('raw','summary','mixed','none')),
    user_text TEXT,
    assistant_text TEXT,
    input_summary TEXT,
    memory_prompt_block TEXT,

    base_state_id TEXT,
    base_state_updated_at TEXT,

    batch_id TEXT,
    result_evaluation_id TEXT,
    result_event_id TEXT,

    error_message TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    started_at TEXT,
    finished_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_agent_affect_jobs_claim
    ON agent_affect_jobs(status, run_after, priority, seq);

CREATE INDEX IF NOT EXISTS idx_agent_affect_jobs_owner_status
    ON agent_affect_jobs(mood_owner_scope, mood_owner_id, status, seq);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_affect_jobs_turn_unique
    ON agent_affect_jobs(turn_id, job_type)
    WHERE turn_id IS NOT NULL AND job_type = 'turn_evaluate';

CREATE TABLE IF NOT EXISTS agent_affect_job_batches (
    id TEXT PRIMARY KEY,
    persona_id TEXT NOT NULL,
    mood_owner_scope TEXT NOT NULL,
    mood_owner_id TEXT NOT NULL,

    job_type TEXT NOT NULL DEFAULT 'turn_evaluate',
    status TEXT NOT NULL DEFAULT 'running'
        CHECK (status IN ('running','done','failed','superseded')),

    job_count INTEGER NOT NULL DEFAULT 0,
    first_job_seq INTEGER NOT NULL,
    last_job_seq INTEGER NOT NULL,
    job_ids_json TEXT NOT NULL DEFAULT '[]',
    session_ids_json TEXT NOT NULL DEFAULT '[]',
    turn_ids_json TEXT NOT NULL DEFAULT '[]',

    batch_input_summary TEXT NOT NULL DEFAULT '',
    context_window_snapshot_json TEXT,

    evaluation_id TEXT,
    affect_event_id TEXT,
    error_message TEXT,

    claimed_by TEXT,
    started_at TEXT NOT NULL,
    finished_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_agent_affect_batches_owner_time
    ON agent_affect_job_batches(mood_owner_scope, mood_owner_id, started_at DESC);
`,
	},
	{
		Version: 23,
		SQL: `
ALTER TABLE agent_affect_evaluations ADD COLUMN batch_id TEXT;
ALTER TABLE agent_affect_events ADD COLUMN batch_id TEXT;

CREATE INDEX IF NOT EXISTS idx_agent_affect_evaluations_batch
    ON agent_affect_evaluations(batch_id);
CREATE INDEX IF NOT EXISTS idx_agent_affect_events_batch
    ON agent_affect_events(batch_id);
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

	if err := ApplySchemaRepairs(db); err != nil {
		return fmt.Errorf("schema repair: %w", err)
	}

	return nil
}

// ApplySchemaRepairs patches additive schema drift from development databases
// whose schema_version rows predate later edits to already-applied migrations.
func ApplySchemaRepairs(db *sql.DB) error {
	if err := ensureApprovalRequestsSchema(db); err != nil {
		return err
	}
	if err := ensureRuntimeSettingsSchema(db); err != nil {
		return err
	}
	if err := ensureLLMProvidersSchema(db); err != nil {
		return err
	}
	return nil
}

func ensureLLMProvidersSchema(db *sql.DB) error {
	var tableName string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='llm_providers'").Scan(&tableName)
	if err == sql.ErrNoRows {
		if _, err := db.Exec(`
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
    updated_at              TEXT NOT NULL DEFAULT (datetime('now')),
    preset_id               TEXT NOT NULL DEFAULT '',
    capabilities_json       TEXT NOT NULL DEFAULT '["chat"]'
);
`); err != nil {
			return fmt.Errorf("ensure llm_providers table: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("check llm_providers table: %w", err)
	}
	columns, err := tableColumns(db, "llm_providers")
	if err != nil {
		return fmt.Errorf("read llm_providers columns: %w", err)
	}
	if !columns["capabilities_json"] {
		if _, err := db.Exec(`ALTER TABLE llm_providers ADD COLUMN capabilities_json TEXT NOT NULL DEFAULT '["chat"]'`); err != nil {
			return fmt.Errorf("add llm_providers.capabilities_json: %w", err)
		}
	}
	return nil
}

func ensureRuntimeSettingsSchema(db *sql.DB) error {
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS runtime_settings (
    namespace  TEXT NOT NULL,
    key        TEXT NOT NULL,
    value_json TEXT NOT NULL,
    source     TEXT NOT NULL DEFAULT 'ui',
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY(namespace, key)
);
`); err != nil {
		return fmt.Errorf("ensure runtime_settings table: %w", err)
	}
	return nil
}

func ensureApprovalRequestsSchema(db *sql.DB) error {
	if _, err := db.Exec(`
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
    approval_kind         TEXT NOT NULL DEFAULT '',
    tool_name             TEXT NOT NULL DEFAULT '',
    normalized_input_hash TEXT NOT NULL DEFAULT '',
    path_digest           TEXT NOT NULL DEFAULT '',
    input_preview         TEXT NOT NULL DEFAULT '',
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL
);
`); err != nil {
		return fmt.Errorf("ensure approval_requests table: %w", err)
	}

	columns, err := tableColumns(db, "approval_requests")
	if err != nil {
		return fmt.Errorf("read approval_requests columns: %w", err)
	}
	for _, column := range []struct {
		name string
		sql  string
	}{
		{"tool_name", "ALTER TABLE approval_requests ADD COLUMN tool_name TEXT NOT NULL DEFAULT ''"},
		{"normalized_input_hash", "ALTER TABLE approval_requests ADD COLUMN normalized_input_hash TEXT NOT NULL DEFAULT ''"},
		{"path_digest", "ALTER TABLE approval_requests ADD COLUMN path_digest TEXT NOT NULL DEFAULT ''"},
		{"input_preview", "ALTER TABLE approval_requests ADD COLUMN input_preview TEXT NOT NULL DEFAULT ''"},
		{"approval_kind", "ALTER TABLE approval_requests ADD COLUMN approval_kind TEXT NOT NULL DEFAULT ''"},
	} {
		if columns[column.name] {
			continue
		}
		if _, err := db.Exec(column.sql); err != nil {
			return fmt.Errorf("add approval_requests.%s: %w", column.name, err)
		}
	}

	if _, err := db.Exec(`
CREATE INDEX IF NOT EXISTS idx_approval_requests_session_status
    ON approval_requests(session_id, status);
CREATE INDEX IF NOT EXISTS idx_approval_requests_task_created
    ON approval_requests(task_id, created_at);
CREATE INDEX IF NOT EXISTS idx_approval_requests_expires_at
    ON approval_requests(expires_at);
CREATE INDEX IF NOT EXISTS idx_approval_requests_binding
    ON approval_requests(session_id, task_id, tool_name, normalized_input_hash, path_digest);
CREATE INDEX IF NOT EXISTS idx_approval_requests_kind_binding
    ON approval_requests(session_id, task_id, approval_kind, tool_name, normalized_input_hash, path_digest);
`); err != nil {
		return fmt.Errorf("ensure approval_requests indexes: %w", err)
	}
	return nil
}

func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, err
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
			return nil, err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}
