package work

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent/internal/protocol"
)

const defaultPendingTTL = 30 * time.Minute

// PendingRegistry stores paused work tasks awaiting an Emotion decision.
type PendingRegistry struct {
	mu        sync.Mutex
	db        *sql.DB
	approvals *ApprovalService
	logger    *slog.Logger
	cfg       PendingRegistryConfig
}

// NewPendingRegistry constructs a SQLite-backed paused-task registry.
func NewPendingRegistry(db *sql.DB, approvals *ApprovalService, logger *slog.Logger, cfg PendingRegistryConfig) *PendingRegistry {
	if db == nil {
		return nil
	}
	return &PendingRegistry{
		db:        db,
		approvals: approvals,
		logger:    logger,
		cfg:       defaultPendingRegistryConfig(cfg),
	}
}

// Put adds or replaces a paused task for the given session/task pair.
func (r *PendingRegistry) Put(sessionID, taskID string, paused *PausedWork) error {
	if r == nil || sessionID == "" || taskID == "" || paused == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UTC()
	if paused.CreatedAt.IsZero() {
		paused.CreatedAt = now
	}
	blob := resumeBlobFromPaused(paused)
	failClosed := shouldFailClosed(paused.Packet)
	var approvalRequestID string
	if failClosed {
		if r.approvals == nil {
			return fmt.Errorf("approval service is required for fail-closed decisions")
		}
		req, err := r.approvals.CreateRequestFromDecision(sessionID, paused.Packet, now.Add(r.cfg.HardTTL))
		if err != nil {
			return fmt.Errorf("create approval request: %w", err)
		}
		approvalRequestID = req.ID
	}
	summary := buildDecisionSummary(statusPending, failClosed, blob, nil, now, true)
	if approvalRequestID != "" {
		summary.Approval = &protocol.ApprovalSummary{
			Required:  true,
			RequestID: approvalRequestID,
			Status:    string(protocol.ApprovalStatusPending),
			ExpiresAt: formatTime(now.Add(r.cfg.HardTTL)),
		}
	}

	_, err := r.db.Exec(`
		INSERT INTO pending_decisions (
			session_id, task_id, status, fail_closed, approval_request_id, category, risk_level,
			summary_json, resume_blob_json, report_json,
			resolved_decision, resolved_reason,
			created_at, status_entered_at, soft_expires_at, hard_expires_at, archive_after,
			claim_id, claim_expires_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, ?, ?, ?, ?, NULL, NULL, NULL, ?)
		ON CONFLICT(session_id, task_id) DO UPDATE SET
			status = excluded.status,
			fail_closed = excluded.fail_closed,
			approval_request_id = excluded.approval_request_id,
			category = excluded.category,
			risk_level = excluded.risk_level,
			summary_json = excluded.summary_json,
			resume_blob_json = excluded.resume_blob_json,
			report_json = NULL,
			resolved_decision = NULL,
			resolved_reason = NULL,
			created_at = excluded.created_at,
			status_entered_at = excluded.status_entered_at,
			soft_expires_at = excluded.soft_expires_at,
			hard_expires_at = excluded.hard_expires_at,
			archive_after = NULL,
			claim_id = NULL,
			claim_expires_at = NULL,
			updated_at = excluded.updated_at
	`,
		sessionID,
		taskID,
		statusPending,
		boolToInt(failClosed),
		nullStringValue(sql.NullString{String: approvalRequestID, Valid: approvalRequestID != ""}),
		string(paused.Packet.Category),
		paused.Packet.RiskLevel,
		marshalJSON(summary),
		marshalJSON(blob),
		formatTime(blob.CreatedAt),
		formatTime(now),
		formatTime(now.Add(r.cfg.SoftTTL)),
		formatTime(now.Add(r.cfg.HardTTL)),
		formatTime(now),
	)
	if err != nil {
		if r.logger != nil {
			r.logger.Error("pending put failed", "session_id", sessionID, "task_id", taskID, "error", err)
		}
		return err
	}
	return nil
}

// Take is a compatibility wrapper over claim-based resume.
func (r *PendingRegistry) Take(sessionID, taskID string) *PausedWork {
	if r == nil {
		return nil
	}
	result := r.ClaimForResume(sessionID, taskID)
	return result.PausedWork
}

func (r *PendingRegistry) fetchMainRow(sessionID, taskID string) (pendingDecisionRow, bool, error) {
	if r == nil {
		return pendingDecisionRow{}, false, nil
	}
	row := r.db.QueryRow(`
		SELECT
			session_id, task_id, status, fail_closed, approval_request_id, category, risk_level,
			summary_json, resume_blob_json, report_json,
			resolved_decision, resolved_reason,
			created_at, status_entered_at, soft_expires_at, hard_expires_at, archive_after,
			claim_id, claim_expires_at, updated_at
		FROM pending_decisions
		WHERE session_id = ? AND task_id = ?
	`, sessionID, taskID)

	var record pendingDecisionRow
	var failClosed int
	err := row.Scan(
		&record.SessionID,
		&record.TaskID,
		&record.Status,
		&failClosed,
		&record.ApprovalRequestID,
		&record.Category,
		&record.RiskLevel,
		&record.SummaryJSON,
		&record.ResumeBlobJSON,
		&record.ReportJSON,
		&record.ResolvedDecision,
		&record.ResolvedReason,
		&record.CreatedAt,
		&record.StatusEnteredAt,
		&record.SoftExpiresAt,
		&record.HardExpiresAt,
		&record.ArchiveAfter,
		&record.ClaimID,
		&record.ClaimExpiresAt,
		&record.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return pendingDecisionRow{}, false, nil
	}
	if err != nil {
		return pendingDecisionRow{}, false, err
	}
	record.FailClosed = failClosed != 0
	return record, true, nil
}

func (r *PendingRegistry) archivedExists(sessionID, taskID string) bool {
	if r == nil {
		return false
	}
	var n int
	err := r.db.QueryRow(`
		SELECT COUNT(1)
		FROM archived_decisions
		WHERE session_id = ? AND task_id = ?
	`, sessionID, taskID).Scan(&n)
	return err == nil && n > 0
}

// ClaimForResume claims a pending/stale row for resume.
func (r *PendingRegistry) ClaimForResume(sessionID, taskID string) ClaimResult {
	if r == nil || sessionID == "" || taskID == "" {
		return ClaimResult{FinalState: finalStateMissing}
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	record, ok, err := r.fetchMainRow(sessionID, taskID)
	if err != nil {
		if r.logger != nil {
			r.logger.Error("claim fetch failed", "session_id", sessionID, "task_id", taskID, "error", err)
		}
		return ClaimResult{FinalState: finalStateMissing}
	}
	if !ok {
		if r.archivedExists(sessionID, taskID) {
			return ClaimResult{FinalState: finalStateArchived}
		}
		return ClaimResult{FinalState: finalStateMissing}
	}

	now := time.Now().UTC()
	switch record.Status {
	case statusPending, statusStale:
		// continue
	default:
		return ClaimResult{FinalState: record.Status}
	}
	if hasActiveClaim(record, now) {
		return ClaimResult{FinalState: finalStateClaimed}
	}

	claimID := uuid.NewString()
	res, err := r.db.Exec(`
		UPDATE pending_decisions
		SET claim_id = ?, claim_expires_at = ?, updated_at = ?
		WHERE session_id = ? AND task_id = ? AND status IN (?, ?)
		  AND (claim_id IS NULL OR claim_expires_at IS NULL OR claim_expires_at <= ?)
	`,
		claimID,
		formatTime(now.Add(r.cfg.ResumeClaimTTL)),
		formatTime(now),
		sessionID,
		taskID,
		statusPending,
		statusStale,
		formatTime(now),
	)
	if err != nil {
		if r.logger != nil {
			r.logger.Error("claim update failed", "session_id", sessionID, "task_id", taskID, "error", err)
		}
		return ClaimResult{FinalState: finalStateMissing}
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ClaimResult{FinalState: finalStateClaimed}
	}

	blob, err := decodeResumeBlob(record.ResumeBlobJSON)
	if err != nil || blob == nil {
		if r.logger != nil {
			r.logger.Error("claim decode resume blob failed", "session_id", sessionID, "task_id", taskID, "error", err)
		}
		return ClaimResult{FinalState: finalStateMissing}
	}
	createdAt := blob.CreatedAt
	if createdAt.IsZero() {
		createdAt = parseTime(record.CreatedAt)
	}
	return ClaimResult{
		PausedWork:        blob.PausedWork(),
		ClaimID:           claimID,
		WasStale:          record.Status == statusStale,
		CreatedAt:         createdAt,
		FailClosed:        record.FailClosed,
		ApprovalRequestID: record.ApprovalRequestID.String,
	}
}

// ReleaseClaim clears a claim without changing the row status.
func (r *PendingRegistry) ReleaseClaim(sessionID, taskID, claimID string) error {
	if r == nil || sessionID == "" || taskID == "" || claimID == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err := r.db.Exec(`
		UPDATE pending_decisions
		SET claim_id = NULL, claim_expires_at = NULL, updated_at = ?
		WHERE session_id = ? AND task_id = ? AND claim_id = ?
	`,
		formatTime(time.Now().UTC()),
		sessionID,
		taskID,
		claimID,
	)
	return err
}

// FinalizeResolved stores the terminal report for a claimed row.
func (r *PendingRegistry) FinalizeResolved(sessionID, taskID, claimID string, resp protocol.DecisionResponse, report *protocol.TaskReport) error {
	if r == nil || sessionID == "" || taskID == "" || claimID == "" || report == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok, err := r.fetchMainRow(sessionID, taskID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	summary, err := hydrateSummary(record)
	if err != nil {
		return err
	}
	summary, err = r.attachApproval(summary, record)
	if err != nil {
		return err
	}
	summary.Status = statusResolved
	summary.Report = report
	summary.Claimable = false
	now := time.Now().UTC()

	_, err = r.db.Exec(`
		UPDATE pending_decisions
		SET status = ?, summary_json = ?, resume_blob_json = NULL, report_json = ?,
		    resolved_decision = ?, resolved_reason = ?,
		    status_entered_at = ?, archive_after = ?, claim_id = NULL, claim_expires_at = NULL, updated_at = ?
		WHERE session_id = ? AND task_id = ? AND claim_id = ?
	`,
		statusResolved,
		marshalJSON(summary),
		marshalJSON(report),
		resp.Decision,
		resp.Reason,
		formatTime(now),
		formatTime(now.Add(r.cfg.ArchiveTTL)),
		formatTime(now),
		sessionID,
		taskID,
		claimID,
	)
	return err
}

// RequeuePaused stores a newly paused snapshot after a claimed resume.
func (r *PendingRegistry) RequeuePaused(sessionID, taskID, claimID string, paused *PausedWork) error {
	if r == nil || sessionID == "" || taskID == "" || claimID == "" || paused == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	if paused.CreatedAt.IsZero() {
		paused.CreatedAt = now
	}
	blob := resumeBlobFromPaused(paused)
	failClosed := shouldFailClosed(paused.Packet)
	var approvalRequestID string
	if failClosed {
		if r.approvals == nil {
			return fmt.Errorf("approval service is required for fail-closed decisions")
		}
		req, err := r.approvals.CreateRequestFromDecision(sessionID, paused.Packet, now.Add(r.cfg.HardTTL))
		if err != nil {
			return fmt.Errorf("create approval request: %w", err)
		}
		approvalRequestID = req.ID
	}
	summary := buildDecisionSummary(statusPending, failClosed, blob, nil, now, true)
	if approvalRequestID != "" {
		summary.Approval = &protocol.ApprovalSummary{
			Required:  true,
			RequestID: approvalRequestID,
			Status:    string(protocol.ApprovalStatusPending),
			ExpiresAt: formatTime(now.Add(r.cfg.HardTTL)),
		}
	}

	_, err := r.db.Exec(`
		UPDATE pending_decisions
		SET status = ?, fail_closed = ?, approval_request_id = ?, category = ?, risk_level = ?,
		    summary_json = ?, resume_blob_json = ?, report_json = NULL,
		    resolved_decision = NULL, resolved_reason = NULL,
		    created_at = ?, status_entered_at = ?, soft_expires_at = ?, hard_expires_at = ?,
		    archive_after = NULL, claim_id = NULL, claim_expires_at = NULL, updated_at = ?
		WHERE session_id = ? AND task_id = ? AND claim_id = ?
	`,
		statusPending,
		boolToInt(failClosed),
		nullStringValue(sql.NullString{String: approvalRequestID, Valid: approvalRequestID != ""}),
		string(paused.Packet.Category),
		paused.Packet.RiskLevel,
		marshalJSON(summary),
		marshalJSON(blob),
		formatTime(blob.CreatedAt),
		formatTime(now),
		formatTime(now.Add(r.cfg.SoftTTL)),
		formatTime(now.Add(r.cfg.HardTTL)),
		formatTime(now),
		sessionID,
		taskID,
		claimID,
	)
	return err
}

// List returns resumable snapshots for pending/stale rows.
func (r *PendingRegistry) List(sessionID string) []*PausedWork {
	if r == nil || sessionID == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rows, err := r.listRows(sessionID, []string{statusPending, statusStale})
	if err != nil {
		return nil
	}
	out := make([]*PausedWork, 0, len(rows))
	for _, row := range rows {
		blob, err := decodeResumeBlob(row.ResumeBlobJSON)
		if err != nil || blob == nil {
			continue
		}
		out = append(out, blob.PausedWork())
	}
	return out
}

// ListInjectable returns Emotion-facing pending/stale summaries.
func (r *PendingRegistry) ListInjectable(sessionID string) []DecisionSummary {
	return r.ListDecisions(sessionID, []string{statusPending, statusStale})
}

// ListDecisions returns summaries for the selected statuses.
func (r *PendingRegistry) ListDecisions(sessionID string, statuses []string) []DecisionSummary {
	if r == nil || sessionID == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rows, err := r.listRows(sessionID, statuses)
	if err != nil {
		if r.logger != nil {
			r.logger.Error("list decisions failed", "session_id", sessionID, "error", err)
		}
		return nil
	}
	out := make([]DecisionSummary, 0, len(rows))
	for _, row := range rows {
		summary, err := hydrateSummary(row)
		if err != nil {
			continue
		}
		summary, err = r.attachApproval(summary, row)
		if err != nil {
			continue
		}
		out = append(out, summary)
	}
	return out
}

func (r *PendingRegistry) listRows(sessionID string, statuses []string) ([]pendingDecisionRow, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(statuses)), ",")
	args := make([]any, 0, len(statuses)+1)
	args = append(args, sessionID)
	for _, status := range statuses {
		args = append(args, status)
	}
	query := fmt.Sprintf(`
		SELECT
			session_id, task_id, status, fail_closed, approval_request_id, category, risk_level,
			summary_json, resume_blob_json, report_json,
			resolved_decision, resolved_reason,
			created_at, status_entered_at, soft_expires_at, hard_expires_at, archive_after,
			claim_id, claim_expires_at, updated_at
		FROM pending_decisions
		WHERE session_id = ? AND status IN (%s)
		ORDER BY created_at ASC
	`, placeholders)
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []pendingDecisionRow
	for rows.Next() {
		var row pendingDecisionRow
		var failClosed int
		if err := rows.Scan(
			&row.SessionID,
			&row.TaskID,
			&row.Status,
			&failClosed,
			&row.ApprovalRequestID,
			&row.Category,
			&row.RiskLevel,
			&row.SummaryJSON,
			&row.ResumeBlobJSON,
			&row.ReportJSON,
			&row.ResolvedDecision,
			&row.ResolvedReason,
			&row.CreatedAt,
			&row.StatusEnteredAt,
			&row.SoftExpiresAt,
			&row.HardExpiresAt,
			&row.ArchiveAfter,
			&row.ClaimID,
			&row.ClaimExpiresAt,
			&row.UpdatedAt,
		); err != nil {
			return nil, err
		}
		row.FailClosed = failClosed != 0
		out = append(out, row)
	}
	return out, rows.Err()
}

// ExpireOnce advances lifecycle states and returns the number of transitions.
func (r *PendingRegistry) ExpireOnce() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	_, _ = r.db.Exec(`
		UPDATE pending_decisions
		SET claim_id = NULL, claim_expires_at = NULL, updated_at = ?
		WHERE claim_id IS NOT NULL AND claim_expires_at IS NOT NULL AND claim_expires_at <= ?
	`, formatTime(now), formatTime(now))

	transitioned := 0
	for _, row := range r.transitionCandidates(statusPending, "soft_expires_at <= ?", formatTime(now)) {
		if hasActiveClaim(row, now) {
			continue
		}
		summary, err := hydrateSummary(row)
		if err != nil {
			continue
		}
		summary, err = r.attachApproval(summary, row)
		if err != nil {
			continue
		}
		summary.Status = statusStale
		summary.StatusEnteredAt = formatTime(now)
		res, err := r.db.Exec(`
			UPDATE pending_decisions
			SET status = ?, summary_json = ?, status_entered_at = ?, updated_at = ?
			WHERE session_id = ? AND task_id = ? AND status = ?
		`,
			statusStale,
			marshalJSON(summary),
			formatTime(now),
			formatTime(now),
			row.SessionID,
			row.TaskID,
			statusPending,
		)
		if err == nil {
			if n, _ := res.RowsAffected(); n > 0 {
				transitioned++
			}
		}
	}

	for _, row := range r.transitionCandidates("", "hard_expires_at <= ? AND status IN (?, ?)", formatTime(now), statusPending, statusStale) {
		if hasActiveClaim(row, now) {
			continue
		}
		switch {
		case row.FailClosed:
			if row.ApprovalRequestID.Valid && row.ApprovalRequestID.String != "" && r.approvals != nil {
				if err := r.approvals.ExpirePendingRequest(row.SessionID, row.TaskID, row.ApprovalRequestID.String); err != nil && r.logger != nil {
					r.logger.Warn("expire approval request failed", "session_id", row.SessionID, "task_id", row.TaskID, "approval_request_id", row.ApprovalRequestID.String, "error", err)
				}
			}
			blob, err := decodeResumeBlob(row.ResumeBlobJSON)
			if err != nil || blob == nil {
				continue
			}
			report := protocol.TaskReport{
				TaskID:    row.TaskID,
				Status:    "failed",
				Goal:      blob.Brief.Goal,
				Summary:   "Decision timeout: auto-rejected by fail-closed policy.",
				CreatedAt: now,
			}
			summary, err := hydrateSummary(row)
			if err != nil {
				continue
			}
			summary, err = r.attachApproval(summary, row)
			if err != nil {
				continue
			}
			summary.Status = statusAutoRejected
			summary.StatusEnteredAt = formatTime(now)
			summary.Report = &report
			res, err := r.db.Exec(`
				UPDATE pending_decisions
				SET status = ?, summary_json = ?, resume_blob_json = NULL, report_json = ?,
				    status_entered_at = ?, archive_after = ?, claim_id = NULL, claim_expires_at = NULL, updated_at = ?
				WHERE session_id = ? AND task_id = ? AND status IN (?, ?)
			`,
				statusAutoRejected,
				marshalJSON(summary),
				marshalJSON(report),
				formatTime(now),
				formatTime(now.Add(r.cfg.ArchiveTTL)),
				formatTime(now),
				row.SessionID,
				row.TaskID,
				statusPending,
				statusStale,
			)
			if err == nil {
				if n, _ := res.RowsAffected(); n > 0 {
					transitioned++
				}
			}
		default:
			summary, err := hydrateSummary(row)
			if err != nil {
				continue
			}
			summary, err = r.attachApproval(summary, row)
			if err != nil {
				continue
			}
			summary.Status = statusExpiredOpen
			summary.StatusEnteredAt = formatTime(now)
			res, err := r.db.Exec(`
				UPDATE pending_decisions
				SET status = ?, summary_json = ?, resume_blob_json = NULL,
				    status_entered_at = ?, archive_after = ?, claim_id = NULL, claim_expires_at = NULL, updated_at = ?
				WHERE session_id = ? AND task_id = ? AND status IN (?, ?)
			`,
				statusExpiredOpen,
				marshalJSON(summary),
				formatTime(now),
				formatTime(now.Add(r.cfg.ArchiveTTL)),
				formatTime(now),
				row.SessionID,
				row.TaskID,
				statusPending,
				statusStale,
			)
			if err == nil {
				if n, _ := res.RowsAffected(); n > 0 {
					transitioned++
				}
			}
		}
	}
	return transitioned
}

func (r *PendingRegistry) transitionCandidates(status string, predicate string, args ...any) []pendingDecisionRow {
	if r == nil {
		return nil
	}
	query := `
		SELECT
			session_id, task_id, status, fail_closed, approval_request_id, category, risk_level,
			summary_json, resume_blob_json, report_json,
			resolved_decision, resolved_reason,
			created_at, status_entered_at, soft_expires_at, hard_expires_at, archive_after,
			claim_id, claim_expires_at, updated_at
		FROM pending_decisions
		WHERE ` + predicate
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []pendingDecisionRow
	for rows.Next() {
		var row pendingDecisionRow
		var failClosed int
		if err := rows.Scan(
			&row.SessionID,
			&row.TaskID,
			&row.Status,
			&failClosed,
			&row.ApprovalRequestID,
			&row.Category,
			&row.RiskLevel,
			&row.SummaryJSON,
			&row.ResumeBlobJSON,
			&row.ReportJSON,
			&row.ResolvedDecision,
			&row.ResolvedReason,
			&row.CreatedAt,
			&row.StatusEnteredAt,
			&row.SoftExpiresAt,
			&row.HardExpiresAt,
			&row.ArchiveAfter,
			&row.ClaimID,
			&row.ClaimExpiresAt,
			&row.UpdatedAt,
		); err != nil {
			continue
		}
		row.FailClosed = failClosed != 0
		out = append(out, row)
	}
	return out
}

// ArchiveOnce moves terminal rows into archived_decisions and returns the number of rows archived.
func (r *PendingRegistry) ArchiveOnce() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	rows := r.transitionCandidates("", "archive_after IS NOT NULL AND archive_after <= ? AND status IN (?, ?, ?)",
		formatTime(now), statusExpiredOpen, statusAutoRejected, statusResolved)
	if len(rows) == 0 {
		return 0
	}

	tx, err := r.db.Begin()
	if err != nil {
		return 0
	}
	defer tx.Rollback()

	archived := 0
	for _, row := range rows {
		_, err := tx.Exec(`
			INSERT INTO archived_decisions (
				session_id, task_id, final_status, fail_closed,
				approval_request_id, category, risk_level, summary_json, report_json,
				resolved_decision, resolved_reason,
				created_at, status_entered_at, archived_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(session_id, task_id) DO UPDATE SET
				final_status = excluded.final_status,
				fail_closed = excluded.fail_closed,
				approval_request_id = excluded.approval_request_id,
				category = excluded.category,
				risk_level = excluded.risk_level,
				summary_json = excluded.summary_json,
				report_json = excluded.report_json,
				resolved_decision = excluded.resolved_decision,
				resolved_reason = excluded.resolved_reason,
				created_at = excluded.created_at,
				status_entered_at = excluded.status_entered_at,
				archived_at = excluded.archived_at
		`,
			row.SessionID,
			row.TaskID,
			row.Status,
			boolToInt(row.FailClosed),
			nullStringValue(row.ApprovalRequestID),
			row.Category,
			row.RiskLevel,
			row.SummaryJSON,
			nullStringValue(row.ReportJSON),
			nullStringValue(row.ResolvedDecision),
			nullStringValue(row.ResolvedReason),
			row.CreatedAt,
			row.StatusEnteredAt,
			formatTime(now),
		)
		if err != nil {
			return 0
		}
		_, err = tx.Exec(`DELETE FROM pending_decisions WHERE session_id = ? AND task_id = ?`, row.SessionID, row.TaskID)
		if err != nil {
			return 0
		}
		archived++
	}
	if err := tx.Commit(); err != nil {
		return 0
	}
	return archived
}

func (r *PendingRegistry) attachApproval(summary DecisionSummary, row pendingDecisionRow) (DecisionSummary, error) {
	if !row.ApprovalRequestID.Valid || row.ApprovalRequestID.String == "" {
		return summary, nil
	}
	if summary.Approval == nil {
		summary.Approval = &protocol.ApprovalSummary{
			Required:  true,
			RequestID: row.ApprovalRequestID.String,
		}
	}
	if r == nil || r.approvals == nil {
		return summary, nil
	}
	req, err := r.approvals.GetRequest(row.SessionID, row.ApprovalRequestID.String)
	if err != nil {
		return summary, err
	}
	if req == nil {
		return summary, nil
	}
	summary.Approval.Required = true
	summary.Approval.RequestID = req.ID
	summary.Approval.Status = req.Status
	summary.Approval.SelectedOptionID = req.SelectedOptionID
	summary.Approval.ExpiresAt = req.ExpiresAt
	return summary, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullStringValue(v sql.NullString) any {
	if !v.Valid {
		return nil
	}
	return v.String
}
