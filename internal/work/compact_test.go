package work

import (
	"reflect"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/protocol"
)

func TestCompactForPause_UnderBudgetUnchanged(t *testing.T) {
	original := []llm.Message{
		{Role: llm.RoleUser, Content: "hello"},
		{Role: llm.RoleAssistant, Content: "world"},
	}

	got, err := compactForPause(original, 10_000)
	if err != nil {
		t.Fatalf("compactForPause returned error: %v", err)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("compacted messages differ\n got: %#v\nwant: %#v", got, original)
	}
}

func TestCompactForPause_TruncatesToolResults(t *testing.T) {
	original := []llm.Message{
		{Role: llm.RoleUser, Content: "question"},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: strings.Repeat("x", 2_000)},
	}

	got, err := compactForPause(original, 130)
	if err != nil {
		t.Fatalf("compactForPause returned error: %v", err)
	}
	if len([]rune(got[1].Content)) > firstPassToolResultRunes+3 {
		t.Fatalf("tool result should be truncated to first pass, got len=%d", len([]rune(got[1].Content)))
	}
}

func TestCompactForPause_UsesSecondPassWhenNeeded(t *testing.T) {
	original := []llm.Message{
		{Role: llm.RoleTool, ToolCallID: "c1", Content: strings.Repeat("x", 1_500)},
		{Role: llm.RoleTool, ToolCallID: "c2", Content: strings.Repeat("x", 1_500)},
	}

	got, err := compactForPause(original, 120)
	if err != nil {
		t.Fatalf("compactForPause returned error: %v", err)
	}
	if len([]rune(got[0].Content)) > secondPassToolResultRunes+3 {
		t.Fatalf("first tool result should use second pass truncation, got len=%d", len([]rune(got[0].Content)))
	}
}

func TestCompactForPause_ReturnsErrorWhenStillOverBudget(t *testing.T) {
	original := []llm.Message{
		{Role: llm.RoleTool, ToolCallID: "c1", Content: strings.Repeat("x", 1_500)},
		{Role: llm.RoleTool, ToolCallID: "c2", Content: strings.Repeat("x", 1_500)},
		{Role: llm.RoleTool, ToolCallID: "c3", Content: strings.Repeat("x", 1_500)},
	}

	if _, err := compactForPause(original, 20); err == nil {
		t.Fatal("expected compactForPause to fail when still over budget")
	}
}

func TestCompactForPause_DoesNotTruncateNonToolMessages(t *testing.T) {
	original := []llm.Message{
		{Role: llm.RoleUser, Content: "user content should stay unchanged"},
		{Role: llm.RoleAssistant, Content: "assistant content should stay unchanged"},
		{
			Role: llm.RoleUser,
			ContentBlocks: []llm.ContentBlock{
				{Type: "text", Text: "plain text block should stay unchanged"},
			},
		},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: strings.Repeat("x", 1200)},
	}

	got, err := compactForPause(original, 200)
	if err != nil {
		t.Fatalf("compactForPause returned error: %v", err)
	}
	if got[0].Content != original[0].Content {
		t.Fatal("user message content should not be truncated")
	}
	if got[1].Content != original[1].Content {
		t.Fatal("assistant message content should not be truncated")
	}
	if got[2].ContentBlocks[0].Text != original[2].ContentBlocks[0].Text {
		t.Fatal("non-tool content blocks should not be truncated")
	}
}

func TestCompactPacket_UnderBudgetUnchanged(t *testing.T) {
	packet := protocol.DecisionPacket{
		TaskID:      "task-1",
		Category:    protocol.CatPreferenceSensitive,
		RiskLevel:   "low",
		GoalSummary: "goal",
		Question:    "question",
		WhyBlocked:  "blocked",
		Options: []protocol.DecisionOption{
			{ID: "a", Summary: "option-a"},
		},
		RelevantFindings: []protocol.DecisionEvidence{
			{Finding: "finding", Source: "source"},
		},
		KeyTradeoffs: []protocol.DecisionTradeoff{
			{Dimension: "speed", Note: "faster but less safe"},
		},
		RecommendedOption:    "a",
		RecommendationReason: "fits existing preference",
	}

	compacted := CompactPacket(packet, 10_000)
	if !reflect.DeepEqual(compacted, packet) {
		t.Fatalf("packet changed unexpectedly\n got: %#v\nwant: %#v", compacted, packet)
	}
}

func TestCompactPacket_DropsSourceThenFindingsThenTradeoffs(t *testing.T) {
	packet := protocol.DecisionPacket{
		TaskID:      "task-1",
		Category:    protocol.CatPreferenceSensitive,
		RiskLevel:   "low",
		GoalSummary: "goal",
		Question:    "question",
		WhyBlocked:  "blocked",
		Options: []protocol.DecisionOption{
			{ID: "a", Summary: "option-a"},
			{ID: "b", Summary: "option-b"},
		},
		RelevantFindings: []protocol.DecisionEvidence{
			{Finding: strings.Repeat("f", 200), Source: strings.Repeat("s", 200)},
			{Finding: strings.Repeat("g", 200), Source: strings.Repeat("t", 200)},
		},
		KeyTradeoffs: []protocol.DecisionTradeoff{
			{Dimension: "speed", Note: strings.Repeat("n", 200)},
			{Dimension: "quality", Note: strings.Repeat("m", 200)},
		},
		RecommendedOption:    "a",
		RecommendationReason: "reason",
	}

	compacted := CompactPacket(packet, 220)
	for _, finding := range compacted.RelevantFindings {
		if finding.Source != "" {
			t.Fatal("finding source should be removed first when compacting")
		}
	}
	if len(compacted.RelevantFindings) >= len(packet.RelevantFindings) &&
		len(compacted.KeyTradeoffs) >= len(packet.KeyTradeoffs) {
		t.Fatal("expected findings or tradeoffs to be trimmed when over budget")
	}
	if compacted.Question == "" || len(compacted.Options) == 0 || compacted.Category == "" || compacted.RiskLevel == "" {
		t.Fatal("compact packet should never drop core decision fields")
	}
}
