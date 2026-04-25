package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/llm"
)

// mockValidator implements SchemaValidator for testing.
type mockValidator struct {
	err error // if non-nil, Validate always returns this error
}

func (v *mockValidator) Validate(schema, input json.RawMessage) error {
	return v.err
}

func setupTestRegistry() *Registry {
	r := NewRegistry()
	r.Register(Spec{
		Name:        "get_time",
		Description: "Get current time",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"tz":{"type":"string"}}}`),
		Scope:       ScopeBoth,
		Permission:  PermReadOnly,
	}, func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"time":"10:00"}`), nil
	})

	r.Register(Spec{
		Name:        "write_file",
		Description: "Write a file",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		Scope:       ScopeWork,
		Permission:  PermWorkspaceWrite,
	}, func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"status":"ok"}`), nil
	})

	r.Register(Spec{
		Name:        "failing_tool",
		Description: "Always fails",
		Parameters:  json.RawMessage(`{}`),
		Scope:       ScopeBoth,
		Permission:  PermReadOnly,
	}, func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		return nil, errors.New("something went wrong")
	})

	return r
}

func testDestructiveClassifier(input json.RawMessage) (bool, string) {
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

func setupDestructiveRegistry() *Registry {
	registry := NewRegistry()
	registry.Register(Spec{
		Name:                  "shell",
		Description:           "Run shell command",
		Parameters:            json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"],"additionalProperties":false}`),
		Scope:                 ScopeWork,
		Permission:            PermWorkspaceWrite,
		DestructiveClassifier: testDestructiveClassifier,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"status":"ok"}`), nil
	})
	return registry
}

func TestDispatcherExecute_Success(t *testing.T) {
	d := NewDispatcher(setupTestRegistry(), &mockValidator{}, slog.Default())

	result := d.Execute(context.Background(), Call{
		ID:    "call_1",
		Name:  "get_time",
		Input: json.RawMessage(`{"tz":"UTC"}`),
	}, PermReadOnly)

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.CallID != "call_1" {
		t.Errorf("CallID: got %q", result.CallID)
	}
	if string(result.Content) != `{"time":"10:00"}` {
		t.Errorf("Content: got %s", result.Content)
	}
}

func TestDispatcherExecute_ToolNotFound(t *testing.T) {
	d := NewDispatcher(setupTestRegistry(), &mockValidator{}, slog.Default())

	result := d.Execute(context.Background(), Call{
		ID:   "call_2",
		Name: "nonexistent",
	}, PermReadOnly)

	if !result.IsError {
		t.Fatal("expected error for nonexistent tool")
	}
}

func TestDispatcherExecute_SchemaValidationFailed(t *testing.T) {
	d := NewDispatcher(setupTestRegistry(), &mockValidator{err: errors.New("missing required field")}, slog.Default())

	result := d.Execute(context.Background(), Call{
		ID:    "call_3",
		Name:  "get_time",
		Input: json.RawMessage(`{}`),
	}, PermReadOnly)

	if !result.IsError {
		t.Fatal("expected error for invalid input")
	}
}

func TestDispatcherExecute_RejectsMissingValidatorWhenSchemaPresent(t *testing.T) {
	d := NewDispatcher(setupTestRegistry(), nil, slog.Default())

	result := d.Execute(context.Background(), Call{
		ID:    "call_missing_validator",
		Name:  "get_time",
		Input: json.RawMessage(`{"tz":"UTC"}`),
	}, PermReadOnly)

	if !result.IsError {
		t.Fatal("expected error when schema is present but validator is nil")
	}
}

func TestDispatcherExecute_AllowsMissingValidatorWhenSchemaEmpty(t *testing.T) {
	r := NewRegistry()
	r.Register(Spec{
		Name:        "no_schema",
		Description: "Tool without schema",
		Scope:       ScopeBoth,
		Permission:  PermReadOnly,
	}, func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"ok":true}`), nil
	})

	d := NewDispatcher(r, nil, slog.Default())
	result := d.Execute(context.Background(), Call{
		ID:    "call_no_schema",
		Name:  "no_schema",
		Input: json.RawMessage(`{"ignored":true}`),
	}, PermReadOnly)

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestDispatcherExecute_PermissionDenied(t *testing.T) {
	d := NewDispatcher(setupTestRegistry(), &mockValidator{}, slog.Default())

	// Try write_file (requires workspace-write) with read-only permission.
	result := d.Execute(context.Background(), Call{
		ID:    "call_4",
		Name:  "write_file",
		Input: json.RawMessage(`{"path":"/tmp/test"}`),
	}, PermReadOnly)

	if !result.IsError {
		t.Fatal("expected permission denied")
	}

	// Same tool with sufficient permission.
	result = d.Execute(context.Background(), Call{
		ID:    "call_5",
		Name:  "write_file",
		Input: json.RawMessage(`{"path":"/tmp/test"}`),
	}, PermWorkspaceWrite)

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
}

func TestDispatcherExecute_DestructiveCommandRequiresApprovalContext(t *testing.T) {
	d := NewDispatcher(setupDestructiveRegistry(), &mockValidator{}, slog.Default())

	call := Call{
		ID:    "call_shell_rm",
		Name:  "shell",
		Input: json.RawMessage(`{"command":"rm -rf tmp"}`),
	}

	result := d.Execute(context.Background(), call, PermApprovedDestructive)
	if !result.IsError {
		t.Fatal("expected destructive bash command to be denied without approval")
	}
	if !result.NeedsApproval {
		t.Fatal("expected destructive bash command denial to mark needs approval")
	}

	result = d.Execute(WithApproval(context.Background(), ApprovalContext{
		RequestID:        "req-1",
		AllowDestructive: true,
	}), call, PermApprovedDestructive)
	if result.IsError {
		t.Fatalf("expected destructive bash command with approval to succeed, got: %s", result.Content)
	}
	if result.NeedsApproval {
		t.Fatal("successful execution should not mark needs approval")
	}
}

func TestDispatcherClassifyCall(t *testing.T) {
	d := NewDispatcher(setupDestructiveRegistry(), MinimalSchemaValidator{}, slog.Default())

	destructive := Call{
		ID:    "call_shell_rm",
		Name:  "shell",
		Input: json.RawMessage(`{"command":"rm -rf tmp"}`),
	}
	nondestructive := Call{
		ID:    "call_shell_echo",
		Name:  "shell",
		Input: json.RawMessage(`{"command":"echo hi"}`),
	}

	tests := []struct {
		name         string
		ctx          context.Context
		call         Call
		permission   Permission
		wantAction   CallAction
		wantRequired Permission
	}{
		{
			name:         "workspace write destructive needs escalation",
			call:         destructive,
			permission:   PermWorkspaceWrite,
			wantAction:   CallActionPermissionEscalationRequired,
			wantRequired: PermApprovedDestructive,
		},
		{
			name:         "approved destructive without approval needs tool approval",
			call:         destructive,
			permission:   PermApprovedDestructive,
			wantAction:   CallActionToolApprovalRequired,
			wantRequired: PermApprovedDestructive,
		},
		{
			name: "approved destructive with active approval executes",
			ctx: WithApproval(context.Background(), ApprovalContext{
				RequestID:        "req-1",
				AllowDestructive: true,
			}),
			call:         destructive,
			permission:   PermApprovedDestructive,
			wantAction:   CallActionExecute,
			wantRequired: PermApprovedDestructive,
		},
		{
			name: "approval context without destructive grant still needs tool approval",
			ctx: WithApproval(context.Background(), ApprovalContext{
				RequestID:        "req-2",
				AllowDestructive: false,
			}),
			call:         destructive,
			permission:   PermApprovedDestructive,
			wantAction:   CallActionToolApprovalRequired,
			wantRequired: PermApprovedDestructive,
		},
		{
			name:         "read only destructive is permission denied",
			call:         destructive,
			permission:   PermReadOnly,
			wantAction:   CallActionPermissionDenied,
			wantRequired: PermApprovedDestructive,
		},
		{
			name:         "non destructive workspace write executes",
			call:         nondestructive,
			permission:   PermWorkspaceWrite,
			wantAction:   CallActionExecute,
			wantRequired: PermWorkspaceWrite,
		},
		{
			name: "schema invalid remains validation error",
			call: Call{
				ID:    "call_invalid",
				Name:  "shell",
				Input: json.RawMessage(`{"command":"rm -rf tmp","extra":"boom"}`),
			},
			permission:   PermApprovedDestructive,
			wantAction:   CallActionError,
			wantRequired: "",
		},
		{
			name: "unknown tool is error",
			call: Call{
				ID:   "call_missing",
				Name: "missing",
			},
			permission:   PermWorkspaceWrite,
			wantAction:   CallActionError,
			wantRequired: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			got := d.ClassifyCall(ctx, tt.call, tt.permission)
			if got.Action != tt.wantAction {
				t.Fatalf("Action = %q, want %q; reason=%s", got.Action, tt.wantAction, got.Reason)
			}
			if got.RequiredPermission != tt.wantRequired {
				t.Fatalf("RequiredPermission = %q, want %q", got.RequiredPermission, tt.wantRequired)
			}
		})
	}
}

func TestDispatcherExecute_NonDestructiveCommandDoesNotRequireApproval(t *testing.T) {
	d := NewDispatcher(setupDestructiveRegistry(), &mockValidator{}, slog.Default())

	result := d.Execute(context.Background(), Call{
		ID:    "call_shell_dir",
		Name:  "shell",
		Input: json.RawMessage(`{"command":"dir"}`),
	}, PermWorkspaceWrite)
	if result.IsError {
		t.Fatalf("expected non-destructive bash command to succeed, got: %s", result.Content)
	}
}

func TestDispatcherExecute_HandlerError(t *testing.T) {
	d := NewDispatcher(setupTestRegistry(), &mockValidator{}, slog.Default())

	result := d.Execute(context.Background(), Call{
		ID:    "call_6",
		Name:  "failing_tool",
		Input: json.RawMessage(`{}`),
	}, PermReadOnly)

	if !result.IsError {
		t.Fatal("expected error from failing handler")
	}
}

func TestDispatcherExecuteAll(t *testing.T) {
	d := NewDispatcher(setupTestRegistry(), &mockValidator{}, slog.Default())

	calls := []Call{
		{ID: "call_a", Name: "get_time", Input: json.RawMessage(`{}`)},
		{ID: "call_b", Name: "nonexistent", Input: json.RawMessage(`{}`)},
	}

	results := d.ExecuteAll(context.Background(), calls, PermReadOnly)
	if len(results) != 2 {
		t.Fatalf("results: got %d, want 2", len(results))
	}
	if results[0].IsError {
		t.Errorf("result[0] should succeed")
	}
	if !results[1].IsError {
		t.Errorf("result[1] should fail (nonexistent)")
	}
}

func TestExtractToolCalls(t *testing.T) {
	resp := &llm.ChatResponse{
		ContentBlocks: []llm.ContentBlock{
			{Type: "text", Text: "Let me check."},
			{Type: "tool_use", ID: "call_1", Name: "get_time", Input: json.RawMessage(`{"tz":"UTC"}`)},
			{Type: "tool_use", ID: "call_2", Name: "get_weather", Input: json.RawMessage(`{"city":"Tokyo"}`)},
		},
	}

	calls := ExtractToolCalls(resp)
	if len(calls) != 2 {
		t.Fatalf("calls: got %d, want 2", len(calls))
	}
	if calls[0].ID != "call_1" || calls[0].Name != "get_time" {
		t.Errorf("call[0]: %+v", calls[0])
	}
	if calls[1].ID != "call_2" || calls[1].Name != "get_weather" {
		t.Errorf("call[1]: %+v", calls[1])
	}
}

func TestResultsToMessagesAnthropic(t *testing.T) {
	results := []Result{
		{CallID: "call_1", Content: json.RawMessage(`{"time":"10:00"}`), IsError: false},
		{CallID: "call_2", Content: json.RawMessage(`{"error":"not found"}`), IsError: true},
	}

	msgs := ResultsToMessages("anthropic", results)
	if len(msgs) != 1 {
		t.Fatalf("messages: got %d, want 1", len(msgs))
	}
	msg := msgs[0]

	if msg.Role != llm.RoleUser {
		t.Errorf("Role: got %q, want %q", msg.Role, llm.RoleUser)
	}
	if len(msg.ContentBlocks) != 2 {
		t.Fatalf("ContentBlocks: got %d, want 2", len(msg.ContentBlocks))
	}

	// First result: success.
	if msg.ContentBlocks[0].Type != "tool_result" {
		t.Errorf("block[0].Type: got %q", msg.ContentBlocks[0].Type)
	}
	if msg.ContentBlocks[0].ID != "call_1" {
		t.Errorf("block[0].ID: got %q", msg.ContentBlocks[0].ID)
	}
	if msg.ContentBlocks[0].Content != `{"time":"10:00"}` {
		t.Errorf("block[0].Content: got %q", msg.ContentBlocks[0].Content)
	}
	if msg.ContentBlocks[0].IsError {
		t.Error("block[0] should not be error")
	}

	// Second result: error.
	if !msg.ContentBlocks[1].IsError {
		t.Error("block[1] should be error")
	}
}

func TestResultsToMessagesOpenAI(t *testing.T) {
	results := []Result{
		{CallID: "call_1", Content: json.RawMessage(`{"time":"10:00"}`), IsError: false},
		{CallID: "call_2", Content: json.RawMessage(`{"error":"not found"}`), IsError: true},
	}

	msgs := ResultsToMessages("openai", results)
	if len(msgs) != 2 {
		t.Fatalf("messages: got %d, want 2", len(msgs))
	}

	for i, wantID := range []string{"call_1", "call_2"} {
		if msgs[i].Role != llm.RoleTool {
			t.Errorf("msg[%d].Role: got %q, want %q", i, msgs[i].Role, llm.RoleTool)
		}
		if msgs[i].ToolCallID != wantID {
			t.Errorf("msg[%d].ToolCallID: got %q, want %q", i, msgs[i].ToolCallID, wantID)
		}
		if msgs[i].Content == "" {
			t.Errorf("msg[%d].Content should not be empty", i)
		}
		if len(msgs[i].ContentBlocks) != 0 {
			t.Errorf("msg[%d].ContentBlocks: got %d, want 0", i, len(msgs[i].ContentBlocks))
		}
	}
}

func TestPermissionSatisfied(t *testing.T) {
	tests := []struct {
		granted  Permission
		required Permission
		want     bool
	}{
		{PermReadOnly, PermReadOnly, true},
		{PermWorkspaceWrite, PermReadOnly, true},
		{PermApprovedDestructive, PermWorkspaceWrite, true},
		{PermReadOnly, PermWorkspaceWrite, false},
		{PermReadOnly, PermApprovedDestructive, false},
		{PermWorkspaceWrite, PermApprovedDestructive, false},
		{Permission("unknown"), PermReadOnly, false},
		{PermReadOnly, Permission("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.granted)+"_"+string(tt.required), func(t *testing.T) {
			got := PermissionSatisfied(tt.granted, tt.required)
			if got != tt.want {
				t.Errorf("PermissionSatisfied(%q, %q) = %v, want %v", tt.granted, tt.required, got, tt.want)
			}
		})
	}
}

func TestExtractAndDispatchRoundTrip(t *testing.T) {
	// End-to-end: extract calls from response → execute → convert to Anthropic message.
	registry := setupTestRegistry()
	d := NewDispatcher(registry, &mockValidator{}, slog.Default())

	resp := &llm.ChatResponse{
		StopReason: "tool_use",
		ContentBlocks: []llm.ContentBlock{
			{Type: "text", Text: "Checking..."},
			{Type: "tool_use", ID: "call_rt", Name: "get_time", Input: json.RawMessage(`{"tz":"UTC"}`)},
		},
	}

	calls := ExtractToolCalls(resp)
	results := d.ExecuteAll(context.Background(), calls, PermReadOnly)
	msgs := ResultsToMessages("anthropic", results)
	if len(msgs) != 1 {
		t.Fatalf("messages: got %d, want 1", len(msgs))
	}
	msg := msgs[0]

	if msg.Role != llm.RoleUser {
		t.Errorf("Role: got %q", msg.Role)
	}
	if len(msg.ContentBlocks) != 1 {
		t.Fatalf("blocks: got %d", len(msg.ContentBlocks))
	}
	if msg.ContentBlocks[0].Type != "tool_result" {
		t.Errorf("type: got %q", msg.ContentBlocks[0].Type)
	}
	if msg.ContentBlocks[0].ID != "call_rt" {
		t.Errorf("ID: got %q", msg.ContentBlocks[0].ID)
	}
	if msg.ContentBlocks[0].Content != `{"time":"10:00"}` {
		t.Errorf("Content: got %q", msg.ContentBlocks[0].Content)
	}
}

func TestExtractAndDispatchRoundTripOpenAI(t *testing.T) {
	registry := setupTestRegistry()
	d := NewDispatcher(registry, &mockValidator{}, slog.Default())

	resp := &llm.ChatResponse{
		StopReason: "tool_use",
		ContentBlocks: []llm.ContentBlock{
			{Type: "tool_use", ID: "call_rt", Name: "get_time", Input: json.RawMessage(`{"tz":"UTC"}`)},
		},
	}

	calls := ExtractToolCalls(resp)
	results := d.ExecuteAll(context.Background(), calls, PermReadOnly)
	msgs := ResultsToMessages("openai", results)

	if len(msgs) != 1 {
		t.Fatalf("messages: got %d, want 1", len(msgs))
	}
	if msgs[0].Role != llm.RoleTool {
		t.Errorf("Role: got %q", msgs[0].Role)
	}
	if msgs[0].ToolCallID != "call_rt" {
		t.Errorf("ToolCallID: got %q", msgs[0].ToolCallID)
	}
	if msgs[0].Content != `{"time":"10:00"}` {
		t.Errorf("Content: got %q", msgs[0].Content)
	}
}

func TestDispatcherExecute_LogsPreviewHashAndSizeWithoutFullPayload(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	registry := NewRegistry()
	registry.Register(Spec{
		Name:        "large_payload",
		Description: "returns a large payload",
		Parameters:  json.RawMessage(`{}`),
		Scope:       ScopeBoth,
		Permission:  PermReadOnly,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"body":"` + strings.Repeat("x", 8000) + `"}`), nil
	})

	d := NewDispatcher(registry, &mockValidator{}, logger)
	result := d.Execute(context.Background(), Call{
		ID:    "call_log",
		Name:  "large_payload",
		Input: json.RawMessage(`{}`),
	}, PermReadOnly)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	logOutput := buf.String()
	if !strings.Contains(logOutput, "preview=") {
		t.Fatalf("logs = %q, want preview field", logOutput)
	}
	if !strings.Contains(logOutput, "hash=") {
		t.Fatalf("logs = %q, want hash field", logOutput)
	}
	if !strings.Contains(logOutput, "size=") {
		t.Fatalf("logs = %q, want size field", logOutput)
	}
	if strings.Contains(logOutput, strings.Repeat("x", 200)) {
		t.Fatal("logs should not contain the full payload")
	}
}
