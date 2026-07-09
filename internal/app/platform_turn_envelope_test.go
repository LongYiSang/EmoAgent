package app

import (
	"testing"

	"github.com/longyisang/emoagent/internal/platform"
	"github.com/longyisang/emoagent/internal/turn"
)

func TestPlatformTurnEnvelopeUsesExternalMessageIDForSourceEventID(t *testing.T) {
	env := platformTurnEnvelope(platform.InboundMessage{
		ExternalMessageID: "external-1",
		ID:                "inbound-1",
		RawEventHash:      "hash-1",
		Text:              "hello",
	}, "session-1", "default")
	if env.SourceEventID != "external-1" {
		t.Fatalf("SourceEventID = %q, want external-1", env.SourceEventID)
	}
}

func TestPlatformTurnEnvelopeFallsBackToInboundID(t *testing.T) {
	env1 := platformTurnEnvelope(platform.InboundMessage{ID: "inbound-1", RawEventHash: "hash-1", Text: "hello"}, "session-1", "default")
	env2 := platformTurnEnvelope(platform.InboundMessage{ID: "inbound-1", RawEventHash: "hash-2", Text: "hello again"}, "session-1", "default")
	if env1.SourceEventID != "inbound-1" {
		t.Fatalf("SourceEventID = %q, want inbound-1", env1.SourceEventID)
	}
	if env1.IdempotencyKey != env2.IdempotencyKey {
		t.Fatalf("IdempotencyKey mismatch = %q/%q, want stable inbound key", env1.IdempotencyKey, env2.IdempotencyKey)
	}
}

func TestPlatformTurnEnvelopeFallsBackToRawEventHash(t *testing.T) {
	env1 := platformTurnEnvelope(platform.InboundMessage{RawEventHash: "hash-1", Text: "hello"}, "session-1", "default")
	env2 := platformTurnEnvelope(platform.InboundMessage{RawEventHash: "hash-1", Text: "hello again"}, "session-1", "default")
	if env1.SourceEventID != "hash-1" {
		t.Fatalf("SourceEventID = %q, want hash-1", env1.SourceEventID)
	}
	if env1.IdempotencyKey != env2.IdempotencyKey {
		t.Fatalf("IdempotencyKey mismatch = %q/%q, want stable hash key", env1.IdempotencyKey, env2.IdempotencyKey)
	}
}

func TestPlatformTurnEnvelopeWithoutAnyIDRemainsEphemeral(t *testing.T) {
	env1 := platformTurnEnvelope(platform.InboundMessage{Text: "hello"}, "session-1", "default")
	env2 := platformTurnEnvelope(platform.InboundMessage{Text: "hello"}, "session-1", "default")
	if env1.SourceEventID != "" || env2.SourceEventID != "" {
		t.Fatalf("SourceEventID = %q/%q, want empty", env1.SourceEventID, env2.SourceEventID)
	}
	if env1.IdempotencyKey == env2.IdempotencyKey {
		t.Fatalf("IdempotencyKey = %q, want ephemeral keys to differ", env1.IdempotencyKey)
	}
	if turn.BuildIdempotencyKey(env1) != env1.IdempotencyKey {
		t.Fatalf("BuildIdempotencyKey did not preserve explicit key")
	}
}

func TestPlatformTurnEnvelopeRetryRequestIDChangesIdempotencyKey(t *testing.T) {
	base := platformTurnEnvelope(platform.InboundMessage{ExternalMessageID: "external-1", Text: "hello"}, "session-1", "default")
	retry := platformTurnEnvelope(platform.InboundMessage{
		ExternalMessageID: "external-1",
		RequestID:         "external-1:retry:2",
		Text:              "hello",
	}, "session-1", "default")
	if retry.SourceEventID != base.SourceEventID {
		t.Fatalf("retry SourceEventID = %q, want %q", retry.SourceEventID, base.SourceEventID)
	}
	if retry.IdempotencyKey == base.IdempotencyKey {
		t.Fatalf("retry IdempotencyKey = %q, want different key from base", retry.IdempotencyKey)
	}
}
