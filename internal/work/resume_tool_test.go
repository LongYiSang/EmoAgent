package work

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/progress"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/tool"
)

func makePausedForResume(t *testing.T, taskID string) *PausedWork {
	t.Helper()
	brief := protocol.TaskBrief{
		TaskID:          taskID,
		Goal:            "complete task",
		PermissionScope: "read-only",
	}
	if err := ValidateAndComplete(&brief); err != nil {
		t.Fatalf("ValidateAndComplete: %v", err)
	}
	return &PausedWork{
		TaskID:          taskID,
		Brief:           brief,
		Messages:        []llm.Message{},
		PendingCallID:   "call-pending",
		Packet:          validDecisionPacket(taskID),
		Round:           1,
		EscalationCount: 1,
	}
}

func TestResumeTool_HappyPathReturnsTaskReport(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			textResp(`{"status":"completed","summary":"done"}`),
		},
	}
	runtime := newTestRuntime(t, client)
	pending := NewPendingRegistry(5 * time.Minute)
	paused := makePausedForResume(t, "task-1")
	pending.Put("session-1", paused.TaskID, paused)

	_, handler := NewResumeTool(runtime, pending, t.TempDir(), testLogger())
	ctx := WithSessionID(context.Background(), "session-1")
	raw, err := handler(ctx, json.RawMessage(`{"task_id":"task-1","decision":"keep","reason":"best"}`))
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var report protocol.TaskReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if report.Status != "completed" {
		t.Fatalf("status = %q, want completed", report.Status)
	}
	if got := pending.Take("session-1", "task-1"); got != nil {
		t.Fatalf("pending should be empty after completion, got %#v", got)
	}
}

func TestResumeTool_SchemaRejectsRemovedStyleDelta(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{})
	pending := NewPendingRegistry(5 * time.Minute)

	spec, _ := NewResumeTool(runtime, pending, t.TempDir(), testLogger())
	input := json.RawMessage(`{"task_id":"task-1","decision":"keep","style_delta":"concise"}`)

	if err := (tool.MinimalSchemaValidator{}).Validate(spec.Parameters, input); err == nil {
		t.Fatal("Validate should reject removed style_delta field")
	}
}

func TestResumeTool_TaskNotFoundReturnsExpired(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{})
	pending := NewPendingRegistry(5 * time.Minute)
	_, handler := NewResumeTool(runtime, pending, t.TempDir(), testLogger())

	raw, err := handler(WithSessionID(context.Background(), "session-1"), json.RawMessage(`{"task_id":"missing","decision":"x"}`))
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var envelope map[string]string
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if envelope["status"] != "expired" {
		t.Fatalf("status = %q, want expired", envelope["status"])
	}
}

func TestResumeTool_HandlerRejectsRemovedStyleDeltaField(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{})
	pending := NewPendingRegistry(5 * time.Minute)
	paused := makePausedForResume(t, "task-1")
	pending.Put("session-1", paused.TaskID, paused)

	_, handler := NewResumeTool(runtime, pending, t.TempDir(), testLogger())
	ctx := WithSessionID(context.Background(), "session-1")
	if _, err := handler(ctx, json.RawMessage(`{"task_id":"task-1","decision":"keep","style_delta":"concise"}`)); err == nil {
		t.Fatal("handler should reject removed style_delta field")
	}
}

func TestResumeTool_RequeuesWhenRuntimePausesAgain(t *testing.T) {
	packetJSON := `{
		"task_id":"task-1",
		"category":"execution_only",
		"risk_level":"low",
		"goal_summary":"need a technical decision",
		"question":"pick one",
		"why_blocked":"blocked",
		"options":[{"id":"a","summary":"A"},{"id":"b","summary":"B"}],
		"suggests_user_input":false
	}`
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("call-2", "request_decision", packetJSON),
		},
	}
	runtime := newTestRuntime(t, client)
	pending := NewPendingRegistry(5 * time.Minute)
	paused := makePausedForResume(t, "task-1")
	pending.Put("session-1", paused.TaskID, paused)

	_, handler := NewResumeTool(runtime, pending, t.TempDir(), testLogger())
	ctx := WithSessionID(context.Background(), "session-1")
	raw, err := handler(ctx, json.RawMessage(`{"task_id":"task-1","decision":"keep","reason":"best"}`))
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var envelope NeedsEmotionDecision
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if envelope.Status != "needs_emotion_decision" {
		t.Fatalf("status = %q, want needs_emotion_decision", envelope.Status)
	}
	if got := pending.Take("session-1", "task-1"); got == nil {
		t.Fatal("paused task should be requeued")
	}
}

func TestResumeTool_EmitsProgressEndOnReport(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			textResp(`{"status":"completed","summary":"done"}`),
		},
	}
	runtime := newTestRuntime(t, client)
	pending := NewPendingRegistry(5 * time.Minute)
	paused := makePausedForResume(t, "task-1")
	pending.Put("session-1", paused.TaskID, paused)

	_, handler := NewResumeTool(runtime, pending, t.TempDir(), testLogger())
	var events []progress.Event
	ctx := progress.WithCallback(WithSessionID(context.Background(), "session-1"), func(event progress.Event) {
		events = append(events, event)
	})

	_, err := handler(ctx, json.RawMessage(`{"task_id":"task-1","decision":"keep","reason":"best"}`))
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !hasProgressKind(events, progress.KindEnd) {
		t.Fatalf("events = %#v, want end event", events)
	}
}

func TestResumeTool_EmitsProgressPausedWhenPausesAgain(t *testing.T) {
	packetJSON := `{
		"task_id":"task-1",
		"category":"execution_only",
		"risk_level":"low",
		"goal_summary":"need a technical decision",
		"question":"pick one",
		"why_blocked":"blocked",
		"options":[{"id":"a","summary":"A"},{"id":"b","summary":"B"}],
		"suggests_user_input":false
	}`
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("call-2", "request_decision", packetJSON),
		},
	}
	runtime := newTestRuntime(t, client)
	pending := NewPendingRegistry(5 * time.Minute)
	paused := makePausedForResume(t, "task-1")
	pending.Put("session-1", paused.TaskID, paused)

	_, handler := NewResumeTool(runtime, pending, t.TempDir(), testLogger())
	var events []progress.Event
	ctx := progress.WithCallback(WithSessionID(context.Background(), "session-1"), func(event progress.Event) {
		events = append(events, event)
	})

	_, err := handler(ctx, json.RawMessage(`{"task_id":"task-1","decision":"keep","reason":"best"}`))
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !hasProgressKind(events, progress.KindPaused) {
		t.Fatalf("events = %#v, want paused event", events)
	}
}
