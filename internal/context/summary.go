package context

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
)

const (
	defaultSummaryMaxTokens       = 4096
	defaultSummaryTemperature     = 0.1
	defaultSummaryFailureCooldown = 2 * time.Minute
)

var summarySystemPrompt = strings.TrimSpace(`
You maintain a structured running summary for an emotion-oriented conversation.
Return JSON only in the form {"running_summary":{...}}.
Preserve still-valid promises_made and do_not_forget entries unless the new messages explicitly revoke them.
Do not emit prose, markdown, or explanations outside the JSON object.
`)

type sessionStateReader interface {
	GetSession(ctx context.Context, id string) (*storage.SessionRecord, error)
}

type sessionStateWriter interface {
	UpdateSessionMetadata(ctx context.Context, id, metadata string) error
}

// LoadSessionState reads the persisted session metadata and upgrades it to the current schema in memory.
func LoadSessionState(ctx context.Context, db sessionStateReader, sessionID string, cfg config.ContextConfig) (*ContextState, error) {
	if db == nil {
		return nil, fmt.Errorf("session state reader is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	session, err := db.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil || strings.TrimSpace(session.Metadata) == "" {
		state := defaultContextState(cfg)
		return &state, nil
	}

	var state ContextState
	if err := json.Unmarshal([]byte(session.Metadata), &state); err != nil {
		return nil, fmt.Errorf("unmarshal session context state: %w", err)
	}

	normalized := normalizeContextState(state, cfg)
	return &normalized, nil
}

// UpdateSessionContextState writes the normalized session context state into sessions.metadata.
func UpdateSessionContextState(ctx context.Context, db sessionStateWriter, sessionID string, state ContextState) error {
	if db == nil {
		return fmt.Errorf("session state writer is required")
	}

	normalized := normalizeContextState(state, config.DefaultConfig().Context)
	payload, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("marshal session context state: %w", err)
	}
	if err := db.UpdateSessionMetadata(ctx, sessionID, string(payload)); err != nil {
		return err
	}
	return nil
}

// UpdateRunningSummary incrementally extends the running summary with older history outside the recent-turn window.
func UpdateRunningSummary(
	ctx context.Context,
	client llm.Client,
	model string,
	summaryMaxTokens int,
	summaryTemperature *float64,
	persona *config.Persona,
	history []storage.MessageRecord,
	state *ContextState,
	cfg config.ContextConfig,
) (*ContextState, SummaryUpdateReport, error) {
	params := llm.RequestParams{
		MaxTokens:   resolveSummaryMaxTokens(summaryMaxTokens),
		Temperature: cloneFloat64Ptr(summaryTemperature),
	}
	return UpdateRunningSummaryWithParams(ctx, client, model, params, persona, history, state, cfg)
}

func UpdateRunningSummaryWithParams(
	ctx context.Context,
	client llm.Client,
	model string,
	params llm.RequestParams,
	persona *config.Persona,
	history []storage.MessageRecord,
	state *ContextState,
	cfg config.ContextConfig,
) (*ContextState, SummaryUpdateReport, error) {
	report := SummaryUpdateReport{SummaryModel: model}
	if client == nil {
		return nil, report, fmt.Errorf("summary client is required")
	}
	if persona == nil {
		return nil, report, fmt.Errorf("persona is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, report, err
	}

	current := defaultContextState(cfg)
	if state != nil {
		current = normalizeContextState(*state, cfg)
	}
	now := time.Now().UTC()
	report.CoveredUntilBefore = current.SummaryCoveredUntilMessageID
	report.CoveredUntilAfter = current.SummaryCoveredUntilMessageID
	report.FailureCount = current.SummaryFailureCount
	report.RetryAfter = current.SummaryRetryAfter

	if !shouldAttemptSummary(&current, now) {
		report.Skipped = true
		report.SkipReason = "summary_retry_cooldown"
		return &current, report, nil
	}

	delta, lastCoveredID := summaryDelta(history, current.SummaryCoveredUntilMessageID, cfg.KeepRecentUserTurns)
	if len(delta) == 0 {
		report.Skipped = true
		report.SkipReason = "no_summary_delta"
		return &current, report, nil
	}
	report.Attempted = true
	report.DeltaCount = len(delta)

	req, err := buildSummaryRequestWithParams(model, params, persona, current.RunningSummary, delta)
	if err != nil {
		markSummaryFailure(&current, err, now, defaultSummaryFailureCooldown)
		copyFailureStateToReport(&report, current)
		return &current, report, err
	}
	start := time.Now()
	resp, err := client.Chat(ctx, req)
	report.DurationMS = time.Since(start).Milliseconds()
	copyResponseStatsToReport(&report, resp)
	if err != nil {
		markSummaryFailure(&current, err, time.Now().UTC(), defaultSummaryFailureCooldown)
		copyFailureStateToReport(&report, current)
		return &current, report, err
	}

	nextSummary, err := parseRunningSummaryResponse(resp)
	if err != nil {
		markSummaryFailure(&current, err, time.Now().UTC(), defaultSummaryFailureCooldown)
		copyFailureStateToReport(&report, current)
		return &current, report, err
	}

	current.RunningSummary = mergeRunningSummary(current.RunningSummary, nextSummary)
	current.SummaryCoveredUntilMessageID = lastCoveredID
	current.SummaryUpdatedAt = time.Now().UTC().Format(time.RFC3339)
	current.KeepRecentUserTurns = cfg.KeepRecentUserTurns
	markSummarySuccess(&current, time.Now().UTC())
	report.CoveredUntilAfter = current.SummaryCoveredUntilMessageID
	copyFailureStateToReport(&report, current)
	return &current, report, nil
}

func defaultContextState(cfg config.ContextConfig) ContextState {
	return ContextState{
		ContextVersion:      CurrentContextVersion,
		Mode:                ModeEmotion,
		RunningSummary:      normalizeRunningSummary(RunningSummary{}),
		KeepRecentUserTurns: cfg.KeepRecentUserTurns,
	}
}

func normalizeContextState(state ContextState, cfg config.ContextConfig) ContextState {
	if state.ContextVersion <= 0 {
		state.ContextVersion = CurrentContextVersion
	}
	if state.Mode == "" {
		state.Mode = ModeEmotion
	}
	state.RunningSummary = normalizeRunningSummary(state.RunningSummary)
	if state.KeepRecentUserTurns <= 0 {
		state.KeepRecentUserTurns = cfg.KeepRecentUserTurns
	}
	if state.SummaryFailureCount < 0 {
		state.SummaryFailureCount = 0
	}
	return state
}

func normalizeRunningSummary(summary RunningSummary) RunningSummary {
	summary.UserFacts = cloneStringSlice(summary.UserFacts)
	summary.RelationshipState.PromisesMade = cloneStringSlice(summary.RelationshipState.PromisesMade)
	summary.OpenLoops = cloneStringSlice(summary.OpenLoops)
	summary.Decisions = cloneStringSlice(summary.Decisions)
	summary.DoNotForget = cloneStringSlice(summary.DoNotForget)
	return summary
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return append([]string(nil), values...)
}

func mergeRunningSummary(current RunningSummary, next RunningSummary) RunningSummary {
	merged := normalizeRunningSummary(next)
	merged.RelationshipState.PromisesMade = mergeProtectedItems(
		current.RelationshipState.PromisesMade,
		merged.RelationshipState.PromisesMade,
	)
	merged.DoNotForget = mergeProtectedItems(current.DoNotForget, merged.DoNotForget)
	return merged
}

func mergeProtectedItems(existing []string, incoming []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	merged := make([]string, 0, len(existing)+len(incoming))
	for _, item := range existing {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		merged = append(merged, item)
	}
	for _, item := range incoming {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		merged = append(merged, item)
	}
	return merged
}

func buildSummaryRequest(model string, summaryMaxTokens int, summaryTemperature *float64, persona *config.Persona, current RunningSummary, delta []storage.MessageRecord) (llm.ChatRequest, error) {
	return buildSummaryRequestWithParams(model, llm.RequestParams{
		MaxTokens:   resolveSummaryMaxTokens(summaryMaxTokens),
		Temperature: cloneFloat64Ptr(summaryTemperature),
	}, persona, current, delta)
}

func buildSummaryRequestWithParams(model string, params llm.RequestParams, persona *config.Persona, current RunningSummary, delta []storage.MessageRecord) (llm.ChatRequest, error) {
	currentPayload, err := json.Marshal(struct {
		RunningSummary RunningSummary `json:"running_summary"`
	}{
		RunningSummary: normalizeRunningSummary(current),
	})
	if err != nil {
		return llm.ChatRequest{}, fmt.Errorf("marshal current running summary: %w", err)
	}

	type summaryMessage struct {
		ID      string `json:"id"`
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	items := make([]summaryMessage, 0, len(delta))
	for _, msg := range delta {
		items = append(items, summaryMessage{
			ID:      msg.ID,
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	historyPayload, err := json.Marshal(struct {
		PersonaSystemPrompt string           `json:"persona_system_prompt,omitempty"`
		Messages            []summaryMessage `json:"messages"`
	}{
		PersonaSystemPrompt: persona.SystemPrompt,
		Messages:            items,
	})
	if err != nil {
		return llm.ChatRequest{}, fmt.Errorf("marshal summary delta history: %w", err)
	}

	if params.MaxTokens <= 0 {
		params.MaxTokens = defaultSummaryMaxTokens
	}
	if params.Temperature == nil {
		value := defaultSummaryTemperature
		params.Temperature = &value
	}
	stream := false
	params.Stream = &stream
	return llm.ChatRequest{
		Model:       model,
		System:      summarySystemPrompt,
		Params:      params,
		MaxTokens:   params.MaxTokens,
		Temperature: *params.Temperature,
		Stream:      false,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: string(currentPayload)},
			{Role: llm.RoleUser, Content: string(historyPayload)},
		},
	}, nil
}

func DefaultSummaryMaxTokens() int {
	return defaultSummaryMaxTokens
}

func DefaultSummaryTemperature() float64 {
	return defaultSummaryTemperature
}

func resolveSummaryMaxTokens(summaryMaxTokens int) int {
	if summaryMaxTokens > 0 {
		return summaryMaxTokens
	}
	return defaultSummaryMaxTokens
}

func resolveSummaryTemperature(summaryTemperature *float64) float64 {
	if summaryTemperature != nil {
		return *summaryTemperature
	}
	return defaultSummaryTemperature
}

func cloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func parseRunningSummaryResponse(resp *llm.ChatResponse) (RunningSummary, error) {
	if resp == nil {
		return RunningSummary{}, fmt.Errorf("summary response is nil")
	}
	candidates := summaryResponseCandidates(resp)
	if len(candidates) == 0 {
		return RunningSummary{}, fmt.Errorf("summary response content is empty")
	}

	for _, candidate := range candidates {
		content, ok := extractSummaryJSON(candidate)
		if !ok {
			continue
		}
		var envelope struct {
			RunningSummary RunningSummary `json:"running_summary"`
		}
		if err := json.Unmarshal([]byte(content), &envelope); err != nil {
			return RunningSummary{}, fmt.Errorf("unmarshal running summary response: %w", err)
		}
		return normalizeRunningSummary(envelope.RunningSummary), nil
	}

	if resp.StopReason == "max_tokens" {
		return RunningSummary{}, fmt.Errorf("summary response truncated: stop_reason=max_tokens raw_stop_reason=%q", resp.RawStopReason)
	}
	return RunningSummary{}, fmt.Errorf("unmarshal running summary response: complete running_summary JSON object not found")
}

func summaryResponseCandidates(resp *llm.ChatResponse) []string {
	if resp == nil {
		return nil
	}
	var candidates []string
	if content := strings.TrimSpace(resp.Content); content != "" {
		candidates = append(candidates, content)
	}
	var blockText strings.Builder
	for _, block := range resp.ContentBlocks {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			blockText.WriteString(block.Text)
		}
	}
	if content := strings.TrimSpace(blockText.String()); content != "" {
		candidates = append(candidates, content)
	}
	if content := strings.TrimSpace(resp.ReasoningContent); content != "" {
		candidates = append(candidates, content)
	}
	return candidates
}

func extractSummaryJSON(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
scanObjects:
	for start := 0; start < len(trimmed); start++ {
		if trimmed[start] != '{' {
			continue
		}
		depth := 0
		inString := false
		escaped := false
		for i := start; i < len(trimmed); i++ {
			ch := trimmed[i]
			if inString {
				if escaped {
					escaped = false
					continue
				}
				switch ch {
				case '\\':
					escaped = true
				case '"':
					inString = false
				}
				continue
			}
			switch ch {
			case '"':
				inString = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					candidate := trimmed[start : i+1]
					if isRunningSummaryEnvelope(candidate) {
						return candidate, true
					}
					continue scanObjects
				}
			}
		}
	}
	return "", false
}

func isRunningSummaryEnvelope(content string) bool {
	var envelope struct {
		RunningSummary json.RawMessage `json:"running_summary"`
	}
	return json.Unmarshal([]byte(content), &envelope) == nil && len(envelope.RunningSummary) > 0
}

func shouldAttemptSummary(state *ContextState, now time.Time) bool {
	if state == nil || strings.TrimSpace(state.SummaryRetryAfter) == "" {
		return true
	}
	retryAt, err := time.Parse(time.RFC3339, state.SummaryRetryAfter)
	if err != nil {
		return true
	}
	return !now.Before(retryAt)
}

func markSummaryFailure(state *ContextState, err error, now time.Time, cooldown time.Duration) {
	if state == nil {
		return
	}
	state.SummaryFailureCount++
	state.SummaryFailedAt = now.Format(time.RFC3339)
	state.SummaryRetryAfter = now.Add(cooldown).Format(time.RFC3339)
	if err != nil {
		state.SummaryLastError = truncateSummaryError(err.Error())
	}
}

func markSummarySuccess(state *ContextState, _ time.Time) {
	if state == nil {
		return
	}
	state.SummaryFailedAt = ""
	state.SummaryRetryAfter = ""
	state.SummaryFailureCount = 0
	state.SummaryLastError = ""
}

func truncateSummaryError(message string) string {
	const max = 300
	if len(message) <= max {
		return message
	}
	return message[:max]
}

func copyResponseStatsToReport(report *SummaryUpdateReport, resp *llm.ChatResponse) {
	if report == nil || resp == nil {
		return
	}
	report.StopReason = resp.StopReason
	report.RawStopReason = resp.RawStopReason
	report.ContentLength = len(resp.Content)
	if report.ContentLength == 0 {
		for _, block := range resp.ContentBlocks {
			if block.Type == "text" {
				report.ContentLength += len(block.Text)
			}
		}
	}
	report.ReasoningLength = len(resp.ReasoningContent)
}

func copyFailureStateToReport(report *SummaryUpdateReport, state ContextState) {
	if report == nil {
		return
	}
	report.FailureCount = state.SummaryFailureCount
	report.RetryAfter = state.SummaryRetryAfter
}

func summaryDelta(history []storage.MessageRecord, coveredUntilID string, keepRecentUserTurns int) ([]storage.MessageRecord, string) {
	recent := KeepRecentUserTurns(history, keepRecentUserTurns)
	olderCount := len(history) - len(recent)
	if olderCount <= 0 {
		return nil, ""
	}
	older := history[:olderCount]

	start := 0
	if coveredUntilID != "" {
		start = -1
		for i := range older {
			if older[i].ID == coveredUntilID {
				start = i + 1
				break
			}
		}
		if start < 0 {
			return nil, ""
		}
	}
	if start >= len(older) {
		return nil, ""
	}

	delta := append([]storage.MessageRecord(nil), older[start:]...)
	return delta, delta[len(delta)-1].ID
}
