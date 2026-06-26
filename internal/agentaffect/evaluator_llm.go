package agentaffect

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
)

type LLMEvaluator struct {
	client llm.Client
	cfg    config.AgentAffectConfig
}

func NewLLMEvaluator(client llm.Client, cfg config.AgentAffectConfig) *LLMEvaluator {
	return &LLMEvaluator{client: client, cfg: cfg}
}

func (e *LLMEvaluator) Evaluate(ctx context.Context, req LLMEvaluationRequest) (LLMEvaluationResult, error) {
	if e.client == nil {
		return LLMEvaluationResult{}, fmt.Errorf("agent affect evaluator client is not configured")
	}
	if e.cfg.Evaluator.TimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(e.cfg.Evaluator.TimeoutMS)*time.Millisecond)
		defer cancel()
	}
	prompt, report, err := buildEvaluationPromptWithBudget(e.cfg, req)
	if err != nil {
		return LLMEvaluationResult{}, err
	}
	stream := false
	temp := e.cfg.Evaluator.Temperature
	chatReq := llm.ChatRequest{
		Model:  e.cfg.Evaluator.Model,
		System: prompt.System,
		Messages: []llm.Message{{
			Role:    llm.RoleUser,
			Content: prompt.User,
		}},
		Params: llm.RequestParams{
			MaxTokens:       e.cfg.Evaluator.MaxOutputTokens,
			Temperature:     &temp,
			Stream:          &stream,
			ReasoningEffort: e.cfg.Evaluator.ReasoningEffort,
			Thinking:        buildThinkingConfig(e.cfg.Evaluator),
		},
		Stream: false,
	}
	started := time.Now()
	resp, err := e.client.Chat(ctx, chatReq)
	if err != nil {
		return LLMEvaluationResult{}, fmt.Errorf("agent affect evaluator failed: %w", err)
	}
	if resp == nil {
		return LLMEvaluationResult{}, fmt.Errorf("agent affect evaluator returned nil response")
	}
	result, err := ParseLLMResponse(resp.Content)
	if err != nil {
		return LLMEvaluationResult{}, fmt.Errorf("agent affect evaluator returned invalid JSON: %w", err)
	}
	if !e.cfg.Evaluator.StoreHiddenThinking {
		resp.ReasoningContent = ""
	}
	result.PromptVersion = agentAffectPromptVersion
	result.PromptHash = prompt.Hash
	result.ContextStrategy = report.Strategy
	result.BudgetReport = report
	result.Usage = LLMUsage{InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens}
	result.LLMProvider = e.cfg.Evaluator.ProviderID
	result.LLMModel = defaultString(resp.Model, e.cfg.Evaluator.Model)
	result.LLMThinkingEnabled = e.cfg.Evaluator.ThinkingEnabled
	result.LatencyMS = time.Since(started).Milliseconds()
	if e.cfg.Context.StorePromptSnapshot && e.cfg.Context.StoreRawInputs {
		result.PromptSnapshot = prompt.System + "\n\n" + prompt.User
	}
	return result, nil
}

func buildThinkingConfig(cfg config.AgentAffectEvaluatorConfig) *llm.ThinkingConfig {
	if !cfg.ThinkingEnabled {
		return nil
	}
	return &llm.ThinkingConfig{Mode: "enabled", Effort: cfg.ReasoningEffort}
}

func inputText(input MoodImpactInput) string {
	switch input.Mode {
	case "summary":
		return input.Summary
	case "mixed":
		return strings.TrimSpace(input.Summary + "\n" + input.Text)
	case "none":
		return ""
	default:
		return input.Text
	}
}
