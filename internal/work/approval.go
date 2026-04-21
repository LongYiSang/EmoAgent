package work

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent/internal/protocol"
)

type ApprovalService struct {
	db     *sql.DB
	logger *slog.Logger
}

type ApprovalConsumeResult struct {
	Request        *protocol.ApprovalRequest
	PreviousStatus protocol.ApprovalStatus
}

func NewApprovalService(db *sql.DB, logger *slog.Logger) *ApprovalService {
	if db == nil {
		return nil
	}
	return &ApprovalService{db: db, logger: logger}
}

func (s *ApprovalService) CreateRequestFromDecision(sessionID string, packet protocol.DecisionPacket, expiresAt time.Time) (*protocol.ApprovalRequest, error) {
	if s == nil {
		return nil, fmt.Errorf("approval service is nil")
	}
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if packet.TaskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	if packet.RejectOptionID == "" {
		return nil, fmt.Errorf("reject_option_id is required")
	}
	if !optionExists(packet.Options, packet.RejectOptionID) {
		return nil, fmt.Errorf("reject_option_id %q does not match any option", packet.RejectOptionID)
	}

	now := time.Now().UTC()
	if expiresAt.IsZero() {
		expiresAt = now.Add(time.Hour)
	}

	optionsJSON, err := json.Marshal(packet.Options)
	if err != nil {
		return nil, fmt.Errorf("marshal options_json: %w", err)
	}

	req := &protocol.ApprovalRequest{
		ID:                   uuid.NewString(),
		SessionID:            sessionID,
		TaskID:               packet.TaskID,
		Category:             string(packet.Category),
		RiskLevel:            derivedRiskLevel(packet.Category),
		GoalSummary:          packet.GoalSummary,
		Question:             packet.Question,
		Options:              append([]protocol.DecisionOption(nil), packet.Options...),
		RecommendedOption:    packet.RecommendedOption,
		RecommendationReason: packet.RecommendationReason,
		RejectOptionID:       packet.RejectOptionID,
		Status:               string(protocol.ApprovalStatusPending),
		ExpiresAt:            formatTime(expiresAt),
		CreatedAt:            formatTime(now),
		UpdatedAt:            formatTime(now),
	}

	_, err = s.db.Exec(`
		INSERT INTO approval_requests (
			id, session_id, task_id, category, risk_level, goal_summary, question,
			options_json, recommended_option, recommendation_reason, reject_option_id,
			status, selected_option_id, actor_channel, actor_ref,
			expires_at, decided_at, consumed_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', '', ?, NULL, NULL, ?, ?)
	`,
		req.ID, req.SessionID, req.TaskID, req.Category, req.RiskLevel, req.GoalSummary, req.Question,
		string(optionsJSON), req.RecommendedOption, req.RecommendationReason, req.RejectOptionID,
		req.Status, req.ExpiresAt, req.CreatedAt, req.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return req, nil
}

func (s *ApprovalService) ApproveRequest(sessionID, requestID, optionID, actorChannel, actorRef string) (*protocol.ApprovalRequest, error) {
	return s.decideRequest(sessionID, requestID, optionID, actorChannel, actorRef, string(protocol.ApprovalStatusApproved), false)
}

func (s *ApprovalService) RejectRequest(sessionID, requestID, actorChannel, actorRef string) (*protocol.ApprovalRequest, error) {
	return s.decideRequest(sessionID, requestID, "", actorChannel, actorRef, string(protocol.ApprovalStatusRejected), true)
}

func (s *ApprovalService) decideRequest(sessionID, requestID, optionID, actorChannel, actorRef, nextStatus string, useRejectOption bool) (*protocol.ApprovalRequest, error) {
	if s == nil {
		return nil, fmt.Errorf("approval service is nil")
	}
	req, err := s.GetRequest(sessionID, requestID)
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("approval request not found")
	}
	if req.Status != string(protocol.ApprovalStatusPending) {
		return nil, fmt.Errorf("approval request is not pending")
	}
	if time.Now().UTC().After(parseTime(req.ExpiresAt)) {
		return nil, fmt.Errorf("approval request expired")
	}
	selected := optionID
	if useRejectOption {
		selected = req.RejectOptionID
	}
	if selected == "" {
		return nil, fmt.Errorf("selected_option_id is required")
	}
	if !optionExists(req.Options, selected) {
		return nil, fmt.Errorf("selected_option_id %q does not match any option", selected)
	}

	now := formatTime(time.Now().UTC())
	res, err := s.db.Exec(`
		UPDATE approval_requests
		SET status = ?, selected_option_id = ?, actor_channel = ?, actor_ref = ?, decided_at = ?, updated_at = ?
		WHERE id = ? AND session_id = ? AND status = ?
	`,
		nextStatus, selected, actorChannel, actorRef, now, now,
		requestID, sessionID, string(protocol.ApprovalStatusPending),
	)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, fmt.Errorf("approval request was not updated")
	}
	return s.GetRequest(sessionID, requestID)
}

func (s *ApprovalService) ConsumeApprovedRequestForResume(sessionID, taskID, requestID string) (*protocol.ApprovalRequest, error) {
	result, err := s.consumeRequestForResume(sessionID, taskID, requestID)
	if err != nil {
		return nil, err
	}
	return result.Request, nil
}

func (s *ApprovalService) consumeRequestForResume(sessionID, taskID, requestID string) (*ApprovalConsumeResult, error) {
	if s == nil {
		return nil, fmt.Errorf("approval service is nil")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRow(`
		SELECT id, session_id, task_id, category, risk_level, goal_summary, question,
		       options_json, recommended_option, recommendation_reason, reject_option_id,
		       status, selected_option_id, actor_channel, actor_ref,
		       expires_at, decided_at, consumed_at, created_at, updated_at
		FROM approval_requests
		WHERE id = ? AND session_id = ? AND task_id = ?
	`, requestID, sessionID, taskID)
	req, err := scanApprovalRequest(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("approval request not found")
	}
	if err != nil {
		return nil, err
	}
	prevStatus := protocol.ApprovalStatus(req.Status)
	if prevStatus != protocol.ApprovalStatusApproved && prevStatus != protocol.ApprovalStatusRejected {
		return nil, fmt.Errorf("approval request is not resumable")
	}

	now := formatTime(time.Now().UTC())
	res, err := tx.Exec(`
		UPDATE approval_requests
		SET status = ?, consumed_at = ?, updated_at = ?
		WHERE id = ? AND session_id = ? AND task_id = ? AND status = ?
	`,
		string(protocol.ApprovalStatusConsumed), now, now,
		requestID, sessionID, taskID, req.Status,
	)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, fmt.Errorf("approval request is not resumable")
	}
	req.Status = string(protocol.ApprovalStatusConsumed)
	req.ConsumedAt = now
	req.UpdatedAt = now
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &ApprovalConsumeResult{
		Request:        &req,
		PreviousStatus: prevStatus,
	}, nil
}

func (s *ApprovalService) ExpirePendingRequest(sessionID, taskID, requestID string) error {
	if s == nil {
		return fmt.Errorf("approval service is nil")
	}
	now := formatTime(time.Now().UTC())
	_, err := s.db.Exec(`
		UPDATE approval_requests
		SET status = ?, updated_at = ?
		WHERE id = ? AND session_id = ? AND task_id = ? AND status = ?
	`,
		string(protocol.ApprovalStatusExpired), now,
		requestID, sessionID, taskID, string(protocol.ApprovalStatusPending),
	)
	return err
}

func (s *ApprovalService) ListSessionApprovals(sessionID string, statuses []protocol.ApprovalStatus) []protocol.ApprovalRequest {
	if s == nil || sessionID == "" {
		return nil
	}
	query := `
		SELECT id, session_id, task_id, category, risk_level, goal_summary, question,
		       options_json, recommended_option, recommendation_reason, reject_option_id,
		       status, selected_option_id, actor_channel, actor_ref,
		       expires_at, decided_at, consumed_at, created_at, updated_at
		FROM approval_requests
		WHERE session_id = ?
	`
	args := []any{sessionID}
	if len(statuses) > 0 {
		query += " AND status IN (" + placeholders(len(statuses)) + ")"
		for _, status := range statuses {
			args = append(args, string(status))
		}
	}
	query += " ORDER BY created_at ASC"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []protocol.ApprovalRequest
	for rows.Next() {
		req, err := scanApprovalRequest(rows)
		if err != nil {
			return nil
		}
		out = append(out, req)
	}
	return out
}

func (s *ApprovalService) GetRequest(sessionID, requestID string) (*protocol.ApprovalRequest, error) {
	if s == nil {
		return nil, fmt.Errorf("approval service is nil")
	}
	row := s.db.QueryRow(`
		SELECT id, session_id, task_id, category, risk_level, goal_summary, question,
		       options_json, recommended_option, recommendation_reason, reject_option_id,
		       status, selected_option_id, actor_channel, actor_ref,
		       expires_at, decided_at, consumed_at, created_at, updated_at
		FROM approval_requests
		WHERE id = ? AND session_id = ?
	`, requestID, sessionID)
	req, err := scanApprovalRequest(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &req, nil
}

type approvalScanner interface {
	Scan(dest ...any) error
}

func scanApprovalRequest(scanner approvalScanner) (protocol.ApprovalRequest, error) {
	var (
		req         protocol.ApprovalRequest
		optionsJSON string
		decidedAt   sql.NullString
		consumedAt  sql.NullString
	)
	err := scanner.Scan(
		&req.ID, &req.SessionID, &req.TaskID, &req.Category, &req.RiskLevel, &req.GoalSummary, &req.Question,
		&optionsJSON, &req.RecommendedOption, &req.RecommendationReason, &req.RejectOptionID,
		&req.Status, &req.SelectedOptionID, &req.ActorChannel, &req.ActorRef,
		&req.ExpiresAt, &decidedAt, &consumedAt, &req.CreatedAt, &req.UpdatedAt,
	)
	if err != nil {
		return protocol.ApprovalRequest{}, err
	}
	if err := json.Unmarshal([]byte(optionsJSON), &req.Options); err != nil {
		return protocol.ApprovalRequest{}, err
	}
	if decidedAt.Valid {
		req.DecidedAt = decidedAt.String
	}
	if consumedAt.Valid {
		req.ConsumedAt = consumedAt.String
	}
	return req, nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	parts := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		if i > 0 {
			parts = append(parts, ',')
		}
		parts = append(parts, '?')
	}
	return string(parts)
}

func optionExists(options []protocol.DecisionOption, optionID string) bool {
	for _, option := range options {
		if option.ID == optionID {
			return true
		}
	}
	return false
}
