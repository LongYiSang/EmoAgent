package work

import (
	"context"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/llm"
)

func TestSnipConsumedToolResults_KeepsCurrentRound(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleAssistant, Content: "I'll read the file"},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: largeContent(4000)},
		{Role: llm.RoleAssistant, Content: "Now I'll check another"},
		{Role: llm.RoleTool, ToolCallID: "c2", Content: "small result"},
	}

	got := snipConsumedToolResults(messages, 2, 500, 2000)

	if got[1].Content == messages[1].Content {
		t.Fatal("round 0 tool result should have been snipped")
	}
	if got[3].Content != "small result" {
		t.Fatal("current round tool result should not be snipped")
	}
}

func TestSnipConsumedToolResults_PreservesSmallResults(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleAssistant, Content: "I'll read"},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: "tiny"},
		{Role: llm.RoleAssistant, Content: "next step"},
	}

	got := snipConsumedToolResults(messages, 2, 500, 2000)
	if got[1].Content != "tiny" {
		t.Fatal("small tool result should not be snipped")
	}
}

func TestSnipConsumedToolResults_AnthropicBlocks(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleAssistant, ContentBlocks: []llm.ContentBlock{
			{Type: "text", Text: "reading file"},
			{Type: "tool_use", ID: "c1", Name: "read_file", Input: []byte(`{"path":"a.txt"}`)},
		}},
		{Role: llm.RoleUser, ContentBlocks: []llm.ContentBlock{
			{Type: "tool_result", ID: "c1", Content: largeContent(3000)},
		}},
		{Role: llm.RoleAssistant, Content: "found the answer"},
	}

	got := snipConsumedToolResults(messages, 2, 500, 2000)

	block := got[1].ContentBlocks[0]
	if block.Content == messages[1].ContentBlocks[0].Content {
		t.Fatal("Anthropic tool_result block in consumed round should be snipped")
	}
}

func TestCompressWorkContext_UnderBudget_NoChange(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleAssistant, Content: "step 1"},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: "small"},
	}
	progress := WorkProgress{}

	got, gotProgress, err := compressWorkContext(
		context.Background(), nil, "",
		messages, progress, "", 100000, 0.7, 2,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(messages) {
		t.Fatalf("messages should be unchanged when under budget")
	}
	if !gotProgress.IsZero() {
		t.Fatal("progress should remain zero when no compression needed")
	}
}

func TestCompressWorkContext_OverBudget_Compresses(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleAssistant, Content: largeContent(800)},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: largeContent(800)},
		{Role: llm.RoleAssistant, Content: largeContent(800)},
		{Role: llm.RoleTool, ToolCallID: "c2", Content: largeContent(800)},
		{Role: llm.RoleAssistant, Content: "recent assistant"},
		{Role: llm.RoleTool, ToolCallID: "c3", Content: "recent tool result"},
	}

	fakeLLM := &scriptedLLM{
		responses: []*llm.ChatResponse{{
			Content: `{"work_progress":{"task_goal":"test","steps_completed":["step1","step2"],"key_findings":["finding1"],"errors_encountered":[],"current_approach":"doing stuff","decisions_received":[]}}`,
		}},
	}

	got, gotProgress, err := compressWorkContext(
		context.Background(), fakeLLM, "test-model",
		messages, WorkProgress{}, "", 1000, 0.7, 2,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) >= len(messages) {
		t.Fatalf("compressed messages (%d) should be fewer than original (%d)", len(got), len(messages))
	}
	if gotProgress.TaskGoal != "test" {
		t.Fatalf("progress.TaskGoal = %q, want %q", gotProgress.TaskGoal, "test")
	}
	if got[0].Role != llm.RoleUser {
		t.Fatal("first compressed message should be a user-role progress summary")
	}
	if got[len(got)-1].Content != "recent tool result" {
		t.Fatal("recent round should be preserved")
	}
}

func TestCompressWorkContext_RetriesInvalidProgressSummary(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleAssistant, Content: largeContent(800)},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: largeContent(800)},
		{Role: llm.RoleAssistant, Content: largeContent(800)},
		{Role: llm.RoleTool, ToolCallID: "c2", Content: largeContent(800)},
		{Role: llm.RoleAssistant, Content: "recent assistant"},
		{Role: llm.RoleTool, ToolCallID: "c3", Content: "recent tool result"},
	}
	fakeLLM := &scriptedLLM{
		responses: []*llm.ChatResponse{
			{Content: `not json`},
			{Content: `{"work_progress":{"task_goal":"repaired","steps_completed":["read file"],"key_findings":[],"errors_encountered":[],"current_approach":"ready_to_finish","decisions_received":[]}}`},
		},
	}

	got, gotProgress, err := compressWorkContext(
		context.Background(), fakeLLM, "test-model",
		messages, WorkProgress{}, "", 1000, 0.7, 2,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fakeLLM.calls) != 2 {
		t.Fatalf("summary calls = %d, want initial + repair", len(fakeLLM.calls))
	}
	if gotProgress.TaskGoal != "repaired" {
		t.Fatalf("progress.TaskGoal = %q, want repaired", gotProgress.TaskGoal)
	}
	if len(got) >= len(messages) {
		t.Fatalf("compressed messages (%d) should be fewer than original (%d)", len(got), len(messages))
	}
}

func TestCompressWorkContext_FallbackCompressesWhenProgressRepairFails(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleAssistant, Content: largeContent(800)},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: largeContent(800)},
		{Role: llm.RoleAssistant, Content: largeContent(800)},
		{Role: llm.RoleTool, ToolCallID: "c2", Content: largeContent(800)},
		{Role: llm.RoleAssistant, Content: "recent assistant"},
		{Role: llm.RoleTool, ToolCallID: "c3", Content: "recent tool result"},
	}
	fakeLLM := &scriptedLLM{
		responses: []*llm.ChatResponse{
			{Content: `not json`},
			{Content: `still not json`},
		},
	}
	current := WorkProgress{TaskGoal: "old goal", KeyFindings: []string{"keep this"}}

	got, gotProgress, err := compressWorkContext(
		context.Background(), fakeLLM, "test-model",
		messages, current, "", 1000, 0.7, 2,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fakeLLM.calls) != 2 {
		t.Fatalf("summary calls = %d, want initial + repair", len(fakeLLM.calls))
	}
	if gotProgress.TaskGoal != "old goal" {
		t.Fatalf("progress.TaskGoal = %q, want old goal", gotProgress.TaskGoal)
	}
	if !containsString(gotProgress.KeyFindings, "keep this") {
		t.Fatalf("KeyFindings = %#v, want preserved current progress", gotProgress.KeyFindings)
	}
	if !containsString(gotProgress.ErrorsEncountered, "[uncompressed round omitted due to progress parse failure]") {
		t.Fatalf("ErrorsEncountered = %#v, want deterministic fallback marker", gotProgress.ErrorsEncountered)
	}
	if len(got) >= len(messages) {
		t.Fatalf("compressed messages (%d) should be fewer than original (%d)", len(got), len(messages))
	}
}

func TestFindKeepBoundary(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleAssistant, Content: "round0"},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: "r0"},
		{Role: llm.RoleAssistant, Content: "round1"},
		{Role: llm.RoleTool, ToolCallID: "c2", Content: "r1"},
		{Role: llm.RoleAssistant, Content: "round2"},
		{Role: llm.RoleTool, ToolCallID: "c3", Content: "r2"},
	}

	got := findKeepBoundary(messages, 2)
	if got != 2 {
		t.Fatalf("findKeepBoundary = %d, want 2", got)
	}

	got = findKeepBoundary(messages, 1)
	if got != 4 {
		t.Fatalf("findKeepBoundary = %d, want 4", got)
	}

	got = findKeepBoundary(messages, 10)
	if got != 0 {
		t.Fatalf("findKeepBoundary = %d, want 0", got)
	}
}

func TestCompressWorkContext_PreservesDecisions(t *testing.T) {
	messages := []llm.Message{
		{Role: llm.RoleAssistant, Content: largeContent(800)},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: largeContent(800)},
		{Role: llm.RoleAssistant, Content: largeContent(800)},
		{Role: llm.RoleTool, ToolCallID: "c2", Content: largeContent(800)},
		{Role: llm.RoleAssistant, Content: "recent"},
	}

	fakeLLM := &scriptedLLM{
		responses: []*llm.ChatResponse{{
			Content: `{"work_progress":{"task_goal":"t","steps_completed":[],"key_findings":[],"errors_encountered":[],"current_approach":"","decisions_received":["new decision"]}}`,
		}},
	}

	existing := WorkProgress{
		DecisionsReceived: []string{"old decision"},
	}

	_, gotProgress, err := compressWorkContext(
		context.Background(), fakeLLM, "m",
		messages, existing, "", 1000, 0.7, 2,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gotProgress.DecisionsReceived) != 2 {
		t.Fatalf("decisions_received len = %d, want 2", len(gotProgress.DecisionsReceived))
	}
}

func TestIsWorkProgressMessage(t *testing.T) {
	progressMsg, err := buildWorkProgressMessage(WorkProgress{TaskGoal: "test"})
	if err != nil {
		t.Fatalf("buildWorkProgressMessage error: %v", err)
	}
	if !isWorkProgressMessage(progressMsg) {
		t.Fatal("should detect synthetic progress message")
	}

	normalMsg := llm.Message{Role: llm.RoleUser, Content: "hello"}
	if isWorkProgressMessage(normalMsg) {
		t.Fatal("should not flag normal user message")
	}

	assistantMsg := llm.Message{Role: llm.RoleAssistant, Content: "thinking"}
	if isWorkProgressMessage(assistantMsg) {
		t.Fatal("should not flag assistant message")
	}
}

func TestFilterOutProgressMessages(t *testing.T) {
	progressMsg, err := buildWorkProgressMessage(WorkProgress{TaskGoal: "test"})
	if err != nil {
		t.Fatalf("buildWorkProgressMessage error: %v", err)
	}

	messages := []llm.Message{
		progressMsg,
		{Role: llm.RoleAssistant, Content: "step"},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: "result"},
	}
	filtered := filterOutProgressMessages(messages)
	if len(filtered) != 2 {
		t.Fatalf("filtered len = %d, want 2", len(filtered))
	}
	if filtered[0].Role != llm.RoleAssistant {
		t.Fatal("first filtered message should be the assistant message")
	}
}

func TestCompressWorkContext_SkipsSyntheticProgress(t *testing.T) {
	progressMsg, err := buildWorkProgressMessage(WorkProgress{TaskGoal: "old summary"})
	if err != nil {
		t.Fatalf("buildWorkProgressMessage error: %v", err)
	}

	messages := []llm.Message{
		progressMsg,
		{Role: llm.RoleAssistant, Content: largeContent(800)},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: largeContent(800)},
		{Role: llm.RoleAssistant, Content: largeContent(800)},
		{Role: llm.RoleTool, ToolCallID: "c2", Content: largeContent(800)},
		{Role: llm.RoleAssistant, Content: "recent"},
	}

	fakeLLM := &scriptedLLM{
		responses: []*llm.ChatResponse{{
			Content: `{"work_progress":{"task_goal":"updated","steps_completed":["a","b"],"key_findings":[],"errors_encountered":[],"current_approach":"c","decisions_received":[]}}`,
		}},
	}

	_, gotProgress, err := compressWorkContext(
		context.Background(), fakeLLM, "m",
		messages, WorkProgress{TaskGoal: "old summary"}, "", 500, 0.7, 2,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotProgress.TaskGoal != "updated" {
		t.Fatalf("progress should be updated, got %q", gotProgress.TaskGoal)
	}
	if len(fakeLLM.calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(fakeLLM.calls))
	}
	deltaMsg := fakeLLM.calls[0].Messages[1].Content
	if strings.Contains(deltaMsg, "old summary") {
		t.Fatal("synthetic progress message should have been filtered from delta")
	}
}

func largeContent(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
