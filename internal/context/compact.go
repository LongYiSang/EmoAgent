package context

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
)

const previewRuneLimit = 160

// KeepRecentUserTurns keeps the latest N user turns and every message after the
// earliest kept user message.
func KeepRecentUserTurns(history []storage.MessageRecord, keep int) []storage.MessageRecord {
	if keep <= 0 || len(history) == 0 {
		return nil
	}

	userTurns := 0
	start := len(history)
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == string(llm.RoleUser) {
			userTurns++
			start = i
			if userTurns >= keep {
				break
			}
		}
	}
	if start >= len(history) {
		return append([]storage.MessageRecord(nil), history...)
	}
	return append([]storage.MessageRecord(nil), history[start:]...)
}

// SnipToolResult reduces a tool payload to a digest suitable for logs and context reuse.
func SnipToolResult(toolName, callID string, content json.RawMessage, softTokens, hardTokens int) ToolDigest {
	raw := string(content)
	preview := sanitizePreview(raw)
	size := len(content)
	hash := sha256.Sum256(content)
	digest := ToolDigest{
		ToolName: toolName,
		CallID:   callID,
		Size:     size,
		Preview:  preview,
		Hash:     hex.EncodeToString(hash[:8]),
	}

	estimated := EstimateTokens(raw)
	switch {
	case estimated <= softTokens:
		digest.FullContent = raw
	case estimated <= hardTokens:
		digest.FullContent = buildToolDigestJSON(toolName, callID, preview, digest.Hash, size, true)
		digest.IsTruncated = true
	default:
		digest.FullContent = ""
		digest.IsTruncated = true
	}
	return digest
}

// ToolResultContent returns the JSON payload that should be reused for a tool result.
func ToolResultContent(digest ToolDigest) string {
	if digest.FullContent != "" {
		return digest.FullContent
	}
	return buildToolDigestJSON(digest.ToolName, digest.CallID, digest.Preview, digest.Hash, digest.Size, digest.IsTruncated)
}

func BuildToolResultMessage(provider string, digest ToolDigest, isError bool) llm.Message {
	content := ToolResultContent(digest)
	switch provider {
	case "anthropic":
		return llm.Message{
			Role: llm.RoleUser,
			ContentBlocks: []llm.ContentBlock{{
				Type:    "tool_result",
				ID:      digest.CallID,
				Content: content,
				IsError: isError,
			}},
		}
	case "openai":
		return llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: digest.CallID,
			Content:    content,
		}
	default:
		return llm.Message{}
	}
}

func sanitizePreview(raw string) string {
	preview := strings.ReplaceAll(raw, "\r", " ")
	preview = strings.ReplaceAll(preview, "\n", " ")
	preview = strings.TrimSpace(preview)
	runes := []rune(preview)
	if len(runes) > previewRuneLimit {
		return string(runes[:previewRuneLimit]) + "…"
	}
	return preview
}

func buildToolDigestJSON(toolName, callID, preview, hash string, size int, truncated bool) string {
	return fmt.Sprintf(`{"tool_name":%q,"call_id":%q,"preview":%q,"hash":%q,"size":%d,"is_truncated":%t}`,
		toolName, callID, preview, hash, size, truncated)
}

// ApplyReactiveCompact reduces an already-assembled request after a provider context overflow.
func ApplyReactiveCompact(
	sessionID string,
	messages []llm.Message,
	state *ContextState,
	summaryModel string,
	cfg config.ContextConfig,
) ([]llm.Message, CompactReport, error) {
	if err := cfg.Validate(); err != nil {
		return nil, CompactReport{}, err
	}

	report := CompactReport{
		SessionID:                    sessionID,
		Mode:                         "reactive",
		CompactReason:                "reactive_overflow",
		SummaryModel:                 summaryModel,
		SummaryCoveredUntilMessageID: summaryCoverage(state),
	}

	preEstimated := estimateMessagesTokens(messages)
	report.PreEstimatedTokens = preEstimated

	compacted, snippedCount := compactToolPayloads(messages, cfg)
	report.SnippedToolResultsCount = snippedCount
	report.SnippedToolResults = snippedCount

	summaryMsg, hasSummary := extractRunningSummaryMessage(compacted)
	if !hasSummary && state != nil && !state.RunningSummary.IsZero() {
		msg, err := buildRunningSummarySlotMessage(state.RunningSummary)
		if err != nil {
			return nil, CompactReport{}, err
		}
		summaryMsg = &msg
		hasSummary = true
	}

	lastUserIdx := findLatestConversationUserIndex(compacted)
	if lastUserIdx < 0 {
		if len(compacted) > 0 {
			lastUserIdx = len(compacted) - 1
		} else {
			lastUserIdx = 0
		}
	}

	kept := make([]llm.Message, 0, len(compacted))
	if hasSummary && summaryMsg != nil {
		kept = append(kept, *summaryMsg)
	}
	if lastUserIdx < len(compacted) {
		for i := lastUserIdx; i < len(compacted); i++ {
			if hasSummary && isSameMessage(compacted[i], *summaryMsg) {
				continue
			}
			kept = append(kept, compacted[i])
		}
	}
	if len(kept) == 0 {
		kept = compacted
	}

	postEstimated := estimateMessagesTokens(kept)
	report.PostEstimatedTokens = postEstimated
	report.KeptRecentTurns = countConversationUserTurns(kept)
	report.KeptRecentUserTurns = report.KeptRecentTurns

	if report.KeptRecentTurns <= 1 && postEstimated > hardLimitTokens(cfg) {
		report.Degraded = true
	}

	return kept, report, nil
}

func summaryCoverage(state *ContextState) string {
	if state == nil {
		return ""
	}
	return state.SummaryCoveredUntilMessageID
}

func hardLimitTokens(cfg config.ContextConfig) int {
	return int(float64(cfg.InputBudgetTokens) * cfg.HardCompactRatio)
}

func estimateMessagesTokens(messages []llm.Message) int {
	total := 0
	for _, msg := range messages {
		total += EstimateTokens(msg.Content)
		total += EstimateTokens(msg.ReasoningContent)
		for _, block := range msg.ContentBlocks {
			total += EstimateTokens(block.Text)
			total += EstimateTokens(block.Content)
			total += EstimateTokens(string(block.Input))
			total += EstimateTokens(block.Name)
		}
	}
	return total
}

func compactToolPayloads(messages []llm.Message, cfg config.ContextConfig) ([]llm.Message, int) {
	compacted := append([]llm.Message(nil), messages...)
	snippedCount := 0
	for i, msg := range compacted {
		updated, changed := compactToolMessage(msg, cfg)
		compacted[i] = updated
		if changed {
			snippedCount++
		}
	}
	return compacted, snippedCount
}

func compactToolMessage(msg llm.Message, cfg config.ContextConfig) (llm.Message, bool) {
	switch {
	case msg.Role == llm.RoleTool:
		digest := SnipToolResult("tool_result", msg.ToolCallID, json.RawMessage(msg.Content), cfg.ToolResultSoftTokens, cfg.ToolResultHardTokens)
		content := ToolResultContent(digest)
		if content == msg.Content {
			return msg, false
		}
		msg.Content = content
		return msg, true
	case len(msg.ContentBlocks) > 0:
		changed := false
		blocks := append([]llm.ContentBlock(nil), msg.ContentBlocks...)
		for i, block := range blocks {
			if block.Type != "tool_result" {
				continue
			}
			digest := SnipToolResult("tool_result", block.ID, json.RawMessage(block.Content), cfg.ToolResultSoftTokens, cfg.ToolResultHardTokens)
			content := ToolResultContent(digest)
			if content != block.Content {
				blocks[i].Content = content
				changed = true
			}
		}
		if !changed {
			return msg, false
		}
		msg.ContentBlocks = blocks
		return msg, true
	default:
		return msg, false
	}
}

func extractRunningSummaryMessage(messages []llm.Message) (*llm.Message, bool) {
	for i := range messages {
		if isRunningSummaryMessage(messages[i]) {
			msg := messages[i]
			return &msg, true
		}
	}
	return nil, false
}

func isRunningSummaryMessage(msg llm.Message) bool {
	if msg.Role != llm.RoleUser || msg.Content == "" {
		return false
	}
	var envelope struct {
		RunningSummary json.RawMessage `json:"running_summary"`
	}
	return json.Unmarshal([]byte(msg.Content), &envelope) == nil && len(envelope.RunningSummary) > 0
}

func isSyntheticUserMessage(msg llm.Message) bool {
	if msg.Role != llm.RoleUser {
		return false
	}
	if isRunningSummaryMessage(msg) {
		return true
	}
	if len(msg.ContentBlocks) > 0 {
		for _, block := range msg.ContentBlocks {
			if block.Type == "tool_result" {
				return true
			}
		}
	}
	var toolEnvelope struct {
		ToolDigests []json.RawMessage `json:"tool_digests"`
	}
	return json.Unmarshal([]byte(msg.Content), &toolEnvelope) == nil && len(toolEnvelope.ToolDigests) > 0
}

func findLatestConversationUserIndex(messages []llm.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llm.RoleUser && !isSyntheticUserMessage(messages[i]) {
			return i
		}
	}
	return -1
}

func countConversationUserTurns(messages []llm.Message) int {
	count := 0
	for _, msg := range messages {
		if msg.Role == llm.RoleUser && !isSyntheticUserMessage(msg) {
			count++
		}
	}
	return count
}

func isSameMessage(a, b llm.Message) bool {
	if a.Role != b.Role || a.Content != b.Content || a.ToolCallID != b.ToolCallID || a.ReasoningContent != b.ReasoningContent {
		return false
	}
	return reflect.DeepEqual(a.ContentBlocks, b.ContentBlocks)
}
