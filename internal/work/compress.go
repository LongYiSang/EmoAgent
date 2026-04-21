package work

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/llm"
)

// snipConsumedToolResults snips tool_result payloads from rounds older than
// currentRoundStart. The LLM has already consumed these results and extracted
// key information into its assistant reply, so the raw content is redundant.
func snipConsumedToolResults(messages []llm.Message, currentRoundStart int, softTokens, hardTokens int) []llm.Message {
	if currentRoundStart <= 0 || currentRoundStart >= len(messages) {
		return append([]llm.Message(nil), messages...)
	}

	result := append([]llm.Message(nil), messages...)
	for i := 0; i < currentRoundStart; i++ {
		msg := result[i]
		switch {
		case msg.Role == llm.RoleTool:
			digest := contextutil.SnipToolResult(
				"tool_result", msg.ToolCallID,
				json.RawMessage(msg.Content),
				softTokens, hardTokens,
			)
			result[i].Content = contextutil.ToolResultContent(digest)
		case len(msg.ContentBlocks) > 0:
			blocks := append([]llm.ContentBlock(nil), msg.ContentBlocks...)
			for j, block := range blocks {
				if block.Type != "tool_result" {
					continue
				}
				digest := contextutil.SnipToolResult(
					"tool_result", block.ID,
					json.RawMessage(block.Content),
					softTokens, hardTokens,
				)
				blocks[j].Content = contextutil.ToolResultContent(digest)
			}
			result[i].ContentBlocks = blocks
		}
	}
	return result
}

// compressWorkContext applies Layer 2 summary compression when estimated tokens
// exceed softRatio * maxInputTokens.
func compressWorkContext(
	ctx context.Context,
	summaryClient llm.Client,
	summaryModel string,
	messages []llm.Message,
	currentProgress WorkProgress,
	systemPrompt string,
	maxInputTokens int,
	softRatio float64,
	keepRounds int,
) ([]llm.Message, WorkProgress, error) {
	totalTokens := estimateMessagesTokens(messages) + contextutil.EstimateTokens(systemPrompt)
	softLimit := int(float64(maxInputTokens) * softRatio)

	if totalTokens <= softLimit {
		return messages, currentProgress, nil
	}

	keepFromIdx := findKeepBoundary(messages, keepRounds)
	if keepFromIdx <= 0 {
		return messages, currentProgress, nil
	}

	oldMessages := messages[:keepFromIdx]
	recentMessages := messages[keepFromIdx:]

	if summaryClient == nil {
		return messages, currentProgress, fmt.Errorf("summary client required for work context compression")
	}

	delta := filterOutProgressMessages(oldMessages)
	if len(delta) == 0 {
		return messages, currentProgress, nil
	}

	req, err := buildProgressSummaryRequest(summaryModel, currentProgress, delta)
	if err != nil {
		return messages, currentProgress, fmt.Errorf("build progress summary request: %w", err)
	}
	resp, err := summaryClient.Chat(ctx, req)
	if err != nil {
		return messages, currentProgress, fmt.Errorf("progress summary LLM call: %w", err)
	}

	newProgress, err := parseProgressSummaryResponse(resp)
	if err != nil {
		return messages, currentProgress, fmt.Errorf("parse progress summary: %w", err)
	}

	newProgress.DecisionsReceived = mergeStringItems(
		currentProgress.DecisionsReceived,
		newProgress.DecisionsReceived,
	)

	progressMsg, err := buildWorkProgressMessage(newProgress)
	if err != nil {
		return messages, currentProgress, fmt.Errorf("build progress message: %w", err)
	}

	compressed := make([]llm.Message, 0, 1+len(recentMessages))
	compressed = append(compressed, progressMsg)
	compressed = append(compressed, recentMessages...)

	slog.Info("work context compressed",
		"before_tokens", totalTokens,
		"after_tokens", estimateMessagesTokens(compressed)+contextutil.EstimateTokens(systemPrompt),
		"old_messages", len(oldMessages),
		"kept_messages", len(recentMessages),
	)

	return compressed, newProgress, nil
}

func isWorkProgressMessage(msg llm.Message) bool {
	if msg.Role != llm.RoleUser || msg.Content == "" {
		return false
	}
	var envelope struct {
		WorkProgress json.RawMessage `json:"work_progress"`
	}
	return json.Unmarshal([]byte(msg.Content), &envelope) == nil && len(envelope.WorkProgress) > 0
}

func filterOutProgressMessages(messages []llm.Message) []llm.Message {
	filtered := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		if isWorkProgressMessage(msg) {
			continue
		}
		filtered = append(filtered, msg)
	}
	return filtered
}

func findKeepBoundary(messages []llm.Message, keepRounds int) int {
	if keepRounds <= 0 || len(messages) == 0 {
		return 0
	}

	rounds := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llm.RoleAssistant {
			rounds++
			if rounds >= keepRounds {
				return i
			}
		}
	}
	return 0
}

func mergeStringItems(existing, incoming []string) []string {
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
