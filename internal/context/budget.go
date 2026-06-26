package context

import (
	"context"
	"encoding/json"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tokenmeter"
)

const (
	messageRoleOverhead   = 4
	contentBlockOverhead  = 8
	toolBlockExtraOverhad = 20
)

// EstimateTokens performs a coarse CJK-aware token estimate for plain text.
func EstimateTokens(text string) int {
	return tokenmeter.DefaultCounter().CountText(context.Background(), "", "", text).InputTokens
}

func NewBudget(cfg config.ContextConfig, system string, messages []llm.Message) Budget {
	budget := Budget{
		InputBudgetTokens:   cfg.InputBudgetTokens,
		SoftLimitTokens:     int(float64(cfg.InputBudgetTokens) * cfg.SoftCompactRatio),
		HardLimitTokens:     int(float64(cfg.InputBudgetTokens) * cfg.HardCompactRatio),
		ReserveOutputTokens: cfg.ReserveOutputTokens,
	}

	estimated := EstimateTokens(system)
	for _, msg := range messages {
		estimated += estimateMessageTokens(msg)
	}
	budget.EstimatedTokens = estimated
	return budget
}

func BuildContextStats(input ContextStatsInput) ContextStats {
	req := input.Request
	source := input.Source
	if source == "" {
		source = "estimate"
	}
	return ContextStats{
		SessionID:                 input.SessionID,
		TurnID:                    input.TurnID,
		RequestID:                 input.RequestID,
		ProviderID:                input.ProviderID,
		Model:                     req.Model,
		Round:                     input.Round,
		EstimatedInputTokens:      EstimateRequestTokens(req),
		ContextLimitTokens:        input.ContextConfig.InputBudgetTokens,
		InputBudgetTokens:         input.ContextConfig.InputBudgetTokens,
		ReserveOutputTokens:       input.ContextConfig.ReserveOutputTokens,
		MaxOutputTokens:           req.MaxTokens,
		RawHistoryEstimatedTokens: EstimateRawHistoryTokens(input.RawHistory),
		CompactReason:             input.CompactReason,
		Source:                    source,
		UpdatedAt:                 input.UpdatedAt,
	}
}

func EstimateRequestTokens(req llm.ChatRequest) int {
	total := EstimateTokens(req.System)
	for _, msg := range req.Messages {
		total += estimateMessageTokens(msg)
	}
	if len(req.Tools) > 0 {
		if payload, err := json.Marshal(req.Tools); err == nil {
			total += EstimateTokens(string(payload))
		}
	}
	return total
}

func EstimateRawHistoryTokens(history []storage.MessageRecord) int {
	total := 0
	for _, msg := range history {
		total += estimateMessageTokens(llm.Message{
			Role:    llm.Role(msg.Role),
			Content: msg.Content,
		})
	}
	return total
}

func estimateMessageTokens(msg llm.Message) int {
	total := messageRoleOverhead + EstimateTokens(msg.Content)
	for _, block := range msg.ContentBlocks {
		total += contentBlockOverhead
		switch block.Type {
		case "text":
			total += EstimateTokens(block.Text)
		case "tool_use":
			total += toolBlockExtraOverhad
			total += EstimateTokens(block.Name)
			total += EstimateTokens(string(block.Input))
		case "tool_result":
			total += toolBlockExtraOverhad
			total += EstimateTokens(block.Content)
		}
	}
	if msg.ToolCallID != "" {
		total += EstimateTokens(msg.ToolCallID)
	}
	if msg.ReasoningContent != "" {
		total += EstimateTokens(msg.ReasoningContent)
	}
	return total
}
