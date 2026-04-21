package work

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/protocol"
)

const (
	statusPending      = "pending"
	statusStale        = "stale"
	statusExpiredOpen  = "expired_open"
	statusAutoRejected = "auto_rejected"
	statusResolved     = "resolved"

	finalStateMissing  = "missing"
	finalStateArchived = "archived"
	finalStateClaimed  = "claimed"
)

type PendingRegistryConfig struct {
	SoftTTL        time.Duration
	HardTTL        time.Duration
	ArchiveTTL     time.Duration
	ResumeClaimTTL time.Duration
}

type ResumeBlob struct {
	TaskID          string                  `json:"task_id"`
	Brief           protocol.TaskBrief      `json:"brief"`
	Messages        []llm.Message           `json:"messages"`
	PendingCallID   string                  `json:"pending_call_id"`
	Packet          protocol.DecisionPacket `json:"packet"`
	Round           int                     `json:"round"`
	EscalationCount int                     `json:"escalation_count"`
	CreatedAt       time.Time               `json:"created_at"`
}

type DecisionSummary = protocol.DecisionSummary

type ClaimResult struct {
	PausedWork        *PausedWork
	ClaimID           string
	WasStale          bool
	CreatedAt         time.Time
	FailClosed        bool
	ApprovalRequestID string
	FinalState        string
}

type pendingDecisionRow struct {
	SessionID         string
	TaskID            string
	Status            string
	FailClosed        bool
	ApprovalRequestID sql.NullString
	Category          string
	RiskLevel         string
	SummaryJSON       string
	ResumeBlobJSON    sql.NullString
	ReportJSON        sql.NullString
	ResolvedDecision  sql.NullString
	ResolvedReason    sql.NullString
	CreatedAt         string
	StatusEnteredAt   string
	SoftExpiresAt     sql.NullString
	HardExpiresAt     sql.NullString
	ArchiveAfter      sql.NullString
	ClaimID           sql.NullString
	ClaimExpiresAt    sql.NullString
	UpdatedAt         string
}

func defaultPendingRegistryConfig(cfg PendingRegistryConfig) PendingRegistryConfig {
	if cfg.SoftTTL <= 0 {
		cfg.SoftTTL = defaultPendingTTL
	}
	if cfg.HardTTL <= cfg.SoftTTL {
		cfg.HardTTL = time.Hour
	}
	if cfg.ArchiveTTL <= 0 {
		cfg.ArchiveTTL = 24 * time.Hour
	}
	if cfg.ResumeClaimTTL <= 0 {
		cfg.ResumeClaimTTL = 10 * time.Minute
	}
	return cfg
}

func shouldFailClosed(packet protocol.DecisionPacket) bool {
	if packet.RiskLevel == "high" {
		return true
	}
	switch packet.Category {
	case protocol.CatIrreversible, protocol.CatHighRisk:
		return true
	default:
		return false
	}
}

func resumeBlobFromPaused(paused *PausedWork) ResumeBlob {
	return ResumeBlob{
		TaskID:          paused.TaskID,
		Brief:           paused.Brief,
		Messages:        append([]llm.Message(nil), paused.Messages...),
		PendingCallID:   paused.PendingCallID,
		Packet:          paused.Packet,
		Round:           paused.Round,
		EscalationCount: paused.EscalationCount,
		CreatedAt:       paused.CreatedAt,
	}
}

func (b ResumeBlob) PausedWork() *PausedWork {
	return &PausedWork{
		TaskID:          b.TaskID,
		Brief:           b.Brief,
		Messages:        append([]llm.Message(nil), b.Messages...),
		PendingCallID:   b.PendingCallID,
		Packet:          b.Packet,
		Round:           b.Round,
		EscalationCount: b.EscalationCount,
		CreatedAt:       b.CreatedAt,
	}
}

func buildDecisionSummary(status string, failClosed bool, blob ResumeBlob, report *protocol.TaskReport, statusEnteredAt time.Time, claimable bool) DecisionSummary {
	return DecisionSummary{
		TaskID:          blob.TaskID,
		Status:          status,
		FailClosed:      failClosed,
		Category:        string(blob.Packet.Category),
		RiskLevel:       blob.Packet.RiskLevel,
		GoalSummary:     blob.Packet.GoalSummary,
		Question:        blob.Packet.Question,
		Options:         append([]protocol.DecisionOption(nil), blob.Packet.Options...),
		Report:          report,
		CreatedAt:       formatTime(blob.CreatedAt),
		StatusEnteredAt: formatTime(statusEnteredAt),
		Claimable:       claimable,
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func parseNullableTime(value sql.NullString) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return parseTime(value.String)
}

func hasActiveClaim(row pendingDecisionRow, now time.Time) bool {
	return row.ClaimID.Valid && parseNullableTime(row.ClaimExpiresAt).After(now)
}

func marshalJSON(v any) string {
	payload, _ := json.Marshal(v)
	return string(payload)
}

func decodeResumeBlob(raw sql.NullString) (*ResumeBlob, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var blob ResumeBlob
	if err := json.Unmarshal([]byte(raw.String), &blob); err != nil {
		return nil, fmt.Errorf("unmarshal resume_blob_json: %w", err)
	}
	return &blob, nil
}

func decodeReport(raw sql.NullString) (*protocol.TaskReport, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var report protocol.TaskReport
	if err := json.Unmarshal([]byte(raw.String), &report); err != nil {
		return nil, fmt.Errorf("unmarshal report_json: %w", err)
	}
	return &report, nil
}

func decodeSummary(raw string) (DecisionSummary, error) {
	var summary DecisionSummary
	if err := json.Unmarshal([]byte(raw), &summary); err != nil {
		return DecisionSummary{}, fmt.Errorf("unmarshal summary_json: %w", err)
	}
	return summary, nil
}

func hydrateSummary(row pendingDecisionRow) (DecisionSummary, error) {
	summary, err := decodeSummary(row.SummaryJSON)
	if err != nil {
		return DecisionSummary{}, err
	}
	summary.Status = row.Status
	summary.FailClosed = row.FailClosed
	summary.Category = row.Category
	summary.RiskLevel = row.RiskLevel
	summary.StatusEnteredAt = row.StatusEnteredAt
	summary.Claimable = !hasActiveClaim(row, time.Now().UTC())

	report, err := decodeReport(row.ReportJSON)
	if err != nil {
		return DecisionSummary{}, err
	}
	summary.Report = report
	if row.ApprovalRequestID.Valid && row.ApprovalRequestID.String != "" {
		if summary.Approval == nil {
			summary.Approval = &protocol.ApprovalSummary{}
		}
		summary.Approval.Required = true
		summary.Approval.RequestID = row.ApprovalRequestID.String
	}
	return summary, nil
}
