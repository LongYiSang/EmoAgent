package chat

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/agentaffect"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
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

func TestTurnRuntimeAsyncAgentAffectReadsMoodAndEnqueuesAfterOutput(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		response: &llm.ChatResponse{
			ID:         "resp-1",
			Content:    "answer",
			StopReason: "end_turn",
		},
	}
	engine, _, _ := newTestEngine(t, fakeLLM)
	bridge := &fakeMemoryBridge{
		ensureResult:  MemorySegmentRef{SegmentID: "segment-current", MemorySessionID: "memory-current"},
		retrieveBlock: "[Memory]\nRelevant memory.",
	}
	engine.memory = bridge
	engine.memoryRetrieval = config.MemoryRetrievalConfig{Enabled: true, InjectPrompt: true, FailOpen: true}
	affect := &fakeAgentAffectRuntime{
		mode:        "async_after_reply",
		currentMood: agentaffect.MoodSnapshot{StateID: "state-1", PersonaID: "default", PromptMoodText: "平稳、温和。"},
		promptBlock: "[Agent Mood]\n当前模拟心情：平稳、温和。",
	}
	engine.agentAffect = affect

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	bridge.ensureCalls = nil
	env := wsMessageToInbound(WSMessage{Type: "message", Content: "hello", RequestID: "request-1"}, sessionID, "default")
	runtime := newChatTurnRuntime(engine, config.TurnPipelineConfig{Enabled: true, MemoryStages: true, RolloutPercent: 100}, turn.NewMemoryJournal(), discardLogger())

	result, err := runtime.Execute(context.Background(), env, &config.Persona{Name: "default", SystemPrompt: "system"}, turn.SinkFunc(func(context.Context, turn.OutboundEvent) error {
		return nil
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != "done" {
		t.Fatalf("status = %q, want done", result.Status)
	}
	if !strings.Contains(fakeLLM.lastRequest.System, "[Memory]\nRelevant memory.") {
		t.Fatalf("system missing memory block:\n%s", fakeLLM.lastRequest.System)
	}
	if !strings.Contains(fakeLLM.lastRequest.System, "[Agent Mood]") {
		t.Fatalf("system missing agent affect block:\n%s", fakeLLM.lastRequest.System)
	}
	if affect.submitCalls != 0 {
		t.Fatalf("SubmitMoodImpact calls = %d, want 0 in async mode", affect.submitCalls)
	}
	if affect.currentReq.PersonaID != "default" || affect.currentReq.SessionID != sessionID {
		t.Fatalf("current mood req = %#v", affect.currentReq)
	}
	if affect.enqueueCalls != 1 {
		t.Fatalf("enqueue calls = %d, want 1", affect.enqueueCalls)
	}
	if affect.enqueueReq.UserText != "hello" || affect.enqueueReq.AssistantText != "answer" {
		t.Fatalf("enqueue req user/assistant = %#v", affect.enqueueReq)
	}
	if affect.enqueueReq.MemoryPromptBlock != "[Memory]\nRelevant memory." {
		t.Fatalf("enqueue memory prompt block = %q", affect.enqueueReq.MemoryPromptBlock)
	}
	if affect.enqueueReq.Trigger.SourceRefID != "episode-user" || affect.enqueueReq.BaseStateID != "state-1" {
		t.Fatalf("enqueue trigger/base state = %#v", affect.enqueueReq)
	}
}

func TestTurnRuntimeAsyncAgentAffectDoesNotEnqueueWithoutAssistantOutput(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		response: &llm.ChatResponse{
			ID:         "resp-1",
			Content:    "",
			StopReason: "end_turn",
		},
	}
	engine, _, _ := newTestEngine(t, fakeLLM)
	engine.memory = &fakeMemoryBridge{ensureResult: MemorySegmentRef{SegmentID: "segment-current", MemorySessionID: "memory-current"}}
	engine.agentAffect = &fakeAgentAffectRuntime{
		mode:        "async_after_reply",
		currentMood: agentaffect.MoodSnapshot{StateID: "state-1", PersonaID: "default", PromptMoodText: "平稳。"},
		promptBlock: "[Agent Mood]\n当前模拟心情：平稳。",
	}

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	env := wsMessageToInbound(WSMessage{Type: "message", Content: "hello", RequestID: "request-1"}, sessionID, "default")
	runtime := newChatTurnRuntime(engine, config.TurnPipelineConfig{Enabled: true, MemoryStages: true, RolloutPercent: 100}, turn.NewMemoryJournal(), discardLogger())

	result, err := runtime.Execute(context.Background(), env, &config.Persona{Name: "default", SystemPrompt: "system"}, turn.SinkFunc(func(context.Context, turn.OutboundEvent) error {
		return nil
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != "done" {
		t.Fatalf("status = %q, want done", result.Status)
	}
	affect := engine.agentAffect.(*fakeAgentAffectRuntime)
	if affect.enqueueCalls != 0 {
		t.Fatalf("enqueue calls = %d, want 0 without assistant output", affect.enqueueCalls)
	}
}

func TestTurnRuntimeSyncBeforeReplyStillSubmitsMoodImpact(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		response: &llm.ChatResponse{
			ID:         "resp-1",
			Content:    "answer",
			StopReason: "end_turn",
		},
	}
	engine, _, _ := newTestEngine(t, fakeLLM)
	bridge := &fakeMemoryBridge{
		ensureResult:  MemorySegmentRef{SegmentID: "segment-current", MemorySessionID: "memory-current"},
		retrieveBlock: "[Memory]\nRelevant memory.",
	}
	engine.memory = bridge
	engine.memoryRetrieval = config.MemoryRetrievalConfig{Enabled: true, InjectPrompt: true, FailOpen: true}
	affect := &fakeAgentAffectRuntime{mode: "sync_before_reply", promptBlock: "[Agent Affect Runtime State]\nmood_vector:\n  valence: 0.100"}
	engine.agentAffect = affect

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	env := wsMessageToInbound(WSMessage{Type: "message", Content: "hello", RequestID: "request-1"}, sessionID, "default")
	runtime := newChatTurnRuntime(engine, config.TurnPipelineConfig{Enabled: true, MemoryStages: true, RolloutPercent: 100}, turn.NewMemoryJournal(), discardLogger())

	result, err := runtime.Execute(context.Background(), env, &config.Persona{Name: "default", SystemPrompt: "system"}, turn.SinkFunc(func(context.Context, turn.OutboundEvent) error {
		return nil
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != "done" {
		t.Fatalf("status = %q, want done", result.Status)
	}
	if affect.submitCalls != 1 {
		t.Fatalf("SubmitMoodImpact calls = %d, want 1 in sync mode", affect.submitCalls)
	}
	if affect.enqueueCalls != 0 {
		t.Fatalf("enqueue calls = %d, want 0 in sync mode", affect.enqueueCalls)
	}
	if affect.submitReq.MemoryPromptBlock != "[Memory]\nRelevant memory." {
		t.Fatalf("affect memory prompt block = %q", affect.submitReq.MemoryPromptBlock)
	}
}

func TestTurnRuntimeAgentAffectFailureDoesNotBlockChat(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		response: &llm.ChatResponse{
			ID:         "resp-1",
			Content:    "answer",
			StopReason: "end_turn",
		},
	}
	engine, _, _ := newTestEngine(t, fakeLLM)
	bridge := &fakeMemoryBridge{
		ensureResult:  MemorySegmentRef{SegmentID: "segment-current", MemorySessionID: "memory-current"},
		retrieveBlock: "[Memory]\nRelevant memory.",
	}
	engine.memory = bridge
	engine.memoryRetrieval = config.MemoryRetrievalConfig{Enabled: true, InjectPrompt: true, FailOpen: true}
	engine.agentAffect = &fakeAgentAffectRuntime{mode: "async_after_reply", currentErr: errors.New("affect unavailable")}

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	env := wsMessageToInbound(WSMessage{Type: "message", Content: "hello", RequestID: "request-1"}, sessionID, "default")
	runtime := newChatTurnRuntime(engine, config.TurnPipelineConfig{Enabled: true, MemoryStages: true, RolloutPercent: 100}, turn.NewMemoryJournal(), discardLogger())

	result, err := runtime.Execute(context.Background(), env, &config.Persona{Name: "default", SystemPrompt: "system"}, turn.SinkFunc(func(context.Context, turn.OutboundEvent) error {
		return nil
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != "done" {
		t.Fatalf("status = %q, want done", result.Status)
	}
	if strings.Contains(fakeLLM.lastRequest.System, "[Agent Affect Runtime State]") {
		t.Fatalf("system should not include affect block on affect failure:\n%s", fakeLLM.lastRequest.System)
	}
}

func TestTurnRuntimeUsesPluginHostStageAndOutboundWrappers(t *testing.T) {
	pluginHost := &fakePluginHost{enabled: true}
	handler, engine := newTestHandlerWithOptions(
		WithTurnPipelineConfig(config.TurnPipelineConfig{Enabled: true, RolloutPercent: 100}),
		WithPluginHost(pluginHost),
	)
	engine.sendReply = "answer"
	env := wsMessageToInbound(WSMessage{Type: "message", Content: "hello", RequestID: "request-1"}, "session-test", "default")
	persona := &config.Persona{Name: "default"}

	var events []turn.OutboundEvent
	result, err := handler.turnRuntime.Execute(context.Background(), env, persona, turn.SinkFunc(func(ctx context.Context, event turn.OutboundEvent) error {
		events = append(events, event)
		return nil
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != "done" {
		t.Fatalf("status = %q, want done", result.Status)
	}
	if pluginHost.wrapStagesCount != 1 {
		t.Fatalf("wrapStagesCount = %d, want 1", pluginHost.wrapStagesCount)
	}
	if pluginHost.wrapSinkCount != 1 {
		t.Fatalf("wrapSinkCount = %d, want 1", pluginHost.wrapSinkCount)
	}
	if pluginHost.outboundCount == 0 || len(events) == 0 {
		t.Fatalf("outbound wrapper count/events = %d/%#v, want outbound routed through plugin host", pluginHost.outboundCount, events)
	}
	if pluginHost.turnEndCount != 1 {
		t.Fatalf("turnEndCount = %d, want 1", pluginHost.turnEndCount)
	}
}

func TestTurnRuntimeDuplicateReplayDoesNotRunPluginWrappers(t *testing.T) {
	pluginHost := &fakePluginHost{enabled: true}
	handler, engine := newTestHandlerWithOptions(
		WithTurnPipelineConfig(config.TurnPipelineConfig{Enabled: true, RolloutPercent: 100}),
		WithPluginHost(pluginHost),
	)
	engine.sendReply = "answer"
	env := wsMessageToInbound(WSMessage{Type: "message", Content: "hello", RequestID: "request-1"}, "session-test", "default")
	persona := &config.Persona{Name: "default"}

	if _, err := handler.turnRuntime.Execute(context.Background(), env, persona, turn.SinkFunc(func(context.Context, turn.OutboundEvent) error { return nil })); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	firstStageWraps := pluginHost.wrapStagesCount
	firstOutbound := pluginHost.outboundCount

	if _, err := handler.turnRuntime.Execute(context.Background(), env, persona, turn.SinkFunc(func(context.Context, turn.OutboundEvent) error { return nil })); err != nil {
		t.Fatalf("duplicate Execute: %v", err)
	}
	if pluginHost.wrapStagesCount != firstStageWraps {
		t.Fatalf("duplicate wrapStagesCount = %d, want unchanged %d", pluginHost.wrapStagesCount, firstStageWraps)
	}
	if pluginHost.outboundCount != firstOutbound {
		t.Fatalf("duplicate outboundCount = %d, want unchanged %d", pluginHost.outboundCount, firstOutbound)
	}
	if engine.sendCount != 1 {
		t.Fatalf("sendCount = %d, want no duplicate engine call", engine.sendCount)
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

type fakeAgentAffectRuntime struct {
	mode         string
	currentReq   agentaffect.GetCurrentMoodRequest
	currentMood  agentaffect.MoodSnapshot
	currentErr   error
	submitReq    agentaffect.SubmitMoodImpactRequest
	submitCalls  int
	submitErr    error
	promptBlock  string
	enqueueReq   agentaffect.EnqueueTurnEvaluationJobRequest
	enqueueCalls int
}

func (f *fakeAgentAffectRuntime) UpdateMode() string {
	return f.mode
}

func (f *fakeAgentAffectRuntime) GetCurrentMood(_ context.Context, req agentaffect.GetCurrentMoodRequest) (agentaffect.GetCurrentMoodResponse, error) {
	f.currentReq = req
	if f.currentErr != nil {
		return agentaffect.GetCurrentMoodResponse{}, f.currentErr
	}
	mood := f.currentMood
	if mood.PersonaID == "" {
		mood.PersonaID = req.PersonaID
	}
	if mood.SessionID == "" {
		mood.SessionID = req.SessionID
	}
	return agentaffect.GetCurrentMoodResponse{Enabled: true, Mood: mood}, nil
}

func (f *fakeAgentAffectRuntime) SubmitMoodImpact(_ context.Context, req agentaffect.SubmitMoodImpactRequest) (agentaffect.SubmitMoodImpactResponse, error) {
	f.submitReq = req
	f.submitCalls++
	if f.submitErr != nil {
		return agentaffect.SubmitMoodImpactResponse{}, f.submitErr
	}
	return agentaffect.SubmitMoodImpactResponse{
		Mood: agentaffect.MoodSnapshot{PersonaID: req.PersonaID, SessionID: req.SessionID},
	}, nil
}

func (f *fakeAgentAffectRuntime) BuildPromptAffectBlock(context.Context, agentaffect.BuildPromptAffectBlockRequest) (string, error) {
	return f.promptBlock, nil
}

func (f *fakeAgentAffectRuntime) EnqueueTurnEvaluationJob(_ context.Context, req agentaffect.EnqueueTurnEvaluationJobRequest) (agentaffect.AffectJobRecord, error) {
	f.enqueueReq = req
	f.enqueueCalls++
	return agentaffect.AffectJobRecord{ID: "job-1"}, nil
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

type fakePluginHost struct {
	enabled         bool
	wrapStagesCount int
	wrapSinkCount   int
	outboundCount   int
	turnEndCount    int
	turnErrorCount  int
}

func (h *fakePluginHost) Enabled() bool {
	return h != nil && h.enabled
}

func (h *fakePluginHost) WrapStages(stages []turn.Stage) []turn.Stage {
	h.wrapStagesCount++
	wrapped := make([]turn.Stage, 0, len(stages))
	for _, stage := range stages {
		stage := stage
		wrapped = append(wrapped, turn.StageFunc{
			NameValue: stage.Name(),
			RunFunc: func(ctx context.Context, tc *turn.TurnContext) (turn.StageResult, error) {
				return stage.Run(ctx, tc)
			},
		})
	}
	return wrapped
}

func (h *fakePluginHost) WrapOutboundSink(next turn.OutboundSink) turn.OutboundSink {
	h.wrapSinkCount++
	return turn.SinkFunc(func(ctx context.Context, event turn.OutboundEvent) error {
		h.outboundCount++
		return next.Emit(ctx, event)
	})
}

func (h *fakePluginHost) DispatchTurnEnd(context.Context, turn.TurnResult, turn.InboundEnvelope) {
	h.turnEndCount++
}

func (h *fakePluginHost) DispatchTurnError(context.Context, turn.TurnResult, error, turn.InboundEnvelope) {
	h.turnErrorCount++
}
