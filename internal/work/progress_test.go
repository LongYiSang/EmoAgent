package work

import (
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/llm"
)

func TestWorkProgress_IsZero(t *testing.T) {
	var p WorkProgress
	if !p.IsZero() {
		t.Fatal("zero-value WorkProgress should report IsZero=true")
	}
	p.TaskGoal = "do something"
	if p.IsZero() {
		t.Fatal("non-empty WorkProgress should report IsZero=false")
	}
}

func TestBuildProgressSummaryRequest_IncludesCurrentProgress(t *testing.T) {
	current := WorkProgress{
		TaskGoal:       "analyze config",
		StepsCompleted: []string{"read config.yaml"},
	}
	messages := []llm.Message{
		{Role: llm.RoleAssistant, Content: "I will read the file"},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: "file contents here"},
	}

	req, err := buildProgressSummaryRequest("gpt-4o-mini", current, messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Model != "gpt-4o-mini" {
		t.Fatalf("model = %q, want gpt-4o-mini", req.Model)
	}
	if req.MaxTokens != progressSummaryMaxTokens {
		t.Fatalf("max_tokens = %d, want %d", req.MaxTokens, progressSummaryMaxTokens)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("expected 2 messages (current progress + delta), got %d", len(req.Messages))
	}
	if !strings.Contains(req.Messages[0].Content, "analyze config") {
		t.Fatal("first message should contain current progress with task_goal")
	}
}

func TestParseProgressSummaryResponse(t *testing.T) {
	resp := &llm.ChatResponse{
		Content: `{"work_progress":{"task_goal":"analyze config","steps_completed":["read file","parsed yaml"],"key_findings":["missing field X"],"errors_encountered":[],"current_approach":"checking defaults","decisions_received":[]}}`,
	}

	got, err := parseProgressSummaryResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TaskGoal != "analyze config" {
		t.Fatalf("task_goal = %q, want %q", got.TaskGoal, "analyze config")
	}
	if len(got.StepsCompleted) != 2 {
		t.Fatalf("steps_completed len = %d, want 2", len(got.StepsCompleted))
	}
	if len(got.KeyFindings) != 1 {
		t.Fatalf("key_findings len = %d, want 1", len(got.KeyFindings))
	}
}

func TestParseProgressSummaryResponse_NilResponse(t *testing.T) {
	_, err := parseProgressSummaryResponse(nil)
	if err == nil {
		t.Fatal("expected error for nil response")
	}
}

func TestParseProgressSummaryResponse_EmptyContent(t *testing.T) {
	_, err := parseProgressSummaryResponse(&llm.ChatResponse{})
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}
