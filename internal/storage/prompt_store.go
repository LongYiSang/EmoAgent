package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent/internal/promptcenter"
)

func (d *DB) GetOverride(ctx context.Context, componentID, scopeType, scopeID string) (*promptcenter.OverrideRecord, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT id, component_id, scope_type, scope_id, mode, override_text,
		       enabled, default_hash_at_edit, note, created_at, updated_at
		FROM prompt_overrides
		WHERE component_id = ? AND scope_type = ? AND scope_id = ?
	`, componentID, scopeType, scopeID)
	record, err := scanPromptOverride(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (d *DB) ListOverrides(ctx context.Context) ([]promptcenter.OverrideRecord, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, component_id, scope_type, scope_id, mode, override_text,
		       enabled, default_hash_at_edit, note, created_at, updated_at
		FROM prompt_overrides
		ORDER BY component_id, scope_type, scope_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []promptcenter.OverrideRecord
	for rows.Next() {
		record, err := scanPromptOverride(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (d *DB) UpsertPromptOverride(ctx context.Context, req promptcenter.UpsertOverrideRequest) error {
	return d.UpsertOverride(ctx, req)
}

func (d *DB) UpsertOverride(ctx context.Context, req promptcenter.UpsertOverrideRequest) error {
	if strings.TrimSpace(req.ComponentID) == "" {
		return fmt.Errorf("component_id is required")
	}
	if strings.TrimSpace(req.ScopeType) == "" {
		return fmt.Errorf("scope_type is required")
	}
	if req.Mode == "" {
		return fmt.Errorf("mode is required")
	}
	if req.Mode == promptcenter.OverrideModeUseDefault {
		req.OverrideText = ""
	}
	now := d.nowText()
	id := uuid.NewString()
	defaultHashAtEdit := req.DefaultHashAtEdit
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO prompt_overrides (
			id, component_id, scope_type, scope_id, mode, override_text,
			enabled, default_hash_at_edit, note, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(component_id, scope_type, scope_id) DO UPDATE SET
			mode = excluded.mode,
			override_text = excluded.override_text,
			enabled = excluded.enabled,
			default_hash_at_edit = excluded.default_hash_at_edit,
			note = excluded.note,
			updated_at = excluded.updated_at
	`, id, req.ComponentID, req.ScopeType, req.ScopeID, string(req.Mode), req.OverrideText,
		boolInt(req.EnabledOrDefault()), defaultHashAtEdit, req.Note, now, now)
	return err
}

func (d *DB) DeleteOverride(ctx context.Context, componentID, scopeType, scopeID string) error {
	_, err := d.db.ExecContext(ctx, `
		DELETE FROM prompt_overrides
		WHERE component_id = ? AND scope_type = ? AND scope_id = ?
	`, componentID, scopeType, scopeID)
	return err
}

func scanPromptOverride(row scanner) (promptcenter.OverrideRecord, error) {
	var record promptcenter.OverrideRecord
	var mode string
	var enabled int
	err := row.Scan(
		&record.ID,
		&record.ComponentID,
		&record.ScopeType,
		&record.ScopeID,
		&mode,
		&record.OverrideText,
		&enabled,
		&record.DefaultHashAtEdit,
		&record.Note,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	record.Mode = promptcenter.OverrideMode(mode)
	record.Enabled = enabled != 0
	return record, err
}

func (d *DB) SaveRenderSnapshot(ctx context.Context, snapshot promptcenter.RenderSnapshot) error {
	if snapshot.ID == "" {
		snapshot.ID = uuid.NewString()
	}
	if snapshot.FinalHash == "" {
		snapshot.FinalHash = promptcenter.HashText(snapshot.RenderedText)
	}
	if snapshot.ComponentsJSON == "" {
		payload, err := json.Marshal(snapshot.Components)
		if err != nil {
			return fmt.Errorf("marshal prompt snapshot components: %w", err)
		}
		snapshot.ComponentsJSON = string(payload)
	}
	if snapshot.ComponentsJSON == "" {
		snapshot.ComponentsJSON = "[]"
	}
	if strings.TrimSpace(snapshot.Purpose) == "" {
		return fmt.Errorf("purpose is required")
	}
	createdAt := snapshot.CreatedAt
	if createdAt == "" {
		createdAt = d.nowText()
	}
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO prompt_render_snapshots (
			id, request_id, turn_id, session_id, agent_id, persona_key, purpose,
			model, final_hash, components_json, rendered_text, truncated, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, snapshot.ID, snapshot.RequestID, snapshot.TurnID, snapshot.SessionID, snapshot.AgentID,
		snapshot.PersonaKey, snapshot.Purpose, snapshot.Model, snapshot.FinalHash, snapshot.ComponentsJSON,
		snapshot.RenderedText, boolInt(snapshot.Truncated), createdAt)
	return err
}

func (d *DB) ListRenderSnapshots(ctx context.Context, filter promptcenter.SnapshotFilter) ([]promptcenter.RenderSnapshotSummary, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	where := []string{"1=1"}
	args := []any{}
	if filter.AgentID != "" {
		where = append(where, "agent_id = ?")
		args = append(args, filter.AgentID)
	}
	if filter.SessionID != "" {
		where = append(where, "session_id = ?")
		args = append(args, filter.SessionID)
	}
	if filter.Purpose != "" {
		where = append(where, "purpose = ?")
		args = append(args, filter.Purpose)
	}
	args = append(args, limit)
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, session_id, agent_id, persona_key, purpose, model, final_hash, truncated, created_at
		FROM prompt_render_snapshots
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY created_at DESC
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []promptcenter.RenderSnapshotSummary
	for rows.Next() {
		var item promptcenter.RenderSnapshotSummary
		var truncated int
		if err := rows.Scan(&item.ID, &item.SessionID, &item.AgentID, &item.PersonaKey, &item.Purpose, &item.Model, &item.FinalHash, &truncated, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Truncated = truncated != 0
		items = append(items, item)
	}
	return items, rows.Err()
}

func (d *DB) GetRenderSnapshot(ctx context.Context, id string) (*promptcenter.RenderSnapshot, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT id, request_id, turn_id, session_id, agent_id, persona_key, purpose,
		       model, final_hash, components_json, rendered_text, truncated, created_at
		FROM prompt_render_snapshots
		WHERE id = ?
	`, id)
	snapshot, err := scanRenderSnapshot(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func scanRenderSnapshot(row scanner) (promptcenter.RenderSnapshot, error) {
	var snapshot promptcenter.RenderSnapshot
	var truncated int
	err := row.Scan(
		&snapshot.ID,
		&snapshot.RequestID,
		&snapshot.TurnID,
		&snapshot.SessionID,
		&snapshot.AgentID,
		&snapshot.PersonaKey,
		&snapshot.Purpose,
		&snapshot.Model,
		&snapshot.FinalHash,
		&snapshot.ComponentsJSON,
		&snapshot.RenderedText,
		&truncated,
		&snapshot.CreatedAt,
	)
	if err != nil {
		return promptcenter.RenderSnapshot{}, err
	}
	snapshot.Truncated = truncated != 0
	if strings.TrimSpace(snapshot.ComponentsJSON) == "" {
		snapshot.ComponentsJSON = "[]"
	}
	if err := json.Unmarshal([]byte(snapshot.ComponentsJSON), &snapshot.Components); err != nil {
		return promptcenter.RenderSnapshot{}, fmt.Errorf("decode prompt snapshot components: %w", err)
	}
	return snapshot, nil
}
