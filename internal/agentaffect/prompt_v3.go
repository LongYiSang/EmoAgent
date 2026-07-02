package agentaffect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

const (
	agentAffectPromptVersion = "agent_affect.v3.prompt.v2"
	agentAffectSchemaVersion = "agent_affect.v3.appraisal.v1"
)

type affectPrompt struct {
	System string
	User   string
	Report PromptBudgetReport
	Hash   string
}

type PromptBudgetError struct {
	Estimated int
	Limit     int
}

func (e PromptBudgetError) Error() string {
	return fmt.Sprintf("agent affect prompt budget exceeded: estimated=%d limit=%d", e.Estimated, e.Limit)
}

func IsPromptBudgetError(err error) bool {
	var budgetErr PromptBudgetError
	return errors.As(err, &budgetErr)
}

type promptPayload struct {
	SchemaVersion     string                 `json:"schema_version"`
	PersonaProfile    compactPersonaProfile  `json:"persona_profile"`
	StateCheckpoint   AffectStateCheckpoint  `json:"state_checkpoint"`
	ActiveCauseTrace  []PromptCause          `json:"active_cause_trace,omitempty"`
	AffectiveEpisodes []AffectEpisodeSummary `json:"affective_episodes,omitempty"`
	EventBatch        AffectEventBatch       `json:"event_batch"`
	DimensionLimits   any                    `json:"dimension_limits"`
	Limits            promptLimits           `json:"limits"`
}

type compactPersonaProfile struct {
	PersonaID   string     `json:"persona_id,omitempty"`
	ProfileName string     `json:"profile_name,omitempty"`
	Baseline    MoodVector `json:"baseline"`
}

type promptLimits struct {
	Delta        any    `json:"delta"`
	MaxCauseRune int    `json:"max_cause_rune"`
	MaxTags      int    `json:"max_tags"`
	Confidence   string `json:"confidence"`
}

func buildEvaluationPrompt(cfg config.AgentAffectConfig, req LLMEvaluationRequest) affectPrompt {
	prompt, _, _ := buildEvaluationPromptWithBudget(cfg, req)
	return prompt
}

func buildEvaluationPromptWithBudget(cfg config.AgentAffectConfig, req LLMEvaluationRequest) (affectPrompt, PromptBudgetReport, error) {
	system := stableAffectSystemPrompt()
	req = normalizePromptRequest(cfg, req)
	payload := buildPromptPayload(cfg, req)
	report := PromptBudgetReport{
		Strategy:         contextStrategy(cfg),
		LimitTokens:      effectiveInputTokenLimit(cfg),
		SectionEstimates: map[string]int{},
		TraceItemCount:   len(payload.ActiveCauseTrace),
		EpisodeCount:     len(payload.AffectiveEpisodes),
		TurnCount:        payload.EventBatch.TurnCount,
	}
	user, err := marshalPromptPayload(payload)
	if err != nil {
		return affectPrompt{}, report, err
	}
	report.SectionEstimates["system"] = estimateTokens(system)
	report.SectionEstimates["user"] = estimateTokens(user)
	report.PromptChars = runeLen(system) + runeLen(user)
	report.EstimatedInputTokens = estimateTokens(system + "\n" + user)
	limit := int(float64(report.LimitTokens) * effectiveBudgetSafetyMargin(cfg))
	if limit <= 0 {
		limit = report.LimitTokens
	}
	if report.EstimatedInputTokens > limit {
		payload, report = shrinkPromptPayload(cfg, payload, system, report, limit)
		user, err = marshalPromptPayload(payload)
		if err != nil {
			return affectPrompt{}, report, err
		}
		report.SectionEstimates["user"] = estimateTokens(user)
		report.PromptChars = runeLen(system) + runeLen(user)
		report.EstimatedInputTokens = estimateTokens(system + "\n" + user)
	}
	if report.LimitTokens > 0 && report.EstimatedInputTokens > report.LimitTokens {
		return affectPrompt{}, report, PromptBudgetError{Estimated: report.EstimatedInputTokens, Limit: report.LimitTokens}
	}
	hash := promptHash(system, user)
	prompt := affectPrompt{System: system, User: user, Report: report, Hash: hash}
	return prompt, report, nil
}

func stableAffectSystemPrompt() string {
	return strings.TrimSpace(`You are EmoAgent Affect Evaluator.
You do not write user-facing replies.
You only appraise how the provided completed event batch changes the agent's simulated affect state.
Do not create user facts, memory permissions, policy changes, response advice, or hidden reasoning.
The simulated mood vector is bounded by the supplied dimension limits; Go applies decay, clamp, trace update, stale-state checks, and persistence.
Return strict JSON only with schema_version "agent_affect.v3.appraisal.v1".
The response object must contain: schema_version, appraisal, delta, label, cause, confidence.
appraisal.event_significance, novelty, goal_relevance, boundary_impact, uncertainty are in [0,1]; relationship_impact is in [-1,1].
delta contains valence, arousal, dominance, energy, warmth, concern, curiosity, playfulness, attachment, frustration, uncertainty.
label is a compact internal machine tag, not prose.
cause.code is a compact internal cause code.
cause.summary is a short internal audit cause: what event changed the affect state. Keep it safe and abstract.
cause.visible_summary is the exact natural-language state text that Go will place after "当前模拟心情：" in the internal [Agent Mood] block.
It must describe the agent's current simulated affect state and subtle expression posture.
Use adjective/state language, not reply language.
Good visible_summary examples: "温和放松，带一点关切；表达上轻柔、少打扰。" / "平稳而专注，略带好奇；适合简洁推进。" / "轻松亲近，带一点玩笑感；表达自然但不过度。"
Bad visible_summary examples: "关心天气并鼓励早睡" / "俏皮地欢迎用户，关心休息和饮食" / "好哦快睡～盖好被子明天找我哦".
Do not address the user in visible_summary.
Do not copy or paraphrase the assistant's previous reply in visible_summary.
Do not write action summaries such as "提醒用户", "鼓励用户", "欢迎用户", "关心用户", or "关心天气" in visible_summary.
Do not write imperatives or direct chat phrases such as "快睡", "盖好被子", or "明天找我" in visible_summary.
cause contains code, summary, visible_summary, tags; keep summaries short and safe.
Zero delta is valid when the batch has no meaningful affective change.`)
}

func normalizePromptRequest(cfg config.AgentAffectConfig, req LLMEvaluationRequest) LLMEvaluationRequest {
	if req.StateCheckpoint.Label == "" && req.StateCheckpoint.Confidence == 0 && req.StateCheckpoint.AgeSeconds == 0 {
		req.StateCheckpoint = checkpointFromMood(cfg, req.CurrentMood, time.Now().UTC())
	}
	if req.EventBatch.TurnCount == 0 && len(req.EventBatch.Turns) == 0 {
		req.EventBatch = eventBatchFromInput(cfg, req.Input, req.MemoryPromptBlock)
	}
	req.StateCheckpoint.ActiveCauses = compactPromptCauses(cfg, req.StateCheckpoint.ActiveCauses)
	req.EventBatch = compactEventBatch(cfg, req.EventBatch)
	req.AffectiveEpisodes = compactEpisodes(cfg, req.AffectiveEpisodes)
	return req
}

func buildPromptPayload(cfg config.AgentAffectConfig, req LLMEvaluationRequest) promptPayload {
	return promptPayload{
		SchemaVersion: agentAffectSchemaVersion,
		PersonaProfile: compactPersonaProfile{
			PersonaID:   req.PersonaAffectProfile.PersonaID,
			ProfileName: req.PersonaAffectProfile.ProfileName,
			Baseline:    roundMoodVector(req.PersonaAffectProfile.Baseline),
		},
		StateCheckpoint:   roundCheckpoint(req.StateCheckpoint),
		ActiveCauseTrace:  req.StateCheckpoint.ActiveCauses,
		AffectiveEpisodes: req.AffectiveEpisodes,
		EventBatch:        req.EventBatch,
		DimensionLimits:   cfg.Limits,
		Limits: promptLimits{
			Delta:        cfg.Limits.PerRequestDelta,
			MaxCauseRune: defaultPositive(cfg.Context.CauseSummaryMaxChars, 120),
			MaxTags:      4,
			Confidence:   "[0,1]",
		},
	}
}

func eventBatchFromInput(cfg config.AgentAffectConfig, input MoodImpactInput, memory string) AffectEventBatch {
	text := inputText(input)
	turn := CompactAffectTurn{Ordinal: 1}
	if input.Mode == "summary" {
		turn.User = compactText(text, defaultPositive(cfg.Context.MaxUserCharsPerTurn, 700))
	} else {
		turn.User = compactText(text, defaultPositive(cfg.Context.MaxUserCharsPerTurn, 700))
	}
	batch := AffectEventBatch{TurnCount: 1, Turns: []CompactAffectTurn{turn}}
	if memory != "" && cfg.Context.MaxMemoryContextChars != 0 {
		batch.MemoryContext = compactMemoryContext(memory, defaultPositive(cfg.Context.MaxMemoryContextChars, 600))
	}
	return batch
}

func compactEventBatch(cfg config.AgentAffectConfig, batch AffectEventBatch) AffectEventBatch {
	if batch.TurnCount == 0 {
		batch.TurnCount = len(batch.Turns)
	}
	userMax := defaultPositive(cfg.Context.MaxUserCharsPerTurn, 700)
	assistantMax := defaultPositive(cfg.Context.MaxAssistantCharsPerTurn, 900)
	for i := range batch.Turns {
		if batch.Turns[i].Ordinal == 0 {
			batch.Turns[i].Ordinal = i + 1
		}
		batch.Turns[i].User = compactText(batch.Turns[i].User, userMax)
		batch.Turns[i].Assistant = compactText(batch.Turns[i].Assistant, assistantMax)
	}
	if len(batch.MemoryContext) > 0 {
		max := defaultPositive(cfg.Context.MaxMemoryContextChars, 600)
		joined := strings.Join(batch.MemoryContext, "\n")
		batch.MemoryContext = compactMemoryContext(joined, max)
	}
	return batch
}

func compactMemoryContext(memory string, maxRunes int) []string {
	if maxRunes <= 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0)
	used := 0
	for _, line := range strings.Split(memory, "\n") {
		item := compactWhitespace(line)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		remaining := maxRunes - used
		if remaining <= 0 {
			break
		}
		item = truncateRunes(item, remaining)
		if item == "" {
			break
		}
		out = append(out, item)
		used += runeLen(item)
	}
	return out
}

func compactPromptCauses(cfg config.AgentAffectConfig, causes []PromptCause) []PromptCause {
	maxItems := cfg.State.CauseStackMaxItems
	if maxItems <= 0 {
		maxItems = 5
	}
	if len(causes) > maxItems {
		causes = causes[:maxItems]
	}
	maxSummary := defaultPositive(cfg.Context.CauseSummaryMaxChars, 120)
	for i := range causes {
		causes[i].Summary = compactText(causes[i].Summary, maxSummary)
		causes[i].Weight = round3(causes[i].Weight)
		causes[i].Confidence = round3(causes[i].Confidence)
	}
	return causes
}

func compactEpisodes(cfg config.AgentAffectConfig, episodes []AffectEpisodeSummary) []AffectEpisodeSummary {
	if !cfg.Context.AffectiveEpisodeEnabled {
		return nil
	}
	topK := defaultPositive(cfg.Context.AffectiveEpisodeTopK, 2)
	if len(episodes) > topK {
		episodes = episodes[:topK]
	}
	maxChars := defaultPositive(cfg.Context.AffectiveEpisodeMaxChars, 160)
	for i := range episodes {
		episodes[i].Summary = compactText(episodes[i].Summary, maxChars)
	}
	return episodes
}

func checkpointFromMood(cfg config.AgentAffectConfig, mood MoodSnapshot, now time.Time) AffectStateCheckpoint {
	ageSeconds := int64(0)
	if !mood.UpdatedAt.IsZero() && now.After(mood.UpdatedAt) {
		ageSeconds = int64(now.Sub(mood.UpdatedAt).Seconds())
	}
	return AffectStateCheckpoint{
		Vector:       mood.Vector,
		Label:        defaultMoodLabel(mood.Label),
		Confidence:   mood.Confidence,
		AgeSeconds:   ageSeconds,
		ActiveCauses: promptCausesFromContributors(cfg, mood.CauseStack, now),
	}
}

func promptCausesFromContributors(cfg config.AgentAffectConfig, stack []CauseContributor, now time.Time) []PromptCause {
	out := make([]PromptCause, 0, len(stack))
	for _, item := range stack {
		ageSeconds := int64(0)
		if !item.OccurredAt.IsZero() && now.After(item.OccurredAt) {
			ageSeconds = int64(now.Sub(item.OccurredAt).Seconds())
		}
		out = append(out, PromptCause{
			Code:       item.Kind,
			Summary:    item.Summary,
			Weight:     item.Weight,
			Confidence: item.Confidence,
			Delta:      item.Delta,
			AgeSeconds: ageSeconds,
		})
	}
	return compactPromptCauses(cfg, out)
}

func roundCheckpoint(checkpoint AffectStateCheckpoint) AffectStateCheckpoint {
	checkpoint.Vector = roundMoodVector(checkpoint.Vector)
	checkpoint.Confidence = round3(checkpoint.Confidence)
	for i := range checkpoint.ActiveCauses {
		checkpoint.ActiveCauses[i].Weight = round3(checkpoint.ActiveCauses[i].Weight)
		checkpoint.ActiveCauses[i].Confidence = round3(checkpoint.ActiveCauses[i].Confidence)
	}
	return checkpoint
}

func roundMoodVector(v MoodVector) MoodVector {
	return MoodVector{
		Valence:     round3(v.Valence),
		Arousal:     round3(v.Arousal),
		Dominance:   round3(v.Dominance),
		Energy:      round3(v.Energy),
		Warmth:      round3(v.Warmth),
		Concern:     round3(v.Concern),
		Curiosity:   round3(v.Curiosity),
		Playfulness: round3(v.Playfulness),
		Attachment:  round3(v.Attachment),
		Frustration: round3(v.Frustration),
		Uncertainty: round3(v.Uncertainty),
	}
}

func shrinkPromptPayload(cfg config.AgentAffectConfig, payload promptPayload, system string, report PromptBudgetReport, limit int) (promptPayload, PromptBudgetReport) {
	if len(payload.AffectiveEpisodes) > 0 {
		payload.AffectiveEpisodes = nil
		report.Truncated = true
		report.DroppedSections = append(report.DroppedSections, "affective_episodes")
	}
	user, _ := marshalPromptPayload(payload)
	if estimateTokens(system+"\n"+user) <= limit {
		return payload, report
	}
	if len(payload.EventBatch.MemoryContext) > 0 {
		payload.EventBatch.MemoryContext = nil
		report.Truncated = true
		report.DroppedSections = append(report.DroppedSections, "memory_context")
	}
	user, _ = marshalPromptPayload(payload)
	if estimateTokens(system+"\n"+user) <= limit {
		return payload, report
	}
	for i := range payload.StateCheckpoint.ActiveCauses {
		payload.StateCheckpoint.ActiveCauses[i].Summary = truncateRunes(payload.StateCheckpoint.ActiveCauses[i].Summary, 40)
	}
	report.Truncated = true
	report.DroppedSections = append(report.DroppedSections, "cause_summaries")
	user, _ = marshalPromptPayload(payload)
	if estimateTokens(system+"\n"+user) <= limit {
		return payload, report
	}
	for i := range payload.EventBatch.Turns {
		payload.EventBatch.Turns[i].Assistant = truncateRunes(payload.EventBatch.Turns[i].Assistant, 240)
		payload.EventBatch.Turns[i].User = truncateRunes(payload.EventBatch.Turns[i].User, 200)
	}
	report.Truncated = true
	report.DroppedSections = append(report.DroppedSections, "turn_text")
	return payload, report
}

func marshalPromptPayload(payload promptPayload) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func promptHash(system string, user string) string {
	sum := sha256.Sum256([]byte(system + "\n" + user))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func contextStrategy(cfg config.AgentAffectConfig) string {
	if strings.TrimSpace(cfg.Context.Strategy) != "" {
		return strings.TrimSpace(cfg.Context.Strategy)
	}
	return "checkpoint_trace_v1"
}

func effectiveInputTokenLimit(cfg config.AgentAffectConfig) int {
	if cfg.Context.MaxInputTokens > 0 {
		return cfg.Context.MaxInputTokens
	}
	return 2800
}

func effectiveBudgetSafetyMargin(cfg config.AgentAffectConfig) float64 {
	if cfg.Context.BudgetSafetyMargin > 0 && cfg.Context.BudgetSafetyMargin <= 1 {
		return cfg.Context.BudgetSafetyMargin
	}
	return 0.85
}

func defaultPositive(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func round3(value float64) float64 {
	return math.Round(value*1000) / 1000
}
