package agentaffect

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
)

type fakeLLMClient struct {
	resp *llm.ChatResponse
	err  error
	req  llm.ChatRequest
}

func (f *fakeLLMClient) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	f.req = req
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func (f *fakeLLMClient) ChatStream(context.Context, llm.ChatRequest, llm.StreamCallback) (*llm.ChatResponse, error) {
	return nil, errors.New("unexpected stream call")
}

func TestLLMEvaluatorInvalidJSONReturnsNoChangeFallback(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	client := &fakeLLMClient{resp: &llm.ChatResponse{Content: "not json"}}
	evaluator := NewLLMEvaluator(client, cfg)

	result, err := evaluator.Evaluate(context.Background(), LLMEvaluationRequest{
		PersonaID:    "default",
		SessionID:    "session-1",
		CurrentMood:  MoodSnapshot{Vector: MoodVector{Valence: 0.2, Warmth: 0.4}},
		Trigger:      TriggerDescriptor{TriggerType: "user_message"},
		Input:        MoodImpactInput{Mode: "raw", Text: "hello"},
		PromptPolicy: cfg,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !result.Fallback {
		t.Fatal("Fallback = false, want true for invalid JSON")
	}
	if !result.Delta.IsZero() {
		t.Fatalf("delta = %#v, want no change", result.Delta)
	}
	if result.Status != EvaluationStatusFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
}

func TestLLMEvaluatorMissingRequiredJSONReturnsNoChangeFallback(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	client := &fakeLLMClient{resp: &llm.ChatResponse{Content: `{}`}}
	evaluator := NewLLMEvaluator(client, cfg)

	result, err := evaluator.Evaluate(context.Background(), LLMEvaluationRequest{
		PersonaID:   "default",
		SessionID:   "session-1",
		CurrentMood: MoodSnapshot{Vector: MoodVector{Valence: 0.2}},
		Trigger:     TriggerDescriptor{TriggerType: "user_message"},
		Input:       MoodImpactInput{Mode: "raw", Text: "hello"},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !result.Fallback || !result.Delta.IsZero() || result.Status != EvaluationStatusFailed {
		t.Fatalf("result = %#v, want failed no-change fallback", result)
	}
}

func TestLLMEvaluatorParsesStrictJSONAndConfiguresChatRequest(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	cfg.Evaluator.ProviderID = "moonshot"
	cfg.Evaluator.Model = "affect-model"
	cfg.Evaluator.ThinkingEnabled = true
	cfg.Evaluator.ReasoningEffort = "medium"
	client := &fakeLLMClient{resp: &llm.ChatResponse{Content: `{
		"delta": {"valence": 0.12, "attachment": 0.04},
		"label": "steady",
		"cause_summary": "User shared progress.",
		"visible_cause_summary": "Shared progress.",
		"confidence": 0.8
	}`}}
	evaluator := NewLLMEvaluator(client, cfg)

	result, err := evaluator.Evaluate(context.Background(), LLMEvaluationRequest{
		PersonaID:            "default",
		SessionID:            "session-1",
		PersonaAffectProfile: AffectProfile{PersonaID: "default", ProfileName: "default", Baseline: MoodVector{Warmth: 0.6}},
		CurrentMood:          MoodSnapshot{Vector: MoodVector{}},
		Trigger:              TriggerDescriptor{TriggerType: "user_message"},
		Input:                MoodImpactInput{Mode: "raw", Text: "I finished it."},
		MemoryPromptBlock:    "[Memory]\nRecent relevant memory.",
		Recent: []AffectEvaluationRecord{{
			ID:           "eval-prev",
			CauseSummary: "Previous affect context.",
		}},
		PromptPolicy: cfg,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Fallback {
		t.Fatal("Fallback = true, want parsed result")
	}
	if result.Delta.Valence != 0.12 || result.Delta.Attachment != 0.04 {
		t.Fatalf("delta = %#v", result.Delta)
	}
	if result.Label != "steady" || result.CauseSummary != "User shared progress." || result.VisibleCauseSummary != "Shared progress." {
		t.Fatalf("parsed result = %#v", result)
	}
	if client.req.Model != "affect-model" {
		t.Fatalf("model = %q, want affect-model", client.req.Model)
	}
	if client.req.Stream {
		t.Fatal("Stream = true, want non-streaming affect evaluator")
	}
	if client.req.Params.Thinking == nil || client.req.Params.Thinking.Effort != "medium" {
		t.Fatalf("thinking params = %#v", client.req.Params.Thinking)
	}
	for _, want := range []string{
		"<persona_affect_profile>",
		"<recent_affect_context mode=\"raw_window\">",
		"[Memory]\nRecent relevant memory.",
		"<previous_evaluations>",
	} {
		if !strings.Contains(client.req.Messages[0].Content, want) {
			t.Fatalf("prompt missing %q:\n%s", want, client.req.Messages[0].Content)
		}
	}
}
