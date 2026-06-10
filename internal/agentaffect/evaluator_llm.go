package agentaffect

import (
	"context"
	"encoding/json"
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
		return NoChangeResult("Agent Affect evaluator client is not configured.", EvaluationStatusFailed), nil
	}
	if e.cfg.Evaluator.TimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(e.cfg.Evaluator.TimeoutMS)*time.Millisecond)
		defer cancel()
	}
	prompt := buildEvaluationPrompt(e.cfg, req)
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
	resp, err := e.client.Chat(ctx, chatReq)
	if err != nil {
		return NoChangeResult(fmt.Sprintf("Agent Affect evaluator failed: %v", err), EvaluationStatusFailed), nil
	}
	result, err := ParseLLMResponse(resp.Content)
	if err != nil {
		fallback := NoChangeResult(fmt.Sprintf("Agent Affect evaluator returned invalid JSON: %v", err), EvaluationStatusFailed)
		fallback.RawResponseJSON = resp.Content
		return fallback, nil
	}
	if !e.cfg.Evaluator.StoreHiddenThinking {
		resp.ReasoningContent = ""
	}
	return result, nil
}

type affectPrompt struct {
	System string
	User   string
}

func buildEvaluationPrompt(cfg config.AgentAffectConfig, req LLMEvaluationRequest) affectPrompt {
	system := strings.TrimSpace(`You are EmoAgent Affect Evaluator.

You do not write user-facing replies.
You do not produce conversation guidance.
You only estimate how the given event changes the Agent's simulated mood state.

The Agent has a persistent simulated mood vector.
You must update it based on:
- current mood
- persona affect profile
- the event text or summary
- recent affect context
- previous affect evaluations
- dimension limits

Output strict JSON only.

Important:
- attachment is allowed to be a visible emotional dimension when configured.
- frustration can exist as an internal mood dimension.
- Do not claim the Agent has biological or human emotions.
- Do not create user facts.
- Do not change memory permissions.
- Do not include hidden reasoning.
- Generate cause_summary yourself as part of the evaluation.
- Do not generate response instructions such as tone, validation_first, or advice strategy.`)

	parts := []string{
		wrapJSON("persona_affect_profile", req.PersonaAffectProfile),
		wrapJSON("current_mood", req.CurrentMood),
		wrapJSON("trigger", req.Trigger),
		fmt.Sprintf("<input mode=%q>\n%s\n</input>", req.Input.Mode, inputText(req.Input)),
		fmt.Sprintf("<recent_affect_context mode=%q>\n%s\n</recent_affect_context>", cfg.Context.Mode, req.MemoryPromptBlock),
		wrapJSON("previous_evaluations", req.Recent),
		wrapJSON("dimension_limits", cfg.Limits),
		`Return JSON matching this schema:
{
  "delta": {
    "valence": 0,
    "arousal": 0,
    "dominance": 0,
    "energy": 0,
    "warmth": 0,
    "concern": 0,
    "curiosity": 0,
    "playfulness": 0,
    "attachment": 0,
    "frustration": 0,
    "uncertainty": 0
  },
  "label": "steady",
  "mood_description": "心情平稳、温和",
  "mood_reason": "没有明显改变心情的事件",
  "prompt_mood_text": "平稳、温和，没有明显额外波动。",
  "cause_summary": "short internal cause summary",
  "visible_cause_summary": "short safe visible summary",
  "confidence": 0.5
}`,
	}
	return affectPrompt{System: system, User: strings.Join(parts, "\n\n")}
}

func buildThinkingConfig(cfg config.AgentAffectEvaluatorConfig) *llm.ThinkingConfig {
	if !cfg.ThinkingEnabled {
		return nil
	}
	return &llm.ThinkingConfig{Mode: "enabled", Effort: cfg.ReasoningEffort}
}

func wrapJSON(tag string, v any) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		data = []byte("{}")
	}
	return fmt.Sprintf("<%s>\n%s\n</%s>", tag, data, tag)
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
