package work

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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

var errUnknownRuntimeDecisionOption = errors.New("runtime decider chose unknown option")

var runtimeDeciderRepairSystemPrompt = `Repair the RuntimeDecider response to the exact required JSON schema.
Do not change the decision unless the original response violated the schema.
Use only option IDs provided in the packet. If safe repair is impossible, return escalate=true with decision="".
Return JSON only. No markdown, code fences, prose, or extra keys.`

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

	req := llm.ChatRequest{
		Model:       d.model,
		Messages:    []llm.Message{{Role: llm.RoleUser, Content: userPayload}},
		System:      buildRuntimeDeciderSystemPrompt(),
		MaxTokens:   d.maxTokens,
		Temperature: d.temperature,
		Stream:      false,
	}
	resp, err := d.client.ChatStream(ctx, req, func(llm.StreamEvent) {})
	if err != nil {
		return RuntimeDecision{
			Escalate:       true,
			EscalateReason: fmt.Sprintf("runtime decider llm error: %v", err),
		}, nil
	}

	decision, parseErr := parseRuntimeDecisionResponse(resp, packet)
	if parseErr == nil {
		return decision, nil
	}
	if errors.Is(parseErr, errUnknownRuntimeDecisionOption) {
		return RuntimeDecision{
			Escalate:       true,
			EscalateReason: parseErr.Error(),
		}, nil
	}

	repairReq, repairBuildErr := buildRuntimeDeciderRepairRequest(req, resp, parseErr)
	if repairBuildErr != nil {
		return RuntimeDecision{Escalate: true, EscalateReason: repairBuildErr.Error()}, nil
	}
	repairResp, repairErr := d.client.ChatStream(ctx, repairReq, func(llm.StreamEvent) {})
	if repairErr != nil {
		return RuntimeDecision{
			Escalate:       true,
			EscalateReason: fmt.Sprintf("runtime decider repair llm error: %v", repairErr),
		}, nil
	}
	decision, parseErr = parseRuntimeDecisionResponse(repairResp, packet)
	if parseErr != nil {
		return RuntimeDecision{
			Escalate:       true,
			EscalateReason: fmt.Sprintf("runtime decider returned invalid schema: %v", parseErr),
		}, nil
	}
	return decision, nil
}

type runtimeDecisionPayload struct {
	Escalate         bool     `json:"escalate"`
	EscalateReason   string   `json:"escalate_reason"`
	Decision         string   `json:"decision"`
	Reason           string   `json:"reason"`
	ConstraintsDelta []string `json:"constraints_delta"`
}

func parseRuntimeDecisionResponse(resp *llm.ChatResponse, packet protocol.DecisionPacket) (RuntimeDecision, error) {
	if resp == nil {
		return RuntimeDecision{}, fmt.Errorf("runtime decider response is nil")
	}
	var parsed runtimeDecisionPayload
	if err := decodeStrictJSON(json.RawMessage(stripCodeFence(chatResponseText(resp))), &parsed); err != nil {
		return RuntimeDecision{}, fmt.Errorf("decode runtime decision: %w", err)
	}
	if parsed.Escalate {
		if strings.TrimSpace(parsed.Decision) != "" {
			return RuntimeDecision{}, fmt.Errorf("runtime decider escalation must not include decision")
		}
		reason := strings.TrimSpace(parsed.EscalateReason)
		if reason == "" {
			return RuntimeDecision{}, fmt.Errorf("runtime decider escalation requires escalate_reason")
		}
		return RuntimeDecision{Escalate: true, EscalateReason: reason}, nil
	}
	if strings.TrimSpace(parsed.EscalateReason) != "" {
		return RuntimeDecision{}, fmt.Errorf("runtime decider non-escalation must not include escalate_reason")
	}
	if strings.TrimSpace(parsed.Decision) == "" {
		return RuntimeDecision{}, fmt.Errorf("runtime decider returned empty decision")
	}
	if strings.TrimSpace(parsed.Reason) == "" {
		return RuntimeDecision{}, fmt.Errorf("runtime decider returned empty reason")
	}
	validOptions := make(map[string]struct{}, len(packet.Options))
	for _, option := range packet.Options {
		validOptions[option.ID] = struct{}{}
	}
	if _, ok := validOptions[parsed.Decision]; !ok {
		return RuntimeDecision{}, fmt.Errorf("%w %q", errUnknownRuntimeDecisionOption, parsed.Decision)
	}
	return RuntimeDecision{
		Escalate:         false,
		Decision:         parsed.Decision,
		Reason:           parsed.Reason,
		ConstraintsDelta: parsed.ConstraintsDelta,
	}, nil
}

func buildRuntimeDeciderRepairRequest(req llm.ChatRequest, resp *llm.ChatResponse, parseErr error) (llm.ChatRequest, error) {
	payload, err := json.Marshal(struct {
		Error           string `json:"error"`
		InvalidResponse string `json:"invalid_response"`
	}{
		Error:           parseErr.Error(),
		InvalidResponse: chatResponseText(resp),
	})
	if err != nil {
		return llm.ChatRequest{}, fmt.Errorf("marshal runtime decider repair payload: %w", err)
	}
	repairReq := req
	repairReq.System = runtimeDeciderRepairSystemPrompt
	repairReq.Messages = append(append([]llm.Message(nil), req.Messages...), llm.Message{
		Role:    llm.RoleUser,
		Content: string(payload),
	})
	repairReq.Temperature = 0
	return repairReq, nil
}
