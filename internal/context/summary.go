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
	summaryMaxTokens   = 1024
	summaryTemperature = 0.1
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
	persona *config.Persona,
	history []storage.MessageRecord,
	state *ContextState,
	cfg config.ContextConfig,
) (*ContextState, error) {
	if client == nil {
		return nil, fmt.Errorf("summary client is required")
	}
	if persona == nil {
		return nil, fmt.Errorf("persona is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	current := defaultContextState(cfg)
	if state != nil {
		current = normalizeContextState(*state, cfg)
	}

	delta, lastCoveredID := summaryDelta(history, current.SummaryCoveredUntilMessageID, cfg.KeepRecentUserTurns)
	if len(delta) == 0 {
		return &current, nil
	}

	req, err := buildSummaryRequest(model, persona, current.RunningSummary, delta)
	if err != nil {
		return nil, err
	}
	resp, err := client.Chat(ctx, req)
	if err != nil {
		return nil, err
	}

	nextSummary, err := parseRunningSummaryResponse(resp)
	if err != nil {
		return nil, err
	}

	current.RunningSummary = mergeRunningSummary(current.RunningSummary, nextSummary)
	current.SummaryCoveredUntilMessageID = lastCoveredID
	current.SummaryUpdatedAt = time.Now().UTC().Format(time.RFC3339)
	current.KeepRecentUserTurns = cfg.KeepRecentUserTurns
	return &current, nil
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

func buildSummaryRequest(model string, persona *config.Persona, current RunningSummary, delta []storage.MessageRecord) (llm.ChatRequest, error) {
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

	return llm.ChatRequest{
		Model:       model,
		System:      summarySystemPrompt,
		MaxTokens:   summaryMaxTokens,
		Temperature: summaryTemperature,
		Stream:      false,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: string(currentPayload)},
			{Role: llm.RoleUser, Content: string(historyPayload)},
		},
	}, nil
}

func parseRunningSummaryResponse(resp *llm.ChatResponse) (RunningSummary, error) {
	if resp == nil {
		return RunningSummary{}, fmt.Errorf("summary response is nil")
	}
	content := strings.TrimSpace(resp.Content)
	if content == "" {
		for _, block := range resp.ContentBlocks {
			if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
				content = strings.TrimSpace(block.Text)
				break
			}
		}
	}
	if content == "" {
		return RunningSummary{}, fmt.Errorf("summary response content is empty")
	}

	var envelope struct {
		RunningSummary RunningSummary `json:"running_summary"`
	}
	if err := json.Unmarshal([]byte(content), &envelope); err != nil {
		return RunningSummary{}, fmt.Errorf("unmarshal running summary response: %w", err)
	}
	return normalizeRunningSummary(envelope.RunningSummary), nil
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
