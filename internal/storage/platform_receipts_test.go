package storage

import (
	"context"
	"errors"
	"testing"
)

func TestPlatformMessageReceiptsDeduplicateHandledMessage(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	msg := PlatformMessageReceiptRecord{
		ExternalMessageID:      "message-1",
		SourceType:             "napcat",
		AdapterInstanceID:      "main",
		PlatformID:             "qq",
		ChannelType:            "private",
		OriginKey:              "napcat:main:private:10001",
		ExternalConversationID: "10001",
		ExternalActorID:        "10001",
		PersonaKey:             "default",
		Text:                   "/sid",
		RawEventHash:           "hash-1",
	}

	first, err := db.BeginPlatformMessageReceipt(ctx, msg)
	if err != nil {
		t.Fatalf("BeginPlatformMessageReceipt first: %v", err)
	}
	if first.ID == "" || first.Duplicate || first.Status != PlatformMessageReceiptStatusProcessing {
		t.Fatalf("first receipt = %#v, want new processing", first)
	}
	if err := db.CompletePlatformMessageReceipt(ctx, first.ID, "session-1", "command_result"); err != nil {
		t.Fatalf("CompletePlatformMessageReceipt: %v", err)
	}

	duplicate, err := db.BeginPlatformMessageReceipt(ctx, msg)
	if err != nil {
		t.Fatalf("BeginPlatformMessageReceipt duplicate: %v", err)
	}
	if duplicate.ID != first.ID || !duplicate.Duplicate || duplicate.Status != PlatformMessageReceiptStatusDuplicate {
		t.Fatalf("duplicate receipt = %#v, want duplicate of %q", duplicate, first.ID)
	}
}

func TestPlatformMessageReceiptFailRecordsError(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	msg := PlatformMessageReceiptRecord{
		ExternalMessageID: "message-fail",
		SourceType:        "napcat",
		AdapterInstanceID: "main",
		PlatformID:        "qq",
		ChannelType:       "private",
		OriginKey:         "napcat:main:private:10001",
		Text:              "hello",
	}

	receipt, err := db.BeginPlatformMessageReceipt(ctx, msg)
	if err != nil {
		t.Fatalf("BeginPlatformMessageReceipt: %v", err)
	}
	if err := db.FailPlatformMessageReceipt(ctx, receipt.ID, "session-1", errors.New("boom")); err != nil {
		t.Fatalf("FailPlatformMessageReceipt: %v", err)
	}

	var status, sessionID, errorMessage string
	if err := db.SqlDB().QueryRowContext(ctx, `
		SELECT status, session_id, error_message
		FROM platform_message_receipts
		WHERE id = ?
	`, receipt.ID).Scan(&status, &sessionID, &errorMessage); err != nil {
		t.Fatalf("query receipt: %v", err)
	}
	if status != "failed" || sessionID != "session-1" || errorMessage != "boom" {
		t.Fatalf("receipt row status/session/error = %q/%q/%q", status, sessionID, errorMessage)
	}
}
