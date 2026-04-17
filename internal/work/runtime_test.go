package work

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/tool"
)

func newTestRegistryAndDispatcher(t *testing.T) (*tool.Registry, *tool.Dispatcher) {
	t.Helper()

	registry := tool.NewRegistry()
	registry.Register(tool.Spec{
		Name:        "echo_tool",
		Description: "returns success",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}},"additionalProperties":false}`),
		Scope:       tool.ScopeWork,
		Permission:  tool.PermReadOnly,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"ok":true}`), nil
	})

	dispatcher := tool.NewDispatcher(registry, tool.MinimalSchemaValidator{}, testLogger())
	return registry, dispatcher
}

func newTestRuntime(t *testing.T, client llm.Client) *Runtime {
	t.Helper()

	registry, dispatcher := newTestRegistryAndDispatcher(t)
	return NewRuntime(RuntimeConfig{
		LLM:            client,
		Provider:       "openai",
		Model:          "test-model",
		MaxTokens:      2048,
		Temperature:    0.2,
		MaxToolRounds:  3,
		MaxInputTokens: 100000,
		Registry:       registry,
		Dispatcher:     dispatcher,
		Logger:         testLogger(),
	})
}

func newValidatedBrief(t *testing.T) protocol.TaskBrief {
	t.Helper()

	brief := protocol.TaskBrief{
		Goal:            "inspect go.mod",
		PermissionScope: "read-only",
	}
	if err := ValidateAndComplete(&brief); err != nil {
		t.Fatalf("ValidateAndComplete returned error: %v", err)
	}
	return brief
}

func TestRuntime_HappyPath(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("call-1", "echo_tool", `{"x":"y"}`),
			textResp(`{"status":"completed","summary":"done","findings":["a"]}`),
		},
	}
	runtime := newTestRuntime(t, client)
	brief := newValidatedBrief(t)

	journal, err := Open(t.TempDir(), brief.TaskID, time.Now(), testLogger())
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer func() { _ = journal.Close() }()

	report := runtime.Run(context.Background(), brief, journal)
	if report.Status != "completed" {
		t.Fatalf("Status = %q, want completed", report.Status)
	}
	if report.TaskID != brief.TaskID {
		t.Fatalf("TaskID = %q, want %q", report.TaskID, brief.TaskID)
	}
	if len(client.calls) != 2 {
		t.Fatalf("LLM calls = %d, want 2", len(client.calls))
	}
	if len(client.calls[0].Messages) != 0 {
		t.Fatalf("first request should start with empty history, got %d messages", len(client.calls[0].Messages))
	}
}

func TestRuntime_MaxToolRoundsExhausted(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("call-1", "echo_tool", `{}`),
			toolUseResp("call-2", "echo_tool", `{}`),
			toolUseResp("call-3", "echo_tool", `{}`),
		},
	}
	runtime := newTestRuntime(t, client)

	report := runtime.Run(context.Background(), newValidatedBrief(t), nil)
	if report.Status != "partial" {
		t.Fatalf("Status = %q, want partial", report.Status)
	}
	if !strings.Contains(strings.ToLower(report.Summary), "max") {
		t.Fatalf("Summary = %q, want round exhaustion hint", report.Summary)
	}
}

func TestRuntime_CanceledContextReturnsFailed(t *testing.T) {
	client := &scriptedLLM{errs: []error{context.Canceled}}
	runtime := newTestRuntime(t, client)
	brief := newValidatedBrief(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	report := runtime.Run(ctx, brief, nil)
	if report.Status != "failed" {
		t.Fatalf("Status = %q, want failed", report.Status)
	}
}

func TestRuntime_LLMErrorReturnsFailed(t *testing.T) {
	client := &scriptedLLM{errs: []error{errors.New("transport failed")}}
	runtime := newTestRuntime(t, client)

	report := runtime.Run(context.Background(), newValidatedBrief(t), nil)
	if report.Status != "failed" {
		t.Fatalf("Status = %q, want failed", report.Status)
	}
	if !strings.Contains(report.Summary, "transport failed") {
		t.Fatalf("Summary = %q, want error details", report.Summary)
	}
}

func TestRuntime_NonJSONFinalFallsBackToPartial(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			textResp("I think the answer is 42."),
		},
	}
	runtime := newTestRuntime(t, client)

	report := runtime.Run(context.Background(), newValidatedBrief(t), nil)
	if report.Status != "partial" {
		t.Fatalf("Status = %q, want partial", report.Status)
	}
	if !strings.Contains(report.Summary, "42") {
		t.Fatalf("Summary = %q, want raw text fallback", report.Summary)
	}
}

func TestRuntime_WritesToolEventsToJournal(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("call-1", "echo_tool", `{"x":"y"}`),
			textResp(`{"status":"completed","summary":"done"}`),
		},
	}
	runtime := newTestRuntime(t, client)
	brief := newValidatedBrief(t)
	root := t.TempDir()

	journal, err := Open(root, brief.TaskID, time.Now(), testLogger())
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	report := runtime.Run(context.Background(), brief, journal)
	if err := journal.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if report.Status != "completed" {
		t.Fatalf("Status = %q, want completed", report.Status)
	}

	data, err := os.ReadFile(filepath.Join(root, time.Now().UTC().Format("2006-01-02"), brief.TaskID+".jsonl"))
	if err != nil {
		t.Fatalf("expected journal file: %v", err)
	}
	text := string(data)
	for _, snippet := range []string{`"kind":"tool_call"`, `"kind":"tool_result"`} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("journal missing %s: %s", snippet, text)
		}
	}
}

func TestEstimateMessagesTokensCountsStructuredContent(t *testing.T) {
	tokens := estimateMessagesTokens([]llm.Message{
		{
			Role:    llm.RoleAssistant,
			Content: "hello",
			ContentBlocks: []llm.ContentBlock{
				{Type: "text", Text: "world"},
				{Type: "tool_use", Input: json.RawMessage(`{"path":"go.mod"}`)},
				{Type: "tool_result", Content: `{"content":"ok"}`},
			},
		},
	})
	if tokens <= contextutil.EstimateTokens("hello") {
		t.Fatalf("estimate should include structured content, got %d", tokens)
	}
}
