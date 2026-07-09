package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type PlatformMessageReceiptStatus string

const (
	PlatformMessageReceiptStatusProcessing PlatformMessageReceiptStatus = "processing"
	PlatformMessageReceiptStatusHandled    PlatformMessageReceiptStatus = "handled"
	PlatformMessageReceiptStatusDuplicate  PlatformMessageReceiptStatus = "duplicate"
	PlatformMessageReceiptStatusFailed     PlatformMessageReceiptStatus = "failed"
	PlatformMessageReceiptStatusIgnored    PlatformMessageReceiptStatus = "ignored"
)

type PlatformMessageReceiptRecord struct {
	ID                     string
	SourceType             string
	AdapterInstanceID      string
	PlatformID             string
	ExternalMessageID      string
	OriginKey              string
	SessionID              string
	PersonaKey             string
	ChannelType            string
	ExternalConversationID string
	ExternalActorID        string
	Text                   string
	RawEventHash           string
	Status                 PlatformMessageReceiptStatus
	ResultType             string
	ErrorMessage           string
	Duplicate              bool
	Retry                  bool
	DuplicateKind          string
	ExistingStatus         PlatformMessageReceiptStatus
	AttemptCount           int
	LastAttemptAt          string
	TurnID                 string
	AgentID                string
	ResolvedPersonaKey     string
	ReceivedAt             string
	HandledAt              string
}

type PlatformMessageReceiptCompletion struct {
	ReceiptID          string
	SessionID          string
	ResultType         string
	TurnID             string
	AgentID            string
	ResolvedPersonaKey string
}

const platformReceiptProcessingStaleAfter = 5 * time.Minute

func (d *DB) BeginPlatformMessageReceipt(ctx context.Context, receipt PlatformMessageReceiptRecord) (PlatformMessageReceiptRecord, error) {
	receipt = normalizePlatformMessageReceipt(receipt)
	if receipt.SourceType == "" || receipt.ExternalMessageID == "" || receipt.OriginKey == "" {
		return PlatformMessageReceiptRecord{}, errors.New("source type, external message id, and origin key are required")
	}
	existing, err := d.getPlatformMessageReceiptByExternalID(ctx, receipt.SourceType, receipt.AdapterInstanceID, receipt.ExternalMessageID)
	if err != nil {
		return PlatformMessageReceiptRecord{}, err
	}
	if existing != nil {
		existing.ExistingStatus = existing.Status
		switch existing.Status {
		case PlatformMessageReceiptStatusFailed:
			return d.retryPlatformMessageReceipt(ctx, *existing, "previous_failed")
		case PlatformMessageReceiptStatusProcessing:
			if platformReceiptIsStale(*existing) {
				return d.retryPlatformMessageReceipt(ctx, *existing, "stale_processing")
			}
			existing.Duplicate = true
			existing.DuplicateKind = "running"
			existing.Status = PlatformMessageReceiptStatusDuplicate
		case PlatformMessageReceiptStatusIgnored:
			existing.Duplicate = true
			existing.DuplicateKind = "ignored"
			existing.Status = PlatformMessageReceiptStatusDuplicate
		default:
			existing.Duplicate = true
			existing.DuplicateKind = "handled"
			existing.Status = PlatformMessageReceiptStatusDuplicate
		}
		return *existing, nil
	}
	now := d.nowText()
	_, err = d.db.ExecContext(ctx, `
		INSERT INTO platform_message_receipts (
			id, source_type, adapter_instance_id, platform_id, external_message_id,
			origin_key, session_id, persona_key, message_hash, status, result_type, error_message,
			attempt_count, last_attempt_at, received_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'processing', '', '', 1, ?, ?)
	`, receipt.ID, receipt.SourceType, receipt.AdapterInstanceID, receipt.PlatformID, receipt.ExternalMessageID,
		receipt.OriginKey, receipt.SessionID, receipt.PersonaKey, receipt.RawEventHash, now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			existing, getErr := d.getPlatformMessageReceiptByExternalID(ctx, receipt.SourceType, receipt.AdapterInstanceID, receipt.ExternalMessageID)
			if getErr != nil {
				return PlatformMessageReceiptRecord{}, getErr
			}
			if existing != nil {
				existing.Duplicate = true
				existing.DuplicateKind = "running"
				existing.ExistingStatus = existing.Status
				existing.Status = PlatformMessageReceiptStatusDuplicate
				return *existing, nil
			}
		}
		return PlatformMessageReceiptRecord{}, err
	}
	receipt.Status = PlatformMessageReceiptStatusProcessing
	receipt.AttemptCount = 1
	receipt.LastAttemptAt = now
	receipt.ReceivedAt = now
	return receipt, nil
}

func (d *DB) CompletePlatformMessageReceipt(ctx context.Context, receiptID string, sessionID string, resultType string) error {
	receiptID = strings.TrimSpace(receiptID)
	if receiptID == "" {
		return errors.New("receipt id is required")
	}
	_, err := d.db.ExecContext(ctx, `
		UPDATE platform_message_receipts
		SET status = 'handled', session_id = ?, result_type = ?, error_message = '', handled_at = ?
		WHERE id = ?
	`, sessionID, resultType, d.nowText(), receiptID)
	return err
}

func (d *DB) CompletePlatformMessageReceiptWithMeta(ctx context.Context, completion PlatformMessageReceiptCompletion) error {
	completion.ReceiptID = strings.TrimSpace(completion.ReceiptID)
	if completion.ReceiptID == "" {
		return errors.New("receipt id is required")
	}
	_, err := d.db.ExecContext(ctx, `
		UPDATE platform_message_receipts
		SET status = 'handled',
		    session_id = ?,
		    result_type = ?,
		    error_message = '',
		    handled_at = ?,
		    turn_id = ?,
		    agent_id = ?,
		    resolved_persona_key = ?
		WHERE id = ?
	`, completion.SessionID, completion.ResultType, d.nowText(), completion.TurnID,
		completion.AgentID, completion.ResolvedPersonaKey, completion.ReceiptID)
	return err
}

func (d *DB) FailPlatformMessageReceipt(ctx context.Context, receiptID string, sessionID string, err error) error {
	receiptID = strings.TrimSpace(receiptID)
	if receiptID == "" {
		return errors.New("receipt id is required")
	}
	_, execErr := d.db.ExecContext(ctx, `
		UPDATE platform_message_receipts
		SET status = 'failed', session_id = ?, error_message = ?, handled_at = ?
		WHERE id = ?
	`, sessionID, errorString(err), d.nowText(), receiptID)
	return execErr
}

func (d *DB) retryPlatformMessageReceipt(ctx context.Context, existing PlatformMessageReceiptRecord, duplicateKind string) (PlatformMessageReceiptRecord, error) {
	now := d.nowText()
	_, err := d.db.ExecContext(ctx, `
		UPDATE platform_message_receipts
		SET status = 'processing',
		    result_type = '',
		    error_message = '',
		    handled_at = NULL,
		    turn_id = '',
		    agent_id = '',
		    resolved_persona_key = '',
		    attempt_count = CASE WHEN attempt_count < 1 THEN 2 ELSE attempt_count + 1 END,
		    last_attempt_at = ?
		WHERE id = ?
	`, now, existing.ID)
	if err != nil {
		return PlatformMessageReceiptRecord{}, err
	}
	existing.Status = PlatformMessageReceiptStatusProcessing
	existing.Retry = true
	existing.DuplicateKind = duplicateKind
	existing.AttemptCount++
	if existing.AttemptCount <= 1 {
		existing.AttemptCount = 2
	}
	existing.LastAttemptAt = now
	existing.ResultType = ""
	existing.ErrorMessage = ""
	existing.HandledAt = ""
	return existing, nil
}

func (d *DB) getPlatformMessageReceiptByExternalID(ctx context.Context, sourceType string, adapterInstanceID string, externalMessageID string) (*PlatformMessageReceiptRecord, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT id, source_type, adapter_instance_id, platform_id, external_message_id,
		       origin_key, session_id, persona_key, message_hash, status, result_type, error_message,
		       COALESCE(attempt_count, 1), COALESCE(last_attempt_at, ''),
		       COALESCE(turn_id, ''), COALESCE(agent_id, ''), COALESCE(resolved_persona_key, ''),
		       received_at, COALESCE(handled_at, '')
		FROM platform_message_receipts
		WHERE source_type = ? AND adapter_instance_id = ? AND external_message_id = ?
	`, strings.TrimSpace(sourceType), strings.TrimSpace(adapterInstanceID), strings.TrimSpace(externalMessageID))
	record, err := scanPlatformMessageReceipt(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func scanPlatformMessageReceipt(row scanner) (PlatformMessageReceiptRecord, error) {
	var record PlatformMessageReceiptRecord
	err := row.Scan(&record.ID, &record.SourceType, &record.AdapterInstanceID, &record.PlatformID,
		&record.ExternalMessageID, &record.OriginKey, &record.SessionID, &record.PersonaKey,
		&record.RawEventHash, &record.Status, &record.ResultType, &record.ErrorMessage,
		&record.AttemptCount, &record.LastAttemptAt, &record.TurnID, &record.AgentID, &record.ResolvedPersonaKey,
		&record.ReceivedAt, &record.HandledAt)
	return record, err
}

func normalizePlatformMessageReceipt(receipt PlatformMessageReceiptRecord) PlatformMessageReceiptRecord {
	receipt.ID = firstNonEmptyConversationValue(receipt.ID, uuid.NewString())
	receipt.SourceType = strings.TrimSpace(receipt.SourceType)
	receipt.AdapterInstanceID = strings.TrimSpace(receipt.AdapterInstanceID)
	receipt.PlatformID = strings.TrimSpace(receipt.PlatformID)
	receipt.ExternalMessageID = strings.TrimSpace(receipt.ExternalMessageID)
	receipt.OriginKey = strings.TrimSpace(receipt.OriginKey)
	receipt.SessionID = strings.TrimSpace(receipt.SessionID)
	receipt.PersonaKey = strings.TrimSpace(receipt.PersonaKey)
	receipt.RawEventHash = strings.TrimSpace(receipt.RawEventHash)
	return receipt
}

func platformReceiptIsStale(receipt PlatformMessageReceiptRecord) bool {
	at := firstNonEmptyConversationValue(receipt.LastAttemptAt, receipt.ReceivedAt)
	if at == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return false
	}
	return time.Since(parsed) > platformReceiptProcessingStaleAfter
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprint(err)
}
