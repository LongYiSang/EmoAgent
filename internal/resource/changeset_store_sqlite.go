package resource

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ChangeSetStore interface {
	Save(context.Context, ChangeSet) error
	Get(context.Context, string) (ChangeSet, bool, error)
	Update(context.Context, ChangeSet) error
	List(context.Context, []ChangeSetStatus) ([]ChangeSet, error)
}

type SQLiteChangeSetStore struct {
	db *sql.DB
}

func NewSQLiteChangeSetStore(db *sql.DB) *SQLiteChangeSetStore {
	return &SQLiteChangeSetStore{db: db}
}

func (s *SQLiteChangeSetStore) Save(ctx context.Context, cs ChangeSet) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("host resource changeset store is unavailable")
	}
	sourceJSON, err := marshalResourceRef(cs.Source)
	if err != nil {
		return err
	}
	targetJSON, err := marshalResourceRef(cs.Target)
	if err != nil {
		return err
	}
	previewJSON, err := json.Marshal(persistedPreview(cs.Preview))
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
INSERT INTO host_resource_changesets (
	id, principal_kind, principal_id, status, operation, source_ref_json, target_ref_json,
	target_display_path, baseline_hash, baseline_file_id, content_hash, plan_hash,
	staging_path, quarantine_path, preview_json, permanent_delete, recursive,
	error_message, created_at, updated_at, applied_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cs.ID, cs.Principal.Kind, cs.Principal.ID, cs.Status, cs.Operation, string(sourceJSON), string(targetJSON),
		cs.TargetDisplayPath, cs.BaselineHash, cs.BaselineFileID, cs.ContentHash, cs.PlanHash,
		cs.StagingPath, cs.QuarantinePath, string(previewJSON), boolInt(cs.PermanentDelete), boolInt(cs.Recursive),
		cs.ErrorMessage, formatTime(cs.CreatedAt), formatTime(cs.UpdatedAt), formatNullableTime(cs.AppliedAt),
	)
	if err != nil {
		return err
	}
	if err := replaceChangeSetOps(ctx, tx, cs); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteChangeSetStore) Get(ctx context.Context, id string) (ChangeSet, bool, error) {
	if s == nil || s.db == nil {
		return ChangeSet{}, false, fmt.Errorf("host resource changeset store is unavailable")
	}
	row := s.db.QueryRowContext(ctx, `
SELECT id, principal_kind, principal_id, status, operation, source_ref_json, target_ref_json,
       target_display_path, baseline_hash, baseline_file_id, content_hash, plan_hash,
       staging_path, quarantine_path, preview_json, permanent_delete, recursive,
       error_message, created_at, updated_at, applied_at
FROM host_resource_changesets WHERE id = ?`, id)
	cs, err := scanChangeSet(row)
	if err == sql.ErrNoRows {
		return ChangeSet{}, false, nil
	}
	if err != nil {
		return ChangeSet{}, false, err
	}
	return cs, true, nil
}

func (s *SQLiteChangeSetStore) Update(ctx context.Context, cs ChangeSet) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("host resource changeset store is unavailable")
	}
	sourceJSON, err := marshalResourceRef(cs.Source)
	if err != nil {
		return err
	}
	targetJSON, err := marshalResourceRef(cs.Target)
	if err != nil {
		return err
	}
	previewJSON, err := json.Marshal(persistedPreview(cs.Preview))
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
UPDATE host_resource_changesets
SET principal_kind = ?, principal_id = ?, status = ?, operation = ?, source_ref_json = ?,
    target_ref_json = ?, target_display_path = ?, baseline_hash = ?, baseline_file_id = ?,
    content_hash = ?, plan_hash = ?, staging_path = ?, quarantine_path = ?, preview_json = ?,
    permanent_delete = ?, recursive = ?, error_message = ?, updated_at = ?, applied_at = ?
WHERE id = ?`,
		cs.Principal.Kind, cs.Principal.ID, cs.Status, cs.Operation, string(sourceJSON),
		string(targetJSON), cs.TargetDisplayPath, cs.BaselineHash, cs.BaselineFileID,
		cs.ContentHash, cs.PlanHash, cs.StagingPath, cs.QuarantinePath, string(previewJSON),
		boolInt(cs.PermanentDelete), boolInt(cs.Recursive), cs.ErrorMessage, formatTime(cs.UpdatedAt),
		formatNullableTime(cs.AppliedAt), cs.ID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("%w: %s", ErrChangeSetNotFound, cs.ID)
	}
	if err := replaceChangeSetOps(ctx, tx, cs); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteChangeSetStore) List(ctx context.Context, statuses []ChangeSetStatus) ([]ChangeSet, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("host resource changeset store is unavailable")
	}
	query := `
SELECT id, principal_kind, principal_id, status, operation, source_ref_json, target_ref_json,
       target_display_path, baseline_hash, baseline_file_id, content_hash, plan_hash,
       staging_path, quarantine_path, preview_json, permanent_delete, recursive,
       error_message, created_at, updated_at, applied_at
FROM host_resource_changesets`
	args := []any{}
	if len(statuses) > 0 {
		placeholders := make([]string, 0, len(statuses))
		for _, status := range statuses {
			placeholders = append(placeholders, "?")
			args = append(args, status)
		}
		query += " WHERE status IN (" + strings.Join(placeholders, ",") + ")"
	}
	query += " ORDER BY created_at DESC, id"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChangeSet
	for rows.Next() {
		cs, err := scanChangeSet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, cs)
	}
	return out, rows.Err()
}

func (s *SQLiteChangeSetStore) replaceOps(ctx context.Context, cs ChangeSet) error {
	return replaceChangeSetOps(ctx, s.db, cs)
}

type changeSetExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func replaceChangeSetOps(ctx context.Context, exec changeSetExecer, cs ChangeSet) error {
	if _, err := exec.ExecContext(ctx, `DELETE FROM host_resource_change_ops WHERE changeset_id = ?`, cs.ID); err != nil {
		return err
	}
	for i, op := range cs.Preview.Ops {
		opJSON, err := json.Marshal(op)
		if err != nil {
			return err
		}
		id := fmt.Sprintf("%s-op-%d", cs.ID, i)
		if _, err := exec.ExecContext(ctx, `
INSERT INTO host_resource_change_ops (
	id, changeset_id, op_index, operation, source_display_path, target_display_path,
	source_hash, target_hash, bytes, metadata_json, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, cs.ID, i, op.Operation, op.SourcePath, op.TargetPath, op.SourceHash, op.TargetHash, op.Bytes, string(opJSON), formatTime(cs.CreatedAt),
		); err != nil {
			return err
		}
	}
	return nil
}

type changeSetScanner interface {
	Scan(dest ...any) error
}

func scanChangeSet(scanner changeSetScanner) (ChangeSet, error) {
	var cs ChangeSet
	var principalKind, status, operation string
	var sourceJSON, targetJSON, previewJSON string
	var permanentDelete, recursive int
	var createdAt, updatedAt string
	var appliedAt sql.NullString
	if err := scanner.Scan(
		&cs.ID, &principalKind, &cs.Principal.ID, &status, &operation, &sourceJSON, &targetJSON,
		&cs.TargetDisplayPath, &cs.BaselineHash, &cs.BaselineFileID, &cs.ContentHash, &cs.PlanHash,
		&cs.StagingPath, &cs.QuarantinePath, &previewJSON, &permanentDelete, &recursive,
		&cs.ErrorMessage, &createdAt, &updatedAt, &appliedAt,
	); err != nil {
		return ChangeSet{}, err
	}
	cs.Principal.Kind = PrincipalKind(principalKind)
	cs.Status = ChangeSetStatus(status)
	cs.Operation = ChangeOperation(operation)
	source, err := unmarshalResourceRef([]byte(sourceJSON))
	if err != nil {
		return ChangeSet{}, err
	}
	target, err := unmarshalResourceRef([]byte(targetJSON))
	if err != nil {
		return ChangeSet{}, err
	}
	cs.Source = source
	cs.Target = target
	if err := json.Unmarshal([]byte(previewJSON), &cs.Preview); err != nil {
		return ChangeSet{}, err
	}
	cs.PermanentDelete = permanentDelete == 1
	cs.Recursive = recursive == 1
	if cs.CreatedAt, err = parseTime(createdAt); err != nil {
		return ChangeSet{}, err
	}
	if cs.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return ChangeSet{}, err
	}
	if appliedAt.Valid {
		parsed, err := parseTime(appliedAt.String)
		if err != nil {
			return ChangeSet{}, err
		}
		cs.AppliedAt = &parsed
	}
	return cs, nil
}

type resourceRefRecord struct {
	ID                string `json:"id"`
	Provider          string `json:"provider"`
	DisplayPath       string `json:"display_path"`
	RootID            string `json:"root_id,omitempty"`
	CanonicalPath     string `json:"canonical_path"`
	CanonicalPathHash string `json:"canonical_path_hash"`
	ResourceType      string `json:"resource_type"`
	FileIdentity      string `json:"file_identity,omitempty"`
}

func marshalResourceRef(ref ResourceRef) ([]byte, error) {
	return json.Marshal(resourceRefRecord{
		ID:                ref.ID,
		Provider:          ref.Provider,
		DisplayPath:       ref.DisplayPath,
		RootID:            ref.RootID,
		CanonicalPath:     ref.CanonicalPath,
		CanonicalPathHash: ref.CanonicalPathHash,
		ResourceType:      ref.ResourceType,
		FileIdentity:      ref.FileIdentity,
	})
}

func unmarshalResourceRef(data []byte) (ResourceRef, error) {
	if len(data) == 0 || string(data) == "{}" {
		return ResourceRef{}, nil
	}
	var record resourceRefRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return ResourceRef{}, err
	}
	return ResourceRef{
		ID:                record.ID,
		Provider:          record.Provider,
		DisplayPath:       record.DisplayPath,
		RootID:            record.RootID,
		CanonicalPath:     record.CanonicalPath,
		CanonicalPathHash: record.CanonicalPathHash,
		ResourceType:      record.ResourceType,
		FileIdentity:      record.FileIdentity,
	}, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

func persistedPreview(preview ChangePreview) ChangePreview {
	preview.Diff = ""
	return preview
}
