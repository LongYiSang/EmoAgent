package tokenmeter

import (
	"context"
	"encoding/json"
	"math"
	"unicode"

	"github.com/longyisang/emoagent/internal/llm"
)

const (
	messageRoleOverhead  = 4
	contentBlockOverhead = 8
	toolBlockExtra       = 20
)

type HeuristicCounter struct{}

func NewHeuristicCounter() HeuristicCounter {
	return HeuristicCounter{}
}

func DefaultCounter() Counter {
	return NewHeuristicCounter()
}

func (HeuristicCounter) CountText(_ context.Context, _, _, text string) CountResult {
	return CountResult{
		InputTokens: estimateText(text),
		Method:      MethodHeuristicCJK,
		Confidence:  0.55,
	}
}

func (c HeuristicCounter) CountMessages(ctx context.Context, providerID, model string, messages []llm.Message) CountResult {
	total := 0
	for _, msg := range messages {
		total += c.estimateMessage(ctx, providerID, model, msg)
	}
	return CountResult{InputTokens: total, Method: MethodHeuristicCJK, Confidence: 0.55}
}

func (c HeuristicCounter) CountChatRequest(ctx context.Context, req CountRequest) CountResult {
	total := c.CountText(ctx, req.ProviderID, req.Model, req.System).InputTokens
	total += c.CountMessages(ctx, req.ProviderID, req.Model, req.Messages).InputTokens
	if len(req.Tools) > 0 {
		if payload, err := json.Marshal(req.Tools); err == nil {
			total += c.CountText(ctx, req.ProviderID, req.Model, string(payload)).InputTokens
		}
	}
	return CountResult{InputTokens: total, Method: MethodHeuristicCJK, Confidence: 0.55}
}

func (c HeuristicCounter) CountChatResponse(ctx context.Context, providerID, model string, resp *llm.ChatResponse) CountResult {
	if resp == nil {
		return CountResult{Method: MethodHeuristicCJK, Confidence: 0.55}
	}
	total := c.CountText(ctx, providerID, model, resp.Content).InputTokens
	total += c.CountText(ctx, providerID, model, resp.ReasoningContent).InputTokens
	for _, block := range resp.ContentBlocks {
		total += contentBlockOverhead
		switch block.Type {
		case string(llm.PartText):
			total += c.CountText(ctx, providerID, model, block.Text).InputTokens
		case string(llm.PartToolUse):
			total += toolBlockExtra
			total += c.CountText(ctx, providerID, model, block.Name).InputTokens
			total += c.CountText(ctx, providerID, model, string(block.Input)).InputTokens
		case string(llm.PartToolResult):
			total += toolBlockExtra
			total += c.CountText(ctx, providerID, model, block.Content).InputTokens
		default:
			total += c.CountText(ctx, providerID, model, block.Text).InputTokens
			total += c.CountText(ctx, providerID, model, block.Content).InputTokens
			total += c.CountText(ctx, providerID, model, string(block.Input)).InputTokens
		}
	}
	return CountResult{OutputTokens: total, Method: MethodHeuristicCJK, Confidence: 0.55}
}

func (c HeuristicCounter) estimateMessage(ctx context.Context, providerID, model string, msg llm.Message) int {
	total := messageRoleOverhead + c.CountText(ctx, providerID, model, msg.Content).InputTokens
	for _, block := range msg.ContentBlocks {
		total += contentBlockOverhead
		switch block.Type {
		case string(llm.PartText):
			total += c.CountText(ctx, providerID, model, block.Text).InputTokens
		case string(llm.PartToolUse):
			total += toolBlockExtra
			total += c.CountText(ctx, providerID, model, block.Name).InputTokens
			total += c.CountText(ctx, providerID, model, string(block.Input)).InputTokens
		case string(llm.PartToolResult):
			total += toolBlockExtra
			total += c.CountText(ctx, providerID, model, block.Content).InputTokens
		default:
			total += c.CountText(ctx, providerID, model, block.Text).InputTokens
			total += c.CountText(ctx, providerID, model, block.Content).InputTokens
			total += c.CountText(ctx, providerID, model, string(block.Input)).InputTokens
		}
	}
	if msg.ToolCallID != "" {
		total += c.CountText(ctx, providerID, model, msg.ToolCallID).InputTokens
	}
	if msg.ReasoningContent != "" {
		total += c.CountText(ctx, providerID, model, msg.ReasoningContent).InputTokens
	}
	return total
}

func estimateText(text string) int {
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

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hangul, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hiragana, r)
}
