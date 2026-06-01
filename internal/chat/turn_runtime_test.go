package chat

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/turn"
)

func TestTurnRuntimeShadowRecordsMockJournalWithoutEngine(t *testing.T) {
	_, engine := newTestHandler()
	journal := turn.NewMemoryJournal()
	runtime := newChatTurnRuntime(engine, config.TurnPipelineConfig{Shadow: true}, journal, slog.New(slog.NewTextHandler(io.Discard, nil)))
	env := wsMessageToInbound(WSMessage{Type: "message", Content: "hello", RequestID: "request-1"}, "session-test", "default")

	result, err := runtime.Shadow(context.Background(), env)
	if err != nil {
		t.Fatalf("Shadow: %v", err)
	}
	if result.Status != "done_mock" {
		t.Fatalf("status = %q, want done_mock", result.Status)
	}
	if engine.sendContent != "" {
		t.Fatalf("engine sendContent = %q, want no engine call", engine.sendContent)
	}
	snapshot, ok := journal.GetTurn(result.TurnID)
	if !ok {
		t.Fatalf("journal missing turn %q", result.TurnID)
	}
	var eventTypes []string
	for _, event := range snapshot.Events {
		eventTypes = append(eventTypes, event.Type)
	}
	want := []string{"turn_started", "normalized", "done_mock"}
	if len(eventTypes) != len(want) {
		t.Fatalf("events = %#v, want %#v", eventTypes, want)
	}
	for i := range want {
		if eventTypes[i] != want[i] {
			t.Fatalf("events[%d] = %q, want %q (all=%#v)", i, eventTypes[i], want[i], eventTypes)
		}
	}
}

func TestJournalingSinkRecordsSafeWorkToolSummary(t *testing.T) {
	journal := turn.NewMemoryJournal()
	turnID := "turn-1"
	if err := journal.StartTurn(context.Background(), turn.TurnRecord{TurnID: turnID, State: turn.StateCreated}); err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	sink := newJournalingSink(turn.SinkFunc(func(context.Context, turn.OutboundEvent) error { return nil }), journal, turnID)

	err := sink.Emit(context.Background(), turn.OutboundEvent{
		Type: turn.EventToolCallEnd,
		Tool: &turn.ToolActivity{
			Name:    "delegate_to_work",
			Status:  "success",
			Hash:    "sha256:abc",
			Preview: `{"status":"completed","task_id":"task-1","summary":"done","raw_tool_output":"SECRET=value"}`,
		},
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	snapshot, ok := journal.GetTurn(turnID)
	if !ok || len(snapshot.Events) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	payload := snapshot.Events[0].Payload
	if payload["task_id"] != "task-1" || payload["status"] != "completed" || payload["summary"] != "done" {
		t.Fatalf("payload = %#v, want safe work summary", payload)
	}
	if _, ok := payload["raw_tool_output"]; ok {
		t.Fatalf("payload leaks raw_tool_output: %#v", payload)
	}
}
