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
	resp     *llm.ChatResponse
	err      error
	req      llm.ChatRequest
	requests []llm.ChatRequest
}

func (f *fakeLLMClient) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	f.req = req
	f.requests = append(f.requests, req)
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func (f *fakeLLMClient) ChatStream(context.Context, llm.ChatRequest, llm.StreamCallback) (*llm.ChatResponse, error) {
	return nil, errors.New("unexpected stream call")
}

func TestLLMEvaluatorInvalidJSONReturnsError(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	client := &fakeLLMClient{resp: &llm.ChatResponse{Content: "not json"}}
	evaluator := NewLLMEvaluator(client, cfg)

	_, err := evaluator.Evaluate(context.Background(), LLMEvaluationRequest{
		PersonaID:    "default",
		SessionID:    "session-1",
		CurrentMood:  MoodSnapshot{Vector: MoodVector{Valence: 0.2, Warmth: 0.4}},
		Trigger:      TriggerDescriptor{TriggerType: "user_message"},
		Input:        MoodImpactInput{Mode: "raw", Text: "hello"},
		PromptPolicy: cfg,
	})
	if err == nil {
		t.Fatal("Evaluate error = nil, want invalid JSON error")
	}
}

func TestLLMEvaluatorMissingRequiredJSONReturnsError(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	client := &fakeLLMClient{resp: &llm.ChatResponse{Content: `{}`}}
	evaluator := NewLLMEvaluator(client, cfg)

	_, err := evaluator.Evaluate(context.Background(), LLMEvaluationRequest{
		PersonaID:   "default",
		SessionID:   "session-1",
		CurrentMood: MoodSnapshot{Vector: MoodVector{Valence: 0.2}},
		Trigger:     TriggerDescriptor{TriggerType: "user_message"},
		Input:       MoodImpactInput{Mode: "raw", Text: "hello"},
	})
	if err == nil {
		t.Fatal("Evaluate error = nil, want schema error")
	}
}

func TestLLMEvaluatorParsesStrictJSONAndConfiguresChatRequest(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	cfg.Evaluator.ProviderID = "moonshot"
	cfg.Evaluator.Model = "affect-model"
	cfg.Evaluator.ThinkingEnabled = true
	cfg.Evaluator.ReasoningEffort = "medium"
	client := &fakeLLMClient{resp: &llm.ChatResponse{Content: `{
		"schema_version": "agent_affect.v3.appraisal.v1",
		"appraisal": {
			"event_significance": 0.7,
			"novelty": 0.4,
			"goal_relevance": 0.2,
			"relationship_impact": 0.3,
			"boundary_impact": 0.1,
			"uncertainty": 0.2
		},
		"delta": {"valence": 0.12, "attachment": 0.04},
		"label": "steady",
		"cause": {
			"code": "shared_progress",
			"summary": "User shared progress.",
			"visible_summary": "Shared progress.",
			"tags": ["progress"]
		},
		"confidence": 0.8
	}`, Usage: llm.Usage{InputTokens: 123, OutputTokens: 45}}}
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
	if !result.HasAppraisal || result.Appraisal.EventSignificance != 0.7 {
		t.Fatalf("appraisal = %#v has=%v, want event significance", result.Appraisal, result.HasAppraisal)
	}
	if result.Usage.InputTokens != 123 || result.Usage.OutputTokens != 45 {
		t.Fatalf("usage = %#v, want provider usage", result.Usage)
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
		"\"persona_profile\"",
		"\"state_checkpoint\"",
		"\"event_batch\"",
		"\"dimension_limits\"",
		"Recent relevant memory.",
		`"schema_version":"agent_affect.v3.appraisal.v1"`,
	} {
		if !strings.Contains(client.req.Messages[0].Content, want) {
			t.Fatalf("prompt missing %q:\n%s", want, client.req.Messages[0].Content)
		}
	}
	for _, forbidden := range []string{"previous_evaluations", "recent_evaluations", "agent_affect.v2.evaluation"} {
		if strings.Contains(client.req.Messages[0].Content, forbidden) {
			t.Fatalf("prompt contains forbidden %q:\n%s", forbidden, client.req.Messages[0].Content)
		}
	}
}

func TestParseLLMResponseRequiresV3SchemaAndDerivesNaturalMoodFields(t *testing.T) {
	result, err := ParseLLMResponse(`{
		"schema_version": "agent_affect.v3.appraisal.v1",
		"appraisal": {
			"event_significance": 0.3,
			"novelty": 0.1,
			"goal_relevance": 0.1,
			"relationship_impact": 0.2,
			"boundary_impact": 0,
			"uncertainty": 0.1
		},
		"delta": {"valence": 0.02, "warmth": 0.03},
		"label": "steady",
		"cause": {
			"code": "safe_visible_cause",
			"summary": "Internal audit summary.",
			"visible_summary": "Safe visible cause summary.",
			"tags": ["steady"]
		},
		"confidence": 0.6
	}`)
	if err != nil {
		t.Fatalf("ParseLLMResponse: %v", err)
	}
	if result.Label != "steady" {
		t.Fatalf("label = %q", result.Label)
	}
	if result.MoodDescription != "Safe visible cause summary." || result.MoodReason != "Internal audit summary." {
		t.Fatalf("natural mood fields = %#v", result)
	}
	if result.PromptMoodText != "Safe visible cause summary." {
		t.Fatalf("prompt_mood_text = %q", result.PromptMoodText)
	}
	if strings.Contains(result.PromptMoodText, result.Label+"；") {
		t.Fatalf("prompt_mood_text leaks machine label: %q", result.PromptMoodText)
	}
}

func TestParseLLMResponseKeepsMachineLabelOutOfPromptMoodText(t *testing.T) {
	result, err := ParseLLMResponse(`{
		"schema_version": "agent_affect.v3.appraisal.v1",
		"appraisal": {
			"event_significance": 0.3,
			"novelty": 0.1,
			"goal_relevance": 0.1,
			"relationship_impact": 0.2,
			"boundary_impact": 0,
			"uncertainty": 0.1
		},
		"delta": {"valence": 0.02, "warmth": 0.03},
		"label": "playful_caring_weather_sleep_reminder",
		"cause": {
			"code": "sleep_reminder",
			"summary": "Internal audit summary.",
			"visible_summary": "俏皮地关心用户，提醒休息。",
			"tags": ["sleep"]
		},
		"confidence": 0.6
	}`)
	if err != nil {
		t.Fatalf("ParseLLMResponse: %v", err)
	}
	if result.Label != "playful_caring_weather_sleep_reminder" {
		t.Fatalf("label = %q", result.Label)
	}
	if result.PromptMoodText != "俏皮地关心用户，提醒休息。" {
		t.Fatalf("prompt_mood_text = %q", result.PromptMoodText)
	}
	if strings.Contains(result.PromptMoodText, result.Label) {
		t.Fatalf("prompt_mood_text leaks label: %q", result.PromptMoodText)
	}
}

func TestParseLLMResponseFallsBackToCauseSummaryForPromptMoodText(t *testing.T) {
	result, err := ParseLLMResponse(`{
		"schema_version": "agent_affect.v3.appraisal.v1",
		"appraisal": {
			"event_significance": 0.3,
			"novelty": 0.1,
			"goal_relevance": 0.1,
			"relationship_impact": 0.2,
			"boundary_impact": 0,
			"uncertainty": 0.1
		},
		"delta": {"valence": 0.02, "warmth": 0.03},
		"label": "playful_caring_weather_sleep_reminder",
		"cause": {
			"code": "sleep_reminder",
			"summary": "安全的自然语言摘要。",
			"visible_summary": "",
			"tags": ["sleep"]
		},
		"confidence": 0.6
	}`)
	if err != nil {
		t.Fatalf("ParseLLMResponse: %v", err)
	}
	if result.PromptMoodText != "安全的自然语言摘要。" {
		t.Fatalf("prompt_mood_text = %q", result.PromptMoodText)
	}
	if strings.Contains(result.PromptMoodText, result.Label) {
		t.Fatalf("prompt_mood_text leaks label: %q", result.PromptMoodText)
	}
}

func TestParseLLMResponseRejectsLegacySchema(t *testing.T) {
	_, err := ParseLLMResponse(`{
		"schema_version": "agent_affect.v2.evaluation.v2",
		"delta": {"valence": 0.01},
		"label": "steady",
		"confidence": 0.5
	}`)
	if err == nil {
		t.Fatal("ParseLLMResponse error = nil, want legacy schema rejection")
	}
}

func TestParseLLMResponseRejectsForbiddenAdviceFields(t *testing.T) {
	_, err := ParseLLMResponse(`{
		"schema_version": "agent_affect.v3.appraisal.v1",
		"appraisal": {
			"event_significance": 0,
			"novelty": 0,
			"goal_relevance": 0,
			"relationship_impact": 0,
			"boundary_impact": 0,
			"uncertainty": 0
		},
		"delta": {"valence": 0},
		"label": "steady",
		"cause": {"code": "neutral", "summary": "neutral", "visible_summary": "neutral"},
		"confidence": 0.5,
		"response_advice": "reply warmly"
	}`)
	if err == nil {
		t.Fatal("ParseLLMResponse error = nil, want forbidden response advice rejection")
	}
}

func TestBuildEvaluationPromptExcludesPreviousEvaluationsAndBoundsHistory(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	req := LLMEvaluationRequest{
		PersonaID:            "default",
		SessionID:            "session-1",
		PersonaAffectProfile: AffectProfile{PersonaID: "default", ProfileName: "default", Baseline: MoodVector{Warmth: 0.6}},
		CurrentMood:          MoodSnapshot{Vector: MoodVector{Warmth: 0.6}, Label: "steady", Confidence: 0.5},
		Trigger:              TriggerDescriptor{TriggerType: "user_message"},
		Input:                MoodImpactInput{Mode: "raw", Text: "ordinary user message"},
		MemoryPromptBlock:    "[Memory]\nRecent relevant memory.",
	}
	noHistory := buildEvaluationPrompt(cfg, req)
	req.Recent = make([]AffectEvaluationRecord, 1000)
	for i := range req.Recent {
		req.Recent[i] = AffectEvaluationRecord{
			ID:                  "eval-history",
			Input:               MoodImpactInput{Mode: "raw", Text: strings.Repeat("历史用户内容", 100)},
			CauseSummary:        strings.Repeat("旧评估摘要", 100),
			VisibleCauseSummary: strings.Repeat("旧可见摘要", 100),
		}
	}
	withHistory := buildEvaluationPrompt(cfg, req)

	if strings.Contains(withHistory.User, "previous_evaluations") || strings.Contains(withHistory.User, "recent_evaluations") {
		t.Fatalf("prompt contains recursive history keys:\n%s", withHistory.User)
	}
	if len([]rune(withHistory.User)) != len([]rune(noHistory.User)) {
		t.Fatalf("prompt grew with history: noHistory=%d withHistory=%d", len([]rune(noHistory.User)), len([]rune(withHistory.User)))
	}
}

func TestLLMEvaluatorTransportErrorReturnsError(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	client := &fakeLLMClient{err: errors.New("transport down")}
	evaluator := NewLLMEvaluator(client, cfg)

	_, err := evaluator.Evaluate(context.Background(), LLMEvaluationRequest{
		PersonaID:   "default",
		SessionID:   "session-1",
		CurrentMood: MoodSnapshot{Vector: MoodVector{Warmth: 0.4}},
		Trigger:     TriggerDescriptor{TriggerType: "user_message"},
		Input:       MoodImpactInput{Mode: "raw", Text: "hello"},
	})
	if err == nil {
		t.Fatal("Evaluate error = nil, want transport error")
	}
}
