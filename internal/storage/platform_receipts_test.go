package storage

import (
	"context"
	"errors"
	"testing"
	"time"
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

func TestPlatformReceiptFailedCanRetry(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	msg := PlatformMessageReceiptRecord{
		ExternalMessageID: "message-retry-failed",
		SourceType:        "napcat",
		AdapterInstanceID: "main",
		OriginKey:         "napcat:main:private:10001",
		Text:              "hello",
	}
	first, err := db.BeginPlatformMessageReceipt(ctx, msg)
	if err != nil {
		t.Fatalf("BeginPlatformMessageReceipt first: %v", err)
	}
	if err := db.FailPlatformMessageReceipt(ctx, first.ID, "session-1", errors.New("boom")); err != nil {
		t.Fatalf("FailPlatformMessageReceipt: %v", err)
	}

	retry, err := db.BeginPlatformMessageReceipt(ctx, msg)
	if err != nil {
		t.Fatalf("BeginPlatformMessageReceipt retry: %v", err)
	}
	if retry.ID != first.ID || !retry.Retry || retry.Duplicate || retry.Status != PlatformMessageReceiptStatusProcessing || retry.AttemptCount != 2 {
		t.Fatalf("retry receipt = %#v, want same row retry processing attempt 2", retry)
	}
}

func TestPlatformReceiptFreshProcessingReturnsRunningDuplicate(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	msg := PlatformMessageReceiptRecord{
		ExternalMessageID: "message-running",
		SourceType:        "napcat",
		AdapterInstanceID: "main",
		OriginKey:         "napcat:main:private:10001",
		Text:              "hello",
	}
	first, err := db.BeginPlatformMessageReceipt(ctx, msg)
	if err != nil {
		t.Fatalf("BeginPlatformMessageReceipt first: %v", err)
	}

	duplicate, err := db.BeginPlatformMessageReceipt(ctx, msg)
	if err != nil {
		t.Fatalf("BeginPlatformMessageReceipt duplicate: %v", err)
	}
	if duplicate.ID != first.ID || !duplicate.Duplicate || duplicate.Retry || duplicate.DuplicateKind != "running" {
		t.Fatalf("duplicate receipt = %#v, want running duplicate", duplicate)
	}
}

func TestPlatformReceiptStaleProcessingCanRetry(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	msg := PlatformMessageReceiptRecord{
		ExternalMessageID: "message-stale",
		SourceType:        "napcat",
		AdapterInstanceID: "main",
		OriginKey:         "napcat:main:private:10001",
		Text:              "hello",
	}
	first, err := db.BeginPlatformMessageReceipt(ctx, msg)
	if err != nil {
		t.Fatalf("BeginPlatformMessageReceipt first: %v", err)
	}
	oldAttempt := db.formatTime(time.Now().Add(-10 * time.Minute))
	if _, err := db.SqlDB().ExecContext(ctx, `
		UPDATE platform_message_receipts
		SET last_attempt_at = ?, received_at = ?
		WHERE id = ?
	`, oldAttempt, oldAttempt, first.ID); err != nil {
		t.Fatalf("mark stale: %v", err)
	}

	retry, err := db.BeginPlatformMessageReceipt(ctx, msg)
	if err != nil {
		t.Fatalf("BeginPlatformMessageReceipt retry: %v", err)
	}
	if retry.ID != first.ID || !retry.Retry || retry.DuplicateKind != "stale_processing" || retry.AttemptCount != 2 {
		t.Fatalf("retry receipt = %#v, want stale retry attempt 2", retry)
	}
}

func TestPlatformReceiptCompletionStoresTurnAgentPersonaMetadata(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	msg := PlatformMessageReceiptRecord{
		ExternalMessageID: "message-meta",
		SourceType:        "napcat",
		AdapterInstanceID: "main",
		OriginKey:         "napcat:main:private:10001",
		Text:              "hello",
	}
	receipt, err := db.BeginPlatformMessageReceipt(ctx, msg)
	if err != nil {
		t.Fatalf("BeginPlatformMessageReceipt: %v", err)
	}
	if err := db.CompletePlatformMessageReceiptWithMeta(ctx, PlatformMessageReceiptCompletion{
		ReceiptID:          receipt.ID,
		SessionID:          "session-1",
		ResultType:         "message",
		TurnID:             "turn-1",
		AgentID:            "Chat",
		ResolvedPersonaKey: "Xia",
	}); err != nil {
		t.Fatalf("CompletePlatformMessageReceiptWithMeta: %v", err)
	}

	var turnID, agentID, personaKey string
	if err := db.SqlDB().QueryRowContext(ctx, `
		SELECT turn_id, agent_id, resolved_persona_key
		FROM platform_message_receipts
		WHERE id = ?
	`, receipt.ID).Scan(&turnID, &agentID, &personaKey); err != nil {
		t.Fatalf("query receipt metadata: %v", err)
	}
	if turnID != "turn-1" || agentID != "Chat" || personaKey != "Xia" {
		t.Fatalf("metadata = %q/%q/%q, want turn-1/Chat/Xia", turnID, agentID, personaKey)
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
