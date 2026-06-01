package turn

import (
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
)

type IdempotencyResult struct {
	Duplicate bool
	TurnID    string
	Status    string
}

type MemoryIdempotencyStore struct {
	mu      sync.Mutex
	records map[string]idempotencyRecord
}

type idempotencyRecord struct {
	turnID string
	status string
}

func NewMemoryIdempotencyStore() *MemoryIdempotencyStore {
	return &MemoryIdempotencyStore{
		records: make(map[string]idempotencyRecord),
	}
}

func BuildIdempotencyKey(env InboundEnvelope) string {
	if env.IdempotencyKey != "" {
		return env.IdempotencyKey
	}

	source := string(env.Source)
	if source == "" {
		source = "unknown"
	}

	switch env.Kind {
	case InboundApprovalAction:
		if env.Approval != nil {
			return joinKey(source, env.SessionID, string(env.Kind), env.Approval.RequestID, env.Approval.Action, env.Approval.OptionID)
		}
		return joinKey(source, env.SessionID, string(env.Kind), env.RequestID)
	case InboundSystemResume:
		if env.SourceEventID != "" {
			return joinKey(source, string(env.Kind), env.SourceEventID)
		}
		return joinKey(source, string(env.Kind), env.RequestID)
	default:
		if env.RequestID == "" && env.SourceEventID == "" {
			return joinKey(source, env.SessionID, string(env.Kind), "ephemeral", uuid.NewString())
		}
		if env.SourceEventID != "" && env.RequestID == "" {
			return joinKey(source, env.SessionID, string(env.Kind), env.SourceEventID)
		}
		return joinKey(source, env.SessionID, string(env.Kind), env.RequestID)
	}
}

func joinKey(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		cleaned = append(cleaned, strings.TrimSpace(part))
	}
	return strings.Join(cleaned, ":")
}

func (s *MemoryIdempotencyStore) Begin(key, turnID string) (IdempotencyResult, error) {
	if key == "" {
		return IdempotencyResult{}, fmt.Errorf("idempotency key is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if record, ok := s.records[key]; ok {
		return IdempotencyResult{
			Duplicate: true,
			TurnID:    record.turnID,
			Status:    record.status,
		}, nil
	}

	s.records[key] = idempotencyRecord{
		turnID: turnID,
		status: "running",
	}
	return IdempotencyResult{TurnID: turnID, Status: "running"}, nil
}

func (s *MemoryIdempotencyStore) Complete(key, status string) error {
	if key == "" {
		return fmt.Errorf("idempotency key is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.records[key]
	if !ok {
		return fmt.Errorf("idempotency key %q not found", key)
	}
	if status == "" {
		status = "done"
	}
	record.status = status
	s.records[key] = record
	return nil
}
