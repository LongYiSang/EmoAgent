package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type ConversationOriginRecord struct {
	ID                     string
	OriginKey              string
	SourceType             string
	AdapterInstanceID      string
	PlatformID             string
	ChannelType            string
	ExternalConversationID string
	ExternalActorID        string
	DisplayName            string
	MetadataJSON           string
	CreatedAt              string
	UpdatedAt              string
}

type ConversationBindingRecord struct {
	ID                string
	OriginKey         string
	PersonaKey        string
	CurrentSessionID  string
	DefaultPersonaKey string
	UniqueScope       string
	VariablesJSON     string
	CreatedAt         string
	UpdatedAt         string
}

type SessionClearMarkerRecord struct {
	ID             string
	OriginKey      string
	SessionID      string
	PersonaKey     string
	AfterMessageID string
	ClearedAt      string
	Reason         string
	MetadataJSON   string
}

type ConversationEventRecord struct {
	ID               string
	OriginKey        string
	SessionID        string
	PersonaKey       string
	EventType        string
	VisibleContent   string
	PayloadJSON      string
	CreatedAt        string
	VisibilityStatus string
}

type CommandConfigRecord struct {
	CommandID     string
	ProviderKind  string
	PluginID      string
	OriginalName  string
	EffectiveName string
	AliasesJSON   string
	Enabled       bool
	Permission    string
	OutputMode    string
	ConfigJSON    string
	UpdatedAt     string
}

type CommandInvocationRecord struct {
	ID           string
	CommandID    string
	CommandName  string
	ProviderKind string
	PluginID     string
	OriginKey    string
	SourceType   string
	SessionID    string
	PersonaKey   string
	ActorID      string
	ActorRole    string
	InputHash    string
	ArgvJSON     string
	FlagsJSON    string
	OutputMode   string
	Status       string
	ResultText   string
	PayloadJSON  string
	ErrorKind    string
	DurationMS   int
	CreatedAt    string
}

type CommandInvocationFilter struct {
	SessionID string
	OriginKey string
	CommandID string
	Limit     int
}

func (d *DB) UpsertConversationOrigin(ctx context.Context, origin ConversationOriginRecord) error {
	origin = normalizeConversationOrigin(origin)
	if origin.ID == "" {
		return errors.New("origin id is required")
	}
	if origin.OriginKey == "" {
		return errors.New("origin key is required")
	}
	if err := validateJSONObject(origin.MetadataJSON, "metadata_json"); err != nil {
		return err
	}
	now := d.nowText()
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO conversation_origins (
			id, origin_key, source_type, adapter_instance_id, platform_id, channel_type,
			external_conversation_id, external_actor_id, display_name, metadata_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(origin_key) DO UPDATE SET
			id = excluded.id,
			source_type = excluded.source_type,
			adapter_instance_id = excluded.adapter_instance_id,
			platform_id = excluded.platform_id,
			channel_type = excluded.channel_type,
			external_conversation_id = excluded.external_conversation_id,
			external_actor_id = excluded.external_actor_id,
			display_name = excluded.display_name,
			metadata_json = excluded.metadata_json,
			updated_at = excluded.updated_at
	`, origin.ID, origin.OriginKey, origin.SourceType, origin.AdapterInstanceID, origin.PlatformID, origin.ChannelType,
		origin.ExternalConversationID, origin.ExternalActorID, origin.DisplayName, origin.MetadataJSON, now, now)
	return err
}

func (d *DB) GetConversationOrigin(ctx context.Context, originKey string) (*ConversationOriginRecord, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT id, origin_key, source_type, adapter_instance_id, platform_id, channel_type,
		       external_conversation_id, external_actor_id, display_name, metadata_json, created_at, updated_at
		FROM conversation_origins
		WHERE origin_key = ?
	`, originKey)
	record, err := scanConversationOrigin(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (d *DB) UpsertConversationBinding(ctx context.Context, binding ConversationBindingRecord) error {
	binding = normalizeConversationBinding(binding)
	if binding.ID == "" {
		return errors.New("binding id is required")
	}
	if binding.OriginKey == "" {
		return errors.New("origin key is required")
	}
	if binding.PersonaKey == "" {
		return errors.New("persona key is required")
	}
	if binding.CurrentSessionID == "" {
		return errors.New("current session id is required")
	}
	if err := validateJSONObject(binding.VariablesJSON, "variables_json"); err != nil {
		return err
	}
	now := d.nowText()
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO conversation_bindings (
			id, origin_key, persona_key, current_session_id, default_persona_key,
			unique_scope, variables_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(origin_key, persona_key) DO UPDATE SET
			id = excluded.id,
			current_session_id = excluded.current_session_id,
			default_persona_key = excluded.default_persona_key,
			unique_scope = excluded.unique_scope,
			variables_json = excluded.variables_json,
			updated_at = excluded.updated_at
	`, binding.ID, binding.OriginKey, binding.PersonaKey, binding.CurrentSessionID, binding.DefaultPersonaKey,
		binding.UniqueScope, binding.VariablesJSON, now, now)
	return err
}

func (d *DB) GetConversationBinding(ctx context.Context, originKey, personaKey string) (*ConversationBindingRecord, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT id, origin_key, persona_key, current_session_id, default_persona_key,
		       unique_scope, variables_json, created_at, updated_at
		FROM conversation_bindings
		WHERE origin_key = ? AND persona_key = ?
	`, originKey, personaKey)
	record, err := scanConversationBinding(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (d *DB) UpsertSessionClearMarker(ctx context.Context, marker SessionClearMarkerRecord) error {
	marker = normalizeSessionClearMarker(marker)
	if marker.ID == "" {
		return errors.New("clear marker id is required")
	}
	if marker.OriginKey == "" || marker.SessionID == "" {
		return errors.New("origin key and session id are required")
	}
	if err := validateJSONObject(marker.MetadataJSON, "metadata_json"); err != nil {
		return err
	}
	now := d.nowText()
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO session_clear_markers (
			id, origin_key, session_id, persona_key, after_message_id, cleared_at, reason, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(origin_key, session_id) DO UPDATE SET
			id = excluded.id,
			persona_key = excluded.persona_key,
			after_message_id = excluded.after_message_id,
			cleared_at = excluded.cleared_at,
			reason = excluded.reason,
			metadata_json = excluded.metadata_json
	`, marker.ID, marker.OriginKey, marker.SessionID, marker.PersonaKey, marker.AfterMessageID, now, marker.Reason, marker.MetadataJSON)
	return err
}

func (d *DB) GetSessionClearMarker(ctx context.Context, originKey, sessionID string) (*SessionClearMarkerRecord, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT id, origin_key, session_id, persona_key, after_message_id, cleared_at, reason, metadata_json
		FROM session_clear_markers
		WHERE origin_key = ? AND session_id = ?
	`, originKey, sessionID)
	record, err := scanSessionClearMarker(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (d *DB) AddConversationEvent(ctx context.Context, event ConversationEventRecord) error {
	event = normalizeConversationEvent(event)
	if event.ID == "" {
		return errors.New("conversation event id is required")
	}
	if event.OriginKey == "" || event.SessionID == "" {
		return errors.New("origin key and session id are required")
	}
	if event.EventType == "" {
		return errors.New("event type is required")
	}
	if err := validateJSONObject(event.PayloadJSON, "payload_json"); err != nil {
		return err
	}
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO conversation_events (
			id, origin_key, session_id, persona_key, event_type,
			visible_content, payload_json, created_at, visibility_status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, event.ID, event.OriginKey, event.SessionID, event.PersonaKey, event.EventType,
		event.VisibleContent, event.PayloadJSON, d.nowText(), event.VisibilityStatus)
	return err
}

func (d *DB) ListConversationEvents(ctx context.Context, sessionID, originKey string, limit int) ([]ConversationEventRecord, error) {
	if limit <= 0 {
		return []ConversationEventRecord{}, nil
	}
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, origin_key, session_id, persona_key, event_type,
		       visible_content, payload_json, created_at, visibility_status
		FROM conversation_events
		WHERE session_id = ? AND (? = '' OR origin_key = ?)
		ORDER BY created_at ASC, rowid ASC
		LIMIT ?
	`, sessionID, originKey, originKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []ConversationEventRecord
	for rows.Next() {
		record, err := scanConversationEvent(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (d *DB) UpsertCommandConfig(ctx context.Context, config CommandConfigRecord) error {
	config = normalizeCommandConfig(config)
	if config.CommandID == "" {
		return errors.New("command id is required")
	}
	if err := validateJSONArray(config.AliasesJSON, "aliases_json"); err != nil {
		return err
	}
	if err := validateJSONObject(config.ConfigJSON, "config_json"); err != nil {
		return err
	}
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO command_configs (
			command_id, provider_kind, plugin_id, original_name, effective_name,
			aliases_json, enabled, permission, output_mode, config_json, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(command_id) DO UPDATE SET
			provider_kind = excluded.provider_kind,
			plugin_id = excluded.plugin_id,
			original_name = excluded.original_name,
			effective_name = excluded.effective_name,
			aliases_json = excluded.aliases_json,
			enabled = excluded.enabled,
			permission = excluded.permission,
			output_mode = excluded.output_mode,
			config_json = excluded.config_json,
			updated_at = excluded.updated_at
	`, config.CommandID, config.ProviderKind, config.PluginID, config.OriginalName, config.EffectiveName,
		config.AliasesJSON, boolInt(config.Enabled), config.Permission, config.OutputMode, config.ConfigJSON, d.nowText())
	return err
}

func (d *DB) GetCommandConfig(ctx context.Context, commandID string) (*CommandConfigRecord, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT command_id, provider_kind, plugin_id, original_name, effective_name,
		       aliases_json, enabled, permission, output_mode, config_json, updated_at
		FROM command_configs
		WHERE command_id = ?
	`, commandID)
	record, err := scanCommandConfig(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (d *DB) ListCommandConfigs(ctx context.Context) ([]CommandConfigRecord, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT command_id, provider_kind, plugin_id, original_name, effective_name,
		       aliases_json, enabled, permission, output_mode, config_json, updated_at
		FROM command_configs
		ORDER BY provider_kind ASC, effective_name ASC, command_id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []CommandConfigRecord
	for rows.Next() {
		record, err := scanCommandConfig(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (d *DB) AddCommandInvocation(ctx context.Context, invocation CommandInvocationRecord) error {
	invocation = normalizeCommandInvocation(invocation)
	if invocation.ID == "" {
		return errors.New("command invocation id is required")
	}
	if invocation.CommandID == "" || invocation.CommandName == "" {
		return errors.New("command id and name are required")
	}
	if invocation.OriginKey == "" || invocation.SessionID == "" {
		return errors.New("origin key and session id are required")
	}
	if invocation.Status == "" {
		return errors.New("status is required")
	}
	if err := validateJSONArray(invocation.ArgvJSON, "argv_json"); err != nil {
		return err
	}
	if err := validateJSONObject(invocation.FlagsJSON, "flags_json"); err != nil {
		return err
	}
	if err := validateJSONObject(invocation.PayloadJSON, "payload_json"); err != nil {
		return err
	}
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO command_invocations (
			id, command_id, command_name, provider_kind, plugin_id, origin_key, source_type,
			session_id, persona_key, actor_id, actor_role, input_hash, argv_json, flags_json,
			output_mode, status, result_text, payload_json, error_kind, duration_ms, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, invocation.ID, invocation.CommandID, invocation.CommandName, invocation.ProviderKind, invocation.PluginID,
		invocation.OriginKey, invocation.SourceType, invocation.SessionID, invocation.PersonaKey, invocation.ActorID,
		invocation.ActorRole, invocation.InputHash, invocation.ArgvJSON, invocation.FlagsJSON, invocation.OutputMode,
		invocation.Status, invocation.ResultText, invocation.PayloadJSON, invocation.ErrorKind, invocation.DurationMS, d.nowText())
	return err
}

func (d *DB) ListCommandInvocations(ctx context.Context, filter CommandInvocationFilter) ([]CommandInvocationRecord, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	clauses := []string{"1 = 1"}
	args := []any{}
	if filter.SessionID != "" {
		clauses = append(clauses, "session_id = ?")
		args = append(args, filter.SessionID)
	}
	if filter.OriginKey != "" {
		clauses = append(clauses, "origin_key = ?")
		args = append(args, filter.OriginKey)
	}
	if filter.CommandID != "" {
		clauses = append(clauses, "command_id = ?")
		args = append(args, filter.CommandID)
	}
	args = append(args, limit)
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, command_id, command_name, provider_kind, plugin_id, origin_key, source_type,
		       session_id, persona_key, actor_id, actor_role, input_hash, argv_json, flags_json,
		       output_mode, status, result_text, payload_json, error_kind, duration_ms, created_at
		FROM command_invocations
		WHERE `+strings.Join(clauses, " AND ")+`
		ORDER BY created_at DESC, rowid DESC
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []CommandInvocationRecord
	for rows.Next() {
		record, err := scanCommandInvocation(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func normalizeConversationOrigin(origin ConversationOriginRecord) ConversationOriginRecord {
	origin.ID = firstNonEmptyConversationValue(origin.ID, uuid.NewString())
	origin.SourceType = firstNonEmptyConversationValue(origin.SourceType, "other")
	origin.ChannelType = firstNonEmptyConversationValue(origin.ChannelType, "other")
	origin.MetadataJSON = firstNonEmptyConversationValue(origin.MetadataJSON, "{}")
	return origin
}

func normalizeConversationBinding(binding ConversationBindingRecord) ConversationBindingRecord {
	binding.ID = firstNonEmptyConversationValue(binding.ID, uuid.NewString())
	binding.DefaultPersonaKey = firstNonEmptyConversationValue(binding.DefaultPersonaKey, binding.PersonaKey)
	binding.UniqueScope = firstNonEmptyConversationValue(binding.UniqueScope, "origin")
	binding.VariablesJSON = firstNonEmptyConversationValue(binding.VariablesJSON, "{}")
	return binding
}

func normalizeSessionClearMarker(marker SessionClearMarkerRecord) SessionClearMarkerRecord {
	marker.ID = firstNonEmptyConversationValue(marker.ID, uuid.NewString())
	marker.Reason = firstNonEmptyConversationValue(marker.Reason, "command_clear")
	marker.MetadataJSON = firstNonEmptyConversationValue(marker.MetadataJSON, "{}")
	return marker
}

func normalizeConversationEvent(event ConversationEventRecord) ConversationEventRecord {
	event.ID = firstNonEmptyConversationValue(event.ID, uuid.NewString())
	event.PayloadJSON = firstNonEmptyConversationValue(event.PayloadJSON, "{}")
	event.VisibilityStatus = firstNonEmptyConversationValue(event.VisibilityStatus, "visible")
	return event
}

func normalizeCommandConfig(config CommandConfigRecord) CommandConfigRecord {
	config.ProviderKind = firstNonEmptyConversationValue(config.ProviderKind, "builtin")
	config.EffectiveName = firstNonEmptyConversationValue(config.EffectiveName, config.OriginalName)
	config.AliasesJSON = firstNonEmptyConversationValue(config.AliasesJSON, "[]")
	config.Permission = firstNonEmptyConversationValue(config.Permission, "member")
	config.OutputMode = firstNonEmptyConversationValue(config.OutputMode, "direct")
	config.ConfigJSON = firstNonEmptyConversationValue(config.ConfigJSON, "{}")
	return config
}

func normalizeCommandInvocation(invocation CommandInvocationRecord) CommandInvocationRecord {
	invocation.ID = firstNonEmptyConversationValue(invocation.ID, uuid.NewString())
	invocation.ProviderKind = firstNonEmptyConversationValue(invocation.ProviderKind, "builtin")
	invocation.SourceType = firstNonEmptyConversationValue(invocation.SourceType, "webui")
	invocation.ActorRole = firstNonEmptyConversationValue(invocation.ActorRole, "member")
	invocation.ArgvJSON = firstNonEmptyConversationValue(invocation.ArgvJSON, "[]")
	invocation.FlagsJSON = firstNonEmptyConversationValue(invocation.FlagsJSON, "{}")
	invocation.OutputMode = firstNonEmptyConversationValue(invocation.OutputMode, "direct")
	invocation.PayloadJSON = firstNonEmptyConversationValue(invocation.PayloadJSON, "{}")
	return invocation
}

func scanConversationOrigin(row scanner) (ConversationOriginRecord, error) {
	var record ConversationOriginRecord
	err := row.Scan(&record.ID, &record.OriginKey, &record.SourceType, &record.AdapterInstanceID,
		&record.PlatformID, &record.ChannelType, &record.ExternalConversationID, &record.ExternalActorID,
		&record.DisplayName, &record.MetadataJSON, &record.CreatedAt, &record.UpdatedAt)
	return record, err
}

func scanConversationBinding(row scanner) (ConversationBindingRecord, error) {
	var record ConversationBindingRecord
	err := row.Scan(&record.ID, &record.OriginKey, &record.PersonaKey, &record.CurrentSessionID,
		&record.DefaultPersonaKey, &record.UniqueScope, &record.VariablesJSON, &record.CreatedAt, &record.UpdatedAt)
	return record, err
}

func scanSessionClearMarker(row scanner) (SessionClearMarkerRecord, error) {
	var record SessionClearMarkerRecord
	err := row.Scan(&record.ID, &record.OriginKey, &record.SessionID, &record.PersonaKey,
		&record.AfterMessageID, &record.ClearedAt, &record.Reason, &record.MetadataJSON)
	return record, err
}

func scanConversationEvent(row scanner) (ConversationEventRecord, error) {
	var record ConversationEventRecord
	err := row.Scan(&record.ID, &record.OriginKey, &record.SessionID, &record.PersonaKey,
		&record.EventType, &record.VisibleContent, &record.PayloadJSON, &record.CreatedAt, &record.VisibilityStatus)
	return record, err
}

func scanCommandConfig(row scanner) (CommandConfigRecord, error) {
	var record CommandConfigRecord
	var enabled int
	err := row.Scan(&record.CommandID, &record.ProviderKind, &record.PluginID, &record.OriginalName,
		&record.EffectiveName, &record.AliasesJSON, &enabled, &record.Permission, &record.OutputMode,
		&record.ConfigJSON, &record.UpdatedAt)
	record.Enabled = enabled != 0
	return record, err
}

func scanCommandInvocation(row scanner) (CommandInvocationRecord, error) {
	var record CommandInvocationRecord
	err := row.Scan(&record.ID, &record.CommandID, &record.CommandName, &record.ProviderKind,
		&record.PluginID, &record.OriginKey, &record.SourceType, &record.SessionID, &record.PersonaKey,
		&record.ActorID, &record.ActorRole, &record.InputHash, &record.ArgvJSON, &record.FlagsJSON,
		&record.OutputMode, &record.Status, &record.ResultText, &record.PayloadJSON, &record.ErrorKind,
		&record.DurationMS, &record.CreatedAt)
	return record, err
}

func validateJSONObject(raw string, field string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%s must be valid JSON object", field)
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return fmt.Errorf("%s must be valid JSON object: %w", field, err)
	}
	return nil
}

func validateJSONArray(raw string, field string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%s must be valid JSON array", field)
	}
	var value []any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return fmt.Errorf("%s must be valid JSON array: %w", field, err)
	}
	return nil
}

func firstNonEmptyConversationValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
