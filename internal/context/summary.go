package context

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/promptcenter"
	"github.com/longyisang/emoagent/internal/storage"
)

const (
	defaultSummaryMaxTokens       = 4096
	defaultSummaryTemperature     = 0.1
	defaultSummaryFailureCooldown = 2 * time.Minute
)

var summarySystemPrompt = strings.TrimSpace(`
You maintain the persistent running_summary for an emotion-oriented companion conversation.
Return exactly one JSON object with this shape:
{
  "running_summary": {
    "session_goal": "",
    "user_facts": [],
    "relationship_state": {
      "tone": "",
      "recent_emotion": "",
      "promises_made": []
    },
    "open_loops": [],
    "decisions": [],
    "do_not_forget": []
  }
}

Update rules:
- Merge the current running_summary with the new messages; do not summarize only the delta.
- Preserve still-valid promises_made and do_not_forget unless new messages explicitly revoke, fulfill, or supersede them.
- Add durable user facts, preferences, boundaries, recurring needs, and relationship-relevant context that could help future conversations.
- Omit transient small talk, one-off wording, raw tool output, stack traces, protocol objects, and internal IDs.
- Do not store credentials, secrets, private keys, access tokens, or sensitive operational data.
- relationship_state.tone should describe the current interaction style in a short phrase.
- relationship_state.recent_emotion should be cautious and descriptive; do not diagnose mental health.
- open_loops should contain unresolved commitments, pending questions, or tasks that still need follow-up.
- decisions should contain user or assistant decisions that change future behavior, task direction, or preferences.
- do_not_forget should contain high-importance memory only; keep it short and deduplicated.
- Remove obsolete items when the new messages clearly make them false or fulfilled.
- Deduplicate semantically similar entries. Keep each array item to one concise sentence.
- Use empty strings and empty arrays when unknown.
- JSON only. No markdown, prose, code fences, or explanations.
`)

var summaryRepairSystemPrompt = strings.TrimSpace(`
Repair the running_summary response to the exact required JSON schema.
Do not add facts that are not present in the provided current summary or messages.
Remove protocol leaks, raw tool output, credentials, secrets, stack traces, internal IDs, and any prose outside JSON.
Return JSON only. No markdown, code fences, or explanations.
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
	return UpdateRunningSummaryWithParamsAndPromptResolver(ctx, client, model, params, persona, history, state, cfg, nil, promptcenter.PromptScope{})
}

func UpdateRunningSummaryWithParamsAndPromptResolver(
	ctx context.Context,
	client llm.Client,
	model string,
	params llm.RequestParams,
	persona *config.Persona,
	history []storage.MessageRecord,
	state *ContextState,
	cfg config.ContextConfig,
	resolver *promptcenter.Resolver,
	scope promptcenter.PromptScope,
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

	systemPrompt, systemComponent := resolvePromptComponent(ctx, resolver, promptcenter.ComponentRunningSummarySystem, scope, summarySystemPrompt)
	report.PromptAudit = &SummaryPromptAudit{
		Purpose:      "context.running_summary.update",
		SystemPrompt: systemPrompt,
		Components:   []promptcenter.RenderComponent{withComponentSection(systemComponent, "running_summary_system")},
		Model:        model,
		Attempted:    true,
	}
	req, err := buildSummaryRequestWithParamsAndSystemPrompt(model, params, persona, current.RunningSummary, delta, systemPrompt)
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
		repairPrompt, repairComponent := resolvePromptComponent(ctx, resolver, promptcenter.ComponentRunningSummaryRepair, scope, summaryRepairSystemPrompt)
		report.RepairPromptAudit = &SummaryPromptAudit{
			Purpose:      "context.running_summary.repair",
			SystemPrompt: repairPrompt,
			Components:   []promptcenter.RenderComponent{withComponentSection(repairComponent, "running_summary_repair")},
			Model:        model,
			Attempted:    true,
		}
		repairReq, repairBuildErr := buildSummaryRepairRequestWithSystemPrompt(req, resp, err, repairPrompt)
		if repairBuildErr == nil {
			repairResp, repairErr := client.Chat(ctx, repairReq)
			copyResponseStatsToReport(&report, repairResp)
			if repairErr == nil {
				nextSummary, err = parseRunningSummaryResponse(repairResp)
			} else {
				err = fmt.Errorf("summary repair LLM call: %w", repairErr)
			}
		} else {
			err = repairBuildErr
		}
	}
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
	return normalizeRunningSummary(next)
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
	return buildSummaryRequestWithParamsAndSystemPrompt(model, params, persona, current, delta, summarySystemPrompt)
}

func buildSummaryRequestWithParamsAndSystemPrompt(model string, params llm.RequestParams, persona *config.Persona, current RunningSummary, delta []storage.MessageRecord, systemPrompt string) (llm.ChatRequest, error) {
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
		System:      strings.TrimSpace(systemPrompt),
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
		summary, err := decodeAndValidateRunningSummary(content)
		if err != nil {
			return RunningSummary{}, fmt.Errorf("validate running summary response: %w", err)
		}
		return summary, nil
	}

	if resp.StopReason == "max_tokens" {
		return RunningSummary{}, fmt.Errorf("summary response truncated: stop_reason=max_tokens raw_stop_reason=%q", resp.RawStopReason)
	}
	return RunningSummary{}, fmt.Errorf("unmarshal running summary response: complete running_summary JSON object not found")
}

func buildSummaryRepairRequest(req llm.ChatRequest, resp *llm.ChatResponse, parseErr error) (llm.ChatRequest, error) {
	return buildSummaryRepairRequestWithSystemPrompt(req, resp, parseErr, summaryRepairSystemPrompt)
}

func buildSummaryRepairRequestWithSystemPrompt(req llm.ChatRequest, resp *llm.ChatResponse, parseErr error, systemPrompt string) (llm.ChatRequest, error) {
	payload, err := json.Marshal(struct {
		Error           string `json:"error"`
		InvalidResponse string `json:"invalid_response"`
	}{
		Error:           parseErr.Error(),
		InvalidResponse: truncateRepairContent(firstSummaryResponseCandidate(resp)),
	})
	if err != nil {
		return llm.ChatRequest{}, fmt.Errorf("marshal summary repair payload: %w", err)
	}

	repairReq := req
	repairReq.System = strings.TrimSpace(systemPrompt)
	repairReq.Messages = append(append([]llm.Message(nil), req.Messages...), llm.Message{
		Role:    llm.RoleUser,
		Content: string(payload),
	})
	repairReq.Temperature = 0
	repairReq.Stream = false
	repairReq.Params = req.Params
	zero := 0.0
	repairReq.Params.Temperature = &zero
	stream := false
	repairReq.Params.Stream = &stream
	return repairReq, nil
}

func firstSummaryResponseCandidate(resp *llm.ChatResponse) string {
	candidates := summaryResponseCandidates(resp)
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func truncateRepairContent(content string) string {
	const max = 8000
	if len(content) <= max {
		return content
	}
	return content[:max]
}

func decodeAndValidateRunningSummary(content string) (RunningSummary, error) {
	var rawEnvelope struct {
		RunningSummary json.RawMessage `json:"running_summary"`
	}
	if err := decodeStrict([]byte(content), &rawEnvelope); err != nil {
		return RunningSummary{}, fmt.Errorf("decode envelope: %w", err)
	}
	if len(rawEnvelope.RunningSummary) == 0 {
		return RunningSummary{}, fmt.Errorf("running_summary is required")
	}
	var summaryFields map[string]json.RawMessage
	if err := json.Unmarshal(rawEnvelope.RunningSummary, &summaryFields); err != nil {
		return RunningSummary{}, fmt.Errorf("running_summary must be an object: %w", err)
	}
	if err := validateRunningSummaryJSONShape(summaryFields); err != nil {
		return RunningSummary{}, err
	}

	var summary RunningSummary
	if err := decodeStrict(rawEnvelope.RunningSummary, &summary); err != nil {
		return RunningSummary{}, fmt.Errorf("decode running_summary: %w", err)
	}
	summary = normalizeRunningSummary(summary)
	if err := validateRunningSummaryContent(summary); err != nil {
		return RunningSummary{}, err
	}
	return summary, nil
}

func validateRunningSummaryJSONShape(fields map[string]json.RawMessage) error {
	for _, spec := range []struct {
		name string
		kind string
		want byte
	}{
		{name: "session_goal", kind: "string", want: '"'},
		{name: "user_facts", kind: "array", want: '['},
		{name: "relationship_state", kind: "object", want: '{'},
		{name: "open_loops", kind: "array", want: '['},
		{name: "decisions", kind: "array", want: '['},
		{name: "do_not_forget", kind: "array", want: '['},
	} {
		if err := requireJSONFieldKind(fields, spec.name, spec.kind, spec.want); err != nil {
			return err
		}
	}
	var relationshipFields map[string]json.RawMessage
	if err := json.Unmarshal(fields["relationship_state"], &relationshipFields); err != nil {
		return fmt.Errorf("relationship_state must be an object: %w", err)
	}
	for _, spec := range []struct {
		name string
		kind string
		want byte
	}{
		{name: "tone", kind: "string", want: '"'},
		{name: "recent_emotion", kind: "string", want: '"'},
		{name: "promises_made", kind: "array", want: '['},
	} {
		if err := requireJSONFieldKind(relationshipFields, "relationship_state."+spec.name, spec.kind, spec.want); err != nil {
			return err
		}
	}
	return nil
}

func requireJSONFieldKind(fields map[string]json.RawMessage, name string, kind string, want byte) error {
	raw, ok := fields[strings.TrimPrefix(name, "relationship_state.")]
	if !ok {
		return fmt.Errorf("%s is required", name)
	}
	actual, ok := firstJSONByte(raw)
	if !ok || actual != want {
		return fmt.Errorf("%s must be a JSON %s", name, kind)
	}
	return nil
}

func firstJSONByte(raw json.RawMessage) (byte, bool) {
	for _, b := range raw {
		switch b {
		case ' ', '\n', '\r', '\t':
			continue
		default:
			return b, true
		}
	}
	return 0, false
}

func decodeStrict(input []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON input")
		}
		return err
	}
	return nil
}

func validateRunningSummaryContent(summary RunningSummary) error {
	values := []string{
		summary.SessionGoal,
		summary.RelationshipState.Tone,
		summary.RelationshipState.RecentEmotion,
	}
	values = append(values, summary.UserFacts...)
	values = append(values, summary.RelationshipState.PromisesMade...)
	values = append(values, summary.OpenLoops...)
	values = append(values, summary.Decisions...)
	values = append(values, summary.DoNotForget...)
	for _, value := range values {
		if len([]rune(value)) > 1000 {
			return fmt.Errorf("summary text item exceeds 1000 runes")
		}
		if containsProtocolLeak(value) {
			return fmt.Errorf("summary contains internal protocol leakage")
		}
	}
	return nil
}

func containsProtocolLeak(value string) bool {
	lower := strings.ToLower(value)
	for _, term := range []string{
		"decision_packet",
		"approval_request_id",
		"taskreport",
		"task_report",
		"tool_approval",
		"permission_escalation_required",
	} {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
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
