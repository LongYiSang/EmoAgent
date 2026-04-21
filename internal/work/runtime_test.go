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
	"github.com/longyisang/emoagent/internal/progress"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/runtimeenv"
	"github.com/longyisang/emoagent/internal/tool"
)

type fakeRuntimeDecider struct {
	decision   RuntimeDecision
	err        error
	calls      int
	lastBrief  protocol.TaskBrief
	lastPacket protocol.DecisionPacket
}

func (d *fakeRuntimeDecider) Decide(_ context.Context, brief protocol.TaskBrief, packet protocol.DecisionPacket) (RuntimeDecision, error) {
	d.calls++
	d.lastBrief = brief
	d.lastPacket = packet
	return d.decision, d.err
}

func mixedToolUseResp(calls ...llm.ContentBlock) *llm.ChatResponse {
	return &llm.ChatResponse{
		ID:            "resp-tool",
		StopReason:    "tool_use",
		ContentBlocks: calls,
	}
}

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
	registry.Register(NewFinishTaskTool(), FinishTaskPlaceholderHandler)
	registry.Register(NewRequestDecisionTool(), RequestDecisionPlaceholderHandler)

	dispatcher := tool.NewDispatcher(registry, tool.MinimalSchemaValidator{}, testLogger())
	return registry, dispatcher
}

func newTestRuntime(t *testing.T, client llm.Client) *Runtime {
	t.Helper()
	registry, dispatcher := newTestRegistryAndDispatcher(t)
	return NewRuntime(RuntimeConfig{
		LLM:                      client,
		Provider:                 "openai",
		Model:                    "test-model",
		MaxTokens:                2048,
		Temperature:              0.2,
		MaxToolRounds:            4,
		MaxInputTokens:           100000,
		Registry:                 registry,
		Dispatcher:               dispatcher,
		Logger:                   testLogger(),
		MaxEscalations:           3,
		PendingSnapshotMaxTokens: 4000,
		EnvironmentFacts: runtimeenv.Facts{
			OS:            "linux",
			WorkspaceRoot: "/repo",
			PathStyle:     "posix",
			BashEnabled:   true,
			ShellDisplay:  "sh -c",
		},
	})
}

func newTestRuntimeWithDecider(t *testing.T, client llm.Client, decider RuntimeDecider) *Runtime {
	t.Helper()
	registry, dispatcher := newTestRegistryAndDispatcher(t)
	return NewRuntime(RuntimeConfig{
		LLM:                      client,
		Provider:                 "openai",
		Model:                    "test-model",
		MaxTokens:                2048,
		Temperature:              0.2,
		MaxToolRounds:            5,
		MaxInputTokens:           100000,
		Registry:                 registry,
		Dispatcher:               dispatcher,
		Logger:                   testLogger(),
		Decider:                  decider,
		MaxEscalations:           3,
		PendingSnapshotMaxTokens: 4000,
		EnvironmentFacts: runtimeenv.Facts{
			OS:            "linux",
			WorkspaceRoot: "/repo",
			PathStyle:     "posix",
			BashEnabled:   true,
			ShellDisplay:  "sh -c",
		},
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

func decisionPacketJSON(category, risk string, includeFinding bool) string {
	packet := map[string]any{
		"task_id":             "task-1",
		"category":            category,
		"risk_level":          risk,
		"goal_summary":        "choose next step",
		"question":            "which option should we use?",
		"why_blocked":         "need decision to continue",
		"options":             []map[string]any{{"id": "a", "summary": "option a"}, {"id": "b", "summary": "option b"}},
		"suggests_user_input": false,
	}
	if includeFinding {
		packet["relevant_findings"] = []map[string]any{{"finding": "this is sensitive"}}
	}
	out, _ := json.Marshal(packet)
	return string(out)
}

func finishTaskPayloadJSON(status, summary string, findings, openQuestions []string) string {
	payload := map[string]any{
		"status":  status,
		"summary": summary,
	}
	if findings != nil {
		payload["findings"] = findings
	}
	if openQuestions != nil {
		payload["open_questions"] = openQuestions
	}
	out, _ := json.Marshal(payload)
	return string(out)
}

func TestRuntime_HappyPath(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("call-1", "echo_tool", `{"x":"y"}`),
			toolUseResp("finish-1", "finish_task", finishTaskPayloadJSON("completed", "done", []string{"a"}, nil)),
		},
	}
	runtime := newTestRuntime(t, client)
	brief := newValidatedBrief(t)

	journal, err := Open(t.TempDir(), brief.TaskID, time.Now().UTC(), testLogger())
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer func() { _ = journal.Close() }()

	outcome := runtime.Run(context.Background(), brief, journal)
	if outcome.Report == nil {
		t.Fatalf("Run should return report, got %#v", outcome)
	}
	if outcome.Report.Status != "completed" {
		t.Fatalf("Status = %q, want completed", outcome.Report.Status)
	}
	if outcome.Report.TaskID != brief.TaskID {
		t.Fatalf("TaskID = %q, want %q", outcome.Report.TaskID, brief.TaskID)
	}
	if outcome.Report.Goal != brief.Goal {
		t.Fatalf("Goal = %q, want %q", outcome.Report.Goal, brief.Goal)
	}
	if outcome.Report.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be set by runtime")
	}
	if len(client.calls) != 2 {
		t.Fatalf("LLM calls = %d, want 2", len(client.calls))
	}
	if len(client.calls[0].Messages) != 0 {
		t.Fatalf("first request should start with empty history, got %d messages", len(client.calls[0].Messages))
	}
}

func TestRuntime_UsesEnvironmentFactsInSystemPrompt(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			textResp(`{"status":"completed","summary":"done"}`),
		},
	}
	registry, dispatcher := newTestRegistryAndDispatcher(t)
	runtime := NewRuntime(RuntimeConfig{
		LLM:            client,
		Provider:       "openai",
		Model:          "test-model",
		MaxTokens:      2048,
		Temperature:    0.2,
		MaxToolRounds:  4,
		MaxInputTokens: 100000,
		Registry:       registry,
		Dispatcher:     dispatcher,
		Logger:         testLogger(),
		EnvironmentFacts: runtimeenv.Facts{
			OS:            "windows",
			WorkspaceRoot: `D:\repo`,
			PathStyle:     "windows",
			BashEnabled:   true,
			ShellDisplay:  "cmd /c",
		},
	})

	outcome := runtime.Run(context.Background(), newValidatedBrief(t), nil)
	if outcome.Report == nil {
		t.Fatalf("Run should return report, got %#v", outcome)
	}
	if len(client.calls) != 1 {
		t.Fatalf("LLM calls = %d, want 1", len(client.calls))
	}
	if !strings.Contains(client.calls[0].System, "OS: Windows") {
		t.Fatalf("system prompt missing OS fact: %s", client.calls[0].System)
	}
	if !strings.Contains(client.calls[0].System, "Shell commands: unavailable in this task.") {
		t.Fatalf("system prompt should mark shell unavailable for read-only tasks: %s", client.calls[0].System)
	}
}

func TestRuntime_LegacyTaskReportJSONFallback(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			textResp(`{"status":"completed","summary":"done","findings":["a"]}`),
		},
	}
	runtime := newTestRuntime(t, client)
	brief := newValidatedBrief(t)

	outcome := runtime.Run(context.Background(), brief, nil)
	if outcome.Report == nil {
		t.Fatalf("Run should return report, got %#v", outcome)
	}
	if outcome.Report.Status != "completed" {
		t.Fatalf("Status = %q, want completed", outcome.Report.Status)
	}
	if outcome.Report.TaskID != brief.TaskID {
		t.Fatalf("TaskID = %q, want %q", outcome.Report.TaskID, brief.TaskID)
	}
}

func TestRuntime_MaxToolRoundsExhausted(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("call-1", "echo_tool", `{}`),
			toolUseResp("call-2", "echo_tool", `{}`),
			toolUseResp("call-3", "echo_tool", `{}`),
			toolUseResp("call-4", "echo_tool", `{}`),
		},
	}
	runtime := newTestRuntime(t, client)

	outcome := runtime.Run(context.Background(), newValidatedBrief(t), nil)
	if outcome.Report == nil {
		t.Fatalf("Run should return report, got %#v", outcome)
	}
	if outcome.Report.Status != "partial" {
		t.Fatalf("Status = %q, want partial", outcome.Report.Status)
	}
	if !strings.Contains(strings.ToLower(outcome.Report.Summary), "max") {
		t.Fatalf("Summary = %q, want round exhaustion hint", outcome.Report.Summary)
	}
}

func TestRuntime_CanceledContextReturnsFailed(t *testing.T) {
	client := &scriptedLLM{errs: []error{context.Canceled}}
	runtime := newTestRuntime(t, client)
	brief := newValidatedBrief(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	outcome := runtime.Run(ctx, brief, nil)
	if outcome.Report == nil {
		t.Fatalf("Run should return report, got %#v", outcome)
	}
	if outcome.Report.Status != "failed" {
		t.Fatalf("Status = %q, want failed", outcome.Report.Status)
	}
}

func TestRuntime_LLMErrorReturnsFailed(t *testing.T) {
	client := &scriptedLLM{errs: []error{errors.New("transport failed")}}
	runtime := newTestRuntime(t, client)

	outcome := runtime.Run(context.Background(), newValidatedBrief(t), nil)
	if outcome.Report == nil {
		t.Fatalf("Run should return report, got %#v", outcome)
	}
	if outcome.Report.Status != "failed" {
		t.Fatalf("Status = %q, want failed", outcome.Report.Status)
	}
	if !strings.Contains(outcome.Report.Summary, "transport failed") {
		t.Fatalf("Summary = %q, want error details", outcome.Report.Summary)
	}
}

func TestRuntime_NonJSONFinalFallsBackToPartial(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			textResp("I think the answer is 42."),
		},
	}
	runtime := newTestRuntime(t, client)

	outcome := runtime.Run(context.Background(), newValidatedBrief(t), nil)
	if outcome.Report == nil {
		t.Fatalf("Run should return report, got %#v", outcome)
	}
	if outcome.Report.Status != "partial" {
		t.Fatalf("Status = %q, want partial", outcome.Report.Status)
	}
	if !strings.Contains(outcome.Report.Summary, "42") {
		t.Fatalf("Summary = %q, want raw text fallback", outcome.Report.Summary)
	}
}

func TestRuntime_WritesToolEventsToJournal(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("call-1", "echo_tool", `{"x":"y"}`),
			toolUseResp("finish-1", "finish_task", finishTaskPayloadJSON("completed", "done", nil, nil)),
		},
	}
	runtime := newTestRuntime(t, client)
	brief := newValidatedBrief(t)
	root := t.TempDir()
	now := time.Now().UTC()

	journal, err := Open(root, brief.TaskID, now, testLogger())
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	outcome := runtime.Run(context.Background(), brief, journal)
	if err := journal.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if outcome.Report == nil || outcome.Report.Status != "completed" {
		t.Fatalf("status = %#v, want completed report", outcome.Report)
	}

	data, err := os.ReadFile(filepath.Join(root, now.Format("2006-01-02"), brief.TaskID+".jsonl"))
	if err != nil {
		t.Fatalf("expected journal file: %v", err)
	}
	text := string(data)
	for _, snippet := range []string{`"kind":"tool_call"`, `"kind":"tool_result"`} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("journal missing %s: %s", snippet, text)
		}
	}
	if !strings.Contains(text, `"content_preview":"{\"ok\":true}"`) {
		t.Fatalf("journal missing content_preview: %s", text)
	}
}

func TestRuntime_RequestDecisionRuntimeDeciderPath(t *testing.T) {
	packet := decisionPacketJSON(string(protocol.CatExecutionOnly), "low", false)
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("decide-1", "request_decision", packet),
			toolUseResp("finish-1", "finish_task", finishTaskPayloadJSON("completed", "done", nil, nil)),
		},
	}
	decider := &fakeRuntimeDecider{
		decision: RuntimeDecision{Decision: "a", Reason: "safe"},
	}
	runtime := newTestRuntimeWithDecider(t, client, decider)
	brief := newValidatedBrief(t)
	brief.TaskID = "task-1"

	outcome := runtime.Run(context.Background(), brief, nil)
	if outcome.Report == nil || outcome.Report.Status != "completed" {
		t.Fatalf("expected completed report, got %#v", outcome)
	}
	if decider.calls != 1 {
		t.Fatalf("decider calls = %d, want 1", decider.calls)
	}
}

func TestRuntime_RequestDecisionUnknownTaskIDIsNormalized(t *testing.T) {
	packet := map[string]any{
		"task_id":             "unknown",
		"category":            string(protocol.CatExecutionOnly),
		"risk_level":          "low",
		"goal_summary":        "choose next step",
		"question":            "which option should we use?",
		"why_blocked":         "need decision to continue",
		"options":             []map[string]any{{"id": "a", "summary": "option a"}, {"id": "b", "summary": "option b"}},
		"suggests_user_input": false,
	}
	packetJSON, _ := json.Marshal(packet)

	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("decide-1", "request_decision", string(packetJSON)),
			toolUseResp("finish-1", "finish_task", finishTaskPayloadJSON("completed", "done", nil, nil)),
		},
	}
	decider := &fakeRuntimeDecider{
		decision: RuntimeDecision{Decision: "a", Reason: "safe"},
	}
	runtime := newTestRuntimeWithDecider(t, client, decider)
	brief := newValidatedBrief(t)
	brief.TaskID = "task-1"

	outcome := runtime.Run(context.Background(), brief, nil)
	if outcome.Report == nil || outcome.Report.Status != "completed" {
		t.Fatalf("expected completed report, got %#v", outcome)
	}
	if decider.calls != 1 {
		t.Fatalf("decider calls = %d, want 1", decider.calls)
	}
	if decider.lastPacket.TaskID != brief.TaskID {
		t.Fatalf("decider packet task_id = %q, want %q", decider.lastPacket.TaskID, brief.TaskID)
	}
}

func TestRuntime_RequestDecisionEscalatesToPaused(t *testing.T) {
	packet := decisionPacketJSON(string(protocol.CatExecutionOnly), "low", false)
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("decide-1", "request_decision", packet),
		},
	}
	decider := &fakeRuntimeDecider{
		decision: RuntimeDecision{Escalate: true, EscalateReason: "not sure"},
	}
	runtime := newTestRuntimeWithDecider(t, client, decider)
	brief := newValidatedBrief(t)
	brief.TaskID = "task-1"

	outcome := runtime.Run(context.Background(), brief, nil)
	if outcome.Paused == nil {
		t.Fatalf("expected paused outcome, got %#v", outcome)
	}
	if outcome.Paused.PendingCallID != "decide-1" {
		t.Fatalf("PendingCallID = %q, want decide-1", outcome.Paused.PendingCallID)
	}
}

func TestRuntime_RequestDecisionBypassesDeciderForEmotionSensitive(t *testing.T) {
	packet := decisionPacketJSON(string(protocol.CatEmotionSensitive), "low", true)
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("decide-1", "request_decision", packet),
		},
	}
	decider := &fakeRuntimeDecider{
		decision: RuntimeDecision{Decision: "a"},
	}
	runtime := newTestRuntimeWithDecider(t, client, decider)
	brief := newValidatedBrief(t)
	brief.TaskID = "task-1"

	outcome := runtime.Run(context.Background(), brief, nil)
	if outcome.Paused == nil {
		t.Fatalf("expected paused outcome, got %#v", outcome)
	}
	if decider.calls != 0 {
		t.Fatalf("decider should not be called, got %d", decider.calls)
	}
}

func TestRuntime_SoleCallViolationReturnsErrorForRequestDecision(t *testing.T) {
	packet := decisionPacketJSON(string(protocol.CatExecutionOnly), "low", false)
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			mixedToolUseResp(
				llm.ContentBlock{Type: "tool_use", ID: "call-decision", Name: "request_decision", Input: json.RawMessage(packet)},
				llm.ContentBlock{Type: "tool_use", ID: "call-echo", Name: "echo_tool", Input: json.RawMessage(`{"x":"y"}`)},
			),
			toolUseResp("decide-2", "request_decision", packet),
			toolUseResp("finish-1", "finish_task", finishTaskPayloadJSON("completed", "done", nil, nil)),
		},
	}
	decider := &fakeRuntimeDecider{decision: RuntimeDecision{Decision: "a"}}
	runtime := newTestRuntimeWithDecider(t, client, decider)
	brief := newValidatedBrief(t)
	brief.TaskID = "task-1"

	outcome := runtime.Run(context.Background(), brief, nil)
	if outcome.Report == nil || outcome.Report.Status != "completed" {
		t.Fatalf("expected completed report, got %#v", outcome)
	}
	if decider.calls != 1 {
		t.Fatalf("decider calls = %d, want 1 (only on sole request_decision)", decider.calls)
	}
}

func TestRuntime_SoleCallViolationReturnsErrorForFinishTask(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			mixedToolUseResp(
				llm.ContentBlock{Type: "tool_use", ID: "call-finish", Name: "finish_task", Input: json.RawMessage(finishTaskPayloadJSON("completed", "done", nil, nil))},
				llm.ContentBlock{Type: "tool_use", ID: "call-echo", Name: "echo_tool", Input: json.RawMessage(`{"x":"y"}`)},
			),
			toolUseResp("finish-2", "finish_task", finishTaskPayloadJSON("completed", "done", nil, nil)),
		},
	}
	runtime := newTestRuntime(t, client)

	outcome := runtime.Run(context.Background(), newValidatedBrief(t), nil)
	if outcome.Report == nil {
		t.Fatalf("Run should return report, got %#v", outcome)
	}
	if outcome.Report.Status != "completed" {
		t.Fatalf("Status = %q, want completed", outcome.Report.Status)
	}
}

func TestRuntime_InvalidFinishTaskPayloadReturnsToolError(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("finish-1", "finish_task", `{"status":"done"}`),
			toolUseResp("finish-2", "finish_task", finishTaskPayloadJSON("completed", "done", nil, []string{"none"})),
		},
	}
	runtime := newTestRuntime(t, client)

	outcome := runtime.Run(context.Background(), newValidatedBrief(t), nil)
	if outcome.Report == nil {
		t.Fatalf("Run should return report, got %#v", outcome)
	}
	if outcome.Report.Status != "completed" {
		t.Fatalf("Status = %q, want completed", outcome.Report.Status)
	}
	if len(outcome.Report.OpenQuestions) != 1 || outcome.Report.OpenQuestions[0] != "none" {
		t.Fatalf("OpenQuestions = %#v, want [none]", outcome.Report.OpenQuestions)
	}
}

func TestRuntime_ResumeCanPauseAgain(t *testing.T) {
	brief := newValidatedBrief(t)
	brief.TaskID = "task-1"
	packet := decisionPacketJSON(string(protocol.CatExecutionOnly), "low", false)
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("decide-2", "request_decision", packet),
		},
	}
	runtime := newTestRuntime(t, client)
	paused := &PausedWork{
		TaskID:          brief.TaskID,
		Brief:           brief,
		Messages:        []llm.Message{},
		PendingCallID:   "pending-1",
		Packet:          validDecisionPacket(brief.TaskID),
		Round:           1,
		EscalationCount: 1,
	}

	outcome := runtime.Resume(context.Background(), paused, protocol.DecisionResponse{
		TaskID:   brief.TaskID,
		Decision: "a",
		Reason:   "continue",
	}, nil)
	if outcome.Paused == nil {
		t.Fatalf("expected second pause, got %#v", outcome)
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

func TestRuntime_EmitsProgressStartToolAndFinishing(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("call-1", "echo_tool", `{"x":"y"}`),
			toolUseResp("finish-1", "finish_task", finishTaskPayloadJSON("completed", "done", nil, nil)),
		},
	}
	runtime := newTestRuntime(t, client)
	brief := newValidatedBrief(t)

	var events []progress.Event
	ctx := progress.WithCallback(context.Background(), func(event progress.Event) {
		events = append(events, event)
	})

	outcome := runtime.Run(ctx, brief, nil)
	if outcome.Report == nil {
		t.Fatalf("expected report outcome, got %#v", outcome)
	}

	var kinds []progress.EventKind
	var toolNames []string
	for _, event := range events {
		kinds = append(kinds, event.Kind)
		if event.Kind == progress.KindTool {
			toolNames = append(toolNames, event.ToolName)
		}
	}
	if !containsEventKind(kinds, progress.KindStart) {
		t.Fatalf("events = %#v, want start", events)
	}
	if !containsEventKind(kinds, progress.KindFinishing) {
		t.Fatalf("events = %#v, want finishing", events)
	}
	if len(toolNames) != 1 || toolNames[0] != "echo_tool" {
		t.Fatalf("toolNames = %#v, want [echo_tool]", toolNames)
	}
}

func containsEventKind(kinds []progress.EventKind, target progress.EventKind) bool {
	for _, kind := range kinds {
		if kind == target {
			return true
		}
	}
	return false
}
