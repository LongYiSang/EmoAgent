package work

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
		PendingToolCall: nil,
		Packet:          validDecisionPacket(taskID),
		Round:           1,
		EscalationCount: 1,
	}
}

func TestResumeBlobRoundTripKeepsPendingToolCall(t *testing.T) {
	paused := makePausedForResume(t, "task-blob")
	paused.PendingToolCall = &tool.Call{
		ID:    "bash-1",
		Name:  "bash",
		Input: json.RawMessage(`{"command":"rm -rf tmp"}`),
	}

	payload, err := json.Marshal(resumeBlobFromPaused(paused))
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	var blob ResumeBlob
	if err := json.Unmarshal(payload, &blob); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	restored := blob.PausedWork()
	if restored.PendingToolCall == nil {
		t.Fatal("PendingToolCall should survive resume blob round-trip")
	}
	if restored.PendingToolCall.Name != "bash" {
		t.Fatalf("PendingToolCall.Name = %q, want bash", restored.PendingToolCall.Name)
	}
	if string(restored.PendingToolCall.Input) != `{"command":"rm -rf tmp"}` {
		t.Fatalf("PendingToolCall.Input = %s", restored.PendingToolCall.Input)
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
		"category":"auto",
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

func TestResumeTool_PausedJournalUsesDerivedRiskLevel(t *testing.T) {
	packetJSON := `{
		"task_id":"task-1",
		"category":"auto",
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

	root := t.TempDir()
	_, handler := NewResumeTool(runtime, pending, root, testLogger())
	ctx := WithSessionID(context.Background(), "session-1")
	if _, err := handler(ctx, json.RawMessage(`{"task_id":"task-1","decision":"keep","reason":"best"}`)); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".jsonl") {
			found = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir returned error: %v", err)
	}
	if found == "" {
		t.Fatal("expected a journal file to be written")
	}

	data, err := os.ReadFile(found)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	var pausedLine string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"kind":"task_paused"`) {
			pausedLine = line
			break
		}
	}
	if pausedLine == "" {
		t.Fatalf("journal = %s, want task_paused line", data)
	}
	if !strings.Contains(pausedLine, `"risk":"low"`) {
		t.Fatalf("task_paused line = %s, want derived low risk", pausedLine)
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
		"category":"auto",
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
	paused.Packet.Category = protocol.CatToolApproval
	paused.Packet.Question = "Allow: rm -rf tmp"
	paused.Packet.Options = []protocol.DecisionOption{
		{ID: "allow", Summary: "Allow execution"},
		{ID: "deny", Summary: "Deny execution"},
	}
	paused.Packet.RecommendedOption = "allow"
	paused.Packet.RejectOptionID = "deny"
	paused.Packet.RecommendationReason = "destructive action needs explicit approval"
	paused.Packet.RelevantFindings = []protocol.DecisionEvidence{{Finding: "This action changes workspace files."}}
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

func TestResumeTool_HumanConfirmationCanResumeWithoutApprovalRequestID(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			textResp(`{"status":"completed","summary":"done"}`),
		},
	}
	runtime := newTestRuntime(t, client)
	pending := newSQLitePendingRegistry(t)
	paused := makePausedForResume(t, "task-human")
	paused.Packet.Category = protocol.CatHumanConfirmation
	paused.Packet.RecommendationReason = "This path needs explicit user confirmation."
	paused.Packet.RejectOptionID = "deny"
	paused.Packet.Options = []protocol.DecisionOption{
		{ID: "ship", Summary: "Proceed"},
		{ID: "deny", Summary: "Do not proceed"},
	}
	paused.Packet.RelevantFindings = []protocol.DecisionEvidence{{Finding: "This action changes workspace files."}}
	if err := pending.Put("session-1", paused.TaskID, paused); err != nil {
		t.Fatalf("Put: %v", err)
	}

	_, handler := NewResumeTool(runtime, pending, t.TempDir(), testLogger())
	ctx := WithSessionID(context.Background(), "session-1")
	raw, err := handler(ctx, json.RawMessage(`{"task_id":"task-human","decision":"ship","reason":"user confirmed in chat"}`))
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
}

func TestResumeTool_PermissionEscalationApproveReexecutesPendingToolCall(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			textResp(`{"status":"completed","summary":"done"}`),
		},
	}
	var executed []string
	runtime := newApprovalTestRuntime(t, client, &executed)
	pending := newSQLitePendingRegistry(t)
	paused := makePausedForResume(t, "task-escalation")
	paused.Brief.PermissionScope = "workspace-write"
	paused.PendingToolCall = &tool.Call{
		ID:    "bash-1",
		Name:  "bash",
		Input: json.RawMessage(`{"command":"rm -rf tmp"}`),
	}
	paused.Packet.Category = protocol.CatPermissionEscalationRequired
	paused.Packet.Question = "Ask the user whether to approve destructive permission for: rm -rf tmp"
	paused.Packet.Options = []protocol.DecisionOption{
		{ID: "approve", Summary: "User approves destructive permission"},
		{ID: "reject", Summary: "User rejects destructive permission"},
	}
	paused.Packet.RecommendedOption = ""
	paused.Packet.RejectOptionID = "reject"
	paused.Packet.SuggestsUserInput = true
	if err := pending.Put("session-1", paused.TaskID, paused); err != nil {
		t.Fatalf("Put: %v", err)
	}

	_, handler := NewResumeTool(runtime, pending, t.TempDir(), testLogger())
	ctx := WithSessionID(context.Background(), "session-1")
	raw, err := handler(ctx, json.RawMessage(`{"task_id":"task-escalation","decision":"approve","reason":"user approved","permission_scope_override":"approved-destructive"}`))
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
	if len(executed) != 1 || executed[0] != "rm -rf tmp" {
		t.Fatalf("executed = %#v, want resumed destructive command", executed)
	}
}

func TestResumeTool_LegacyHumanConfirmationApprovalRequestIDDoesNotBlockResume(t *testing.T) {
	client := &scriptedLLM{
		responses: []*llm.ChatResponse{
			textResp(`{"status":"completed","summary":"done"}`),
		},
	}
	runtime := newTestRuntime(t, client)
	pending := newSQLitePendingRegistry(t)
	paused := makePausedForResume(t, "task-human-legacy")
	paused.Packet.Category = protocol.CatHumanConfirmation
	paused.Packet.RecommendationReason = "This path needs explicit user confirmation."
	paused.Packet.RejectOptionID = "deny"
	paused.Packet.Options = []protocol.DecisionOption{
		{ID: "ship", Summary: "Proceed"},
		{ID: "deny", Summary: "Do not proceed"},
	}
	paused.Packet.RelevantFindings = []protocol.DecisionEvidence{{Finding: "This action changes workspace files."}}
	if err := pending.Put("session-1", paused.TaskID, paused); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := pending.db.Exec(`
		UPDATE pending_decisions
		SET approval_request_id = ?
		WHERE session_id = ? AND task_id = ?
	`, "legacy-approval-id", "session-1", paused.TaskID); err != nil {
		t.Fatalf("inject legacy approval_request_id: %v", err)
	}

	_, handler := NewResumeTool(runtime, pending, t.TempDir(), testLogger())
	ctx := WithSessionID(context.Background(), "session-1")
	raw, err := handler(ctx, json.RawMessage(`{"task_id":"task-human-legacy","decision":"ship","reason":"user confirmed in chat"}`))
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
	paused.Packet.Category = protocol.CatToolApproval
	paused.Packet.Question = "Allow: rm -rf tmp"
	paused.Packet.RecommendedOption = "flat"
	paused.Packet.Options = []protocol.DecisionOption{
		{ID: "flat", Summary: "Allow execution"},
		{ID: "deny", Summary: "Deny execution"},
	}
	paused.Packet.RejectOptionID = "deny"
	paused.Packet.RecommendationReason = "destructive action needs explicit approval"
	paused.Packet.RelevantFindings = []protocol.DecisionEvidence{{Finding: "This action changes workspace files."}}
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
