package tokenmeter

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/longyisang/emoagent/internal/llm"
)

func TestHeuristicCounterCountsCJKEnglishMixed(t *testing.T) {
	counter := NewHeuristicCounter()

	english := counter.CountText(context.Background(), "", "", "hello world")
	cjk := counter.CountText(context.Background(), "", "", "你好世界")
	mixed := counter.CountText(context.Background(), "", "", "hello 你好")

	if english.InputTokens != 3 {
		t.Fatalf("english tokens = %d, want 3", english.InputTokens)
	}
	if cjk.InputTokens != 2 {
		t.Fatalf("cjk tokens = %d, want 2", cjk.InputTokens)
	}
	if mixed.InputTokens != 3 {
		t.Fatalf("mixed tokens = %d, want 3", mixed.InputTokens)
	}
	if english.Method != MethodHeuristicCJK || english.Confidence <= 0 {
		t.Fatalf("count metadata = %#v, want heuristic method/confidence", english)
	}
}

func TestCountChatRequestIncludesSystemMessagesAndTools(t *testing.T) {
	counter := NewHeuristicCounter()
	noTools := counter.CountChatRequest(context.Background(), CountRequest{
		System: "system",
		Messages: []llm.Message{{
			Role:    llm.RoleUser,
			Content: "hello",
		}},
	})
	withTools := counter.CountChatRequest(context.Background(), CountRequest{
		System: "system",
		Messages: []llm.Message{{
			Role:    llm.RoleUser,
			Content: "hello",
		}},
		Tools: []llm.ToolDef{{
			Name:        "get_time",
			Description: "Get current time",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"tz":{"type":"string"}}}`),
		}},
	})

	if noTools.InputTokens <= 0 {
		t.Fatalf("noTools = %#v, want positive estimate", noTools)
	}
	if withTools.InputTokens <= noTools.InputTokens {
		t.Fatalf("withTools = %d, noTools = %d, want tools included", withTools.InputTokens, noTools.InputTokens)
	}
}

func TestCountChatResponseIncludesContentReasoningAndToolCalls(t *testing.T) {
	counter := NewHeuristicCounter()
	textOnly := counter.CountChatResponse(context.Background(), "", "", &llm.ChatResponse{Content: "answer"})
	withReasoningAndTool := counter.CountChatResponse(context.Background(), "", "", &llm.ChatResponse{
		Content:          "answer",
		ReasoningContent: "hidden reasoning",
		ContentBlocks: []llm.ContentBlock{{
			Type:  "tool_use",
			Name:  "get_time",
			Input: json.RawMessage(`{"tz":"UTC"}`),
		}},
	})

	if withReasoningAndTool.OutputTokens <= textOnly.OutputTokens {
		t.Fatalf("withReasoningAndTool = %d, textOnly = %d, want output extras included", withReasoningAndTool.OutputTokens, textOnly.OutputTokens)
	}
}

func TestUsageScopeContextMerge(t *testing.T) {
	ctx := WithUsageScope(context.Background(), UsageScope{
		Component: "emotion_chat",
		Operation: "chat_stream",
		SessionID: "session-1",
		Model:     "model-a",
	})
	ctx = MergeUsageScope(ctx, UsageScope{
		Operation: "summary_update",
		RequestID: "req-1",
		Model:     "model-b",
	})

	scope, ok := UsageScopeFromContext(ctx)
	if !ok {
		t.Fatal("UsageScopeFromContext ok = false")
	}
	if scope.Component != "emotion_chat" || scope.Operation != "summary_update" || scope.SessionID != "session-1" || scope.RequestID != "req-1" || scope.Model != "model-b" {
		t.Fatalf("scope = %#v, want merged scope", scope)
	}
}
