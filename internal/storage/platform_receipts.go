package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

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
	ReceivedAt             string
	HandledAt              string
}

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
		existing.Duplicate = true
		existing.Status = PlatformMessageReceiptStatusDuplicate
		return *existing, nil
	}
	_, err = d.db.ExecContext(ctx, `
		INSERT INTO platform_message_receipts (
			id, source_type, adapter_instance_id, platform_id, external_message_id,
			origin_key, session_id, persona_key, message_hash, status, result_type, error_message, received_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'processing', '', '', ?)
	`, receipt.ID, receipt.SourceType, receipt.AdapterInstanceID, receipt.PlatformID, receipt.ExternalMessageID,
		receipt.OriginKey, receipt.SessionID, receipt.PersonaKey, receipt.RawEventHash, d.nowText())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			existing, getErr := d.getPlatformMessageReceiptByExternalID(ctx, receipt.SourceType, receipt.AdapterInstanceID, receipt.ExternalMessageID)
			if getErr != nil {
				return PlatformMessageReceiptRecord{}, getErr
			}
			if existing != nil {
				existing.Duplicate = true
				existing.Status = PlatformMessageReceiptStatusDuplicate
				return *existing, nil
			}
		}
		return PlatformMessageReceiptRecord{}, err
	}
	receipt.Status = PlatformMessageReceiptStatusProcessing
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

func (d *DB) getPlatformMessageReceiptByExternalID(ctx context.Context, sourceType string, adapterInstanceID string, externalMessageID string) (*PlatformMessageReceiptRecord, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT id, source_type, adapter_instance_id, platform_id, external_message_id,
		       origin_key, session_id, persona_key, message_hash, status, result_type, error_message,
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

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprint(err)
}
