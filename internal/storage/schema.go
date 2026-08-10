package storage

import (
	"database/sql"
	"fmt"
	"strings"
)

// Migration represents a single schema migration.
type Migration struct {
	Version int
	SQL     string
}

const pluginRuntimeSchemaSQL = `
CREATE TABLE IF NOT EXISTS plugin_installations (
    id TEXT PRIMARY KEY,
    plugin_id TEXT NOT NULL,
    version TEXT NOT NULL,
    name TEXT NOT NULL,
    manifest_json TEXT NOT NULL,
    source_type TEXT NOT NULL,
    source_ref TEXT NOT NULL DEFAULT '',
    package_digest TEXT NOT NULL DEFAULT '',
    manifest_digest TEXT NOT NULL DEFAULT '',
    signature_status TEXT NOT NULL DEFAULT '',
    publisher_id TEXT NOT NULL DEFAULT '',
    installed_at TEXT NOT NULL DEFAULT (datetime('now')),
    installed_by TEXT NOT NULL DEFAULT 'local',
    store_path TEXT NOT NULL,
    UNIQUE(plugin_id, version)
);

CREATE TABLE IF NOT EXISTS plugin_enabled_state (
    plugin_id TEXT PRIMARY KEY,
    version TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0,1)),
    user_grant_json TEXT NOT NULL DEFAULT '{}',
    trust_level TEXT NOT NULL DEFAULT '',
    trust_accepted_at TEXT NOT NULL DEFAULT '',
    trust_ack_hash TEXT NOT NULL DEFAULT '',
    trust_review_reasons_json TEXT NOT NULL DEFAULT '[]',
    default_tool_exposure TEXT NOT NULL DEFAULT '',
    default_invocation_policy TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS plugin_runtime_records (
    plugin_id TEXT PRIMARY KEY,
    version TEXT NOT NULL DEFAULT '',
    runtime_kind TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'stopped',
    pid INTEGER,
    last_started_at TEXT,
    last_stopped_at TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    restart_count INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS plugin_access_events (
    id TEXT PRIMARY KEY,
    plugin_id TEXT NOT NULL,
    access_kind TEXT NOT NULL,
    capability TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    request_summary TEXT NOT NULL DEFAULT '',
    input_hash TEXT NOT NULL DEFAULT '',
    output_hash TEXT NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS plugin_trust_acceptance_history (
    id TEXT PRIMARY KEY,
    plugin_id TEXT NOT NULL,
    version TEXT NOT NULL,
    trust_level TEXT NOT NULL DEFAULT '',
    accepted_at TEXT NOT NULL DEFAULT '',
    trust_ack_hash TEXT NOT NULL DEFAULT '',
    trust_review_reasons_json TEXT NOT NULL DEFAULT '[]',
    default_tool_exposure TEXT NOT NULL DEFAULT '',
    default_invocation_policy TEXT NOT NULL DEFAULT '',
    user_grant_hash TEXT NOT NULL DEFAULT '',
    host_policy_fingerprint TEXT NOT NULL DEFAULT '',
    dependency_lock_digest TEXT NOT NULL DEFAULT '',
    package_digest TEXT NOT NULL DEFAULT '',
    manifest_digest TEXT NOT NULL DEFAULT '',
    signature_status TEXT NOT NULL DEFAULT '',
    publisher_id TEXT NOT NULL DEFAULT '',
    source_type TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS plugin_provider_usage (
    id TEXT PRIMARY KEY,
    plugin_id TEXT NOT NULL,
    provider_id TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    purpose TEXT NOT NULL DEFAULT '',
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    estimated_tokens INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    error_message TEXT NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS plugin_kv (
    plugin_id TEXT NOT NULL,
    key TEXT NOT NULL,
    value_json TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY(plugin_id, key)
);
`

const pluginRuntimeIndexSQL = `
CREATE INDEX IF NOT EXISTS idx_plugin_access_events_plugin_time
    ON plugin_access_events(plugin_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_plugin_trust_acceptance_history_plugin_time
    ON plugin_trust_acceptance_history(plugin_id, accepted_at DESC);

CREATE INDEX IF NOT EXISTS idx_plugin_provider_usage_plugin_time
    ON plugin_provider_usage(plugin_id, created_at DESC);
`

const promptCenterSchemaSQL = `
CREATE TABLE IF NOT EXISTS prompt_overrides (
    id                    TEXT PRIMARY KEY,
    component_id          TEXT NOT NULL,
    scope_type            TEXT NOT NULL CHECK (scope_type IN ('global', 'agent')),
    scope_id              TEXT NOT NULL DEFAULT '',
    mode                  TEXT NOT NULL CHECK (mode IN ('custom', 'use_default')),
    override_text         TEXT NOT NULL DEFAULT '',
    enabled               INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    default_hash_at_edit  TEXT NOT NULL DEFAULT '',
    note                  TEXT NOT NULL DEFAULT '',
    created_at            TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at            TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(component_id, scope_type, scope_id),
    CHECK (
      (scope_type = 'global' AND scope_id = '') OR
      (scope_type = 'agent' AND scope_id <> '')
    ),
    CHECK (
      (mode = 'custom' AND length(override_text) > 0) OR
      (mode = 'use_default' AND override_text = '')
    )
);

CREATE INDEX IF NOT EXISTS idx_prompt_overrides_component
    ON prompt_overrides(component_id, scope_type, scope_id);

CREATE INDEX IF NOT EXISTS idx_prompt_overrides_agent
    ON prompt_overrides(scope_id, component_id)
    WHERE scope_type = 'agent';

CREATE TABLE IF NOT EXISTS prompt_render_snapshots (
    id                TEXT PRIMARY KEY,
    request_id        TEXT NOT NULL DEFAULT '',
    turn_id           TEXT NOT NULL DEFAULT '',
    session_id        TEXT NOT NULL DEFAULT '',
    agent_id          TEXT NOT NULL DEFAULT '',
    persona_key       TEXT NOT NULL DEFAULT '',
    purpose           TEXT NOT NULL,
    model             TEXT NOT NULL DEFAULT '',
    final_hash        TEXT NOT NULL,
    components_json   TEXT NOT NULL DEFAULT '[]',
    rendered_text     TEXT NOT NULL DEFAULT '',
    truncated         INTEGER NOT NULL DEFAULT 0 CHECK (truncated IN (0, 1)),
    created_at        TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_prompt_render_snapshots_session_time
    ON prompt_render_snapshots(session_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_prompt_render_snapshots_agent_time
    ON prompt_render_snapshots(agent_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_prompt_render_snapshots_purpose_time
    ON prompt_render_snapshots(purpose, created_at DESC);
`

const resourceGrantSchemaSQL = `
CREATE TABLE IF NOT EXISTS resource_grants (
    id TEXT PRIMARY KEY,
    principal_kind TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    capability TEXT NOT NULL,
    resource_json TEXT NOT NULL,
    operations_json TEXT NOT NULL DEFAULT '[]',
    constraints_json TEXT NOT NULL DEFAULT '{}',
    lifetime TEXT NOT NULL CHECK (lifetime IN ('once','task','session','persistent')),
    status TEXT NOT NULL CHECK (status IN ('pending','active','consumed','revoked','expired')),
    approval_request_id TEXT NOT NULL DEFAULT '',
    binding_hash TEXT NOT NULL,
    issued_by TEXT NOT NULL DEFAULT 'policy',
    created_at TEXT NOT NULL,
    expires_at TEXT,
    consumed_at TEXT,
    revoked_at TEXT,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS resource_grant_events (
    id TEXT PRIMARY KEY,
    grant_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    principal_kind TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    summary_hash TEXT NOT NULL DEFAULT '',
    provenance_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    FOREIGN KEY(grant_id) REFERENCES resource_grants(id)
);

`

const resourceGrantIndexSQL = `
CREATE INDEX IF NOT EXISTS idx_resource_grants_principal_status
    ON resource_grants(principal_kind, principal_id, status);
CREATE INDEX IF NOT EXISTS idx_resource_grants_capability_status
    ON resource_grants(capability, status);
CREATE INDEX IF NOT EXISTS idx_resource_grants_expires
    ON resource_grants(status, expires_at);
CREATE INDEX IF NOT EXISTS idx_resource_grant_events_grant_time
    ON resource_grant_events(grant_id, created_at);
`

const hostResourceChangeSetSchemaSQL = `
CREATE TABLE IF NOT EXISTS host_resource_changesets (
    id TEXT PRIMARY KEY,
    principal_kind TEXT NOT NULL DEFAULT '',
    principal_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('staged','approval_pending','applying','applied','conflict','failed','cancelled','restored')),
    operation TEXT NOT NULL,
    source_ref_json TEXT NOT NULL DEFAULT '{}',
    target_ref_json TEXT NOT NULL DEFAULT '{}',
    target_display_path TEXT NOT NULL DEFAULT '',
    baseline_hash TEXT NOT NULL DEFAULT '',
    baseline_file_id TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    plan_hash TEXT NOT NULL,
    staging_path TEXT NOT NULL DEFAULT '',
    quarantine_path TEXT NOT NULL DEFAULT '',
    preview_json TEXT NOT NULL DEFAULT '{}',
    permanent_delete INTEGER NOT NULL DEFAULT 0 CHECK (permanent_delete IN (0,1)),
    recursive INTEGER NOT NULL DEFAULT 0 CHECK (recursive IN (0,1)),
    error_message TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    applied_at TEXT,
    canceled_at TEXT,
    restored_at TEXT
);

CREATE TABLE IF NOT EXISTS host_resource_change_ops (
    id TEXT PRIMARY KEY,
    changeset_id TEXT NOT NULL,
    op_index INTEGER NOT NULL,
    operation TEXT NOT NULL,
    source_display_path TEXT NOT NULL DEFAULT '',
    target_display_path TEXT NOT NULL DEFAULT '',
    source_hash TEXT NOT NULL DEFAULT '',
    target_hash TEXT NOT NULL DEFAULT '',
    bytes INTEGER NOT NULL DEFAULT 0,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    FOREIGN KEY(changeset_id) REFERENCES host_resource_changesets(id)
);

`

const hostResourceChangeSetIndexSQL = `
CREATE INDEX IF NOT EXISTS idx_host_resource_changesets_status_time
    ON host_resource_changesets(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_host_resource_changesets_principal_time
    ON host_resource_changesets(principal_kind, principal_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_host_resource_changesets_plan_hash
    ON host_resource_changesets(plan_hash);
CREATE INDEX IF NOT EXISTS idx_host_resource_change_ops_changeset
    ON host_resource_change_ops(changeset_id, op_index);
`

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
	{
		Version: 24,
		SQL:     pluginRuntimeSchemaSQL + pluginRuntimeIndexSQL,
	},
	{
		Version: 25,
		SQL: `
CREATE TABLE IF NOT EXISTS media_assets (
    id                  TEXT PRIMARY KEY,
    sha256              TEXT NOT NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('image','audio','video','file')),
    mime_type           TEXT NOT NULL,
    original_filename   TEXT,
    file_ext            TEXT,
    byte_size           INTEGER NOT NULL,
    width               INTEGER,
    height              INTEGER,
    duration_ms         INTEGER,
    storage_backend     TEXT NOT NULL DEFAULT 'local',
    storage_uri         TEXT NOT NULL,
    thumbnail_uri       TEXT,
    created_by_role     TEXT NOT NULL CHECK (created_by_role IN ('user','assistant','system','tool')),
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    visibility_status   TEXT NOT NULL DEFAULT 'visible'
        CHECK (visibility_status IN ('visible','hidden','forgotten','purged')),
    scan_status         TEXT NOT NULL DEFAULT 'pending'
        CHECK (scan_status IN ('pending','clean','rejected','failed')),
    retention_policy    TEXT NOT NULL DEFAULT 'chat_asset',
    expires_at          TEXT,
    reference_count     INTEGER NOT NULL DEFAULT 0,
    purged_at           TEXT,
    purge_reason        TEXT,
    UNIQUE(sha256, byte_size)
);

CREATE TABLE IF NOT EXISTS message_parts (
    id              TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL,
    message_id      TEXT NOT NULL,
    role            TEXT NOT NULL CHECK (role IN ('user','assistant','system','tool')),
    ordinal         INTEGER NOT NULL,
    part_type       TEXT NOT NULL CHECK (part_type IN ('text','image','audio','video','file','tool_use','tool_result')),
    text_content    TEXT,
    media_asset_id  TEXT,
    memory_render_policy TEXT NOT NULL DEFAULT 'placeholder_only'
        CHECK (memory_render_policy IN ('placeholder_only','text_only','never')),
    history_render_policy TEXT NOT NULL DEFAULT 'placeholder_only'
        CHECK (history_render_policy IN ('placeholder_only','resend_if_reactivated','never')),
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY(media_asset_id) REFERENCES media_assets(id)
);

CREATE INDEX IF NOT EXISTS idx_message_parts_message
    ON message_parts(session_id, message_id, ordinal);
CREATE INDEX IF NOT EXISTS idx_message_parts_media
    ON message_parts(media_asset_id);

CREATE TABLE IF NOT EXISTS provider_media_refs (
    id              TEXT PRIMARY KEY,
    media_asset_id  TEXT NOT NULL,
    provider_id     TEXT NOT NULL,
    model_scope     TEXT,
    ref_type        TEXT NOT NULL,
    remote_ref      TEXT NOT NULL,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at      TEXT,
    last_used_at    TEXT,
    delete_status   TEXT NOT NULL DEFAULT 'active'
        CHECK (delete_status IN ('active','delete_queued','deleted','delete_failed')),
    metadata_json   TEXT,
    FOREIGN KEY(media_asset_id) REFERENCES media_assets(id)
);

CREATE TABLE IF NOT EXISTS message_media_deliveries (
    id              TEXT PRIMARY KEY,
    message_id      TEXT NOT NULL,
    part_id         TEXT NOT NULL,
    media_asset_id  TEXT NOT NULL,
    provider_id     TEXT NOT NULL,
    model_id        TEXT NOT NULL,
    turn_id         TEXT NOT NULL,
    delivery_scope  TEXT NOT NULL CHECK (delivery_scope IN ('current_turn','reactivated_reference','history_placeholder')),
    transport       TEXT NOT NULL CHECK (transport IN ('data_url','base64','remote_url','provider_file','placeholder')),
    status          TEXT NOT NULL CHECK (status IN ('prepared','sent','failed','omitted')),
    byte_size_sent  INTEGER,
    error_message   TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_media_deliveries_lookup
    ON message_media_deliveries(media_asset_id, provider_id, model_id, created_at DESC);

CREATE TABLE IF NOT EXISTS llm_model_capabilities (
    id                    TEXT PRIMARY KEY,
    provider_id            TEXT NOT NULL,
    model_id               TEXT NOT NULL,
    input_modalities_json  TEXT NOT NULL DEFAULT '["text"]',
    output_modalities_json TEXT NOT NULL DEFAULT '["text"]',
    image_transports_json  TEXT NOT NULL DEFAULT '[]',
    image_formats_json     TEXT NOT NULL DEFAULT '[]',
    max_images_per_request INTEGER,
    max_image_bytes        INTEGER,
    max_request_bytes      INTEGER,
    max_long_edge_pixels   INTEGER,
    supports_vision_tools      INTEGER NOT NULL DEFAULT 0,
    supports_vision_streaming  INTEGER NOT NULL DEFAULT 0,
    supports_vision_json_mode  INTEGER NOT NULL DEFAULT 0,
    param_policy_json      TEXT,
    capability_source      TEXT NOT NULL DEFAULT 'unknown'
        CHECK (capability_source IN ('unknown','provider_metadata','provider_docs_preset','manual_override','probe_passed','probe_failed','merged')),
    confidence             REAL NOT NULL DEFAULT 0.0 CHECK (confidence >= 0 AND confidence <= 1),
    last_refreshed_at      TEXT,
    last_verified_at       TEXT,
    raw_provider_json      TEXT,
    created_at             TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at             TEXT,
    UNIQUE(provider_id, model_id)
);
`,
	},
	{
		Version: 26,
		SQL:     `SELECT 1;`,
	},
	{
		Version: 27,
		SQL:     promptCenterSchemaSQL,
	},
	{
		Version: 28,
		SQL:     `SELECT 1;`,
	},
	{
		Version: 29,
		SQL:     `SELECT 1;`,
	},
	{
		Version: 30,
		SQL:     `SELECT 1;`,
	},
	{
		Version: 31,
		SQL: `
CREATE TABLE IF NOT EXISTS plugin_trust_acceptance_history (
    id TEXT PRIMARY KEY,
    plugin_id TEXT NOT NULL,
    version TEXT NOT NULL,
    trust_level TEXT NOT NULL DEFAULT '',
    accepted_at TEXT NOT NULL DEFAULT '',
    trust_ack_hash TEXT NOT NULL DEFAULT '',
    trust_review_reasons_json TEXT NOT NULL DEFAULT '[]',
    default_tool_exposure TEXT NOT NULL DEFAULT '',
    default_invocation_policy TEXT NOT NULL DEFAULT '',
    user_grant_hash TEXT NOT NULL DEFAULT '',
    host_policy_fingerprint TEXT NOT NULL DEFAULT '',
    dependency_lock_digest TEXT NOT NULL DEFAULT '',
    package_digest TEXT NOT NULL DEFAULT '',
    manifest_digest TEXT NOT NULL DEFAULT '',
    signature_status TEXT NOT NULL DEFAULT '',
    publisher_id TEXT NOT NULL DEFAULT '',
    source_type TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_plugin_trust_acceptance_history_plugin_time
    ON plugin_trust_acceptance_history(plugin_id, accepted_at DESC);
`,
	},
	{
		Version: 32,
		SQL:     `SELECT 1;`,
	},
	{
		Version: 33,
		SQL:     `SELECT 1;`,
	},
	{
		Version: 34,
		SQL: `
CREATE TABLE IF NOT EXISTS llm_usage_events (
    id TEXT PRIMARY KEY,
    request_id TEXT NOT NULL DEFAULT '',
    provider_request_id TEXT NOT NULL DEFAULT '',
    response_id TEXT NOT NULL DEFAULT '',
    session_id TEXT NOT NULL DEFAULT '',
    turn_id TEXT NOT NULL DEFAULT '',
    agent_id TEXT NOT NULL DEFAULT '',
    persona_key TEXT NOT NULL DEFAULT '',
    plugin_id TEXT NOT NULL DEFAULT '',
    task_id TEXT NOT NULL DEFAULT '',
    component TEXT NOT NULL DEFAULT 'unknown',
    operation TEXT NOT NULL DEFAULT '',
    provider_id TEXT NOT NULL DEFAULT '',
    provider_name TEXT NOT NULL DEFAULT '',
    protocol TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    endpoint TEXT NOT NULL DEFAULT '',
    stream INTEGER NOT NULL DEFAULT 0 CHECK (stream IN (0,1)),
    status TEXT NOT NULL DEFAULT 'success',
    error_kind TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens INTEGER NOT NULL DEFAULT 0,
    estimated_input_tokens INTEGER NOT NULL DEFAULT 0,
    estimated_output_tokens INTEGER NOT NULL DEFAULT 0,
    estimated_total_tokens INTEGER NOT NULL DEFAULT 0,
    estimate_method TEXT NOT NULL DEFAULT '',
    estimate_confidence REAL NOT NULL DEFAULT 0,
    actual_input_tokens INTEGER NOT NULL DEFAULT 0,
    actual_output_tokens INTEGER NOT NULL DEFAULT 0,
    actual_total_tokens INTEGER NOT NULL DEFAULT 0,
    cached_input_tokens INTEGER NOT NULL DEFAULT 0,
    cache_hit_input_tokens INTEGER NOT NULL DEFAULT 0,
    cache_miss_input_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens INTEGER NOT NULL DEFAULT 0,
    cache_write_tokens INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens INTEGER NOT NULL DEFAULT 0,
    image_tokens INTEGER NOT NULL DEFAULT 0,
    audio_tokens INTEGER NOT NULL DEFAULT 0,
    usage_source TEXT NOT NULL DEFAULT 'unknown'
        CHECK (usage_source IN ('provider_usage','estimated','hybrid','unknown')),
    prompt_hash TEXT NOT NULL DEFAULT '',
    completion_hash TEXT NOT NULL DEFAULT '',
    raw_usage_json TEXT NOT NULL DEFAULT '',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_llm_usage_events_created
    ON llm_usage_events(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_llm_usage_events_session_time
    ON llm_usage_events(session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_llm_usage_events_provider_model_time
    ON llm_usage_events(provider_id, model, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_llm_usage_events_component_time
    ON llm_usage_events(component, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_llm_usage_events_plugin_time
    ON llm_usage_events(plugin_id, created_at DESC);

CREATE TABLE IF NOT EXISTS token_estimator_calibrations (
    id TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    estimate_method TEXT NOT NULL DEFAULT '',
    bucket TEXT NOT NULL DEFAULT 'global',
    sample_count INTEGER NOT NULL DEFAULT 0,
    estimated_input_tokens_sum INTEGER NOT NULL DEFAULT 0,
    actual_input_tokens_sum INTEGER NOT NULL DEFAULT 0,
    estimated_output_tokens_sum INTEGER NOT NULL DEFAULT 0,
    actual_output_tokens_sum INTEGER NOT NULL DEFAULT 0,
    correction_factor REAL NOT NULL DEFAULT 1.0,
    mean_abs_pct_error REAL NOT NULL DEFAULT 0,
    last_event_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT '',
    UNIQUE(provider_id, model, estimate_method, bucket)
);
CREATE INDEX IF NOT EXISTS idx_token_estimator_calibrations_provider_model
    ON token_estimator_calibrations(provider_id, model, estimate_method);
`,
	},
	{
		Version: 35,
		SQL: `
CREATE TABLE IF NOT EXISTS pending_direct_tool_calls (
    approval_request_id   TEXT PRIMARY KEY,
    session_id            TEXT NOT NULL,
    turn_id               TEXT NOT NULL DEFAULT '',
    task_id               TEXT NOT NULL,
    call_id               TEXT NOT NULL,
    tool_name             TEXT NOT NULL,
    input_json            TEXT NOT NULL,
    max_permission        TEXT NOT NULL DEFAULT 'read-only',
    provider              TEXT NOT NULL DEFAULT '',
    approval_kind         TEXT NOT NULL DEFAULT '',
    normalized_input_hash TEXT NOT NULL DEFAULT '',
    path_digest           TEXT NOT NULL DEFAULT '',
    input_preview         TEXT NOT NULL DEFAULT '',
    status                TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','claimed','consumed','rejected','expired','failed')),
    claim_id              TEXT NOT NULL DEFAULT '',
    claimed_at            TEXT,
    consumed_at           TEXT,
    created_at            TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at            TEXT NOT NULL,
    error_message         TEXT NOT NULL DEFAULT '',
    FOREIGN KEY(approval_request_id) REFERENCES approval_requests(id) ON DELETE CASCADE,
    UNIQUE(session_id, task_id)
);
CREATE INDEX IF NOT EXISTS idx_pending_direct_tool_calls_session_status
    ON pending_direct_tool_calls(session_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_pending_direct_tool_calls_task
    ON pending_direct_tool_calls(session_id, task_id);
CREATE INDEX IF NOT EXISTS idx_pending_direct_tool_calls_expires
    ON pending_direct_tool_calls(expires_at)
    WHERE status IN ('pending','claimed');
CREATE INDEX IF NOT EXISTS idx_pending_direct_tool_calls_tool
    ON pending_direct_tool_calls(tool_name, created_at DESC);
`,
	},
	{
		Version: 36,
		SQL:     `SELECT 1;`,
	},
	{
		Version: 37,
		SQL: `
CREATE TABLE IF NOT EXISTS conversation_origins (
    id                         TEXT PRIMARY KEY,
    origin_key                 TEXT NOT NULL UNIQUE,
    source_type                TEXT NOT NULL,
    adapter_instance_id        TEXT NOT NULL DEFAULT '',
    platform_id                TEXT NOT NULL DEFAULT '',
    channel_type               TEXT NOT NULL,
    external_conversation_id   TEXT NOT NULL DEFAULT '',
    external_actor_id          TEXT NOT NULL DEFAULT '',
    display_name               TEXT NOT NULL DEFAULT '',
    metadata_json              TEXT NOT NULL DEFAULT '{}',
    created_at                 TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at                 TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_conversation_origins_source
    ON conversation_origins(source_type, adapter_instance_id, channel_type, external_conversation_id);

CREATE TABLE IF NOT EXISTS conversation_bindings (
    id                         TEXT PRIMARY KEY,
    origin_key                 TEXT NOT NULL,
    persona_key                TEXT NOT NULL,
    current_session_id         TEXT NOT NULL,
    default_persona_key        TEXT NOT NULL DEFAULT '',
    unique_scope               TEXT NOT NULL DEFAULT 'origin',
    variables_json             TEXT NOT NULL DEFAULT '{}',
    created_at                 TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at                 TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY(origin_key) REFERENCES conversation_origins(origin_key) ON DELETE CASCADE,
    FOREIGN KEY(current_session_id) REFERENCES sessions(id) ON DELETE CASCADE,
    UNIQUE(origin_key, persona_key)
);
CREATE INDEX IF NOT EXISTS idx_conversation_bindings_current_session
    ON conversation_bindings(current_session_id);

CREATE TABLE IF NOT EXISTS session_clear_markers (
    id                         TEXT PRIMARY KEY,
    origin_key                 TEXT NOT NULL,
    session_id                 TEXT NOT NULL,
    persona_key                TEXT NOT NULL,
    after_message_id           TEXT NOT NULL DEFAULT '',
    cleared_at                 TEXT NOT NULL DEFAULT (datetime('now')),
    reason                     TEXT NOT NULL DEFAULT 'command_clear',
    metadata_json              TEXT NOT NULL DEFAULT '{}',
    UNIQUE(origin_key, session_id),
    FOREIGN KEY(origin_key) REFERENCES conversation_origins(origin_key) ON DELETE CASCADE,
    FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_session_clear_markers_session
    ON session_clear_markers(session_id, origin_key);

CREATE TABLE IF NOT EXISTS conversation_events (
    id                         TEXT PRIMARY KEY,
    origin_key                 TEXT NOT NULL,
    session_id                 TEXT NOT NULL,
    persona_key                TEXT NOT NULL,
    event_type                 TEXT NOT NULL,
    visible_content            TEXT NOT NULL DEFAULT '',
    payload_json               TEXT NOT NULL DEFAULT '{}',
    created_at                 TEXT NOT NULL DEFAULT (datetime('now')),
    visibility_status          TEXT NOT NULL DEFAULT 'visible',
    FOREIGN KEY(origin_key) REFERENCES conversation_origins(origin_key) ON DELETE CASCADE,
    FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_conversation_events_session_time
    ON conversation_events(session_id, created_at);
`,
	},
	{
		Version: 38,
		SQL: `
CREATE TABLE IF NOT EXISTS command_configs (
    command_id                 TEXT PRIMARY KEY,
    provider_kind              TEXT NOT NULL,
    plugin_id                  TEXT NOT NULL DEFAULT '',
    original_name              TEXT NOT NULL,
    effective_name             TEXT NOT NULL,
    aliases_json               TEXT NOT NULL DEFAULT '[]',
    enabled                    INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0, 1)),
    permission                 TEXT NOT NULL DEFAULT 'member',
    output_mode                TEXT NOT NULL DEFAULT 'direct',
    config_json                TEXT NOT NULL DEFAULT '{}',
    updated_at                 TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_command_configs_effective
    ON command_configs(effective_name, enabled);

CREATE TABLE IF NOT EXISTS command_invocations (
    id                         TEXT PRIMARY KEY,
    command_id                 TEXT NOT NULL,
    command_name               TEXT NOT NULL,
    provider_kind              TEXT NOT NULL,
    plugin_id                  TEXT NOT NULL DEFAULT '',
    origin_key                 TEXT NOT NULL,
    source_type                TEXT NOT NULL,
    session_id                 TEXT NOT NULL,
    persona_key                TEXT NOT NULL,
    actor_id                   TEXT NOT NULL DEFAULT '',
    actor_role                 TEXT NOT NULL DEFAULT 'member',
    input_hash                 TEXT NOT NULL DEFAULT '',
    argv_json                  TEXT NOT NULL DEFAULT '[]',
    flags_json                 TEXT NOT NULL DEFAULT '{}',
    output_mode                TEXT NOT NULL DEFAULT 'direct',
    status                     TEXT NOT NULL,
    result_text                TEXT NOT NULL DEFAULT '',
    payload_json               TEXT NOT NULL DEFAULT '{}',
    error_kind                 TEXT NOT NULL DEFAULT '',
    duration_ms                INTEGER NOT NULL DEFAULT 0,
    created_at                 TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_command_invocations_session_time
    ON command_invocations(session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_command_invocations_origin_time
    ON command_invocations(origin_key, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_command_invocations_command_time
    ON command_invocations(command_id, created_at DESC);
`,
	},
	{
		Version: 39,
		SQL: `
CREATE TABLE IF NOT EXISTS platform_message_receipts (
    id                         TEXT PRIMARY KEY,
    source_type                TEXT NOT NULL,
    adapter_instance_id        TEXT NOT NULL DEFAULT '',
    platform_id                TEXT NOT NULL DEFAULT '',
    external_message_id        TEXT NOT NULL,
    origin_key                 TEXT NOT NULL,
    session_id                 TEXT NOT NULL DEFAULT '',
    persona_key                TEXT NOT NULL DEFAULT '',
    message_hash               TEXT NOT NULL DEFAULT '',
    status                     TEXT NOT NULL DEFAULT 'processing'
        CHECK(status IN ('processing','handled','duplicate','failed','ignored')),
    result_type                TEXT NOT NULL DEFAULT '',
    error_message              TEXT NOT NULL DEFAULT '',
    received_at                TEXT NOT NULL DEFAULT (datetime('now')),
    handled_at                 TEXT,
    UNIQUE(source_type, adapter_instance_id, external_message_id)
);

CREATE INDEX IF NOT EXISTS idx_platform_message_receipts_origin_time
    ON platform_message_receipts(origin_key, received_at DESC);

CREATE INDEX IF NOT EXISTS idx_platform_message_receipts_status
    ON platform_message_receipts(status, received_at DESC);
`,
	},
	{
		Version: 40,
		SQL: `
ALTER TABLE platform_message_receipts ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 1;
ALTER TABLE platform_message_receipts ADD COLUMN last_attempt_at TEXT;
ALTER TABLE platform_message_receipts ADD COLUMN turn_id TEXT NOT NULL DEFAULT '';
ALTER TABLE platform_message_receipts ADD COLUMN agent_id TEXT NOT NULL DEFAULT '';
ALTER TABLE platform_message_receipts ADD COLUMN resolved_persona_key TEXT NOT NULL DEFAULT '';
`,
	},
	{
		Version: 41,
		SQL: `
CREATE TABLE IF NOT EXISTS bad_samples (
    id TEXT PRIMARY KEY,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    reason TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    origin_key TEXT NOT NULL DEFAULT '',
    persona_key TEXT NOT NULL DEFAULT '',
    target_turn_id TEXT NOT NULL DEFAULT '',
    context_json TEXT NOT NULL,
    context_schema_version INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_bad_samples_created_at ON bad_samples(created_at DESC);
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
	if err := ensurePendingDirectToolCallsSchema(db); err != nil {
		return err
	}
	if err := ensureRuntimeSettingsSchema(db); err != nil {
		return err
	}
	if err := ensureAgentAffectSchema(db); err != nil {
		return err
	}
	if err := ensureLLMProvidersSchema(db); err != nil {
		return err
	}
	if err := ensureAgentConfigsSchema(db); err != nil {
		return err
	}
	if err := ensurePluginRuntimeSchema(db); err != nil {
		return err
	}
	if err := ensurePromptCenterSchema(db); err != nil {
		return err
	}
	if err := ensureResourceGrantSchema(db); err != nil {
		return err
	}
	if err := ensureHostResourceChangeSetSchema(db); err != nil {
		return err
	}
	if err := ensurePlatformMessageReceiptsSchema(db); err != nil {
		return err
	}
	if err := ensureNoImageBase64Guards(db); err != nil {
		return err
	}
	return nil
}

func ensurePlatformMessageReceiptsSchema(db *sql.DB) error {
	repairs := map[string][]struct {
		name string
		sql  string
	}{
		"platform_message_receipts": {
			{"attempt_count", "ALTER TABLE platform_message_receipts ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 1"},
			{"last_attempt_at", "ALTER TABLE platform_message_receipts ADD COLUMN last_attempt_at TEXT"},
			{"turn_id", "ALTER TABLE platform_message_receipts ADD COLUMN turn_id TEXT NOT NULL DEFAULT ''"},
			{"agent_id", "ALTER TABLE platform_message_receipts ADD COLUMN agent_id TEXT NOT NULL DEFAULT ''"},
			{"resolved_persona_key", "ALTER TABLE platform_message_receipts ADD COLUMN resolved_persona_key TEXT NOT NULL DEFAULT ''"},
		},
	}
	if err := ensureTableColumns(db, repairs); err != nil {
		return fmt.Errorf("repair platform_message_receipts schema: %w", err)
	}
	return nil
}

func ensureAgentConfigsSchema(db *sql.DB) error {
	columns, err := tableColumns(db, "agent_configs")
	if err != nil {
		return fmt.Errorf("read agent_configs columns: %w", err)
	}
	if len(columns) == 0 {
		return nil
	}
	repairs := map[string][]struct {
		name string
		sql  string
	}{
		"agent_configs": {
			{"user_address_json", "ALTER TABLE agent_configs ADD COLUMN user_address_json TEXT NOT NULL DEFAULT '{}'"},
		},
	}
	if err := ensureTableColumns(db, repairs); err != nil {
		return fmt.Errorf("repair agent_configs schema: %w", err)
	}
	return nil
}

func ensureHostResourceChangeSetSchema(db *sql.DB) error {
	if _, err := db.Exec(hostResourceChangeSetSchemaSQL); err != nil {
		return fmt.Errorf("ensure host resource changeset schema: %w", err)
	}
	repairs := map[string][]struct {
		name string
		sql  string
	}{
		"host_resource_changesets": {
			{"id", "ALTER TABLE host_resource_changesets ADD COLUMN id TEXT NOT NULL DEFAULT ''"},
			{"principal_kind", "ALTER TABLE host_resource_changesets ADD COLUMN principal_kind TEXT NOT NULL DEFAULT ''"},
			{"principal_id", "ALTER TABLE host_resource_changesets ADD COLUMN principal_id TEXT NOT NULL DEFAULT ''"},
			{"status", "ALTER TABLE host_resource_changesets ADD COLUMN status TEXT NOT NULL DEFAULT 'staged'"},
			{"operation", "ALTER TABLE host_resource_changesets ADD COLUMN operation TEXT NOT NULL DEFAULT ''"},
			{"source_ref_json", "ALTER TABLE host_resource_changesets ADD COLUMN source_ref_json TEXT NOT NULL DEFAULT '{}'"},
			{"target_ref_json", "ALTER TABLE host_resource_changesets ADD COLUMN target_ref_json TEXT NOT NULL DEFAULT '{}'"},
			{"target_display_path", "ALTER TABLE host_resource_changesets ADD COLUMN target_display_path TEXT NOT NULL DEFAULT ''"},
			{"baseline_hash", "ALTER TABLE host_resource_changesets ADD COLUMN baseline_hash TEXT NOT NULL DEFAULT ''"},
			{"baseline_file_id", "ALTER TABLE host_resource_changesets ADD COLUMN baseline_file_id TEXT NOT NULL DEFAULT ''"},
			{"content_hash", "ALTER TABLE host_resource_changesets ADD COLUMN content_hash TEXT NOT NULL DEFAULT ''"},
			{"plan_hash", "ALTER TABLE host_resource_changesets ADD COLUMN plan_hash TEXT NOT NULL DEFAULT ''"},
			{"staging_path", "ALTER TABLE host_resource_changesets ADD COLUMN staging_path TEXT NOT NULL DEFAULT ''"},
			{"quarantine_path", "ALTER TABLE host_resource_changesets ADD COLUMN quarantine_path TEXT NOT NULL DEFAULT ''"},
			{"preview_json", "ALTER TABLE host_resource_changesets ADD COLUMN preview_json TEXT NOT NULL DEFAULT '{}'"},
			{"permanent_delete", "ALTER TABLE host_resource_changesets ADD COLUMN permanent_delete INTEGER NOT NULL DEFAULT 0"},
			{"recursive", "ALTER TABLE host_resource_changesets ADD COLUMN recursive INTEGER NOT NULL DEFAULT 0"},
			{"error_message", "ALTER TABLE host_resource_changesets ADD COLUMN error_message TEXT NOT NULL DEFAULT ''"},
			{"created_at", "ALTER TABLE host_resource_changesets ADD COLUMN created_at TEXT NOT NULL DEFAULT ''"},
			{"updated_at", "ALTER TABLE host_resource_changesets ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''"},
			{"applied_at", "ALTER TABLE host_resource_changesets ADD COLUMN applied_at TEXT"},
			{"canceled_at", "ALTER TABLE host_resource_changesets ADD COLUMN canceled_at TEXT"},
			{"restored_at", "ALTER TABLE host_resource_changesets ADD COLUMN restored_at TEXT"},
		},
		"host_resource_change_ops": {
			{"id", "ALTER TABLE host_resource_change_ops ADD COLUMN id TEXT NOT NULL DEFAULT ''"},
			{"changeset_id", "ALTER TABLE host_resource_change_ops ADD COLUMN changeset_id TEXT NOT NULL DEFAULT ''"},
			{"op_index", "ALTER TABLE host_resource_change_ops ADD COLUMN op_index INTEGER NOT NULL DEFAULT 0"},
			{"operation", "ALTER TABLE host_resource_change_ops ADD COLUMN operation TEXT NOT NULL DEFAULT ''"},
			{"source_display_path", "ALTER TABLE host_resource_change_ops ADD COLUMN source_display_path TEXT NOT NULL DEFAULT ''"},
			{"target_display_path", "ALTER TABLE host_resource_change_ops ADD COLUMN target_display_path TEXT NOT NULL DEFAULT ''"},
			{"source_hash", "ALTER TABLE host_resource_change_ops ADD COLUMN source_hash TEXT NOT NULL DEFAULT ''"},
			{"target_hash", "ALTER TABLE host_resource_change_ops ADD COLUMN target_hash TEXT NOT NULL DEFAULT ''"},
			{"bytes", "ALTER TABLE host_resource_change_ops ADD COLUMN bytes INTEGER NOT NULL DEFAULT 0"},
			{"metadata_json", "ALTER TABLE host_resource_change_ops ADD COLUMN metadata_json TEXT NOT NULL DEFAULT '{}'"},
			{"created_at", "ALTER TABLE host_resource_change_ops ADD COLUMN created_at TEXT NOT NULL DEFAULT ''"},
		},
	}
	if err := ensureTableColumns(db, repairs); err != nil {
		return fmt.Errorf("repair host resource changeset schema: %w", err)
	}
	if _, err := db.Exec(hostResourceChangeSetIndexSQL); err != nil {
		return fmt.Errorf("ensure host resource changeset indexes: %w", err)
	}
	return nil
}

func ensureAgentAffectSchema(db *sql.DB) error {
	profileColumns, err := tableColumns(db, "agent_affect_profiles")
	if err != nil {
		return fmt.Errorf("read agent_affect_profiles columns: %w", err)
	}
	if len(profileColumns) == 0 {
		for _, migration := range migrations {
			if migration.Version < 20 || migration.Version > 23 {
				continue
			}
			if _, err := db.Exec(migration.SQL); err != nil {
				return fmt.Errorf("bootstrap agent affect migration %d: %w", migration.Version, err)
			}
		}
	}
	repairs := map[string][]struct {
		name string
		sql  string
	}{
		"agent_affect_evaluations": {
			{"appraisal_json", "ALTER TABLE agent_affect_evaluations ADD COLUMN appraisal_json TEXT NOT NULL DEFAULT '{}'"},
			{"context_strategy", "ALTER TABLE agent_affect_evaluations ADD COLUMN context_strategy TEXT NOT NULL DEFAULT ''"},
			{"prompt_chars", "ALTER TABLE agent_affect_evaluations ADD COLUMN prompt_chars INTEGER NOT NULL DEFAULT 0"},
			{"estimated_input_tokens", "ALTER TABLE agent_affect_evaluations ADD COLUMN estimated_input_tokens INTEGER NOT NULL DEFAULT 0"},
			{"actual_input_tokens", "ALTER TABLE agent_affect_evaluations ADD COLUMN actual_input_tokens INTEGER NOT NULL DEFAULT 0"},
			{"actual_output_tokens", "ALTER TABLE agent_affect_evaluations ADD COLUMN actual_output_tokens INTEGER NOT NULL DEFAULT 0"},
			{"prompt_truncated", "ALTER TABLE agent_affect_evaluations ADD COLUMN prompt_truncated INTEGER NOT NULL DEFAULT 0"},
			{"budget_report_json", "ALTER TABLE agent_affect_evaluations ADD COLUMN budget_report_json TEXT NOT NULL DEFAULT '{}'"},
		},
		"agent_affect_events": {
			{"source_kind", "ALTER TABLE agent_affect_events ADD COLUMN source_kind TEXT NOT NULL DEFAULT ''"},
			{"source_ref_type", "ALTER TABLE agent_affect_events ADD COLUMN source_ref_type TEXT NOT NULL DEFAULT ''"},
			{"source_ref_id", "ALTER TABLE agent_affect_events ADD COLUMN source_ref_id TEXT NOT NULL DEFAULT ''"},
			{"source_ref_hash", "ALTER TABLE agent_affect_events ADD COLUMN source_ref_hash TEXT NOT NULL DEFAULT ''"},
			{"cause_code", "ALTER TABLE agent_affect_events ADD COLUMN cause_code TEXT NOT NULL DEFAULT ''"},
			{"affect_tags_json", "ALTER TABLE agent_affect_events ADD COLUMN affect_tags_json TEXT NOT NULL DEFAULT '[]'"},
		},
	}
	if err := ensureTableColumns(db, repairs); err != nil {
		return fmt.Errorf("repair agent affect schema: %w", err)
	}
	if _, err := db.Exec(`
CREATE INDEX IF NOT EXISTS idx_agent_affect_events_owner_significance_time
    ON agent_affect_events(persona_id, mood_owner_scope, mood_owner_id, significance DESC, created_at DESC);
`); err != nil {
		return fmt.Errorf("ensure agent affect indexes: %w", err)
	}
	return nil
}

func ensureResourceGrantSchema(db *sql.DB) error {
	if _, err := db.Exec(resourceGrantSchemaSQL); err != nil {
		return fmt.Errorf("ensure resource grant schema: %w", err)
	}
	repairs := map[string][]struct {
		name string
		sql  string
	}{
		"resource_grants": {
			{"id", "ALTER TABLE resource_grants ADD COLUMN id TEXT NOT NULL DEFAULT ''"},
			{"principal_kind", "ALTER TABLE resource_grants ADD COLUMN principal_kind TEXT NOT NULL DEFAULT ''"},
			{"principal_id", "ALTER TABLE resource_grants ADD COLUMN principal_id TEXT NOT NULL DEFAULT ''"},
			{"capability", "ALTER TABLE resource_grants ADD COLUMN capability TEXT NOT NULL DEFAULT ''"},
			{"resource_json", "ALTER TABLE resource_grants ADD COLUMN resource_json TEXT NOT NULL DEFAULT '{}'"},
			{"operations_json", "ALTER TABLE resource_grants ADD COLUMN operations_json TEXT NOT NULL DEFAULT '[]'"},
			{"constraints_json", "ALTER TABLE resource_grants ADD COLUMN constraints_json TEXT NOT NULL DEFAULT '{}'"},
			{"lifetime", "ALTER TABLE resource_grants ADD COLUMN lifetime TEXT NOT NULL DEFAULT 'once'"},
			{"status", "ALTER TABLE resource_grants ADD COLUMN status TEXT NOT NULL DEFAULT 'pending'"},
			{"approval_request_id", "ALTER TABLE resource_grants ADD COLUMN approval_request_id TEXT NOT NULL DEFAULT ''"},
			{"binding_hash", "ALTER TABLE resource_grants ADD COLUMN binding_hash TEXT NOT NULL DEFAULT ''"},
			{"issued_by", "ALTER TABLE resource_grants ADD COLUMN issued_by TEXT NOT NULL DEFAULT 'policy'"},
			{"created_at", "ALTER TABLE resource_grants ADD COLUMN created_at TEXT NOT NULL DEFAULT ''"},
			{"expires_at", "ALTER TABLE resource_grants ADD COLUMN expires_at TEXT"},
			{"consumed_at", "ALTER TABLE resource_grants ADD COLUMN consumed_at TEXT"},
			{"revoked_at", "ALTER TABLE resource_grants ADD COLUMN revoked_at TEXT"},
			{"updated_at", "ALTER TABLE resource_grants ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''"},
		},
		"resource_grant_events": {
			{"id", "ALTER TABLE resource_grant_events ADD COLUMN id TEXT NOT NULL DEFAULT ''"},
			{"grant_id", "ALTER TABLE resource_grant_events ADD COLUMN grant_id TEXT NOT NULL DEFAULT ''"},
			{"event_type", "ALTER TABLE resource_grant_events ADD COLUMN event_type TEXT NOT NULL DEFAULT ''"},
			{"principal_kind", "ALTER TABLE resource_grant_events ADD COLUMN principal_kind TEXT NOT NULL DEFAULT ''"},
			{"principal_id", "ALTER TABLE resource_grant_events ADD COLUMN principal_id TEXT NOT NULL DEFAULT ''"},
			{"summary_hash", "ALTER TABLE resource_grant_events ADD COLUMN summary_hash TEXT NOT NULL DEFAULT ''"},
			{"provenance_json", "ALTER TABLE resource_grant_events ADD COLUMN provenance_json TEXT NOT NULL DEFAULT '{}'"},
			{"created_at", "ALTER TABLE resource_grant_events ADD COLUMN created_at TEXT NOT NULL DEFAULT ''"},
		},
	}
	if err := ensureTableColumns(db, repairs); err != nil {
		return fmt.Errorf("repair resource grant schema: %w", err)
	}
	if _, err := db.Exec(resourceGrantIndexSQL); err != nil {
		return fmt.Errorf("ensure resource grant indexes: %w", err)
	}
	return nil
}

func ensureTableColumns(db *sql.DB, repairs map[string][]struct {
	name string
	sql  string
}) error {
	for table, columns := range repairs {
		existing, err := tableColumns(db, table)
		if err != nil {
			return fmt.Errorf("read %s columns: %w", table, err)
		}
		for _, column := range columns {
			if existing[column.name] {
				continue
			}
			if _, err := db.Exec(column.sql); err != nil {
				return fmt.Errorf("add %s.%s: %w", table, column.name, err)
			}
		}
	}
	return nil
}

func ensurePromptCenterSchema(db *sql.DB) error {
	if _, err := db.Exec(promptCenterSchemaSQL); err != nil {
		return fmt.Errorf("ensure prompt center schema: %w", err)
	}
	return nil
}

func ensurePluginRuntimeSchema(db *sql.DB) error {
	if _, err := db.Exec(pluginRuntimeSchemaSQL); err != nil {
		return fmt.Errorf("ensure plugin runtime schema: %w", err)
	}
	repairs := map[string][]struct {
		name string
		sql  string
	}{
		"plugin_installations": {
			{"id", "ALTER TABLE plugin_installations ADD COLUMN id TEXT NOT NULL DEFAULT ''"},
			{"plugin_id", "ALTER TABLE plugin_installations ADD COLUMN plugin_id TEXT NOT NULL DEFAULT ''"},
			{"version", "ALTER TABLE plugin_installations ADD COLUMN version TEXT NOT NULL DEFAULT ''"},
			{"name", "ALTER TABLE plugin_installations ADD COLUMN name TEXT NOT NULL DEFAULT ''"},
			{"manifest_json", "ALTER TABLE plugin_installations ADD COLUMN manifest_json TEXT NOT NULL DEFAULT '{}'"},
			{"source_type", "ALTER TABLE plugin_installations ADD COLUMN source_type TEXT NOT NULL DEFAULT ''"},
			{"source_ref", "ALTER TABLE plugin_installations ADD COLUMN source_ref TEXT NOT NULL DEFAULT ''"},
			{"package_digest", "ALTER TABLE plugin_installations ADD COLUMN package_digest TEXT NOT NULL DEFAULT ''"},
			{"manifest_digest", "ALTER TABLE plugin_installations ADD COLUMN manifest_digest TEXT NOT NULL DEFAULT ''"},
			{"signature_status", "ALTER TABLE plugin_installations ADD COLUMN signature_status TEXT NOT NULL DEFAULT ''"},
			{"publisher_id", "ALTER TABLE plugin_installations ADD COLUMN publisher_id TEXT NOT NULL DEFAULT ''"},
			{"installed_at", "ALTER TABLE plugin_installations ADD COLUMN installed_at TEXT NOT NULL DEFAULT ''"},
			{"installed_by", "ALTER TABLE plugin_installations ADD COLUMN installed_by TEXT NOT NULL DEFAULT 'local'"},
			{"store_path", "ALTER TABLE plugin_installations ADD COLUMN store_path TEXT NOT NULL DEFAULT ''"},
		},
		"plugin_enabled_state": {
			{"plugin_id", "ALTER TABLE plugin_enabled_state ADD COLUMN plugin_id TEXT NOT NULL DEFAULT ''"},
			{"version", "ALTER TABLE plugin_enabled_state ADD COLUMN version TEXT NOT NULL DEFAULT ''"},
			{"enabled", "ALTER TABLE plugin_enabled_state ADD COLUMN enabled INTEGER NOT NULL DEFAULT 0"},
			{"user_grant_json", "ALTER TABLE plugin_enabled_state ADD COLUMN user_grant_json TEXT NOT NULL DEFAULT '{}'"},
			{"trust_level", "ALTER TABLE plugin_enabled_state ADD COLUMN trust_level TEXT NOT NULL DEFAULT ''"},
			{"trust_accepted_at", "ALTER TABLE plugin_enabled_state ADD COLUMN trust_accepted_at TEXT NOT NULL DEFAULT ''"},
			{"trust_ack_hash", "ALTER TABLE plugin_enabled_state ADD COLUMN trust_ack_hash TEXT NOT NULL DEFAULT ''"},
			{"trust_review_reasons_json", "ALTER TABLE plugin_enabled_state ADD COLUMN trust_review_reasons_json TEXT NOT NULL DEFAULT '[]'"},
			{"default_tool_exposure", "ALTER TABLE plugin_enabled_state ADD COLUMN default_tool_exposure TEXT NOT NULL DEFAULT ''"},
			{"default_invocation_policy", "ALTER TABLE plugin_enabled_state ADD COLUMN default_invocation_policy TEXT NOT NULL DEFAULT ''"},
			{"updated_at", "ALTER TABLE plugin_enabled_state ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''"},
		},
		"plugin_runtime_records": {
			{"plugin_id", "ALTER TABLE plugin_runtime_records ADD COLUMN plugin_id TEXT NOT NULL DEFAULT ''"},
			{"version", "ALTER TABLE plugin_runtime_records ADD COLUMN version TEXT NOT NULL DEFAULT ''"},
			{"runtime_kind", "ALTER TABLE plugin_runtime_records ADD COLUMN runtime_kind TEXT NOT NULL DEFAULT ''"},
			{"status", "ALTER TABLE plugin_runtime_records ADD COLUMN status TEXT NOT NULL DEFAULT 'stopped'"},
			{"pid", "ALTER TABLE plugin_runtime_records ADD COLUMN pid INTEGER"},
			{"last_started_at", "ALTER TABLE plugin_runtime_records ADD COLUMN last_started_at TEXT"},
			{"last_stopped_at", "ALTER TABLE plugin_runtime_records ADD COLUMN last_stopped_at TEXT"},
			{"last_error", "ALTER TABLE plugin_runtime_records ADD COLUMN last_error TEXT NOT NULL DEFAULT ''"},
			{"restart_count", "ALTER TABLE plugin_runtime_records ADD COLUMN restart_count INTEGER NOT NULL DEFAULT 0"},
			{"updated_at", "ALTER TABLE plugin_runtime_records ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''"},
		},
		"plugin_access_events": {
			{"id", "ALTER TABLE plugin_access_events ADD COLUMN id TEXT NOT NULL DEFAULT ''"},
			{"plugin_id", "ALTER TABLE plugin_access_events ADD COLUMN plugin_id TEXT NOT NULL DEFAULT ''"},
			{"access_kind", "ALTER TABLE plugin_access_events ADD COLUMN access_kind TEXT NOT NULL DEFAULT ''"},
			{"capability", "ALTER TABLE plugin_access_events ADD COLUMN capability TEXT NOT NULL DEFAULT ''"},
			{"status", "ALTER TABLE plugin_access_events ADD COLUMN status TEXT NOT NULL DEFAULT ''"},
			{"request_summary", "ALTER TABLE plugin_access_events ADD COLUMN request_summary TEXT NOT NULL DEFAULT ''"},
			{"input_hash", "ALTER TABLE plugin_access_events ADD COLUMN input_hash TEXT NOT NULL DEFAULT ''"},
			{"output_hash", "ALTER TABLE plugin_access_events ADD COLUMN output_hash TEXT NOT NULL DEFAULT ''"},
			{"duration_ms", "ALTER TABLE plugin_access_events ADD COLUMN duration_ms INTEGER NOT NULL DEFAULT 0"},
			{"created_at", "ALTER TABLE plugin_access_events ADD COLUMN created_at TEXT NOT NULL DEFAULT ''"},
		},
		"plugin_trust_acceptance_history": {
			{"id", "ALTER TABLE plugin_trust_acceptance_history ADD COLUMN id TEXT NOT NULL DEFAULT ''"},
			{"plugin_id", "ALTER TABLE plugin_trust_acceptance_history ADD COLUMN plugin_id TEXT NOT NULL DEFAULT ''"},
			{"version", "ALTER TABLE plugin_trust_acceptance_history ADD COLUMN version TEXT NOT NULL DEFAULT ''"},
			{"trust_level", "ALTER TABLE plugin_trust_acceptance_history ADD COLUMN trust_level TEXT NOT NULL DEFAULT ''"},
			{"accepted_at", "ALTER TABLE plugin_trust_acceptance_history ADD COLUMN accepted_at TEXT NOT NULL DEFAULT ''"},
			{"trust_ack_hash", "ALTER TABLE plugin_trust_acceptance_history ADD COLUMN trust_ack_hash TEXT NOT NULL DEFAULT ''"},
			{"trust_review_reasons_json", "ALTER TABLE plugin_trust_acceptance_history ADD COLUMN trust_review_reasons_json TEXT NOT NULL DEFAULT '[]'"},
			{"default_tool_exposure", "ALTER TABLE plugin_trust_acceptance_history ADD COLUMN default_tool_exposure TEXT NOT NULL DEFAULT ''"},
			{"default_invocation_policy", "ALTER TABLE plugin_trust_acceptance_history ADD COLUMN default_invocation_policy TEXT NOT NULL DEFAULT ''"},
			{"user_grant_hash", "ALTER TABLE plugin_trust_acceptance_history ADD COLUMN user_grant_hash TEXT NOT NULL DEFAULT ''"},
			{"host_policy_fingerprint", "ALTER TABLE plugin_trust_acceptance_history ADD COLUMN host_policy_fingerprint TEXT NOT NULL DEFAULT ''"},
			{"dependency_lock_digest", "ALTER TABLE plugin_trust_acceptance_history ADD COLUMN dependency_lock_digest TEXT NOT NULL DEFAULT ''"},
			{"package_digest", "ALTER TABLE plugin_trust_acceptance_history ADD COLUMN package_digest TEXT NOT NULL DEFAULT ''"},
			{"manifest_digest", "ALTER TABLE plugin_trust_acceptance_history ADD COLUMN manifest_digest TEXT NOT NULL DEFAULT ''"},
			{"signature_status", "ALTER TABLE plugin_trust_acceptance_history ADD COLUMN signature_status TEXT NOT NULL DEFAULT ''"},
			{"publisher_id", "ALTER TABLE plugin_trust_acceptance_history ADD COLUMN publisher_id TEXT NOT NULL DEFAULT ''"},
			{"source_type", "ALTER TABLE plugin_trust_acceptance_history ADD COLUMN source_type TEXT NOT NULL DEFAULT ''"},
			{"created_at", "ALTER TABLE plugin_trust_acceptance_history ADD COLUMN created_at TEXT NOT NULL DEFAULT ''"},
		},
		"plugin_provider_usage": {
			{"id", "ALTER TABLE plugin_provider_usage ADD COLUMN id TEXT NOT NULL DEFAULT ''"},
			{"plugin_id", "ALTER TABLE plugin_provider_usage ADD COLUMN plugin_id TEXT NOT NULL DEFAULT ''"},
			{"provider_id", "ALTER TABLE plugin_provider_usage ADD COLUMN provider_id TEXT NOT NULL DEFAULT ''"},
			{"model", "ALTER TABLE plugin_provider_usage ADD COLUMN model TEXT NOT NULL DEFAULT ''"},
			{"purpose", "ALTER TABLE plugin_provider_usage ADD COLUMN purpose TEXT NOT NULL DEFAULT ''"},
			{"input_tokens", "ALTER TABLE plugin_provider_usage ADD COLUMN input_tokens INTEGER NOT NULL DEFAULT 0"},
			{"output_tokens", "ALTER TABLE plugin_provider_usage ADD COLUMN output_tokens INTEGER NOT NULL DEFAULT 0"},
			{"estimated_tokens", "ALTER TABLE plugin_provider_usage ADD COLUMN estimated_tokens INTEGER NOT NULL DEFAULT 0"},
			{"status", "ALTER TABLE plugin_provider_usage ADD COLUMN status TEXT NOT NULL DEFAULT ''"},
			{"error_message", "ALTER TABLE plugin_provider_usage ADD COLUMN error_message TEXT NOT NULL DEFAULT ''"},
			{"duration_ms", "ALTER TABLE plugin_provider_usage ADD COLUMN duration_ms INTEGER NOT NULL DEFAULT 0"},
			{"created_at", "ALTER TABLE plugin_provider_usage ADD COLUMN created_at TEXT NOT NULL DEFAULT ''"},
		},
		"plugin_kv": {
			{"plugin_id", "ALTER TABLE plugin_kv ADD COLUMN plugin_id TEXT NOT NULL DEFAULT ''"},
			{"key", "ALTER TABLE plugin_kv ADD COLUMN key TEXT NOT NULL DEFAULT ''"},
			{"value_json", "ALTER TABLE plugin_kv ADD COLUMN value_json TEXT NOT NULL DEFAULT '{}'"},
			{"updated_at", "ALTER TABLE plugin_kv ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''"},
		},
	}
	for table, columns := range repairs {
		existing, err := tableColumns(db, table)
		if err != nil {
			return fmt.Errorf("read %s columns: %w", table, err)
		}
		for _, column := range columns {
			if existing[column.name] {
				continue
			}
			if _, err := db.Exec(column.sql); err != nil {
				return fmt.Errorf("add %s.%s: %w", table, column.name, err)
			}
		}
	}
	if _, err := db.Exec(pluginRuntimeIndexSQL); err != nil {
		return fmt.Errorf("ensure plugin runtime indexes: %w", err)
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
    changeset_id          TEXT NOT NULL DEFAULT '',
    plan_hash             TEXT NOT NULL DEFAULT '',
    resource_id           TEXT NOT NULL DEFAULT '',
    canonical_path_hash   TEXT NOT NULL DEFAULT '',
    baseline_hash         TEXT NOT NULL DEFAULT '',
    baseline_file_id      TEXT NOT NULL DEFAULT '',
    delete_mode           TEXT NOT NULL DEFAULT '',
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
		{"changeset_id", "ALTER TABLE approval_requests ADD COLUMN changeset_id TEXT NOT NULL DEFAULT ''"},
		{"plan_hash", "ALTER TABLE approval_requests ADD COLUMN plan_hash TEXT NOT NULL DEFAULT ''"},
		{"resource_id", "ALTER TABLE approval_requests ADD COLUMN resource_id TEXT NOT NULL DEFAULT ''"},
		{"canonical_path_hash", "ALTER TABLE approval_requests ADD COLUMN canonical_path_hash TEXT NOT NULL DEFAULT ''"},
		{"baseline_hash", "ALTER TABLE approval_requests ADD COLUMN baseline_hash TEXT NOT NULL DEFAULT ''"},
		{"baseline_file_id", "ALTER TABLE approval_requests ADD COLUMN baseline_file_id TEXT NOT NULL DEFAULT ''"},
		{"delete_mode", "ALTER TABLE approval_requests ADD COLUMN delete_mode TEXT NOT NULL DEFAULT ''"},
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
CREATE INDEX IF NOT EXISTS idx_approval_requests_changeset_binding
    ON approval_requests(session_id, task_id, changeset_id, plan_hash);
`); err != nil {
		return fmt.Errorf("ensure approval_requests indexes: %w", err)
	}
	return nil
}

func ensurePendingDirectToolCallsSchema(db *sql.DB) error {
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS pending_direct_tool_calls (
    approval_request_id   TEXT PRIMARY KEY,
    session_id            TEXT NOT NULL,
    turn_id               TEXT NOT NULL DEFAULT '',
    task_id               TEXT NOT NULL,
    call_id               TEXT NOT NULL,
    tool_name             TEXT NOT NULL,
    input_json            TEXT NOT NULL,
    max_permission        TEXT NOT NULL DEFAULT 'read-only',
    provider              TEXT NOT NULL DEFAULT '',
    approval_kind         TEXT NOT NULL DEFAULT '',
    normalized_input_hash TEXT NOT NULL DEFAULT '',
    path_digest           TEXT NOT NULL DEFAULT '',
    input_preview         TEXT NOT NULL DEFAULT '',
    status                TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','claimed','consumed','rejected','expired','failed')),
    claim_id              TEXT NOT NULL DEFAULT '',
    claimed_at            TEXT,
    consumed_at           TEXT,
    created_at            TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at            TEXT NOT NULL,
    error_message         TEXT NOT NULL DEFAULT '',
    FOREIGN KEY(approval_request_id) REFERENCES approval_requests(id) ON DELETE CASCADE,
    UNIQUE(session_id, task_id)
);
`); err != nil {
		return fmt.Errorf("ensure pending_direct_tool_calls table: %w", err)
	}

	repairs := map[string][]struct {
		name string
		sql  string
	}{
		"pending_direct_tool_calls": {
			{"approval_request_id", "ALTER TABLE pending_direct_tool_calls ADD COLUMN approval_request_id TEXT NOT NULL DEFAULT ''"},
			{"session_id", "ALTER TABLE pending_direct_tool_calls ADD COLUMN session_id TEXT NOT NULL DEFAULT ''"},
			{"turn_id", "ALTER TABLE pending_direct_tool_calls ADD COLUMN turn_id TEXT NOT NULL DEFAULT ''"},
			{"task_id", "ALTER TABLE pending_direct_tool_calls ADD COLUMN task_id TEXT NOT NULL DEFAULT ''"},
			{"call_id", "ALTER TABLE pending_direct_tool_calls ADD COLUMN call_id TEXT NOT NULL DEFAULT ''"},
			{"tool_name", "ALTER TABLE pending_direct_tool_calls ADD COLUMN tool_name TEXT NOT NULL DEFAULT ''"},
			{"input_json", "ALTER TABLE pending_direct_tool_calls ADD COLUMN input_json TEXT NOT NULL DEFAULT ''"},
			{"max_permission", "ALTER TABLE pending_direct_tool_calls ADD COLUMN max_permission TEXT NOT NULL DEFAULT 'read-only'"},
			{"provider", "ALTER TABLE pending_direct_tool_calls ADD COLUMN provider TEXT NOT NULL DEFAULT ''"},
			{"approval_kind", "ALTER TABLE pending_direct_tool_calls ADD COLUMN approval_kind TEXT NOT NULL DEFAULT ''"},
			{"normalized_input_hash", "ALTER TABLE pending_direct_tool_calls ADD COLUMN normalized_input_hash TEXT NOT NULL DEFAULT ''"},
			{"path_digest", "ALTER TABLE pending_direct_tool_calls ADD COLUMN path_digest TEXT NOT NULL DEFAULT ''"},
			{"input_preview", "ALTER TABLE pending_direct_tool_calls ADD COLUMN input_preview TEXT NOT NULL DEFAULT ''"},
			{"status", "ALTER TABLE pending_direct_tool_calls ADD COLUMN status TEXT NOT NULL DEFAULT 'pending'"},
			{"claim_id", "ALTER TABLE pending_direct_tool_calls ADD COLUMN claim_id TEXT NOT NULL DEFAULT ''"},
			{"claimed_at", "ALTER TABLE pending_direct_tool_calls ADD COLUMN claimed_at TEXT"},
			{"consumed_at", "ALTER TABLE pending_direct_tool_calls ADD COLUMN consumed_at TEXT"},
			{"created_at", "ALTER TABLE pending_direct_tool_calls ADD COLUMN created_at TEXT NOT NULL DEFAULT ''"},
			{"expires_at", "ALTER TABLE pending_direct_tool_calls ADD COLUMN expires_at TEXT NOT NULL DEFAULT ''"},
			{"error_message", "ALTER TABLE pending_direct_tool_calls ADD COLUMN error_message TEXT NOT NULL DEFAULT ''"},
		},
	}
	if err := ensureTableColumns(db, repairs); err != nil {
		return fmt.Errorf("repair pending_direct_tool_calls schema: %w", err)
	}

	if _, err := db.Exec(`
CREATE INDEX IF NOT EXISTS idx_pending_direct_tool_calls_session_status
    ON pending_direct_tool_calls(session_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_pending_direct_tool_calls_task
    ON pending_direct_tool_calls(session_id, task_id);
CREATE INDEX IF NOT EXISTS idx_pending_direct_tool_calls_expires
    ON pending_direct_tool_calls(expires_at)
    WHERE status IN ('pending','claimed');
CREATE INDEX IF NOT EXISTS idx_pending_direct_tool_calls_tool
    ON pending_direct_tool_calls(tool_name, created_at DESC);
`); err != nil {
		return fmt.Errorf("ensure pending_direct_tool_calls indexes: %w", err)
	}
	return nil
}

func ensureNoImageBase64Guards(db *sql.DB) error {
	guards := map[string][]string{
		"messages": {
			"content",
			"metadata",
		},
		"message_parts": {
			"text_content",
		},
		"message_media_deliveries": {
			"error_message",
		},
		"media_assets": {
			"storage_uri",
			"thumbnail_uri",
		},
		"memory_segments": {
			"summary",
			"last_extraction_error_message",
		},
		"memory_extraction_jobs": {
			"request_json",
			"result_json",
			"mirror_sync_result_json",
			"error_message",
		},
		"agent_affect_evaluations": {
			"input_text",
			"input_summary",
			"context_window_snapshot_json",
			"prompt_snapshot",
			"response_json",
		},
		"agent_affect_jobs": {
			"trigger_json",
			"user_text",
			"assistant_text",
			"input_summary",
			"memory_prompt_block",
			"error_message",
		},
		"agent_affect_job_batches": {
			"batch_input_summary",
			"context_window_snapshot_json",
			"error_message",
		},
		"agent_affect_plugin_writes": {
			"request_json",
		},
	}
	for table, columns := range guards {
		existing, err := tableColumns(db, table)
		if err != nil {
			return fmt.Errorf("read %s columns: %w", table, err)
		}
		available := make([]string, 0, len(columns))
		for _, column := range columns {
			if existing[column] {
				available = append(available, column)
			}
		}
		if len(available) == 0 {
			continue
		}
		condition := noImageBase64GuardCondition(available)
		for _, operation := range []string{"INSERT", "UPDATE"} {
			triggerName := fmt.Sprintf("trg_no_image_base64_%s_%s", table, strings.ToLower(operation))
			if _, err := db.Exec(fmt.Sprintf("DROP TRIGGER IF EXISTS %s", quoteSQLiteIdent(triggerName))); err != nil {
				return fmt.Errorf("drop %s trigger on %s: %w", strings.ToLower(operation), table, err)
			}
			sql := fmt.Sprintf(`
CREATE TRIGGER %s
BEFORE %s ON %s
WHEN %s
BEGIN
    SELECT RAISE(ABORT, 'image base64 is not allowed in %s');
END;
`, quoteSQLiteIdent(triggerName), operation, quoteSQLiteIdent(table), condition, table)
			if _, err := db.Exec(sql); err != nil {
				return fmt.Errorf("create %s trigger on %s: %w", strings.ToLower(operation), table, err)
			}
		}
	}
	return nil
}

func noImageBase64GuardCondition(columns []string) string {
	parts := make([]string, 0, len(columns))
	for _, column := range columns {
		value := "LOWER(COALESCE(NEW." + quoteSQLiteIdent(column) + ", ''))"
		parts = append(parts, fmt.Sprintf(
			"((INSTR(%[1]s, 'data:image/') > 0 AND INSTR(%[1]s, ';base64,') > 0) OR INSTR(%[1]s, 'ivborw0kggo') > 0 OR INSTR(%[1]s, '/9j/') > 0)",
			value,
		))
	}
	return strings.Join(parts, " OR ")
}

func quoteSQLiteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
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
