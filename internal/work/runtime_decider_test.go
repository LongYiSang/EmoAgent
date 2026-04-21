package work

import (
	"context"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/protocol"
)

func testExecutionOnlyPacket() protocol.DecisionPacket {
	return protocol.DecisionPacket{
		TaskID:      "task-1",
		Category:    protocol.CatAuto,
		GoalSummary: "Goal summary",
		Question:    "Choose implementation strategy",
		WhyBlocked:  "Need one option to continue",
		Options: []protocol.DecisionOption{
			{ID: "a", Summary: "Option A"},
			{ID: "b", Summary: "Option B"},
		},
	}
}

func TestRuntimeDecider_ParsesValidDecision(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			textResp(`{"escalate":false,"decision":"a","reason":"least risky","constraints_delta":["keep tests"]}`),
		},
	}
	decider := NewLLMRuntimeDecider(client, "test-model")

	decision, err := decider.Decide(context.Background(), protocol.TaskBrief{
		TaskID:     "task-1",
		Goal:       "Implement feature",
		Background: "Need safe path",
	}, testExecutionOnlyPacket())
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}
	if decision.Escalate {
		t.Fatalf("decision escalated unexpectedly: %#v", decision)
	}
	if decision.Decision != "a" {
		t.Fatalf("Decision = %q, want a", decision.Decision)
	}
}

func TestRuntimeDecider_SystemPromptOmitsStyleDelta(t *testing.T) {
	text := buildRuntimeDeciderSystemPrompt()

	if strings.Contains(text, "style_delta") {
		t.Fatalf("system prompt should not mention removed style_delta field: %s", text)
	}
	if !strings.Contains(text, `"constraints_delta": ["optional additional constraints"]`) {
		t.Fatalf("system prompt should still mention constraints_delta: %s", text)
	}
}

func TestRuntimeDecider_HandlesExplicitEscalation(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			textResp(`{"escalate":true,"escalate_reason":"insufficient confidence"}`),
		},
	}
	decider := NewLLMRuntimeDecider(client, "test-model")

	decision, err := decider.Decide(context.Background(), protocol.TaskBrief{Goal: "goal"}, testExecutionOnlyPacket())
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}
	if !decision.Escalate {
		t.Fatalf("expected escalation, got %#v", decision)
	}
}

func TestRuntimeDecider_InvalidJSONFallsBackToEscalation(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			textResp("not-json"),
		},
	}
	decider := NewLLMRuntimeDecider(client, "test-model")

	decision, err := decider.Decide(context.Background(), protocol.TaskBrief{Goal: "goal"}, testExecutionOnlyPacket())
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}
	if !decision.Escalate {
		t.Fatalf("expected escalation, got %#v", decision)
	}
}

func TestRuntimeDecider_PromptHasMinimalContext(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			textResp(`{"escalate":false,"decision":"a","reason":"ok"}`),
		},
	}
	decider := NewLLMRuntimeDecider(client, "test-model")
	packet := testExecutionOnlyPacket()
	_, err := decider.Decide(context.Background(), protocol.TaskBrief{
		TaskID:     "task-1",
		Goal:       "Ship feature X",
		Background: "Use existing helper Y",
	}, packet)
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("LLM calls = %d, want 1", len(client.calls))
	}
	req := client.calls[0]
	if !strings.Contains(req.Messages[0].Content, "Ship feature X") {
		t.Fatalf("payload missing goal: %s", req.Messages[0].Content)
	}
	if !strings.Contains(req.Messages[0].Content, "Use existing helper Y") {
		t.Fatalf("payload missing background: %s", req.Messages[0].Content)
	}
	if !strings.Contains(req.Messages[0].Content, packet.Question) {
		t.Fatalf("payload missing packet question: %s", req.Messages[0].Content)
	}
	if !strings.Contains(req.Messages[0].Content, packet.Options[0].Summary) {
		t.Fatalf("payload missing packet options: %s", req.Messages[0].Content)
	}
	for _, forbidden := range []string{"persona", "session history", "previous messages"} {
		if strings.Contains(strings.ToLower(req.System), forbidden) || strings.Contains(strings.ToLower(req.Messages[0].Content), forbidden) {
			t.Fatalf("prompt should not contain %q", forbidden)
		}
	}
}

func TestRuntimeDecider_RefusesNonExecutionCategory(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			textResp(`{"escalate":false,"decision":"a","reason":"ok"}`),
		},
	}
	decider := NewLLMRuntimeDecider(client, "test-model")
	packet := testExecutionOnlyPacket()
	packet.Category = protocol.CatEmotionJudgment

	decision, err := decider.Decide(context.Background(), protocol.TaskBrief{Goal: "goal"}, packet)
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}
	if !decision.Escalate {
		t.Fatalf("expected non-execution category to escalate, got %#v", decision)
	}
	if len(client.calls) != 0 {
		t.Fatalf("LLM should not be called for non-execution categories, got %d calls", len(client.calls))
	}
}
