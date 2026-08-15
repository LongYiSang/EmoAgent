package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/plugin"
	"github.com/longyisang/emoagent/internal/storage"
)

// Event types a proposal may claim. Held to a fixed set so the gate prompt can
// reason about them and a plugin cannot invent categories the host does not
// understand.
var proactiveEventTypes = map[string]bool{
	"stuck":       true,
	"task_switch": true,
	"idle":        true,
	"milestone":   true,
	"custom":      true,
}

const proactiveSummaryMaxRunes = 600

// Propose records a plugin's suggestion that something might be worth speaking
// up about. It validates and rate-limits, but never decides anything: whether,
// when, and where to say something is the host's call alone.
func (s *ProactiveService) Propose(ctx context.Context, proposal plugin.ProactiveProposal) (string, error) {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return "", fmt.Errorf("proactive service is not configured")
	}
	cfg := config.DefaultProactiveConfig()
	if s.infra.Config != nil {
		cfg = config.NormalizeProactiveConfig(s.infra.Config.Proactive)
	}

	personaKey := strings.TrimSpace(proposal.PersonaKey)
	if personaKey == "" && s.agentRuntime != nil {
		personaKey = s.agentRuntime.ActivePersonaKey()
	}
	if personaKey == "" {
		return "", fmt.Errorf("persona_key is required")
	}

	eventType := strings.TrimSpace(proposal.EventType)
	if eventType == "" {
		eventType = "custom"
	}
	if !proactiveEventTypes[eventType] {
		return "", fmt.Errorf("unsupported event_type %q", proposal.EventType)
	}

	summary := strings.TrimSpace(proposal.Summary)
	if summary == "" {
		return "", fmt.Errorf("summary is required")
	}
	if runes := []rune(summary); len(runes) > proactiveSummaryMaxRunes {
		summary = string(runes[:proactiveSummaryMaxRunes])
	}

	// Backpressure. Without it a misbehaving plugin can fill the table and make
	// every later gate call more expensive.
	pending, err := s.infra.DB.CountProactiveCandidates(ctx, personaKey, []string{
		storage.ProactiveCandidateStatusPending,
		storage.ProactiveCandidateStatusSkipped,
	})
	if err != nil {
		return "", err
	}
	if pending >= cfg.Candidates.MaxPending {
		return "", fmt.Errorf("proactive candidate queue is full (%d pending, max %d)", pending, cfg.Candidates.MaxPending)
	}

	now := time.Now()
	observedFrom := firstNonEmptyCommandValue(strings.TrimSpace(proposal.ObservedFrom), now.Format(time.RFC3339Nano))
	observedTo := firstNonEmptyCommandValue(strings.TrimSpace(proposal.ObservedTo), now.Format(time.RFC3339Nano))

	payloadJSON := "{}"
	if len(proposal.Payload) > 0 {
		encoded, err := json.Marshal(proposal.Payload)
		if err != nil {
			return "", fmt.Errorf("encode payload: %w", err)
		}
		payloadJSON = string(encoded)
	}

	id := uuid.NewString()
	record := storage.ProactiveCandidateRecord{
		ID:             id,
		SourcePluginID: proposal.SourcePluginID,
		PersonaKey:     personaKey,
		EventType:      eventType,
		Summary:        summary,
		ObservedFrom:   observedFrom,
		ObservedTo:     observedTo,
		Importance:     proposal.Importance,
		PayloadJSON:    payloadJSON,
		CreatedAt:      now.Format(time.RFC3339Nano),
		ExpiresAt:      now.Add(time.Duration(cfg.Candidates.TTLHours) * time.Hour).Format(time.RFC3339Nano),
	}
	if err := s.infra.DB.InsertProactiveCandidate(ctx, record); err != nil {
		return "", err
	}
	return id, nil
}
