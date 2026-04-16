package context

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

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
