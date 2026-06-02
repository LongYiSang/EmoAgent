package plugin

import (
	"context"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/turn"
)

type InvocationAudit struct {
	PluginID     string
	InvocationID string
	Hook         HookName
	Stage        turn.StageName
	Status       string
	DurationMS   int64
	Capability   Capability
	ErrorKind    string
	InputHash    string
	PatchCount   int
	StartedAt    time.Time
}

type AuditSink interface {
	RecordInvocation(context.Context, InvocationAudit) error
}

type TurnJournalAudit struct {
	journal turn.TurnJournal
}

func NewTurnJournalAudit(journal turn.TurnJournal) *TurnJournalAudit {
	if journal == nil {
		return nil
	}
	return &TurnJournalAudit{journal: journal}
}

func (a *TurnJournalAudit) RecordInvocation(ctx context.Context, audit InvocationAudit) error {
	if a == nil || a.journal == nil || audit.InvocationID == "" {
		return nil
	}
	return a.journal.RecordEvent(ctx, auditTurnID(audit), turn.JournalEvent{
		Stage: audit.Stage,
		Type:  "plugin_invocation",
		Payload: map[string]any{
			"plugin_id":     audit.PluginID,
			"invocation_id": audit.InvocationID,
			"hook":          audit.Hook,
			"status":        audit.Status,
			"duration_ms":   audit.DurationMS,
			"capability":    audit.Capability,
			"error_kind":    audit.ErrorKind,
			"input_hash":    audit.InputHash,
			"patch_count":   audit.PatchCount,
		},
	})
}

func auditTurnID(audit InvocationAudit) string {
	parts := strings.Split(audit.InvocationID, ":")
	if len(parts) >= 3 && parts[2] != "" {
		return parts[2]
	}
	return ""
}
