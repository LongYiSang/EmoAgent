package context

import (
	"encoding/json"
	"fmt"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
)

const delegationGuideline = `## Delegation Guideline

When the user's request fits the criteria below, call delegate_to_work instead of trying to handle it yourself:
- Requires reading files, exploring directories, or running commands.
- Needs multi-step tool calls (3 or more steps) to complete.
- Produces large or noisy intermediate output that should stay out of the main chat.
- Requires verification or long-chain research.

When the user just wants to talk, vent, ask a trivial factual question, or wants you to express something, handle it yourself. Do not delegate casual conversation.

The TaskReport you receive is for your eyes only. Never paste raw tool output into your reply; summarize findings in your own voice.`

// BuildEmotionContext assembles the emotion context with no persisted session state.
func BuildEmotionContext(persona *config.Persona, history []storage.MessageRecord, cfg config.ContextConfig) (AssembledContext, error) {
	return buildEmotionContext(persona, history, nil, nil, cfg)
}

// BuildEmotionContextWithState assembles the emotion context using persisted session state.
func BuildEmotionContextWithState(persona *config.Persona, history []storage.MessageRecord, state *ContextState, cfg config.ContextConfig) (AssembledContext, error) {
	return buildEmotionContext(persona, history, state, nil, cfg)
}

// BuildEmotionContextWithToolDigests assembles the emotion context with an explicit ToolDigest slot.
func BuildEmotionContextWithToolDigests(persona *config.Persona, history []storage.MessageRecord, toolDigests []ToolDigest, cfg config.ContextConfig) (AssembledContext, error) {
	return buildEmotionContext(persona, history, nil, toolDigests, cfg)
}

func buildEmotionContext(persona *config.Persona, history []storage.MessageRecord, state *ContextState, toolDigests []ToolDigest, cfg config.ContextConfig) (AssembledContext, error) {
	if persona == nil {
		return AssembledContext{}, fmt.Errorf("persona is required")
	}
	if err := cfg.Validate(); err != nil {
		return AssembledContext{}, err
	}

	recent := KeepRecentUserTurns(history, cfg.KeepRecentUserTurns)
	recentMessages := make([]llm.Message, 0, len(recent))
	for _, msg := range recent {
		recentMessages = append(recentMessages, llm.Message{
			Role:    llm.Role(msg.Role),
			Content: msg.Content,
		})
	}

	messages, err := composeEmotionMessages(state, toolDigests, recentMessages)
	if err != nil {
		return AssembledContext{}, err
	}
	system := buildEmotionSystemPrompt(persona.SystemPrompt)
	budget := NewBudget(cfg, system, messages)
	return AssembledContext{
		System:      system,
		ToolDigests: append([]ToolDigest(nil), toolDigests...),
		Messages:    messages,
		Budget:      budget,
		CompactReport: CompactReport{
			Mode:                    "deterministic",
			CompactReason:           "budget_soft",
			KeptRecentTurns:         cfg.KeepRecentUserTurns,
			SnippedToolResultsCount: len(toolDigests),
			PreEstimatedTokens:      budget.EstimatedTokens,
			PostEstimatedTokens:     budget.EstimatedTokens,
			KeptRecentUserTurns:     cfg.KeepRecentUserTurns,
			SnippedToolResults:      len(toolDigests),
			UsedToolDigest:          len(toolDigests) > 0,
		},
	}, nil
}

func buildEmotionSystemPrompt(base string) string {
	if base == "" {
		return delegationGuideline
	}
	return base + "\n\n" + delegationGuideline
}

func composeEmotionMessages(state *ContextState, toolDigests []ToolDigest, recentMessages []llm.Message) ([]llm.Message, error) {
	capHint := len(recentMessages) + 1
	if len(toolDigests) > 0 {
		capHint++
	}
	if state != nil && !state.RunningSummary.IsZero() {
		capHint++
	}
	messages := make([]llm.Message, 0, capHint)
	for _, slot := range EmotionSlotOrder {
		switch slot {
		case SlotPinnedContext:
			continue
		case SlotRunningSummary:
			if state == nil || state.RunningSummary.IsZero() {
				continue
			}
			msg, err := buildRunningSummarySlotMessage(state.RunningSummary)
			if err != nil {
				return nil, err
			}
			messages = append(messages, msg)
		case SlotToolDigest:
			if len(toolDigests) == 0 {
				continue
			}
			msg, err := buildToolDigestSlotMessage(toolDigests)
			if err != nil {
				return nil, err
			}
			messages = append(messages, msg)
		case SlotRecentTurns:
			messages = append(messages, recentMessages...)
		default:
			return nil, fmt.Errorf("unsupported emotion slot: %s", slot)
		}
	}
	return messages, nil
}

func buildRunningSummarySlotMessage(summary RunningSummary) (llm.Message, error) {
	payload, err := json.Marshal(struct {
		RunningSummary RunningSummary `json:"running_summary"`
	}{
		RunningSummary: normalizeRunningSummary(summary),
	})
	if err != nil {
		return llm.Message{}, fmt.Errorf("marshal running summary slot: %w", err)
	}
	return llm.Message{
		Role:    llm.RoleUser,
		Content: string(payload),
	}, nil
}

func buildToolDigestSlotMessage(toolDigests []ToolDigest) (llm.Message, error) {
	payload, err := json.Marshal(struct {
		ToolDigests []ToolDigest `json:"tool_digests"`
	}{
		ToolDigests: toolDigests,
	})
	if err != nil {
		return llm.Message{}, fmt.Errorf("marshal tool digest slot: %w", err)
	}
	return llm.Message{
		Role:    llm.RoleUser,
		Content: string(payload),
	}, nil
}
