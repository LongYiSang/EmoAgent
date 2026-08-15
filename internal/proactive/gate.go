package proactive

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/promptcenter"
)

// fallbackGateSystemPrompt is used only when no prompt resolver is wired (unit
// tests, degraded startup). The authoritative text lives in the prompt center as
// proactive.gate.system so it can be tuned from the admin UI — this feature
// needs a lot of tuning, and editing Go source to do it is the wrong loop.
const fallbackGateSystemPrompt = `你是一个「打断时机」判断器，为一个情感陪伴 Agent 服务。
你不决定说什么，只决定「现在这一刻打断用户，是否合适」。
大多数时候应该保持沉默。只输出一个 JSON 对象：
{"decision":"speak"|"skip","reason":"简短中文理由","urgency":0.0-1.0,"hint":"给主 Agent 的一句提示；skip 时留空"}`

// Gate asks a cheap model whether now is a good moment to interrupt.
//
// Modelled on internal/chat/prompt_router.go, which is structurally the same
// thing: one inexpensive classification that gates the main pipeline. The one
// deliberate inversion is the failure direction — the router falls back to work
// mode, this falls back to silence. No error may ever cause an unsolicited
// message to be sent.
type Gate struct {
	client   llm.Client
	model    string
	params   llm.RequestParams
	cfg      config.ProactiveGateConfig
	logger   *slog.Logger
	resolver *promptcenter.Resolver
}

func NewGate(client llm.Client, model string, params llm.RequestParams, cfg config.ProactiveGateConfig, logger *slog.Logger) *Gate {
	return &Gate{client: client, model: model, params: params, cfg: cfg, logger: logger}
}

// WithPromptResolver makes the gate prompt editable through the prompt center.
func (g *Gate) WithPromptResolver(resolver *promptcenter.Resolver) *Gate {
	if g != nil {
		g.resolver = resolver
	}
	return g
}

func (g *Gate) systemPrompt(ctx context.Context, personaKey string) string {
	if g == nil || g.resolver == nil {
		return fallbackGateSystemPrompt
	}
	text := strings.TrimSpace(g.resolver.ResolveText(ctx, promptcenter.ComponentProactiveGateSystem, promptcenter.PromptScope{PersonaKey: personaKey}))
	if text == "" {
		return fallbackGateSystemPrompt
	}
	return text
}

// Decide returns the gate verdict. It never returns an error: any failure is
// reported as a skip with reason gate_error, because failing open here would
// mean messaging the user because something broke.
func (g *Gate) Decide(ctx context.Context, personaKey string, in GateInput) Decision {
	decision := Decision{Verdict: VerdictSkip, Reason: ReasonGateError, GateConsulted: true}
	if g == nil || g.client == nil {
		decision.Reason = ReasonNotConfigured
		return decision
	}
	model := g.model
	if strings.TrimSpace(g.cfg.Model) != "" {
		model = g.cfg.Model
	}
	decision.GateModel = model

	payload, err := json.Marshal(in)
	if err != nil {
		g.warn("proactive gate payload encode failed", err)
		return decision
	}

	params := llm.CloneRequestParams(g.params)
	params.MaxTokens = g.cfg.MaxOutputTokens
	zero := 0.0
	params.Temperature = &zero
	stream := false
	params.Stream = &stream

	request := llm.ChatRequest{
		Model:       model,
		System:      g.systemPrompt(ctx, personaKey),
		Messages:    []llm.Message{{Role: llm.RoleUser, Content: string(payload)}},
		Params:      params,
		MaxTokens:   g.cfg.MaxOutputTokens,
		Temperature: 0,
		Stream:      false,
	}

	callCtx := ctx
	if g.cfg.TimeoutMS > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, time.Duration(g.cfg.TimeoutMS)*time.Millisecond)
		defer cancel()
	}

	startedAt := time.Now()
	resp, err := g.client.Chat(callCtx, request)
	decision.GateLatencyMS = time.Since(startedAt).Milliseconds()
	if err != nil {
		g.warn("proactive gate call failed, staying silent", err)
		return decision
	}
	if resp != nil {
		decision.GateTokens = resp.Usage.TotalTokens
	}

	parsed, err := parseGateResponse(resp)
	if err != nil {
		g.warn("proactive gate response unparseable, staying silent", err)
		return decision
	}

	if parsed.Decision != string(VerdictSpeak) {
		decision.Reason = ReasonGateDeclined
		if trimmed := strings.TrimSpace(parsed.Reason); trimmed != "" {
			decision.Reason = trimmed
		}
		return decision
	}

	decision.Verdict = VerdictSpeak
	decision.Reason = strings.TrimSpace(parsed.Reason)
	decision.Urgency = clampUnit(parsed.Urgency)
	decision.Hint = strings.TrimSpace(parsed.Hint)
	return decision
}

func (g *Gate) warn(msg string, err error) {
	if g != nil && g.logger != nil {
		g.logger.Warn(msg, "error", err)
	}
}

func parseGateResponse(resp *llm.ChatResponse) (GateOutput, error) {
	if resp == nil {
		return GateOutput{}, fmt.Errorf("proactive gate response is nil")
	}
	for _, candidate := range llm.ResponseTextCandidates(resp) {
		object, ok := llm.ExtractFirstJSONObject(candidate)
		if !ok {
			continue
		}
		var parsed GateOutput
		if err := json.Unmarshal([]byte(object), &parsed); err != nil {
			return GateOutput{}, fmt.Errorf("decode proactive gate response: %w", err)
		}
		switch parsed.Decision {
		case string(VerdictSpeak), string(VerdictSkip):
			return parsed, nil
		default:
			return GateOutput{}, fmt.Errorf("proactive gate decision %q is invalid", parsed.Decision)
		}
	}
	return GateOutput{}, fmt.Errorf("proactive gate JSON object not found")
}

func clampUnit(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
