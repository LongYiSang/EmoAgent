package work

import (
	"encoding/json"
	"fmt"

	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/protocol"
)

const (
	firstPassToolResultRunes  = 500
	secondPassToolResultRunes = 200
	maxPacketEstimateFallback = int(^uint(0) >> 1)
)

// compactForPause bounds paused Work snapshots by truncating tool_result payloads.
func compactForPause(messages []llm.Message, maxTokens int) ([]llm.Message, error) {
	if maxTokens <= 0 {
		return append([]llm.Message(nil), messages...), nil
	}
	if estimateMessagesTokens(messages) <= maxTokens {
		return append([]llm.Message(nil), messages...), nil
	}

	first := truncateToolResults(messages, firstPassToolResultRunes)
	if estimateMessagesTokens(first) <= maxTokens {
		return first, nil
	}

	second := truncateToolResults(messages, secondPassToolResultRunes)
	if estimateMessagesTokens(second) <= maxTokens {
		return second, nil
	}

	return nil, fmt.Errorf("pause snapshot exceeds token budget after compaction")
}

func truncateToolResults(messages []llm.Message, maxRunes int) []llm.Message {
	compacted := append([]llm.Message(nil), messages...)
	for i, msg := range compacted {
		switch {
		case msg.Role == llm.RoleTool:
			trimmed, _ := truncateContent(msg.Content, maxRunes)
			compacted[i].Content = trimmed
		case len(msg.ContentBlocks) > 0:
			blocks := append([]llm.ContentBlock(nil), msg.ContentBlocks...)
			for j, block := range blocks {
				if block.Type != "tool_result" {
					continue
				}
				trimmed, _ := truncateContent(block.Content, maxRunes)
				blocks[j].Content = trimmed
			}
			compacted[i].ContentBlocks = blocks
		}
	}
	return compacted
}

// CompactPacket trims non-core fields to keep decision packets budget-friendly.
func CompactPacket(packet protocol.DecisionPacket, budgetTokens int) protocol.DecisionPacket {
	if budgetTokens <= 0 {
		return packet
	}
	if packetTokenEstimate(packet) <= budgetTokens {
		return packet
	}

	compacted := packet
	for i := range compacted.RelevantFindings {
		compacted.RelevantFindings[i].Source = ""
	}
	if packetTokenEstimate(compacted) <= budgetTokens {
		return compacted
	}

	for len(compacted.RelevantFindings) > 0 && packetTokenEstimate(compacted) > budgetTokens {
		compacted.RelevantFindings = compacted.RelevantFindings[:len(compacted.RelevantFindings)-1]
	}
	if packetTokenEstimate(compacted) <= budgetTokens {
		return compacted
	}

	for len(compacted.KeyTradeoffs) > 0 && packetTokenEstimate(compacted) > budgetTokens {
		compacted.KeyTradeoffs = compacted.KeyTradeoffs[:len(compacted.KeyTradeoffs)-1]
	}
	return compacted
}

func packetTokenEstimate(packet protocol.DecisionPacket) int {
	payload, err := json.Marshal(packet)
	if err != nil {
		// conservative fallback
		return maxPacketEstimateFallback
	}
	return contextutil.EstimateTokens(string(payload))
}
