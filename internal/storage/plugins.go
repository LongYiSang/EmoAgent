package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type PluginInstallation struct {
	ID              string `json:"id"`
	PluginID        string `json:"plugin_id"`
	Version         string `json:"version"`
	Name            string `json:"name"`
	ManifestJSON    string `json:"manifest_json"`
	SourceType      string `json:"source_type"`
	SourceRef       string `json:"source_ref"`
	PackageDigest   string `json:"package_digest"`
	ManifestDigest  string `json:"manifest_digest"`
	SignatureStatus string `json:"signature_status"`
	PublisherID     string `json:"publisher_id"`
	InstalledAt     string `json:"installed_at"`
	InstalledBy     string `json:"installed_by"`
	StorePath       string `json:"store_path"`
}

type PluginEnabledState struct {
	PluginID      string `json:"plugin_id"`
	Version       string `json:"version"`
	Enabled       bool   `json:"enabled"`
	UserGrantJSON string `json:"user_grant_json"`
	UpdatedAt     string `json:"updated_at"`
}

type PluginRuntimeRecord struct {
	PluginID      string `json:"plugin_id"`
	Version       string `json:"version"`
	RuntimeKind   string `json:"runtime_kind"`
	Status        string `json:"status"`
	PID           *int   `json:"pid,omitempty"`
	LastStartedAt string `json:"last_started_at"`
	LastStoppedAt string `json:"last_stopped_at"`
	LastError     string `json:"last_error"`
	RestartCount  int    `json:"restart_count"`
	UpdatedAt     string `json:"updated_at"`
}

type PluginAccessEvent struct {
	ID             string `json:"id"`
	PluginID       string `json:"plugin_id"`
	AccessKind     string `json:"access_kind"`
	Capability     string `json:"capability"`
	Status         string `json:"status"`
	RequestSummary string `json:"request_summary"`
	InputHash      string `json:"input_hash"`
	OutputHash     string `json:"output_hash"`
	DurationMS     int64  `json:"duration_ms"`
	CreatedAt      string `json:"created_at"`
}

type PluginProviderUsage struct {
	ID              string `json:"id"`
	PluginID        string `json:"plugin_id"`
	ProviderID      string `json:"provider_id"`
	Model           string `json:"model"`
	Purpose         string `json:"purpose"`
	InputTokens     int    `json:"input_tokens"`
	OutputTokens    int    `json:"output_tokens"`
	EstimatedTokens int    `json:"estimated_tokens"`
	Status          string `json:"status"`
	ErrorMessage    string `json:"error_message"`
	DurationMS      int64  `json:"duration_ms"`
	CreatedAt       string `json:"created_at"`
}

func (d *DB) UpsertPluginInstallation(ctx context.Context, record PluginInstallation) error {
	if err := validatePluginInstallation(record); err != nil {
		return err
	}
	if record.ID == "" {
		record.ID = record.PluginID + "@" + record.Version
	}
	if record.InstalledBy == "" {
		record.InstalledBy = "local"
	}
	now := d.nowText()
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO plugin_installations (
			id, plugin_id, version, name, manifest_json, source_type, source_ref,
			package_digest, manifest_digest, signature_status, publisher_id,
			installed_at, installed_by, store_path
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(plugin_id, version) DO UPDATE SET
			name = excluded.name,
			manifest_json = excluded.manifest_json,
			source_type = excluded.source_type,
			source_ref = excluded.source_ref,
			package_digest = excluded.package_digest,
			manifest_digest = excluded.manifest_digest,
			signature_status = excluded.signature_status,
			publisher_id = excluded.publisher_id,
			installed_by = excluded.installed_by,
			store_path = excluded.store_path
	`, record.ID, record.PluginID, record.Version, record.Name, record.ManifestJSON, record.SourceType, record.SourceRef,
		record.PackageDigest, record.ManifestDigest, record.SignatureStatus, record.PublisherID, now, record.InstalledBy, record.StorePath)
	return err
}

func validatePluginInstallation(record PluginInstallation) error {
	if strings.TrimSpace(record.PluginID) == "" {
		return fmt.Errorf("plugin_id is required")
	}
	if strings.TrimSpace(record.Version) == "" {
		return fmt.Errorf("version is required")
	}
	if strings.TrimSpace(record.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !json.Valid([]byte(record.ManifestJSON)) {
		return fmt.Errorf("manifest_json must be valid JSON")
	}
	if strings.TrimSpace(record.SourceType) == "" {
		return fmt.Errorf("source_type is required")
	}
	if strings.TrimSpace(record.StorePath) == "" {
		return fmt.Errorf("store_path is required")
	}
	return nil
}

func (d *DB) GetPluginInstallation(ctx context.Context, pluginID string) (*PluginInstallation, error) {
	row := d.db.QueryRowContext(ctx, pluginInstallationSelectSQL()+`
		WHERE plugin_id = ?
		ORDER BY installed_at DESC
		LIMIT 1
	`, pluginID)
	record, err := scanPluginInstallation(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (d *DB) GetPluginInstallationVersion(ctx context.Context, pluginID, version string) (*PluginInstallation, error) {
	row := d.db.QueryRowContext(ctx, pluginInstallationSelectSQL()+`
		WHERE plugin_id = ? AND version = ?
	`, pluginID, version)
	record, err := scanPluginInstallation(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (d *DB) ListPluginInstallations(ctx context.Context) ([]PluginInstallation, error) {
	rows, err := d.db.QueryContext(ctx, pluginInstallationSelectSQL()+`
		ORDER BY plugin_id, installed_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []PluginInstallation
	for rows.Next() {
		record, err := scanPluginInstallation(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (d *DB) DeletePluginInstallation(ctx context.Context, pluginID string) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM plugin_installations WHERE plugin_id = ?`, pluginID)
	return err
}

func pluginInstallationSelectSQL() string {
	return `
		SELECT id, plugin_id, version, name, manifest_json, source_type, source_ref,
		       package_digest, manifest_digest, signature_status, publisher_id,
		       installed_at, installed_by, store_path
		FROM plugin_installations`
}

func scanPluginInstallation(row scanner) (PluginInstallation, error) {
	var record PluginInstallation
	err := row.Scan(
		&record.ID,
		&record.PluginID,
		&record.Version,
		&record.Name,
		&record.ManifestJSON,
		&record.SourceType,
		&record.SourceRef,
		&record.PackageDigest,
		&record.ManifestDigest,
		&record.SignatureStatus,
		&record.PublisherID,
		&record.InstalledAt,
		&record.InstalledBy,
		&record.StorePath,
	)
	return record, err
}

func (d *DB) SetPluginEnabled(ctx context.Context, pluginID, version string, enabled bool, grantJSON string) error {
	if strings.TrimSpace(pluginID) == "" {
		return fmt.Errorf("plugin_id is required")
	}
	if strings.TrimSpace(version) == "" {
		return fmt.Errorf("version is required")
	}
	if grantJSON == "" {
		grantJSON = "{}"
	}
	if !json.Valid([]byte(grantJSON)) {
		return fmt.Errorf("user_grant_json must be valid JSON")
	}
	now := d.nowText()
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO plugin_enabled_state (plugin_id, version, enabled, user_grant_json, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(plugin_id) DO UPDATE SET
			version = excluded.version,
			enabled = excluded.enabled,
			user_grant_json = excluded.user_grant_json,
			updated_at = excluded.updated_at
	`, pluginID, version, boolInt(enabled), grantJSON, now)
	return err
}

func (d *DB) GetPluginEnabledState(ctx context.Context, pluginID string) (*PluginEnabledState, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT plugin_id, version, enabled, user_grant_json, updated_at
		FROM plugin_enabled_state
		WHERE plugin_id = ?
	`, pluginID)
	record, err := scanPluginEnabledState(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (d *DB) ListPluginEnabledStates(ctx context.Context) ([]PluginEnabledState, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT plugin_id, version, enabled, user_grant_json, updated_at
		FROM plugin_enabled_state
		ORDER BY plugin_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []PluginEnabledState
	for rows.Next() {
		record, err := scanPluginEnabledState(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func scanPluginEnabledState(row scanner) (PluginEnabledState, error) {
	var record PluginEnabledState
	var enabled int
	err := row.Scan(&record.PluginID, &record.Version, &enabled, &record.UserGrantJSON, &record.UpdatedAt)
	record.Enabled = enabled != 0
	return record, err
}

func (d *DB) UpsertPluginRuntimeRecord(ctx context.Context, record PluginRuntimeRecord) error {
	if strings.TrimSpace(record.PluginID) == "" {
		return fmt.Errorf("plugin_id is required")
	}
	if record.Status == "" {
		record.Status = "stopped"
	}
	now := d.nowText()
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO plugin_runtime_records (
			plugin_id, version, runtime_kind, status, pid, last_started_at,
			last_stopped_at, last_error, restart_count, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(plugin_id) DO UPDATE SET
			version = excluded.version,
			runtime_kind = excluded.runtime_kind,
			status = excluded.status,
			pid = excluded.pid,
			last_started_at = excluded.last_started_at,
			last_stopped_at = excluded.last_stopped_at,
			last_error = excluded.last_error,
			restart_count = excluded.restart_count,
			updated_at = excluded.updated_at
	`, record.PluginID, record.Version, record.RuntimeKind, record.Status, nullableInt(record.PID),
		nullableString(record.LastStartedAt), nullableString(record.LastStoppedAt), record.LastError, record.RestartCount, now)
	return err
}

func (d *DB) GetPluginRuntimeRecord(ctx context.Context, pluginID string) (*PluginRuntimeRecord, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT plugin_id, version, runtime_kind, status, pid, last_started_at,
		       last_stopped_at, last_error, restart_count, updated_at
		FROM plugin_runtime_records
		WHERE plugin_id = ?
	`, pluginID)
	record, err := scanPluginRuntimeRecord(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func scanPluginRuntimeRecord(row scanner) (PluginRuntimeRecord, error) {
	var record PluginRuntimeRecord
	var pid sql.NullInt64
	var started sql.NullString
	var stopped sql.NullString
	err := row.Scan(
		&record.PluginID,
		&record.Version,
		&record.RuntimeKind,
		&record.Status,
		&pid,
		&started,
		&stopped,
		&record.LastError,
		&record.RestartCount,
		&record.UpdatedAt,
	)
	if pid.Valid {
		value := int(pid.Int64)
		record.PID = &value
	}
	record.LastStartedAt = started.String
	record.LastStoppedAt = stopped.String
	return record, err
}

func (d *DB) RecordPluginAccessEvent(ctx context.Context, event PluginAccessEvent) error {
	if strings.TrimSpace(event.PluginID) == "" {
		return fmt.Errorf("plugin_id is required")
	}
	if strings.TrimSpace(event.AccessKind) == "" {
		return fmt.Errorf("access_kind is required")
	}
	if strings.TrimSpace(event.Status) == "" {
		return fmt.Errorf("status is required")
	}
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	now := d.nowText()
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO plugin_access_events (
			id, plugin_id, access_kind, capability, status, request_summary,
			input_hash, output_hash, duration_ms, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, event.ID, event.PluginID, event.AccessKind, event.Capability, event.Status, event.RequestSummary,
		event.InputHash, event.OutputHash, event.DurationMS, now)
	return err
}

func (d *DB) ListPluginAccessEvents(ctx context.Context, pluginID string, limit int) ([]PluginAccessEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, plugin_id, access_kind, capability, status, request_summary,
		       input_hash, output_hash, duration_ms, created_at
		FROM plugin_access_events
		WHERE plugin_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, pluginID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []PluginAccessEvent
	for rows.Next() {
		var event PluginAccessEvent
		if err := rows.Scan(&event.ID, &event.PluginID, &event.AccessKind, &event.Capability, &event.Status, &event.RequestSummary, &event.InputHash, &event.OutputHash, &event.DurationMS, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (d *DB) RecordPluginProviderUsage(ctx context.Context, usage PluginProviderUsage) error {
	if strings.TrimSpace(usage.PluginID) == "" {
		return fmt.Errorf("plugin_id is required")
	}
	if strings.TrimSpace(usage.Status) == "" {
		return fmt.Errorf("status is required")
	}
	if usage.ID == "" {
		usage.ID = uuid.NewString()
	}
	now := d.nowText()
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO plugin_provider_usage (
			id, plugin_id, provider_id, model, purpose, input_tokens,
			output_tokens, estimated_tokens, status, error_message, duration_ms, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, usage.ID, usage.PluginID, usage.ProviderID, usage.Model, usage.Purpose, usage.InputTokens,
		usage.OutputTokens, usage.EstimatedTokens, usage.Status, usage.ErrorMessage, usage.DurationMS, now)
	return err
}

func (d *DB) ListPluginProviderUsage(ctx context.Context, pluginID string, limit int) ([]PluginProviderUsage, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, plugin_id, provider_id, model, purpose, input_tokens,
		       output_tokens, estimated_tokens, status, error_message, duration_ms, created_at
		FROM plugin_provider_usage
		WHERE plugin_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, pluginID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var usages []PluginProviderUsage
	for rows.Next() {
		var usage PluginProviderUsage
		if err := rows.Scan(&usage.ID, &usage.PluginID, &usage.ProviderID, &usage.Model, &usage.Purpose, &usage.InputTokens, &usage.OutputTokens, &usage.EstimatedTokens, &usage.Status, &usage.ErrorMessage, &usage.DurationMS, &usage.CreatedAt); err != nil {
			return nil, err
		}
		usages = append(usages, usage)
	}
	return usages, rows.Err()
}

func (d *DB) PluginKVGet(ctx context.Context, pluginID, key string) (string, bool, error) {
	var value string
	err := d.db.QueryRowContext(ctx, `
		SELECT value_json
		FROM plugin_kv
		WHERE plugin_id = ? AND key = ?
	`, pluginID, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (d *DB) PluginKVSet(ctx context.Context, pluginID, key, valueJSON string) error {
	if strings.TrimSpace(pluginID) == "" {
		return fmt.Errorf("plugin_id is required")
	}
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("key is required")
	}
	if !json.Valid([]byte(valueJSON)) {
		return fmt.Errorf("value_json must be valid JSON")
	}
	now := d.nowText()
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO plugin_kv (plugin_id, key, value_json, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(plugin_id, key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at
	`, pluginID, key, valueJSON, now)
	return err
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
