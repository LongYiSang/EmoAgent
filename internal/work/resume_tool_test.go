package work

import (
	"context"
	"encoding/json"
	"strings"
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
	pending := newSQLitePendingRegistry(t)
	paused := makePausedForResume(t, "task-1")
	if err := pending.Put("session-1", paused.TaskID, paused); err != nil {
		t.Fatalf("Put: %v", err)
	}

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
	got := pending.ClaimForResume("session-1", "task-1")
	if got.PausedWork != nil || got.FinalState != "resolved" {
		t.Fatalf("claim after completion = %#v, want final_state=resolved", got)
	}
}

func TestResumeTool_SchemaRejectsRemovedStyleDelta(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{})
	pending := newSQLitePendingRegistry(t)

	spec, _ := NewResumeTool(runtime, pending, t.TempDir(), testLogger())
	input := json.RawMessage(`{"task_id":"task-1","decision":"keep","style_delta":"concise"}`)

	if err := (tool.MinimalSchemaValidator{}).Validate(spec.Parameters, input); err == nil {
		t.Fatal("Validate should reject removed style_delta field")
	}
}

func TestResumeTool_TaskNotFoundReturnsExpired(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{})
	pending := newSQLitePendingRegistry(t)
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
	if envelope["final_state"] != "missing" {
		t.Fatalf("final_state = %q, want missing", envelope["final_state"])
	}
}

func TestResumeTool_HandlerRejectsRemovedStyleDeltaField(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{})
	pending := newSQLitePendingRegistry(t)
	paused := makePausedForResume(t, "task-1")
	if err := pending.Put("session-1", paused.TaskID, paused); err != nil {
		t.Fatalf("Put: %v", err)
	}

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
	pending := newSQLitePendingRegistry(t)
	paused := makePausedForResume(t, "task-1")
	if err := pending.Put("session-1", paused.TaskID, paused); err != nil {
		t.Fatalf("Put: %v", err)
	}

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
	if got := pending.ListInjectable("session-1"); len(got) != 1 || got[0].TaskID != "task-1" {
		t.Fatalf("ListInjectable = %#v, want requeued task", got)
	}
}

func TestResumeTool_EmitsProgressEndOnReport(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			textResp(`{"status":"completed","summary":"done"}`),
		},
	}
	runtime := newTestRuntime(t, client)
	pending := newSQLitePendingRegistry(t)
	paused := makePausedForResume(t, "task-1")
	if err := pending.Put("session-1", paused.TaskID, paused); err != nil {
		t.Fatalf("Put: %v", err)
	}

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
	pending := newSQLitePendingRegistry(t)
	paused := makePausedForResume(t, "task-1")
	if err := pending.Put("session-1", paused.TaskID, paused); err != nil {
		t.Fatalf("Put: %v", err)
	}

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

func TestResumeTool_ExpiredRowReturnsFinalState(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{})
	pending := newSQLitePendingRegistryWithTTLs(t, 5*time.Millisecond, 10*time.Millisecond, time.Hour, 10*time.Millisecond)
	paused := makePausedForResume(t, "task-1")
	if err := pending.Put("session-1", paused.TaskID, paused); err != nil {
		t.Fatalf("Put: %v", err)
	}
	time.Sleep(15 * time.Millisecond)
	_ = pending.ExpireOnce()

	_, handler := NewResumeTool(runtime, pending, t.TempDir(), testLogger())
	raw, err := handler(WithSessionID(context.Background(), "session-1"), json.RawMessage(`{"task_id":"task-1","decision":"keep","reason":"best"}`))
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var envelope map[string]string
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if envelope["status"] != "expired" || envelope["final_state"] != "expired_open" {
		t.Fatalf("envelope = %#v, want expired/expired_open", envelope)
	}
}

func TestResumeTool_FailClosedRequiresApprovalRequestID(t *testing.T) {
	runtime := newTestRuntime(t, &scriptedLLM{})
	pending := newSQLitePendingRegistry(t)
	paused := makePausedForResume(t, "task-risk")
	paused.Packet.Category = protocol.CatHighRisk
	paused.Packet.RiskLevel = "high"
	paused.Packet.RecommendationReason = "destructive action needs explicit approval"
	if err := pending.Put("session-1", paused.TaskID, paused); err != nil {
		t.Fatalf("Put: %v", err)
	}

	_, handler := NewResumeTool(runtime, pending, t.TempDir(), testLogger())
	ctx := WithSessionID(context.Background(), "session-1")
	raw, err := handler(ctx, json.RawMessage(`{"task_id":"task-risk","decision":"ship","reason":"do it"}`))
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var envelope map[string]string
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if envelope["status"] != "awaiting_approval" {
		t.Fatalf("status = %q, want awaiting_approval", envelope["status"])
	}
}

func TestResumeTool_ConsumesApprovedRequestAndUsesSelectedOption(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			textResp(`{"status":"completed","summary":"done"}`),
		},
	}
	runtime := newTestRuntime(t, client)
	pending := newSQLitePendingRegistry(t)
	paused := makePausedForResume(t, "task-risk")
	paused.Brief.PermissionScope = "workspace-write"
	paused.Packet.Category = protocol.CatHighRisk
	paused.Packet.RiskLevel = "high"
	paused.Packet.RecommendedOption = "flat"
	paused.Packet.RecommendationReason = "destructive action needs explicit approval"
	if err := pending.Put("session-1", paused.TaskID, paused); err != nil {
		t.Fatalf("Put: %v", err)
	}

	list := pending.ListInjectable("session-1")
	if len(list) != 1 || list[0].Approval == nil || list[0].Approval.RequestID == "" {
		t.Fatalf("ListInjectable = %#v, want approval request id", list)
	}
	requestID := list[0].Approval.RequestID
	if _, err := pending.approvals.ApproveRequest("session-1", requestID, "flat", "web", ""); err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}

	_, handler := NewResumeTool(runtime, pending, t.TempDir(), testLogger())
	ctx := WithSessionID(context.Background(), "session-1")
	raw, err := handler(ctx, json.RawMessage(`{"task_id":"task-risk","decision":"wrong","reason":"ignored","approval_request_id":"`+requestID+`"}`))
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
	if len(client.calls) != 1 {
		t.Fatalf("runtime llm calls = %d, want 1", len(client.calls))
	}
	last := client.calls[0].Messages[len(client.calls[0].Messages)-1]
	if !strings.Contains(last.Content, `"decision":"flat"`) {
		t.Fatalf("resume payload = %q, want selected approval option", last.Content)
	}

	req, err := pending.approvals.GetRequest("session-1", requestID)
	if err != nil {
		t.Fatalf("GetRequest: %v", err)
	}
	if req == nil || req.Status != string(protocol.ApprovalStatusConsumed) {
		t.Fatalf("approval request = %#v, want consumed", req)
	}
}
