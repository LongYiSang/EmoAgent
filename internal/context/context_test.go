package context_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	ctxpkg "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/storage"
)

func TestBuildEmotionContextUsesPinnedContextAndRecentTurns(t *testing.T) {
	persona := &config.Persona{
		Name:         "default",
		SystemPrompt: "You are warm.",
	}
	history := []storage.MessageRecord{
		{ID: "1", Role: "user", Content: "old question"},
		{ID: "2", Role: "assistant", Content: "old answer"},
		{ID: "3", Role: "user", Content: "recent question"},
		{ID: "4", Role: "assistant", Content: "recent answer"},
	}

	assembled, err := ctxpkg.BuildEmotionContext(persona, history, config.ContextConfig{
		InputBudgetTokens:    24000,
		SoftCompactRatio:     0.75,
		HardCompactRatio:     0.92,
		ReserveOutputTokens:  4096,
		KeepRecentUserTurns:  1,
		ToolResultSoftTokens: 1000,
		ToolResultHardTokens: 3000,
	})
	if err != nil {
		t.Fatalf("BuildEmotionContext: %v", err)
	}

	if assembled.System != "You are warm." {
		t.Fatalf("System = %q, want %q", assembled.System, "You are warm.")
	}
	if len(assembled.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want 2", len(assembled.Messages))
	}
	if assembled.Messages[0].Content != "recent question" {
		t.Fatalf("Messages[0] = %#v, want recent user turn first", assembled.Messages[0])
	}
	if assembled.Messages[1].Content != "recent answer" {
		t.Fatalf("Messages[1] = %#v, want recent assistant turn preserved", assembled.Messages[1])
	}
}

func TestKeepRecentByUserTurnsNotMessageCount(t *testing.T) {
	history := []storage.MessageRecord{
		{ID: "1", Role: "user", Content: "u1"},
		{ID: "2", Role: "assistant", Content: "a1"},
		{ID: "3", Role: "assistant", Content: "a1-followup"},
		{ID: "4", Role: "user", Content: "u2"},
		{ID: "5", Role: "assistant", Content: "a2"},
	}

	kept := ctxpkg.KeepRecentUserTurns(history, 1)
	if len(kept) != 2 {
		t.Fatalf("len(kept) = %d, want 2", len(kept))
	}
	if kept[0].Content != "u2" || kept[1].Content != "a2" {
		t.Fatalf("kept = %#v, want only final user turn", kept)
	}
}

func TestCJKTokenEstimatorDiffersFromASCII(t *testing.T) {
	ascii := ctxpkg.EstimateTokens("hello world")
	cjk := ctxpkg.EstimateTokens("你好世界")
	if ascii <= 0 || cjk <= 0 {
		t.Fatalf("EstimateTokens returned non-positive values: ascii=%d cjk=%d", ascii, cjk)
	}
	if cjk == ascii {
		t.Fatalf("EstimateTokens returned same value for ASCII and CJK: %d", cjk)
	}
}

func TestToolDigestTruncatesLargeToolResult(t *testing.T) {
	content := json.RawMessage(`{"body":"` + strings.Repeat("中", 5000) + `"}`)

	digest := ctxpkg.SnipToolResult("web_search", "call_1", content, 100, 200)
	if !digest.IsTruncated {
		t.Fatal("expected tool digest to be truncated")
	}
	if digest.Size <= 0 {
		t.Fatalf("Size = %d, want > 0", digest.Size)
	}
	if digest.Preview == "" {
		t.Fatal("expected preview to be populated")
	}
	if len(digest.FullContent) != 0 {
		t.Fatalf("FullContent = %q, want empty after hard truncation", digest.FullContent)
	}
}

func TestBuildEmotionContextPlacesToolDigestBeforeRecentTurns(t *testing.T) {
	persona := &config.Persona{
		Name:         "default",
		SystemPrompt: "You are warm.",
	}
	history := []storage.MessageRecord{
		{ID: "1", Role: "user", Content: "recent question"},
		{ID: "2", Role: "assistant", Content: "recent answer"},
	}
	digests := []ctxpkg.ToolDigest{
		{
			ToolName:    "web_search",
			CallID:      "call_1",
			Preview:     "Top results: A, B, C",
			Hash:        "deadbeef",
			Size:        128,
			IsTruncated: true,
		},
	}

	assembled, err := ctxpkg.BuildEmotionContextWithToolDigests(persona, history, digests, config.ContextConfig{
		InputBudgetTokens:    24000,
		SoftCompactRatio:     0.75,
		HardCompactRatio:     0.92,
		ReserveOutputTokens:  4096,
		KeepRecentUserTurns:  1,
		ToolResultSoftTokens: 1000,
		ToolResultHardTokens: 3000,
	})
	if err != nil {
		t.Fatalf("BuildEmotionContextWithToolDigests: %v", err)
	}

	if len(assembled.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(assembled.Messages))
	}
	if assembled.Messages[0].Role != "user" {
		t.Fatalf("Messages[0].Role = %q, want user", assembled.Messages[0].Role)
	}
	if !strings.Contains(assembled.Messages[0].Content, `"tool_digests"`) {
		t.Fatalf("Messages[0] = %#v, want tool digest payload first", assembled.Messages[0])
	}
	if assembled.Messages[1].Content != "recent question" {
		t.Fatalf("Messages[1] = %#v, want recent question after digest", assembled.Messages[1])
	}
	if assembled.Messages[2].Content != "recent answer" {
		t.Fatalf("Messages[2] = %#v, want recent answer last", assembled.Messages[2])
	}
	if !assembled.CompactReport.UsedToolDigest {
		t.Fatal("CompactReport.UsedToolDigest = false, want true")
	}
}
