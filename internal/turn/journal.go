package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	Source         InboundSource
	SourceEventID  string
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
	Outbound    []OutboundEvent
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
	snapshot.Outbound = append([]OutboundEvent(nil), snapshot.Outbound...)
	for i := range snapshot.Events {
		snapshot.Events[i].Payload = sanitizePayload(snapshot.Events[i].Payload)
	}
	for i := range snapshot.Outbound {
		snapshot.Outbound[i] = sanitizeOutboundEvent(snapshot.Outbound[i])
	}
	return snapshot
}

func (j *MemoryJournal) RecordOutbound(ctx context.Context, turnID string, event OutboundEvent) error {
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
		event.Seq = int64(len(snapshot.Outbound) + 1)
	}
	event.TurnID = turnID
	event = sanitizeOutboundEvent(event)
	snapshot.Outbound = append(snapshot.Outbound, event)
	snapshot.Events = append(snapshot.Events, JournalEvent{
		Seq:       int64(len(snapshot.Events) + 1),
		Stage:     StageOutboundCommit,
		Type:      event.Type,
		Payload:   outboundEventPayload(event),
		CreatedAt: event.CreatedAt,
	})
	return nil
}

func (j *MemoryJournal) ListOutbound(ctx context.Context, turnID string) ([]OutboundEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()

	snapshot, ok := j.turns[turnID]
	if !ok {
		return nil, fmt.Errorf("turn %q not found", turnID)
	}
	outbound := append([]OutboundEvent(nil), snapshot.Outbound...)
	for i := range outbound {
		outbound[i] = sanitizeOutboundEvent(outbound[i])
	}
	return outbound, nil
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

func sanitizeOutboundEvent(event OutboundEvent) OutboundEvent {
	event.Content = sanitizeString(event.Content)
	event.Payload = sanitizePayload(event.Payload)
	if event.Tool != nil {
		tool := *event.Tool
		tool.Preview = ""
		event.Tool = &tool
	}
	if event.Reasoning != nil {
		reasoning := *event.Reasoning
		reasoning.Content = ""
		event.Reasoning = &reasoning
	}
	return event
}

func outboundEventPayload(event OutboundEvent) map[string]any {
	payload := map[string]any{
		"outbound_type": event.Type,
	}
	if event.Content != "" {
		payload["content_bytes"] = len([]byte(event.Content))
		payload["content_hash"] = contentHash(event.Content)
	}
	if event.Payload != nil {
		safePayload := sanitizePayload(event.Payload)
		payload["payload"] = safePayload
		for key, value := range safePayload {
			if _, exists := payload[key]; !exists {
				payload[key] = value
			}
		}
	}
	if event.Tool != nil {
		payload["tool"] = event.Tool.Name
		payload["tool_status"] = event.Tool.Status
		payload["hash"] = event.Tool.Hash
		payload["size"] = event.Tool.Size
		payload["is_truncated"] = event.Tool.IsTruncated
	}
	if event.Reasoning != nil {
		payload["reasoning_id"] = event.Reasoning.ID
		payload["reasoning_status"] = event.Reasoning.Status
		payload["reasoning_provider"] = event.Reasoning.Provider
		payload["reasoning_model"] = event.Reasoning.Model
		payload["reasoning_kind"] = event.Reasoning.Kind
	}
	if event.Approval != nil && event.Approval.Request != nil {
		payload["approval_request_id"] = event.Approval.Request.ID
		payload["task_id"] = event.Approval.Request.TaskID
		payload["status"] = event.Approval.Request.Status
	}
	return sanitizePayload(payload)
}

func contentHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func toDebugString(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return string(data)
}
