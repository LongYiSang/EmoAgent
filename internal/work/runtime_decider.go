package work

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/protocol"
)

// RuntimeDecider handles low-risk auto decisions within Work runtime.
type RuntimeDecider interface {
	Decide(ctx context.Context, brief protocol.TaskBrief, packet protocol.DecisionPacket) (RuntimeDecision, error)
}

// RuntimeDecision is the normalized result returned by RuntimeDecider.
type RuntimeDecision struct {
	Escalate         bool
	EscalateReason   string
	Decision         string
	Reason           string
	ConstraintsDelta []string
}

// LLMRuntimeDecider is the default RuntimeDecider implementation backed by LLM.
type LLMRuntimeDecider struct {
	client      llm.Client
	model       string
	maxTokens   int
	temperature float64
}

// NewLLMRuntimeDecider creates a RuntimeDecider using the supplied LLM model.
func NewLLMRuntimeDecider(client llm.Client, model string) *LLMRuntimeDecider {
	return &LLMRuntimeDecider{
		client:      client,
		model:       model,
		maxTokens:   512,
		temperature: 0.0,
	}
}

func (d *LLMRuntimeDecider) Decide(ctx context.Context, brief protocol.TaskBrief, packet protocol.DecisionPacket) (RuntimeDecision, error) {
	if packet.Category != protocol.CatAuto {
		return RuntimeDecision{
			Escalate:       true,
			EscalateReason: fmt.Sprintf("unsupported category %q for runtime decider", packet.Category),
		}, nil
	}
	if d == nil || d.client == nil || d.model == "" {
		return RuntimeDecision{
			Escalate:       true,
			EscalateReason: "runtime decider unavailable",
		}, nil
	}

	userPayload, err := buildRuntimeDeciderUserPayload(brief, packet)
	if err != nil {
		return RuntimeDecision{
			Escalate:       true,
			EscalateReason: err.Error(),
		}, nil
	}

	resp, err := d.client.ChatStream(ctx, llm.ChatRequest{
		Model:       d.model,
		Messages:    []llm.Message{{Role: llm.RoleUser, Content: userPayload}},
		System:      buildRuntimeDeciderSystemPrompt(),
		MaxTokens:   d.maxTokens,
		Temperature: d.temperature,
		Stream:      false,
	}, func(llm.StreamEvent) {})
	if err != nil {
		return RuntimeDecision{
			Escalate:       true,
			EscalateReason: fmt.Sprintf("runtime decider llm error: %v", err),
		}, nil
	}

	var parsed struct {
		Escalate         bool     `json:"escalate"`
		EscalateReason   string   `json:"escalate_reason"`
		Decision         string   `json:"decision"`
		Reason           string   `json:"reason"`
		ConstraintsDelta []string `json:"constraints_delta"`
	}
	if err := json.Unmarshal([]byte(stripCodeFence(resp.Content)), &parsed); err != nil {
		return RuntimeDecision{
			Escalate:       true,
			EscalateReason: "runtime decider returned invalid JSON",
		}, nil
	}

	if parsed.Escalate {
		reason := parsed.EscalateReason
		if reason == "" {
			reason = "runtime decider requested escalation"
		}
		return RuntimeDecision{
			Escalate:       true,
			EscalateReason: reason,
		}, nil
	}

	if parsed.Decision == "" {
		return RuntimeDecision{
			Escalate:       true,
			EscalateReason: "runtime decider returned empty decision",
		}, nil
	}
	validOptions := make(map[string]struct{}, len(packet.Options))
	for _, option := range packet.Options {
		validOptions[option.ID] = struct{}{}
	}
	if _, ok := validOptions[parsed.Decision]; !ok {
		return RuntimeDecision{
			Escalate:       true,
			EscalateReason: fmt.Sprintf("runtime decider chose unknown option %q", parsed.Decision),
		}, nil
	}

	return RuntimeDecision{
		Escalate:         false,
		Decision:         parsed.Decision,
		Reason:           parsed.Reason,
		ConstraintsDelta: parsed.ConstraintsDelta,
	}, nil
}
