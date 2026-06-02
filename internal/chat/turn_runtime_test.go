package chat

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/protocol"
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

func TestShouldUseTurnPipelineHonorsAllowDenyAndStablePercent(t *testing.T) {
	cfg := config.TurnPipelineConfig{
		Enabled:        false,
		RolloutPercent: 0,
		AllowPersonas:  []string{"default"},
		AllowSessions:  []string{"session-allow"},
		DenySessions:   []string{"session-deny"},
	}
	if !shouldUseTurnPipeline(cfg, "default", "session-1") {
		t.Fatal("allow_personas should enable pipeline even when global enabled=false")
	}
	if !shouldUseTurnPipeline(cfg, "other", "session-allow") {
		t.Fatal("allow_sessions should enable pipeline even when global enabled=false")
	}
	if shouldUseTurnPipeline(cfg, "default", "session-deny") {
		t.Fatal("deny_sessions should override allowlists")
	}
	if shouldUseTurnPipeline(config.TurnPipelineConfig{Enabled: true, RolloutPercent: 0}, "default", "session-2") {
		t.Fatal("enabled with rollout_percent=0 and no allowlist should stay on old path")
	}

	percentCfg := config.TurnPipelineConfig{Enabled: true, RolloutPercent: 50}
	first := shouldUseTurnPipeline(percentCfg, "default", "session-stable")
	for i := 0; i < 20; i++ {
		if got := shouldUseTurnPipeline(percentCfg, "default", "session-stable"); got != first {
			t.Fatalf("percent rollout not stable: first=%v got=%v", first, got)
		}
	}
	if !shouldUseTurnPipeline(config.TurnPipelineConfig{Enabled: true, RolloutPercent: 100}, "default", "session-any") {
		t.Fatal("rollout_percent=100 should enable pipeline")
	}
}

func TestTurnRuntimeStagesHonorMemoryStageConfig(t *testing.T) {
	_, engine := newTestHandler()
	env := wsMessageToInbound(WSMessage{Type: "message", Content: "hello", RequestID: "request-1"}, "session-test", "default")

	wrapped := newChatTurnRuntime(engine, config.TurnPipelineConfig{Enabled: true, MemoryStages: false}, turn.NewMemoryJournal(), discardLogger())
	if got := stageNames(wrapped.stages(env, &config.Persona{Name: "default"})); !sameStageNames(got, []turn.StageName{turn.StageNormalize, turn.StageEmotionLoop, turn.StageApprovalWait}) {
		t.Fatalf("wrapped stages = %#v", got)
	}

	staged := newChatTurnRuntime(engine, config.TurnPipelineConfig{Enabled: true, MemoryStages: true}, turn.NewMemoryJournal(), discardLogger())
	if got := stageNames(staged.stages(env, &config.Persona{Name: "default"})); !sameStageNames(got, []turn.StageName{
		turn.StageNormalize,
		turn.StageMemoryPrepare,
		turn.StageEmotionPrepare,
		turn.StageEmotionLoop,
		turn.StageMemoryCommit,
		turn.StageApprovalWait,
	}) {
		t.Fatalf("memory stages = %#v", got)
	}
}

func TestTurnRuntimeDuplicateRunningReturnsBusyWithoutSecondExecution(t *testing.T) {
	_, engine := newTestHandler()
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	engine.sendHook = func(context.Context) {
		once.Do(func() { close(started) })
		<-release
	}
	engine.sendReply = "done"

	runtime := newChatTurnRuntime(engine, config.TurnPipelineConfig{Enabled: true}, turn.NewMemoryJournal(), discardLogger())
	env := wsMessageToInbound(WSMessage{Type: "message", Content: "hello", RequestID: "request-1"}, "session-test", "default")
	persona := &config.Persona{Name: "default"}

	firstDone := make(chan error, 1)
	go func() {
		_, err := runtime.Execute(context.Background(), env, persona, turn.SinkFunc(func(context.Context, turn.OutboundEvent) error { return nil }))
		firstDone <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first execution did not start")
	}

	var duplicateEvents []turn.OutboundEvent
	result, err := runtime.Execute(context.Background(), env, persona, turn.SinkFunc(func(ctx context.Context, event turn.OutboundEvent) error {
		duplicateEvents = append(duplicateEvents, event)
		return nil
	}))
	if err != nil {
		t.Fatalf("duplicate Execute: %v", err)
	}
	if result.Status != "busy" {
		t.Fatalf("duplicate status = %q, want busy", result.Status)
	}
	if len(duplicateEvents) != 1 || duplicateEvents[0].Type != turn.EventTurnStatus || duplicateEvents[0].Payload["status"] != "busy" {
		t.Fatalf("duplicate events = %#v, want busy turn_status", duplicateEvents)
	}
	if engine.sendCount != 1 {
		t.Fatalf("sendCount = %d, want first execution only", engine.sendCount)
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Execute: %v", err)
	}
}

func TestTurnRuntimeDuplicateDoneReplaysSanitizedSummary(t *testing.T) {
	_, engine := newTestHandler()
	engine.sendReply = "answer"
	runtime := newChatTurnRuntime(engine, config.TurnPipelineConfig{Enabled: true}, turn.NewMemoryJournal(), discardLogger())
	env := wsMessageToInbound(WSMessage{Type: "message", Content: "hello SECRET=value", RequestID: "request-1"}, "session-test", "default")
	persona := &config.Persona{Name: "default"}

	if _, err := runtime.Execute(context.Background(), env, persona, turn.SinkFunc(func(context.Context, turn.OutboundEvent) error { return nil })); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	var replay []turn.OutboundEvent
	result, err := runtime.Execute(context.Background(), env, persona, turn.SinkFunc(func(ctx context.Context, event turn.OutboundEvent) error {
		replay = append(replay, event)
		return nil
	}))
	if err != nil {
		t.Fatalf("duplicate Execute: %v", err)
	}
	if result.Status != "done" {
		t.Fatalf("duplicate status = %q, want done", result.Status)
	}
	if engine.sendCount != 1 {
		t.Fatalf("sendCount = %d, want no duplicate engine call", engine.sendCount)
	}
	if len(replay) == 0 || replay[0].Type != turn.EventTurnStatus || replay[0].Payload["status"] != "done" {
		t.Fatalf("replay = %#v, want done turn_status", replay)
	}
	for _, event := range replay {
		if event.Payload["raw_tool_output"] != nil || event.Content == "hello SECRET=value" {
			t.Fatalf("replay leaks raw payload: %#v", replay)
		}
	}
}

func TestTurnRuntimeDuplicateApprovalWaitReplaysApprovalRequired(t *testing.T) {
	_, engine := newTestHandler()
	engine.sendErr = errApprovalPending
	engine.approvals = []protocol.ApprovalRequest{{
		ID:             "approval-1",
		SessionID:      "session-test",
		TaskID:         "task-1",
		Status:         string(protocol.ApprovalStatusPending),
		RejectOptionID: "cancel",
	}}
	runtime := newChatTurnRuntime(engine, config.TurnPipelineConfig{Enabled: true}, turn.NewMemoryJournal(), discardLogger())
	env := wsMessageToInbound(WSMessage{Type: "message", Content: "needs approval", RequestID: "request-1"}, "session-test", "default")
	persona := &config.Persona{Name: "default"}

	if _, err := runtime.Execute(context.Background(), env, persona, turn.SinkFunc(func(context.Context, turn.OutboundEvent) error { return nil })); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	var replay []turn.OutboundEvent
	result, err := runtime.Execute(context.Background(), env, persona, turn.SinkFunc(func(ctx context.Context, event turn.OutboundEvent) error {
		replay = append(replay, event)
		return nil
	}))
	if err != nil {
		t.Fatalf("duplicate Execute: %v", err)
	}
	if result.Status != "approval_wait" {
		t.Fatalf("duplicate status = %q, want approval_wait", result.Status)
	}
	if !hasEventType(replay, turn.EventApprovalRequired) {
		t.Fatalf("replay = %#v, want approval_required", replay)
	}
	if engine.sendCount != 1 {
		t.Fatalf("sendCount = %d, want no duplicate engine call", engine.sendCount)
	}
}

func TestTurnRuntimeCompletesIdempotencyOnFailure(t *testing.T) {
	_, engine := newTestHandler()
	engine.sendErr = errors.New("llm down")
	runtime := newChatTurnRuntime(engine, config.TurnPipelineConfig{Enabled: true}, turn.NewMemoryJournal(), discardLogger())
	env := wsMessageToInbound(WSMessage{Type: "message", Content: "hello", RequestID: "request-1"}, "session-test", "default")

	if _, err := runtime.Execute(context.Background(), env, &config.Persona{Name: "default"}, turn.SinkFunc(func(context.Context, turn.OutboundEvent) error { return nil })); err == nil {
		t.Fatal("first Execute error = nil, want failure")
	}
	var events []turn.OutboundEvent
	result, err := runtime.Execute(context.Background(), env, &config.Persona{Name: "default"}, turn.SinkFunc(func(ctx context.Context, event turn.OutboundEvent) error {
		events = append(events, event)
		return nil
	}))
	if err != nil {
		t.Fatalf("duplicate Execute should report previous failure without rerun: %v", err)
	}
	if result.Status != "previous_failed" || result.ErrorKind != "llm_failed" {
		t.Fatalf("duplicate result = %#v, want previous_failed/llm_failed", result)
	}
	if len(events) != 1 || events[0].Payload["error_kind"] != "llm_failed" {
		t.Fatalf("duplicate events = %#v, want error_kind llm_failed", events)
	}
	if engine.sendCount != 1 {
		t.Fatalf("sendCount = %d, want no duplicate rerun", engine.sendCount)
	}
}

func TestTurnRuntimeMarksOutboundFailedWhenSinkCloseFails(t *testing.T) {
	_, engine := newTestHandler()
	engine.sendReply = "answer"
	journal := turn.NewMemoryJournal()
	runtime := newChatTurnRuntime(engine, config.TurnPipelineConfig{Enabled: true}, journal, discardLogger())
	env := wsMessageToInbound(WSMessage{Type: "message", Content: "hello", RequestID: "request-1"}, "session-test", "default")
	closeErr := errors.New("flush failed")

	result, err := runtime.Execute(context.Background(), env, &config.Persona{Name: "default"}, closeFailingSink{err: closeErr})
	if !errors.Is(err, closeErr) {
		t.Fatalf("Execute error = %v, want close error", err)
	}
	if result.Status != "failed" || result.ErrorKind != "outbound_failed" {
		t.Fatalf("result = %#v, want failed/outbound_failed", result)
	}
	snapshot, ok := journal.GetTurn(result.TurnID)
	if !ok {
		t.Fatalf("journal missing turn %q", result.TurnID)
	}
	if snapshot.Status != "failed" || snapshot.ErrorKind != "outbound_failed" {
		t.Fatalf("snapshot status/error = %q/%q, want failed/outbound_failed", snapshot.Status, snapshot.ErrorKind)
	}
}

type closeFailingSink struct {
	err error
}

func (s closeFailingSink) Emit(context.Context, turn.OutboundEvent) error {
	return nil
}

func (s closeFailingSink) Close(context.Context) error {
	return s.err
}

func stageNames(stages []turn.Stage) []turn.StageName {
	names := make([]turn.StageName, 0, len(stages))
	for _, stage := range stages {
		names = append(names, stage.Name())
	}
	return names
}

func sameStageNames(got, want []turn.StageName) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func hasEventType(events []turn.OutboundEvent, eventType string) bool {
	for _, event := range events {
		if event.Type == eventType {
			return true
		}
	}
	return false
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
