package turn

import (
	"database/sql"
	"fmt"
	"time"
)

type SQLiteIdempotencyStore struct {
	db *sql.DB
}

func NewSQLiteIdempotencyStore(db *sql.DB) *SQLiteIdempotencyStore {
	return &SQLiteIdempotencyStore{db: db}
}

func (s *SQLiteIdempotencyStore) Begin(key, turnID string) (IdempotencyResult, error) {
	if s == nil || s.db == nil {
		return IdempotencyResult{}, fmt.Errorf("sqlite idempotency database is required")
	}
	if key == "" {
		return IdempotencyResult{}, fmt.Errorf("idempotency key is required")
	}
	if turnID == "" {
		return IdempotencyResult{}, fmt.Errorf("turn id is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.Begin()
	if err != nil {
		return IdempotencyResult{}, fmt.Errorf("begin idempotency tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO turns (id, kind, state, status, started_at, updated_at)
		VALUES (?, '', ?, 'running', ?, ?)
	`, turnID, string(StateCreated), now, now); err != nil {
		return IdempotencyResult{}, fmt.Errorf("begin idempotency turn placeholder: %w", err)
	}
	result, err := tx.Exec(`
		INSERT OR IGNORE INTO turn_idempotency (idempotency_key, turn_id, status, created_at, updated_at)
		VALUES (?, ?, 'running', ?, ?)
	`, key, turnID, now, now)
	if err != nil {
		return IdempotencyResult{}, fmt.Errorf("begin idempotency: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return IdempotencyResult{}, fmt.Errorf("begin idempotency rows: %w", err)
	}
	if rows == 1 {
		if err := tx.Commit(); err != nil {
			return IdempotencyResult{}, fmt.Errorf("commit idempotency: %w", err)
		}
		return IdempotencyResult{TurnID: turnID, Status: "running"}, nil
	}

	_, _ = tx.Exec(`DELETE FROM turns WHERE id = ? AND kind = ''`, turnID)
	var existing idempotencyRecord
	if err := tx.QueryRow(`
		SELECT turn_id, status
		FROM turn_idempotency
		WHERE idempotency_key = ?
	`, key).Scan(&existing.turnID, &existing.status); err != nil {
		return IdempotencyResult{}, fmt.Errorf("read duplicate idempotency: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return IdempotencyResult{}, fmt.Errorf("commit duplicate idempotency: %w", err)
	}
	return IdempotencyResult{
		Duplicate: true,
		TurnID:    existing.turnID,
		Status:    existing.status,
	}, nil
}

func (s *SQLiteIdempotencyStore) Complete(key, status string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sqlite idempotency database is required")
	}
	if key == "" {
		return fmt.Errorf("idempotency key is required")
	}
	if status == "" {
		status = "done"
	}
	result, err := s.db.Exec(`
		UPDATE turn_idempotency
		SET status = ?, updated_at = ?
		WHERE idempotency_key = ?
	`, status, time.Now().UTC().Format(time.RFC3339Nano), key)
	if err != nil {
		return fmt.Errorf("complete idempotency: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("complete idempotency rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("idempotency key %q not found", key)
	}
	return nil
}
