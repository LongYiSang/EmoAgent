package toolapproval

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent/internal/tool"
)

type DirectToolCallStatus string

const (
	DirectToolCallStatusPending  DirectToolCallStatus = "pending"
	DirectToolCallStatusClaimed  DirectToolCallStatus = "claimed"
	DirectToolCallStatusConsumed DirectToolCallStatus = "consumed"
	DirectToolCallStatusRejected DirectToolCallStatus = "rejected"
	DirectToolCallStatusExpired  DirectToolCallStatus = "expired"
	DirectToolCallStatusFailed   DirectToolCallStatus = "failed"
)

type DirectToolCallStore struct {
	db *sql.DB
}

type PendingDirectToolCall struct {
	ApprovalRequestID   string
	SessionID           string
	TurnID              string
	TaskID              string
	CallID              string
	ToolName            string
	Input               json.RawMessage
	MaxPermission       tool.Permission
	Provider            string
	ApprovalKind        string
	NormalizedInputHash string
	PathDigest          string
	InputPreview        string
	Status              DirectToolCallStatus
	ClaimID             string
	ClaimedAt           time.Time
	ConsumedAt          time.Time
	CreatedAt           time.Time
	ExpiresAt           time.Time
	ErrorMessage        string
}

func NewDirectToolCallStore(db *sql.DB) *DirectToolCallStore {
	if db == nil {
		return nil
	}
	return &DirectToolCallStore{db: db}
}

func (s *DirectToolCallStore) Put(ctx context.Context, row PendingDirectToolCall) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("direct tool call store is nil")
	}
	if strings.TrimSpace(row.ApprovalRequestID) == "" {
		return fmt.Errorf("approval_request_id is required")
	}
	if strings.TrimSpace(row.SessionID) == "" {
		return fmt.Errorf("session_id is required")
	}
	if strings.TrimSpace(row.TaskID) == "" {
		return fmt.Errorf("task_id is required")
	}
	if strings.TrimSpace(row.ToolName) == "" {
		return fmt.Errorf("tool_name is required")
	}
	if row.Status == "" {
		row.Status = DirectToolCallStatusPending
	}
	if row.MaxPermission == "" {
		row.MaxPermission = tool.PermReadOnly
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	if row.ExpiresAt.IsZero() {
		row.ExpiresAt = row.CreatedAt.Add(time.Hour)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pending_direct_tool_calls (
			approval_request_id, session_id, turn_id, task_id, call_id, tool_name,
			input_json, max_permission, provider, approval_kind, normalized_input_hash,
			path_digest, input_preview, status, claim_id, claimed_at, consumed_at,
			created_at, expires_at, error_message
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?, ?)
	`,
		row.ApprovalRequestID, row.SessionID, row.TurnID, row.TaskID, row.CallID, row.ToolName,
		string(row.Input), string(row.MaxPermission), row.Provider, row.ApprovalKind, row.NormalizedInputHash,
		row.PathDigest, row.InputPreview, string(row.Status), row.ClaimID,
		formatStoreTime(row.CreatedAt), formatStoreTime(row.ExpiresAt), row.ErrorMessage,
	)
	if err != nil {
		return fmt.Errorf("insert pending direct tool call: %w", err)
	}
	return nil
}

func (s *DirectToolCallStore) GetByApproval(ctx context.Context, sessionID, approvalRequestID string) (PendingDirectToolCall, bool, error) {
	if s == nil || s.db == nil {
		return PendingDirectToolCall{}, false, fmt.Errorf("direct tool call store is nil")
	}
	row := s.db.QueryRowContext(ctx, pendingDirectToolCallSelectSQL()+`
		WHERE approval_request_id = ? AND session_id = ?
	`, approvalRequestID, sessionID)
	got, err := scanPendingDirectToolCall(row)
	if err == sql.ErrNoRows {
		return PendingDirectToolCall{}, false, nil
	}
	if err != nil {
		return PendingDirectToolCall{}, false, err
	}
	return got, true, nil
}

func (s *DirectToolCallStore) Claim(ctx context.Context, sessionID, approvalRequestID string) (PendingDirectToolCall, string, bool, error) {
	if s == nil || s.db == nil {
		return PendingDirectToolCall{}, "", false, fmt.Errorf("direct tool call store is nil")
	}
	claimID := uuid.NewString()
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `
		UPDATE pending_direct_tool_calls
		SET status = ?, claim_id = ?, claimed_at = ?
		WHERE approval_request_id = ? AND session_id = ? AND status = ?
		  AND (expires_at = '' OR expires_at > ?)
	`, string(DirectToolCallStatusClaimed), claimID, formatStoreTime(now), approvalRequestID, sessionID, string(DirectToolCallStatusPending), formatStoreTime(now))
	if err != nil {
		return PendingDirectToolCall{}, "", false, fmt.Errorf("claim pending direct tool call: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 1 {
		row, ok, err := s.GetByApproval(ctx, sessionID, approvalRequestID)
		return row, claimID, ok, err
	}
	row, ok, err := s.GetByApproval(ctx, sessionID, approvalRequestID)
	if err != nil {
		return PendingDirectToolCall{}, "", false, err
	}
	if !ok {
		return PendingDirectToolCall{}, "", false, nil
	}
	if row.Status == DirectToolCallStatusPending && !row.ExpiresAt.IsZero() && !now.Before(row.ExpiresAt) {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE pending_direct_tool_calls
			SET status = ?
			WHERE approval_request_id = ? AND session_id = ? AND status = ?
		`, string(DirectToolCallStatusExpired), approvalRequestID, sessionID, string(DirectToolCallStatusPending)); err != nil {
			return PendingDirectToolCall{}, "", false, fmt.Errorf("expire pending direct tool call: %w", err)
		}
		row.Status = DirectToolCallStatusExpired
	}
	return row, "", false, nil
}

func (s *DirectToolCallStore) MarkConsumed(ctx context.Context, sessionID, approvalRequestID, claimID string) error {
	return s.markResolved(ctx, sessionID, approvalRequestID, claimID, DirectToolCallStatusConsumed, "")
}

func (s *DirectToolCallStore) MarkRejected(ctx context.Context, sessionID, approvalRequestID, claimID string) error {
	return s.markResolved(ctx, sessionID, approvalRequestID, claimID, DirectToolCallStatusRejected, "")
}

func (s *DirectToolCallStore) MarkFailed(ctx context.Context, sessionID, approvalRequestID, claimID string, cause error) error {
	msg := ""
	if cause != nil {
		msg = cause.Error()
	}
	return s.markResolved(ctx, sessionID, approvalRequestID, claimID, DirectToolCallStatusFailed, msg)
}

func (s *DirectToolCallStore) ExpireStale(ctx context.Context, now time.Time) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("direct tool call store is nil")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE pending_direct_tool_calls
		SET status = ?
		WHERE status IN (?, ?) AND expires_at <= ?
	`, string(DirectToolCallStatusExpired), string(DirectToolCallStatusPending), string(DirectToolCallStatusClaimed), formatStoreTime(now))
	if err != nil {
		return 0, fmt.Errorf("expire stale direct tool calls: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *DirectToolCallStore) markResolved(ctx context.Context, sessionID, approvalRequestID, claimID string, status DirectToolCallStatus, errMessage string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("direct tool call store is nil")
	}
	if strings.TrimSpace(claimID) == "" {
		return fmt.Errorf("claim_id is required")
	}
	now := formatStoreTime(time.Now().UTC())
	res, err := s.db.ExecContext(ctx, `
		UPDATE pending_direct_tool_calls
		SET status = ?, consumed_at = ?, error_message = ?
		WHERE approval_request_id = ? AND session_id = ? AND claim_id = ? AND status = ?
	`, string(status), now, errMessage, approvalRequestID, sessionID, claimID, string(DirectToolCallStatusClaimed))
	if err != nil {
		return fmt.Errorf("mark pending direct tool call %s: %w", status, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("pending direct tool call is not claimed by %s", claimID)
	}
	return nil
}

func pendingDirectToolCallSelectSQL() string {
	return `
		SELECT approval_request_id, session_id, turn_id, task_id, call_id, tool_name,
		       input_json, max_permission, provider, approval_kind, normalized_input_hash,
		       path_digest, input_preview, status, claim_id, claimed_at, consumed_at,
		       created_at, expires_at, error_message
		FROM pending_direct_tool_calls
	`
}

type pendingDirectToolCallScanner interface {
	Scan(dest ...any) error
}

func scanPendingDirectToolCall(scanner pendingDirectToolCallScanner) (PendingDirectToolCall, error) {
	var (
		row        PendingDirectToolCall
		inputJSON  string
		permission string
		status     string
		claimedAt  sql.NullString
		consumedAt sql.NullString
		createdAt  string
		expiresAt  string
	)
	err := scanner.Scan(
		&row.ApprovalRequestID, &row.SessionID, &row.TurnID, &row.TaskID, &row.CallID, &row.ToolName,
		&inputJSON, &permission, &row.Provider, &row.ApprovalKind, &row.NormalizedInputHash,
		&row.PathDigest, &row.InputPreview, &status, &row.ClaimID, &claimedAt, &consumedAt,
		&createdAt, &expiresAt, &row.ErrorMessage,
	)
	if err != nil {
		return PendingDirectToolCall{}, err
	}
	row.Input = json.RawMessage(inputJSON)
	row.MaxPermission = tool.Permission(permission)
	row.Status = DirectToolCallStatus(status)
	if claimedAt.Valid {
		row.ClaimedAt = parseStoreTime(claimedAt.String)
	}
	if consumedAt.Valid {
		row.ConsumedAt = parseStoreTime(consumedAt.String)
	}
	row.CreatedAt = parseStoreTime(createdAt)
	row.ExpiresAt = parseStoreTime(expiresAt)
	return row, nil
}

func formatStoreTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseStoreTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}
