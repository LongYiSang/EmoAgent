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

func testBashDestructiveClassifier(input json.RawMessage) (bool, string) {
	var payload struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return false, ""
	}
	command := strings.ToLower(" " + payload.Command + " ")
	if strings.Contains(command, " rm ") || strings.Contains(command, " rm -") || strings.Contains(command, " del ") {
		return true, "test destructive command"
	}
	return false, ""
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

func newApprovalTestRuntime(t *testing.T, client llm.Client, executed *[]string) *Runtime {
	t.Helper()

	registry := tool.NewRegistry()
	registry.Register(tool.Spec{
		Name:                  "bash",
		Description:           "runs a shell command",
		Parameters:            json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"],"additionalProperties":false}`),
		Scope:                 tool.ScopeWork,
		Permission:            tool.PermWorkspaceWrite,
		DestructiveClassifier: testBashDestructiveClassifier,
	}, func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var payload struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(input, &payload); err != nil {
			return nil, err
		}
		if executed != nil {
			*executed = append(*executed, payload.Command)
		}
		return json.Marshal(map[string]string{"command": payload.Command})
	})
	registry.Register(NewFinishTaskTool(), FinishTaskPlaceholderHandler)
	registry.Register(NewRequestDecisionTool(), RequestDecisionPlaceholderHandler)

	dispatcher := tool.NewDispatcher(registry, tool.MinimalSchemaValidator{}, testLogger())
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
		Goal:               "inspect go.mod",
		AcceptanceCriteria: []string{"go.mod inspection is reported"},
		PermissionScope:    "read-only",
	}
	if err := ValidateAndComplete(&brief); err != nil {
		t.Fatalf("ValidateAndComplete returned error: %v", err)
	}
	return brief
}

func decisionPacketJSON(category string, includeFinding bool) string {
	packet := map[string]any{
		"task_id":             "task-1",
		"category":            category,
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

func messagesContainToolError(messages []llm.Message, callID string, snippet string) bool {
	for _, message := range messages {
		if message.ToolCallID == callID && strings.Contains(message.Content, snippet) {
			return true
		}
		for _, block := range message.ContentBlocks {
			if block.ID == callID && block.IsError && strings.Contains(block.Content, snippet) {
				return true
			}
		}
	}
	return false
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
	packet := decisionPacketJSON(string(protocol.CatAuto), false)
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
		"category":            string(protocol.CatAuto),
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
	packet := decisionPacketJSON(string(protocol.CatAuto), false)
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

func TestRuntime_RequestDecisionBypassesDeciderForEmotionJudgment(t *testing.T) {
	packet := decisionPacketJSON(string(protocol.CatEmotionJudgment), true)
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

func TestRuntime_AutoPausesOnApprovalBlockedTool(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("bash-1", "bash", `{"command":"rm -rf tmp"}`),
		},
	}
	var executed []string
	runtime := newApprovalTestRuntime(t, client, &executed)
	brief := newValidatedBrief(t)
	brief.TaskID = "task-approval"
	brief.Goal = "delete generated tmp directory"
	brief.PermissionScope = "approved-destructive"

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
	if outcome.Paused == nil {
		t.Fatalf("expected paused outcome, got %#v", outcome)
	}
	if outcome.Paused.PendingCallID != "bash-1" {
		t.Fatalf("PendingCallID = %q, want bash-1", outcome.Paused.PendingCallID)
	}
	if len(executed) != 0 {
		t.Fatalf("bash tool should not have executed before pause, got %#v", executed)
	}
	if outcome.Paused.PendingToolCall == nil {
		t.Fatal("PendingToolCall should be captured on approval interception")
	}
	if outcome.Paused.PendingToolCall.Name != "bash" {
		t.Fatalf("PendingToolCall.Name = %q, want bash", outcome.Paused.PendingToolCall.Name)
	}
	if string(outcome.Paused.PendingToolCall.Input) != `{"command":"rm -rf tmp"}` {
		t.Fatalf("PendingToolCall.Input = %s", outcome.Paused.PendingToolCall.Input)
	}
	if outcome.Paused.Packet.Category != protocol.CatToolApproval {
		t.Fatalf("Category = %q, want %q", outcome.Paused.Packet.Category, protocol.CatToolApproval)
	}
	for _, snippet := range []string{
		"我准备执行一个受限命令，尚未执行。",
		"操作：执行 bash 命令",
		"命令：rm -rf tmp",
		"确认执行请点击“允许执行”；取消请点击“拒绝”。",
	} {
		if !strings.Contains(outcome.Paused.Packet.Question, snippet) {
			t.Fatalf("Question = %q, missing %q", outcome.Paused.Packet.Question, snippet)
		}
	}
	if outcome.Paused.Packet.WhyBlocked != `Tool "bash" requires explicit human approval before execution.` {
		t.Fatalf("WhyBlocked = %q", outcome.Paused.Packet.WhyBlocked)
	}
	if len(outcome.Paused.Packet.Options) != 2 {
		t.Fatalf("Options = %#v, want allow/deny", outcome.Paused.Packet.Options)
	}
	if outcome.Paused.Packet.Options[0].ID != "allow" || outcome.Paused.Packet.Options[1].ID != "deny" {
		t.Fatalf("Options = %#v, want allow then deny", outcome.Paused.Packet.Options)
	}
	if outcome.Paused.Packet.RecommendedOption != "allow" {
		t.Fatalf("RecommendedOption = %q, want allow", outcome.Paused.Packet.RecommendedOption)
	}
	if outcome.Paused.Packet.RejectOptionID != "deny" {
		t.Fatalf("RejectOptionID = %q, want deny", outcome.Paused.Packet.RejectOptionID)
	}
	if !strings.Contains(outcome.Paused.Packet.RecommendationReason, brief.Goal) {
		t.Fatalf("RecommendationReason = %q, want goal context", outcome.Paused.Packet.RecommendationReason)
	}
	if outcome.Paused.Packet.SuggestsUserInput {
		t.Fatal("tool approval interception should not suggest user input")
	}

	data, err := os.ReadFile(filepath.Join(root, now.Format("2006-01-02"), brief.TaskID+".jsonl"))
	if err != nil {
		t.Fatalf("expected journal file: %v", err)
	}
	if !strings.Contains(string(data), `"kind":"tool_approval_intercepted"`) {
		t.Fatalf("journal missing tool_approval_intercepted event: %s", data)
	}
}

func TestRuntime_WorkspaceWriteDestructiveCallBecomesPermissionEscalationPause(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("bash-1", "bash", `{"command":"del hi.txt"}`),
		},
	}
	var executed []string
	runtime := newApprovalTestRuntime(t, client, &executed)
	brief := newValidatedBrief(t)
	brief.TaskID = "task-permission-escalation"
	brief.Goal = "delete hi.txt after user approval"
	brief.PermissionScope = "workspace-write"

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
	if outcome.Paused == nil {
		t.Fatalf("expected paused outcome, got %#v", outcome)
	}
	if len(executed) != 0 {
		t.Fatalf("destructive tool should not run before user approval, got %#v", executed)
	}
	if outcome.Paused.Packet.Category != protocol.CatPermissionEscalationRequired {
		t.Fatalf("Category = %q, want %q", outcome.Paused.Packet.Category, protocol.CatPermissionEscalationRequired)
	}
	if outcome.Paused.PendingToolCall == nil || outcome.Paused.PendingToolCall.ID != "bash-1" {
		t.Fatalf("PendingToolCall = %#v, want blocked bash call", outcome.Paused.PendingToolCall)
	}
	if len(outcome.Paused.Packet.Options) != 2 {
		t.Fatalf("Options = %#v, want approve/reject", outcome.Paused.Packet.Options)
	}
	if outcome.Paused.Packet.Options[0].ID != "approve" || outcome.Paused.Packet.Options[1].ID != "reject" {
		t.Fatalf("Options = %#v, want approve/reject ids", outcome.Paused.Packet.Options)
	}
	if !outcome.Paused.Packet.SuggestsUserInput {
		t.Fatal("permission escalation pause should require user input")
	}

	data, err := os.ReadFile(filepath.Join(root, now.Format("2006-01-02"), brief.TaskID+".jsonl"))
	if err != nil {
		t.Fatalf("expected journal file: %v", err)
	}
	if !strings.Contains(string(data), `"kind":"permission_escalation_intercepted"`) {
		t.Fatalf("journal missing permission_escalation_intercepted event: %s", data)
	}
}

func TestBuildToolApprovalPacket_NonBashUsesReadableCallSummary(t *testing.T) {
	brief := protocol.TaskBrief{
		TaskID:          "task-approval-generic",
		Goal:            "clean generated artifacts",
		PermissionScope: "approved-destructive",
	}
	call := tool.Call{
		ID:   "delete-1",
		Name: "dangerous_delete",
		Input: json.RawMessage(`{
			"path":"tmp/output",
			"recursive":true,
			"force":true
		}`),
	}

	packet := buildToolApprovalPacket(brief, call)
	if packet.Category != protocol.CatToolApproval {
		t.Fatalf("Category = %q, want %q", packet.Category, protocol.CatToolApproval)
	}
	if !strings.Contains(packet.Question, "尚未执行") {
		t.Fatalf("Question = %q, want pending execution wording", packet.Question)
	}
	if !strings.Contains(packet.Question, "操作：") {
		t.Fatalf("Question = %q, want operation summary", packet.Question)
	}
	if !strings.Contains(packet.Question, "目标：dangerous_delete") {
		t.Fatalf("Question = %q, want tool call preview", packet.Question)
	}
	if strings.Contains(packet.Question, "{") {
		t.Fatalf("Question = %q, should avoid raw JSON", packet.Question)
	}
}

func TestBuildToolApprovalPacket_IncludesBindingWithoutRawWriteContent(t *testing.T) {
	brief := protocol.TaskBrief{
		TaskID:          "task-approval-write",
		Goal:            "update config",
		PermissionScope: "approved-destructive",
	}
	call := tool.Call{
		ID:    "write-1",
		Name:  "write_file",
		Input: json.RawMessage(`{"path":"config/.env","content":"SECRET=value","create_dirs":false}`),
	}

	packet := buildToolApprovalPacket(brief, call)
	if !strings.Contains(packet.Question, "尚未执行") {
		t.Fatalf("Question = %q, want pending execution wording", packet.Question)
	}
	if strings.Contains(packet.Question, "SECRET=value") {
		t.Fatalf("Question leaks write_file content: %q", packet.Question)
	}
	if packet.ToolApprovalBinding == nil {
		t.Fatal("ToolApprovalBinding is nil")
	}
	if packet.ToolApprovalBinding.ToolName != "write_file" {
		t.Fatalf("ToolName = %q, want write_file", packet.ToolApprovalBinding.ToolName)
	}
	if packet.ToolApprovalBinding.NormalizedInputHash == "" {
		t.Fatal("NormalizedInputHash should be set")
	}
	if packet.ToolApprovalBinding.PathDigest == "" {
		t.Fatal("PathDigest should be set")
	}
	if strings.Contains(packet.ToolApprovalBinding.InputPreview, "SECRET=value") {
		t.Fatalf("InputPreview leaks write_file content: %q", packet.ToolApprovalBinding.InputPreview)
	}
}

func TestRuntime_AutoPausesOnApprovalBlockedToolFiltersSiblingToolCalls(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			mixedToolUseResp(
				llm.ContentBlock{Type: "text", Text: "Need approval before continuing."},
				llm.ContentBlock{Type: "tool_use", ID: "bash-1", Name: "bash", Input: json.RawMessage(`{"command":"rm -rf tmp"}`)},
				llm.ContentBlock{Type: "tool_use", ID: "bash-2", Name: "bash", Input: json.RawMessage(`{"command":"echo hi"}`)},
			),
			toolUseResp("finish-1", "finish_task", finishTaskPayloadJSON("completed", "done", nil, nil)),
		},
	}
	var executed []string
	runtime := newApprovalTestRuntime(t, client, &executed)
	brief := newValidatedBrief(t)
	brief.TaskID = "task-approval-mixed"
	brief.Goal = "delete generated tmp directory"
	brief.PermissionScope = "approved-destructive"

	outcome := runtime.Run(context.Background(), brief, nil)
	if outcome.Paused == nil {
		t.Fatalf("expected paused outcome, got %#v", outcome)
	}
	if len(executed) != 0 {
		t.Fatalf("no tool should execute before approval pause, got %#v", executed)
	}
	if len(outcome.Paused.Messages) != 1 {
		t.Fatalf("paused messages = %d, want 1 assistant tool-use message", len(outcome.Paused.Messages))
	}
	pausedMsg := outcome.Paused.Messages[0]
	if pausedMsg.Role != llm.RoleAssistant {
		t.Fatalf("paused message role = %q, want assistant", pausedMsg.Role)
	}
	if len(pausedMsg.ContentBlocks) != 2 {
		t.Fatalf("paused content blocks = %#v, want text + blocked tool_use only", pausedMsg.ContentBlocks)
	}
	if pausedMsg.ContentBlocks[0].Type != "text" || pausedMsg.ContentBlocks[0].Text != "Need approval before continuing." {
		t.Fatalf("first block = %#v, want preserved text", pausedMsg.ContentBlocks[0])
	}
	if pausedMsg.ContentBlocks[1].Type != "tool_use" || pausedMsg.ContentBlocks[1].ID != "bash-1" {
		t.Fatalf("second block = %#v, want blocked tool call only", pausedMsg.ContentBlocks[1])
	}
	for _, block := range pausedMsg.ContentBlocks {
		if block.Type == "tool_use" && block.ID == "bash-2" {
			t.Fatalf("paused snapshot must not keep sibling tool_use blocks: %#v", pausedMsg.ContentBlocks)
		}
	}

	binding, err := tool.BuildApprovalBinding(*outcome.Paused.PendingToolCall, "req-1")
	if err != nil {
		t.Fatalf("BuildApprovalBinding: %v", err)
	}
	resumed := runtime.Resume(tool.WithApproval(context.Background(), tool.ApprovalContext{
		RequestID:           binding.RequestID,
		AllowDestructive:    true,
		ToolName:            binding.ToolName,
		NormalizedInputHash: binding.NormalizedInputHash,
		PathDigest:          binding.PathDigest,
	}), outcome.Paused, protocol.DecisionResponse{TaskID: brief.TaskID}, nil)
	if resumed.Report == nil || resumed.Report.Status != "completed" {
		t.Fatalf("expected completed report after resume, got %#v", resumed)
	}
	if len(client.calls) != 2 {
		t.Fatalf("LLM calls = %d, want 2", len(client.calls))
	}
	if len(client.calls[1].Messages) < 2 {
		t.Fatalf("resumed messages = %#v, want filtered assistant message plus tool result", client.calls[1].Messages)
	}
	resumedAssistant := client.calls[1].Messages[0]
	if len(resumedAssistant.ContentBlocks) != 2 {
		t.Fatalf("resumed assistant blocks = %#v, want filtered paused snapshot", resumedAssistant.ContentBlocks)
	}
	for _, block := range resumedAssistant.ContentBlocks {
		if block.Type == "tool_use" && block.ID == "bash-2" {
			t.Fatalf("resumed transcript must not contain sibling tool_use blocks: %#v", resumedAssistant.ContentBlocks)
		}
	}
	last := client.calls[1].Messages[len(client.calls[1].Messages)-1]
	if last.ToolCallID != "bash-1" {
		t.Fatalf("tool result call id = %q, want bash-1", last.ToolCallID)
	}
}

func TestRuntime_RequestDecisionMixedWithBlockedToolDoesNotExecuteSibling(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			mixedToolUseResp(
				llm.ContentBlock{Type: "tool_use", ID: "decide-1", Name: "request_decision", Input: json.RawMessage(decisionPacketJSON(string(protocol.CatAuto), false))},
				llm.ContentBlock{Type: "tool_use", ID: "bash-1", Name: "bash", Input: json.RawMessage(`{"command":"rm -rf tmp"}`)},
			),
			toolUseResp("finish-1", "finish_task", finishTaskPayloadJSON("completed", "done", nil, nil)),
		},
	}
	var executed []string
	runtime := newApprovalTestRuntime(t, client, &executed)
	brief := newValidatedBrief(t)
	brief.TaskID = "task-approval-decision-mixed"
	brief.Goal = "delete generated tmp directory"
	brief.PermissionScope = "approved-destructive"

	outcome := runtime.Run(context.Background(), brief, nil)
	if len(executed) != 0 {
		t.Fatalf("mixed protocol round must not execute sibling tools, got %#v", executed)
	}
	if outcome.Report == nil || outcome.Report.Status != "completed" {
		t.Fatalf("expected completed report after protocol retry, got %#v", outcome)
	}
	if len(client.calls) != 2 {
		t.Fatalf("LLM calls = %d, want 2", len(client.calls))
	}
	if !messagesContainToolError(client.calls[1].Messages, "decide-1", "request_decision must be the sole tool call in this round") {
		t.Fatalf("second request missing request_decision protocol error: %#v", client.calls[1].Messages)
	}
	if !messagesContainToolError(client.calls[1].Messages, "bash-1", "request_decision must be the sole tool call in this round") {
		t.Fatalf("second request missing sibling protocol error: %#v", client.calls[1].Messages)
	}
}

func TestRuntime_FinishTaskMixedWithBlockedToolDoesNotExecuteSibling(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			mixedToolUseResp(
				llm.ContentBlock{Type: "tool_use", ID: "finish-1", Name: "finish_task", Input: json.RawMessage(finishTaskPayloadJSON("completed", "done", nil, nil))},
				llm.ContentBlock{Type: "tool_use", ID: "bash-1", Name: "bash", Input: json.RawMessage(`{"command":"rm -rf tmp"}`)},
			),
			toolUseResp("finish-2", "finish_task", finishTaskPayloadJSON("completed", "done", nil, nil)),
		},
	}
	var executed []string
	runtime := newApprovalTestRuntime(t, client, &executed)
	brief := newValidatedBrief(t)
	brief.TaskID = "task-approval-finish-mixed"
	brief.Goal = "delete generated tmp directory"
	brief.PermissionScope = "approved-destructive"

	outcome := runtime.Run(context.Background(), brief, nil)
	if len(executed) != 0 {
		t.Fatalf("mixed protocol round must not execute sibling tools, got %#v", executed)
	}
	if outcome.Report == nil || outcome.Report.Status != "completed" {
		t.Fatalf("expected completed report after protocol retry, got %#v", outcome)
	}
	if len(client.calls) != 2 {
		t.Fatalf("LLM calls = %d, want 2", len(client.calls))
	}
	if !messagesContainToolError(client.calls[1].Messages, "finish-1", "finish_task must be the sole tool call in this round") {
		t.Fatalf("second request missing finish_task protocol error: %#v", client.calls[1].Messages)
	}
	if !messagesContainToolError(client.calls[1].Messages, "bash-1", "finish_task must be the sole tool call in this round") {
		t.Fatalf("second request missing sibling protocol error: %#v", client.calls[1].Messages)
	}
}

func TestRuntime_SoleCallViolationReturnsErrorForRequestDecision(t *testing.T) {
	packet := decisionPacketJSON(string(protocol.CatAuto), false)
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
	packet := decisionPacketJSON(string(protocol.CatAuto), false)
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

func TestRuntime_ResumeReExecutesApprovedToolCall(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("finish-1", "finish_task", finishTaskPayloadJSON("completed", "done", nil, nil)),
		},
	}
	var executed []string
	runtime := newApprovalTestRuntime(t, client, &executed)

	brief := protocol.TaskBrief{
		TaskID:             "task-approval",
		Goal:               "delete generated tmp directory",
		AcceptanceCriteria: []string{"Generated tmp directory is deleted"},
		PermissionScope:    "approved-destructive",
	}
	if err := ValidateAndComplete(&brief); err != nil {
		t.Fatalf("ValidateAndComplete returned error: %v", err)
	}

	call := tool.Call{
		ID:    "bash-1",
		Name:  "bash",
		Input: json.RawMessage(`{"command":"rm -rf tmp"}`),
	}
	paused := &PausedWork{
		TaskID: brief.TaskID,
		Brief:  brief,
		Messages: []llm.Message{{
			Role: llm.RoleAssistant,
			ContentBlocks: []llm.ContentBlock{
				{Type: "tool_use", ID: call.ID, Name: call.Name, Input: call.Input},
			},
		}},
		PendingToolCall: &call,
		Packet:          validDecisionPacket(brief.TaskID),
		Round:           1,
		EscalationCount: 1,
	}

	binding, err := tool.BuildApprovalBinding(call, "req-1")
	if err != nil {
		t.Fatalf("BuildApprovalBinding: %v", err)
	}
	outcome := runtime.Resume(tool.WithApproval(context.Background(), tool.ApprovalContext{
		RequestID:           binding.RequestID,
		AllowDestructive:    true,
		ToolName:            binding.ToolName,
		NormalizedInputHash: binding.NormalizedInputHash,
		PathDigest:          binding.PathDigest,
	}), paused, protocol.DecisionResponse{TaskID: brief.TaskID}, nil)
	if outcome.Report == nil {
		t.Fatalf("expected report outcome, got %#v", outcome)
	}
	if outcome.Report.Status != "completed" {
		t.Fatalf("Status = %q, want completed", outcome.Report.Status)
	}
	if len(executed) != 1 || executed[0] != "rm -rf tmp" {
		t.Fatalf("executed = %#v, want resumed destructive command", executed)
	}
	if len(client.calls) != 1 {
		t.Fatalf("LLM calls = %d, want 1", len(client.calls))
	}
	last := client.calls[0].Messages[len(client.calls[0].Messages)-1]
	if last.Role != llm.RoleTool {
		t.Fatalf("last message role = %q, want tool", last.Role)
	}
	if last.ToolCallID != call.ID {
		t.Fatalf("ToolCallID = %q, want %q", last.ToolCallID, call.ID)
	}
	if !strings.Contains(last.Content, `"command":"rm -rf tmp"`) {
		t.Fatalf("tool result content = %q, want resumed bash output", last.Content)
	}
	if strings.Contains(last.Content, `"decision"`) {
		t.Fatalf("resume should re-execute pending tool call instead of injecting a decision response: %q", last.Content)
	}
}

func TestRuntime_ResumeRejectedApprovalDoesNotExecuteBlockedTool(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("finish-1", "finish_task", finishTaskPayloadJSON("completed", "replanned", nil, nil)),
		},
	}
	var executed []string
	runtime := newApprovalTestRuntime(t, client, &executed)

	brief := protocol.TaskBrief{
		TaskID:             "task-approval-reject",
		Goal:               "delete generated tmp directory",
		AcceptanceCriteria: []string{"Deletion is either completed or safely rejected"},
		PermissionScope:    "approved-destructive",
	}
	if err := ValidateAndComplete(&brief); err != nil {
		t.Fatalf("ValidateAndComplete returned error: %v", err)
	}

	call := tool.Call{
		ID:    "bash-1",
		Name:  "bash",
		Input: json.RawMessage(`{"command":"rm -rf tmp"}`),
	}
	paused := &PausedWork{
		TaskID: brief.TaskID,
		Brief:  brief,
		Messages: []llm.Message{{
			Role: llm.RoleAssistant,
			ContentBlocks: []llm.ContentBlock{
				{Type: "tool_use", ID: call.ID, Name: call.Name, Input: call.Input},
			},
		}},
		PendingCallID:   call.ID,
		PendingToolCall: &call,
		Packet:          buildToolApprovalPacket(brief, call),
		Round:           1,
		EscalationCount: 1,
	}

	outcome := runtime.Resume(context.Background(), paused, protocol.DecisionResponse{
		TaskID:   brief.TaskID,
		Decision: "deny",
		Reason:   "do not delete files",
	}, nil)
	if outcome.Report == nil || outcome.Report.Status != "completed" {
		t.Fatalf("expected completed report, got %#v", outcome)
	}
	if len(executed) != 0 {
		t.Fatalf("rejected approval must not execute blocked tool, got %#v", executed)
	}
	if len(client.calls) != 1 {
		t.Fatalf("LLM calls = %d, want 1", len(client.calls))
	}
	last := client.calls[0].Messages[len(client.calls[0].Messages)-1]
	if last.Role != llm.RoleTool {
		t.Fatalf("last message role = %q, want tool", last.Role)
	}
	if last.ToolCallID != call.ID {
		t.Fatalf("tool result call id = %q, want %q", last.ToolCallID, call.ID)
	}
	if !strings.Contains(last.Content, "approval denied") {
		t.Fatalf("tool result content = %q, want approval denied error", last.Content)
	}
	if strings.Contains(last.Content, "approval required") {
		t.Fatalf("tool result content = %q, should not trigger another approval-required loop", last.Content)
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
