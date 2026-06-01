package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type TurnJournal interface {
	StartTurn(ctx context.Context, record TurnRecord) error
	RecordTransition(ctx context.Context, turnID string, from, to TurnState, metrics StageMetrics) error
	RecordEvent(ctx context.Context, turnID string, event JournalEvent) error
	CompleteTurn(ctx context.Context, turnID, status, errorKind string) error
}

type TurnRecord struct {
	TurnID         string
	IdempotencyKey string
	Kind           InboundKind
	SessionID      string
	PersonaKey     string
	State          TurnState
	Status         string
	ErrorKind      string
	StartedAt      time.Time
	CompletedAt    time.Time
}

type TurnTransition struct {
	From       TurnState
	To         TurnState
	Stage      StageName
	DurationMS int64
	CreatedAt  time.Time
}

type JournalEvent struct {
	Seq       int64
	Stage     StageName
	Type      string
	Payload   map[string]any
	CreatedAt time.Time
}

type TurnSnapshot struct {
	TurnRecord
	Transitions []TurnTransition
	Events      []JournalEvent
}

type MemoryJournal struct {
	mu    sync.Mutex
	turns map[string]*TurnSnapshot
}

func NewMemoryJournal() *MemoryJournal {
	return &MemoryJournal{
		turns: make(map[string]*TurnSnapshot),
	}
}

func (j *MemoryJournal) StartTurn(ctx context.Context, record TurnRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.TurnID == "" {
		return fmt.Errorf("turn id is required")
	}
	if record.State == "" {
		record.State = StateCreated
	}
	if record.Status == "" {
		record.Status = "running"
	}
	if record.StartedAt.IsZero() {
		record.StartedAt = time.Now()
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	j.turns[record.TurnID] = &TurnSnapshot{TurnRecord: record}
	return nil
}

func (j *MemoryJournal) RecordTransition(ctx context.Context, turnID string, from, to TurnState, metrics StageMetrics) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if turnID == "" {
		return fmt.Errorf("turn id is required")
	}
	if metrics.Stage == "" {
		metrics.Stage = StageName(to)
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	snapshot, ok := j.turns[turnID]
	if !ok {
		return fmt.Errorf("turn %q not found", turnID)
	}
	snapshot.State = to
	snapshot.Transitions = append(snapshot.Transitions, TurnTransition{
		From:       from,
		To:         to,
		Stage:      metrics.Stage,
		DurationMS: metrics.DurationMS,
		CreatedAt:  time.Now(),
	})
	return nil
}

func (j *MemoryJournal) RecordEvent(ctx context.Context, turnID string, event JournalEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if turnID == "" {
		return fmt.Errorf("turn id is required")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	snapshot, ok := j.turns[turnID]
	if !ok {
		return fmt.Errorf("turn %q not found", turnID)
	}
	if event.Seq == 0 {
		event.Seq = int64(len(snapshot.Events) + 1)
	}
	event.Payload = sanitizePayload(event.Payload)
	snapshot.Events = append(snapshot.Events, event)
	return nil
}

func (j *MemoryJournal) CompleteTurn(ctx context.Context, turnID, status, errorKind string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if turnID == "" {
		return fmt.Errorf("turn id is required")
	}
	if status == "" {
		status = "done"
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	snapshot, ok := j.turns[turnID]
	if !ok {
		return fmt.Errorf("turn %q not found", turnID)
	}
	snapshot.Status = status
	snapshot.ErrorKind = errorKind
	snapshot.CompletedAt = time.Now()
	return nil
}

func (j *MemoryJournal) GetTurn(turnID string) (TurnSnapshot, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()

	snapshot, ok := j.turns[turnID]
	if !ok {
		return TurnSnapshot{}, false
	}
	return cloneSnapshot(*snapshot), true
}

func cloneSnapshot(snapshot TurnSnapshot) TurnSnapshot {
	snapshot.Transitions = append([]TurnTransition(nil), snapshot.Transitions...)
	snapshot.Events = append([]JournalEvent(nil), snapshot.Events...)
	for i := range snapshot.Events {
		snapshot.Events[i].Payload = sanitizePayload(snapshot.Events[i].Payload)
	}
	return snapshot
}

func sanitizePayload(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	safe := make(map[string]any, len(payload))
	for key, value := range payload {
		if forbiddenPayloadKey(key) {
			continue
		}
		safe[key] = sanitizeValue(value)
	}
	return safe
}

func sanitizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return sanitizePayload(typed)
	case map[string]string:
		safe := make(map[string]any, len(typed))
		for key, value := range typed {
			if forbiddenPayloadKey(key) {
				continue
			}
			safe[key] = sanitizeString(value)
		}
		return safe
	case []any:
		safe := make([]any, 0, len(typed))
		for _, item := range typed {
			safe = append(safe, sanitizeValue(item))
		}
		return safe
	case []string:
		safe := make([]string, 0, len(typed))
		for _, item := range typed {
			safe = append(safe, sanitizeString(item))
		}
		return safe
	case string:
		return sanitizeString(typed)
	default:
		return typed
	}
}

func forbiddenPayloadKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case "raw_tool_output", "prompt", "raw_prompt", "hidden_memory", "content", "file_content", "raw_file_content", "raw_content", "chain_of_thought", "sensitive_reasoning":
		return true
	default:
		return false
	}
}

func sanitizeString(value string) string {
	upper := strings.ToUpper(value)
	if strings.Contains(upper, "SECRET=") ||
		strings.Contains(upper, "TOKEN=") ||
		strings.Contains(upper, "PASSWORD=") ||
		strings.Contains(upper, "API_KEY=") {
		return "[redacted]"
	}
	return value
}

func toDebugString(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return string(data)
}
