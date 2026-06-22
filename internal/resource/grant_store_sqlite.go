package resource

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type SQLiteGrantStore struct {
	db *sql.DB
}

func NewSQLiteGrantStore(db *sql.DB) *SQLiteGrantStore {
	return &SQLiteGrantStore{db: db}
}

func (s *SQLiteGrantStore) Create(ctx context.Context, grant GrantEnvelope) (GrantEnvelope, error) {
	if s == nil || s.db == nil {
		return GrantEnvelope{}, fmt.Errorf("resource grant store is unavailable")
	}
	if strings.TrimSpace(grant.ID) == "" {
		grant.ID = "grant-" + shortHash(fmt.Sprintf("%d", time.Now().UnixNano()))
	}
	if grant.CreatedAt.IsZero() {
		grant.CreatedAt = time.Now().UTC()
	}
	if grant.Status == "" {
		grant.Status = GrantStatusActive
	}
	if grant.Lifetime == "" {
		grant.Lifetime = GrantLifetimeOnce
	}
	if grant.IssuedBy == "" {
		grant.IssuedBy = GrantIssuedByPolicy
	}
	resourceJSON, err := json.Marshal(grant.Resource)
	if err != nil {
		return GrantEnvelope{}, err
	}
	operationsJSON, err := json.Marshal(grant.Operations)
	if err != nil {
		return GrantEnvelope{}, err
	}
	constraintsJSON, err := json.Marshal(grant.Constraints)
	if err != nil {
		return GrantEnvelope{}, err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO resource_grants (
	id, principal_kind, principal_id, capability, resource_json, operations_json,
	constraints_json, lifetime, status, approval_request_id, binding_hash,
	issued_by, created_at, expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		grant.ID, grant.Principal.Kind, grant.Principal.ID, grant.Capability, string(resourceJSON), string(operationsJSON),
		string(constraintsJSON), grant.Lifetime, grant.Status, grant.ApprovalRequest, grant.BindingHash,
		grant.IssuedBy, formatTime(grant.CreatedAt), formatNullableTime(grant.ExpiresAt),
	)
	if err != nil {
		return GrantEnvelope{}, err
	}
	if err := s.writeEvent(ctx, grant, "created"); err != nil {
		return GrantEnvelope{}, err
	}
	return grant, nil
}

func (s *SQLiteGrantStore) Get(ctx context.Context, id string) (GrantEnvelope, bool, error) {
	if s == nil || s.db == nil {
		return GrantEnvelope{}, false, fmt.Errorf("resource grant store is unavailable")
	}
	row := s.db.QueryRowContext(ctx, `
SELECT id, principal_kind, principal_id, capability, resource_json, operations_json,
       constraints_json, lifetime, status, approval_request_id, binding_hash,
       issued_by, created_at, expires_at
FROM resource_grants WHERE id = ?`, id)
	grant, err := scanGrant(row)
	if err == sql.ErrNoRows {
		return GrantEnvelope{}, false, nil
	}
	if err != nil {
		return GrantEnvelope{}, false, err
	}
	return grant, true, nil
}

func (s *SQLiteGrantStore) List(ctx context.Context, filter GrantListFilter) ([]GrantEnvelope, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("resource grant store is unavailable")
	}
	query := `
SELECT id, principal_kind, principal_id, capability, resource_json, operations_json,
       constraints_json, lifetime, status, approval_request_id, binding_hash,
       issued_by, created_at, expires_at
FROM resource_grants WHERE 1=1`
	args := []any{}
	if filter.Principal != nil {
		query += " AND principal_kind = ? AND principal_id = ?"
		args = append(args, filter.Principal.Kind, filter.Principal.ID)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	if filter.Capability != "" {
		query += " AND capability = ?"
		args = append(args, filter.Capability)
	}
	query += " ORDER BY created_at DESC, id"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var grants []GrantEnvelope
	for rows.Next() {
		grant, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}
	return grants, rows.Err()
}

func (s *SQLiteGrantStore) Consume(ctx context.Context, id string, principal PrincipalRef) (GrantEnvelope, error) {
	grant, err := s.requireActiveForPrincipal(ctx, id, principal)
	if err != nil {
		return GrantEnvelope{}, err
	}
	if grant.Lifetime == GrantLifetimeOnce {
		grant.Status = GrantStatusConsumed
		if _, err := s.db.ExecContext(ctx, `UPDATE resource_grants SET status = ?, consumed_at = ?, updated_at = ? WHERE id = ?`,
			grant.Status, formatTime(time.Now().UTC()), formatTime(time.Now().UTC()), id); err != nil {
			return GrantEnvelope{}, err
		}
	}
	if err := s.writeEvent(ctx, grant, "consumed"); err != nil {
		return GrantEnvelope{}, err
	}
	return grant, nil
}

func (s *SQLiteGrantStore) Revoke(ctx context.Context, id string, principal PrincipalRef) (GrantEnvelope, error) {
	grant, ok, err := s.Get(ctx, id)
	if err != nil {
		return GrantEnvelope{}, err
	}
	if !ok {
		return GrantEnvelope{}, fmt.Errorf("%w: %s", ErrGrantNotFound, id)
	}
	if grant.Principal != principal {
		return GrantEnvelope{}, fmt.Errorf("grant principal mismatch")
	}
	if grant.Status != GrantStatusActive && grant.Status != GrantStatusPending {
		return GrantEnvelope{}, fmt.Errorf("grant is not revocable")
	}
	grant.Status = GrantStatusRevoked
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `UPDATE resource_grants SET status = ?, revoked_at = ?, updated_at = ? WHERE id = ?`,
		grant.Status, formatTime(now), formatTime(now), id); err != nil {
		return GrantEnvelope{}, err
	}
	if err := s.writeEvent(ctx, grant, "revoked"); err != nil {
		return GrantEnvelope{}, err
	}
	return grant, nil
}

func (s *SQLiteGrantStore) Expire(ctx context.Context, now time.Time) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, principal_kind, principal_id, capability, resource_json, operations_json,
       constraints_json, lifetime, status, approval_request_id, binding_hash,
       issued_by, created_at, expires_at
FROM resource_grants
WHERE status IN ('pending','active') AND expires_at IS NOT NULL AND expires_at <= ?`, formatTime(now))
	if err != nil {
		return 0, err
	}
	var grants []GrantEnvelope
	for rows.Next() {
		grant, err := scanGrant(rows)
		if err != nil {
			rows.Close()
			return 0, err
		}
		grants = append(grants, grant)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, grant := range grants {
		grant.Status = GrantStatusExpired
		if _, err := s.db.ExecContext(ctx, `UPDATE resource_grants SET status = ?, updated_at = ? WHERE id = ?`,
			grant.Status, formatTime(now), grant.ID); err != nil {
			return 0, err
		}
		if err := s.writeEvent(ctx, grant, "expired"); err != nil {
			return 0, err
		}
	}
	return len(grants), nil
}

func (s *SQLiteGrantStore) requireActiveForPrincipal(ctx context.Context, id string, principal PrincipalRef) (GrantEnvelope, error) {
	grant, ok, err := s.Get(ctx, id)
	if err != nil {
		return GrantEnvelope{}, err
	}
	if !ok {
		return GrantEnvelope{}, fmt.Errorf("%w: %s", ErrGrantNotFound, id)
	}
	if grant.Principal != principal {
		return GrantEnvelope{}, fmt.Errorf("grant principal mismatch")
	}
	if grant.Status != GrantStatusActive {
		return GrantEnvelope{}, fmt.Errorf("grant is not active")
	}
	if grant.ExpiresAt != nil && !grant.ExpiresAt.After(time.Now().UTC()) {
		return GrantEnvelope{}, fmt.Errorf("grant is expired")
	}
	return grant, nil
}

type grantScanner interface {
	Scan(dest ...any) error
}

func scanGrant(scanner grantScanner) (GrantEnvelope, error) {
	var grant GrantEnvelope
	var principalKind, resourceJSON, operationsJSON, constraintsJSON, lifetime, status string
	var createdAt string
	var expiresAt sql.NullString
	err := scanner.Scan(
		&grant.ID, &principalKind, &grant.Principal.ID, &grant.Capability, &resourceJSON, &operationsJSON,
		&constraintsJSON, &lifetime, &status, &grant.ApprovalRequest, &grant.BindingHash,
		&grant.IssuedBy, &createdAt, &expiresAt,
	)
	if err != nil {
		return GrantEnvelope{}, err
	}
	grant.Principal.Kind = PrincipalKind(principalKind)
	grant.Lifetime = GrantLifetime(lifetime)
	grant.Status = GrantStatus(status)
	if err := json.Unmarshal([]byte(resourceJSON), &grant.Resource); err != nil {
		return GrantEnvelope{}, err
	}
	if err := json.Unmarshal([]byte(operationsJSON), &grant.Operations); err != nil {
		return GrantEnvelope{}, err
	}
	if err := json.Unmarshal([]byte(constraintsJSON), &grant.Constraints); err != nil {
		return GrantEnvelope{}, err
	}
	parsedCreated, err := parseTime(createdAt)
	if err != nil {
		return GrantEnvelope{}, err
	}
	grant.CreatedAt = parsedCreated
	if expiresAt.Valid && strings.TrimSpace(expiresAt.String) != "" {
		parsedExpires, err := parseTime(expiresAt.String)
		if err != nil {
			return GrantEnvelope{}, err
		}
		grant.ExpiresAt = &parsedExpires
	}
	return grant, nil
}

func (s *SQLiteGrantStore) writeEvent(ctx context.Context, grant GrantEnvelope, eventType string) error {
	id := fmt.Sprintf("event-%s-%s-%d", grant.ID, eventType, time.Now().UnixNano())
	_, err := s.db.ExecContext(ctx, `
INSERT INTO resource_grant_events (
	id, grant_id, event_type, principal_kind, principal_id, summary_hash, provenance_json, created_at
) VALUES (?, ?, ?, ?, ?, ?, '{}', ?)`,
		id, grant.ID, eventType, grant.Principal.Kind, grant.Principal.ID, grantSummaryHash(grant, eventType), formatTime(time.Now().UTC()))
	return err
}

func grantSummaryHash(grant GrantEnvelope, eventType string) string {
	sum := sha256.Sum256([]byte(grant.ID + "\x00" + eventType + "\x00" + grant.BindingHash))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func formatNullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func parseTime(value string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC(), nil
	}
	return time.Parse("2006-01-02 15:04:05", value)
}
