package turn

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMemoryJournalRecordsStateTransitionsAndMetrics(t *testing.T) {
	journal := NewMemoryJournal()
	turnID := "turn-1"

	if err := journal.StartTurn(context.Background(), TurnRecord{
		TurnID:         turnID,
		IdempotencyKey: "key-1",
		Kind:           InboundUserMessage,
		SessionID:      "session-1",
		PersonaKey:     "default",
		State:          StateCreated,
		StartedAt:      time.Unix(1, 0),
	}); err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	if err := journal.RecordTransition(context.Background(), turnID, StateCreated, StateNormalizing, StageMetrics{
		Stage:      StageNormalize,
		DurationMS: 12,
	}); err != nil {
		t.Fatalf("RecordTransition: %v", err)
	}
	if err := journal.RecordEvent(context.Background(), turnID, JournalEvent{
		Stage: StageEmotionLoop,
		Type:  "work_summary",
		Payload: map[string]any{
			"task_id":             "task-1",
			"approval_request_id": "approval-1",
			"summary":             "needs approval",
		},
	}); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if err := journal.CompleteTurn(context.Background(), turnID, "approval_wait", ""); err != nil {
		t.Fatalf("CompleteTurn: %v", err)
	}

	snapshot, ok := journal.GetTurn(turnID)
	if !ok {
		t.Fatalf("turn %q not found", turnID)
	}
	if snapshot.Status != "approval_wait" {
		t.Fatalf("status = %q, want approval_wait", snapshot.Status)
	}
	if len(snapshot.Transitions) != 1 || snapshot.Transitions[0].DurationMS != 12 {
		t.Fatalf("transitions = %#v, want duration 12", snapshot.Transitions)
	}
	if len(snapshot.Events) != 1 {
		t.Fatalf("events = %#v, want one event", snapshot.Events)
	}
	payload := snapshot.Events[0].Payload
	if payload["task_id"] != "task-1" || payload["approval_request_id"] != "approval-1" {
		t.Fatalf("payload = %#v, want task and approval refs", payload)
	}
}

func TestMemoryJournalSanitizesForbiddenPayload(t *testing.T) {
	journal := NewMemoryJournal()
	turnID := "turn-1"
	if err := journal.StartTurn(context.Background(), TurnRecord{TurnID: turnID, State: StateCreated}); err != nil {
		t.Fatalf("StartTurn: %v", err)
	}

	err := journal.RecordEvent(context.Background(), turnID, JournalEvent{
		Stage: StageEmotionLoop,
		Type:  "tool_call_end",
		Payload: map[string]any{
			"tool":            "write_file",
			"summary":         "wrote file",
			"raw_tool_output": "SECRET=value",
			"prompt":          "hidden prompt",
			"hidden_memory":   "forgotten fact",
			"content":         "full file content",
		},
	})
	if err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	snapshot, ok := journal.GetTurn(turnID)
	if !ok || len(snapshot.Events) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	got := snapshot.Events[0].Payload
	for _, forbidden := range []string{"raw_tool_output", "prompt", "hidden_memory", "content"} {
		if _, exists := got[forbidden]; exists {
			t.Fatalf("payload contains forbidden key %q: %#v", forbidden, got)
		}
	}
	text := strings.TrimSpace(toDebugString(got))
	if strings.Contains(text, "SECRET=value") || strings.Contains(text, "forgotten fact") {
		t.Fatalf("payload leaks forbidden value: %s", text)
	}
	if got["tool"] != "write_file" || got["summary"] != "wrote file" {
		t.Fatalf("payload = %#v, want safe summary retained", got)
	}
}
