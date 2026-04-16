package context

import (
	"math"
	"unicode"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
)

const (
	messageRoleOverhead   = 4
	contentBlockOverhead  = 8
	toolBlockExtraOverhad = 20
)

// EstimateTokens performs a coarse CJK-aware token estimate for plain text.
func EstimateTokens(text string) int {
	cjk := 0
	other := 0
	for _, r := range text {
		if isCJK(r) {
			cjk++
			continue
		}
		other++
	}
	tokens := int(math.Ceil(float64(cjk)*0.5 + float64(other)*0.25))
	if tokens < 1 && len(text) > 0 {
		return 1
	}
	return tokens
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

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hangul, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hiragana, r)
}
