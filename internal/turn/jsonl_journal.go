package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type JSONLJournal struct {
	dir string
}

func NewJSONLJournal(dir string) *JSONLJournal {
	return &JSONLJournal{dir: dir}
}

func (j *JSONLJournal) StartTurn(ctx context.Context, record TurnRecord) error {
	return j.write(ctx, record.TurnID, map[string]any{
		"type":            "turn_started",
		"turn_id":         record.TurnID,
		"idempotency_key": record.IdempotencyKey,
		"kind":            record.Kind,
		"session_id":      record.SessionID,
		"persona_key":     record.PersonaKey,
		"state":           record.State,
		"status":          record.Status,
	})
}

func (j *JSONLJournal) RecordTransition(ctx context.Context, turnID string, from, to TurnState, metrics StageMetrics) error {
	return j.write(ctx, turnID, map[string]any{
		"type":        "state_transition",
		"turn_id":     turnID,
		"stage":       metrics.Stage,
		"from":        from,
		"to":          to,
		"duration_ms": metrics.DurationMS,
	})
}

func (j *JSONLJournal) RecordEvent(ctx context.Context, turnID string, event JournalEvent) error {
	return j.write(ctx, turnID, map[string]any{
		"type":    event.Type,
		"turn_id": turnID,
		"seq":     event.Seq,
		"stage":   event.Stage,
		"payload": sanitizePayload(event.Payload),
	})
}

func (j *JSONLJournal) RecordOutbound(ctx context.Context, turnID string, event OutboundEvent) error {
	return j.write(ctx, turnID, map[string]any{
		"type":    event.Type,
		"turn_id": turnID,
		"seq":     event.Seq,
		"stage":   StageOutboundCommit,
		"payload": outboundEventPayload(sanitizeOutboundEvent(event)),
	})
}

func (j *JSONLJournal) CompleteTurn(ctx context.Context, turnID, status, errorKind string) error {
	return j.write(ctx, turnID, map[string]any{
		"type":       "turn_completed",
		"turn_id":    turnID,
		"status":     status,
		"error_kind": errorKind,
	})
}

func (j *JSONLJournal) write(ctx context.Context, turnID string, payload map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if j == nil || j.dir == "" {
		return fmt.Errorf("jsonl journal dir is required")
	}
	if turnID == "" {
		return fmt.Errorf("turn id is required")
	}
	payload = sanitizePayload(payload)
	payload["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	dayDir := filepath.Join(j.dir, time.Now().UTC().Format("2006-01-02"))
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		return fmt.Errorf("create jsonl dir: %w", err)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal jsonl payload: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dayDir, turnID+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open jsonl journal: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write jsonl journal: %w", err)
	}
	return nil
}
