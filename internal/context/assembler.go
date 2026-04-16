package context

import (
	"encoding/json"
	"fmt"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
)

// BuildEmotionContext assembles the 5a emotion context slots without summary state.
func BuildEmotionContext(persona *config.Persona, history []storage.MessageRecord, cfg config.ContextConfig) (AssembledContext, error) {
	return BuildEmotionContextWithToolDigests(persona, history, nil, cfg)
}

// BuildEmotionContextWithToolDigests assembles the 5a emotion context slots with an explicit ToolDigest slot.
func BuildEmotionContextWithToolDigests(persona *config.Persona, history []storage.MessageRecord, toolDigests []ToolDigest, cfg config.ContextConfig) (AssembledContext, error) {
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

	messages, err := composeEmotionMessages(toolDigests, recentMessages)
	if err != nil {
		return AssembledContext{}, err
	}
	budget := NewBudget(cfg, persona.SystemPrompt, messages)
	return AssembledContext{
		System:      persona.SystemPrompt,
		ToolDigests: append([]ToolDigest(nil), toolDigests...),
		Messages:    messages,
		Budget:      budget,
		CompactReport: CompactReport{
			KeptRecentUserTurns: cfg.KeepRecentUserTurns,
			SnippedToolResults:  len(toolDigests),
			UsedToolDigest:      len(toolDigests) > 0,
			PreEstimatedTokens:  budget.EstimatedTokens,
			PostEstimatedTokens: budget.EstimatedTokens,
		},
	}, nil
}

func composeEmotionMessages(toolDigests []ToolDigest, recentMessages []llm.Message) ([]llm.Message, error) {
	messages := make([]llm.Message, 0, len(recentMessages)+1)
	for _, slot := range EmotionSlotOrder {
		switch slot {
		case SlotPinnedContext:
			continue
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
