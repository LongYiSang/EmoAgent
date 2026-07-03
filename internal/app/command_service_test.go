package app

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/chat"
	"github.com/longyisang/emoagent/internal/config"
	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/conversation"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/memoryhost"
	"github.com/longyisang/emoagent/internal/storage"
)

func TestCommandServiceSidRecordsResultWithoutMessage(t *testing.T) {
	ctx := context.Background()
	_, db, commands := newTestCommandService(t)
	origin := conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"}
	sessionID := createTestSession(t, db, "session-sid", "default")

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/sid",
		Origin:     origin,
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/sid was not handled")
	}
	requireCommandMessage(t, resp, "sid", "success")
	if !strings.Contains(resp.Messages[0].Content, "webui:local:main") || !strings.Contains(resp.Messages[0].Content, sessionID) {
		t.Fatalf("/sid content missing origin/session: %q", resp.Messages[0].Content)
	}
	assertNoMessages(t, db, sessionID)
	assertInvocation(t, db, "builtin.sid", "success")
	assertConversationEvent(t, db, sessionID, "command_result")
}

func TestCommandServiceBuiltinConfigDisableAndAlias(t *testing.T) {
	ctx := context.Background()
	_, db, commands := newTestCommandService(t)
	sessionID := createTestSession(t, db, "session-builtin-config", "default")
	if err := commands.UpdateCommandConfig(ctx, storage.CommandConfigRecord{
		CommandID:     "builtin.sid",
		EffectiveName: "session",
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpdateCommandConfig alias: %v", err)
	}

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/session",
		Origin:     conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"},
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle alias: %v", err)
	}
	if !handled {
		t.Fatal("/session was not handled")
	}
	requireCommandMessage(t, resp, "session", "success")

	if err := commands.UpdateCommandConfig(ctx, storage.CommandConfigRecord{
		CommandID: "builtin.sid",
		Enabled:   false,
	}); err != nil {
		t.Fatalf("UpdateCommandConfig disabled: %v", err)
	}
	resp, handled, err = commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/session",
		Origin:     conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"},
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle disabled: %v", err)
	}
	if !handled {
		t.Fatal("/session disabled was not handled")
	}
	requireCommandMessage(t, resp, "session", "failed")
	assertInvocation(t, db, "builtin.sid", "failed")
}

func TestCommandServicePermissionConfigDeniesMember(t *testing.T) {
	ctx := context.Background()
	_, db, commands := newTestCommandService(t)
	sessionID := createTestSession(t, db, "session-permission-config", "default")
	if err := commands.UpdateCommandConfig(ctx, storage.CommandConfigRecord{
		CommandID:  "builtin.sid",
		Enabled:    true,
		Permission: "admin",
	}); err != nil {
		t.Fatalf("UpdateCommandConfig permission: %v", err)
	}

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/sid",
		Origin:     conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"},
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/sid was not handled")
	}
	requireCommandMessage(t, resp, "sid", "failed")
	assertInvocation(t, db, "builtin.sid", "failed")
}

func TestCommandServiceNewCreatesSessionAndRebindsOrigin(t *testing.T) {
	ctx := context.Background()
	_, db, commands := newTestCommandService(t)
	origin := conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"}
	oldSessionID := createTestSession(t, db, "session-old", "default")
	if _, err := commands.conversation.Bindings().BindSession(ctx, origin, "default", oldSessionID, false); err != nil {
		t.Fatalf("BindSession: %v", err)
	}

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/new",
		Origin:     origin,
		SessionID:  oldSessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/new was not handled")
	}
	if resp.SessionID == "" || resp.SessionID == oldSessionID {
		t.Fatalf("new SessionID = %q, old = %q", resp.SessionID, oldSessionID)
	}
	binding, err := db.GetConversationBinding(ctx, origin.OriginKey, "default")
	if err != nil {
		t.Fatalf("GetConversationBinding: %v", err)
	}
	if binding == nil || binding.CurrentSessionID != resp.SessionID {
		t.Fatalf("binding current session = %#v, want %q", binding, resp.SessionID)
	}
	requireWSMessage(t, resp.Messages, "context_switched")
	requireCommandMessage(t, resp, "new", "success")
	assertNoMessages(t, db, oldSessionID)
	assertNoMessages(t, db, resp.SessionID)
	assertInvocation(t, db, "builtin.new", "success")
	assertConversationEvent(t, db, resp.SessionID, "context_switched")
	assertConversationEvent(t, db, resp.SessionID, "command_result")
}

func TestCommandServiceSwitchBindsExistingSession(t *testing.T) {
	ctx := context.Background()
	_, db, commands := newTestCommandService(t)
	origin := conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"}
	currentSessionID := createTestSession(t, db, "session-current", "default")
	targetSessionID := createTestSession(t, db, "session-target", "default")
	if _, err := commands.conversation.Bindings().BindSession(ctx, origin, "default", currentSessionID, false); err != nil {
		t.Fatalf("BindSession: %v", err)
	}

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/switch " + targetSessionID,
		Origin:     origin,
		SessionID:  currentSessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/switch was not handled")
	}
	if resp.SessionID != targetSessionID {
		t.Fatalf("resp.SessionID = %q, want %q", resp.SessionID, targetSessionID)
	}
	requireWSMessage(t, resp.Messages, "context_switched")
	requireCommandMessage(t, resp, "switch", "success")
	assertInvocation(t, db, "builtin.switch", "success")
}

func TestCommandServiceClearWritesMarkerOnly(t *testing.T) {
	ctx := context.Background()
	_, db, commands := newTestCommandService(t)
	origin := conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"}
	sessionID := createTestSession(t, db, "session-clear", "default")
	if err := db.AddMessage(ctx, "m1", sessionID, "user", "keep in db"); err != nil {
		t.Fatalf("AddMessage m1: %v", err)
	}
	if err := db.AddMessage(ctx, "m2", sessionID, "assistant", "also keep in db"); err != nil {
		t.Fatalf("AddMessage m2: %v", err)
	}

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/clear",
		Origin:     origin,
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/clear was not handled")
	}
	requireCommandMessage(t, resp, "clear", "success")
	marker, err := db.GetSessionClearMarker(ctx, origin.OriginKey, sessionID)
	if err != nil {
		t.Fatalf("GetSessionClearMarker: %v", err)
	}
	if marker == nil || marker.AfterMessageID != "m2" {
		t.Fatalf("marker = %#v, want after m2", marker)
	}
	messages, err := db.GetAllMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(messages))
	}
	assertInvocation(t, db, "builtin.clear", "success")
}

func TestCommandServiceStopCancelsActiveRuns(t *testing.T) {
	ctx := context.Background()
	_, db, commands := newTestCommandService(t)
	origin := conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"}
	sessionID := createTestSession(t, db, "session-stop", "default")
	cancelled := false
	unregister := commands.conversation.RunRegistry().Register(conversation.RunRef{
		OriginKey: origin.OriginKey,
		SessionID: sessionID,
		Kind:      "emotion_turn",
	}, func() { cancelled = true })
	defer unregister()

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/stop",
		Origin:     origin,
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/stop was not handled")
	}
	requireCommandMessage(t, resp, "stop", "success")
	if !cancelled {
		t.Fatal("active run was not cancelled")
	}
	assertInvocation(t, db, "builtin.stop", "success")
}

func TestCommandServiceResetWritesBarrierAndKeepsMessages(t *testing.T) {
	ctx := context.Background()
	app, db, commands := newTestCommandService(t)
	origin := conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"}
	sessionID := createTestSession(t, db, "session-reset", "default")
	setTestActiveRuntime(app, &ActiveAgentRuntime{PersonaKey: "default", Context: config.DefaultConfig().Context})
	if err := db.AddMessage(ctx, "m1", sessionID, "user", "before reset"); err != nil {
		t.Fatalf("AddMessage m1: %v", err)
	}
	if err := db.AddMessage(ctx, "m2", sessionID, "assistant", "before reset reply"); err != nil {
		t.Fatalf("AddMessage m2: %v", err)
	}

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/reset",
		Origin:     origin,
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/reset was not handled")
	}
	requireCommandMessage(t, resp, "reset", "success")
	state, err := contextutil.LoadSessionState(ctx, db, sessionID, config.DefaultConfig().Context)
	if err != nil {
		t.Fatalf("LoadSessionState: %v", err)
	}
	if state.ResetBarrier == nil || state.ResetBarrier.AfterMessageID != "m2" || state.ResetBarrier.Epoch != 1 {
		t.Fatalf("ResetBarrier = %#v, want epoch 1 after m2", state.ResetBarrier)
	}
	messages, err := db.GetAllMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(messages))
	}
	assertInvocation(t, db, "builtin.reset", "success")
}

func TestCommandServiceCompactUpdatesRunningSummaryWithoutMessage(t *testing.T) {
	ctx := context.Background()
	app, db, commands := newTestCommandService(t)
	origin := conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"}
	sessionID := createTestSession(t, db, "session-compact", "default")
	cfg := config.DefaultConfig().Context
	cfg.KeepRecentUserTurns = 1
	fakeLLM := &commandCompactFakeLLM{}
	setTestActiveRuntime(app, &ActiveAgentRuntime{
		PersonaKey: "default",
		EmotionSummary: ModelRuntime{
			Client: fakeLLM,
			Model:  "summary-model",
		},
		Context: cfg,
	})
	for _, msg := range []struct {
		id      string
		role    string
		content string
	}{
		{id: "m1", role: "user", content: "older user"},
		{id: "m2", role: "assistant", content: "older assistant"},
		{id: "m3", role: "user", content: "recent user"},
		{id: "m4", role: "assistant", content: "recent assistant"},
	} {
		if err := db.AddMessage(ctx, msg.id, sessionID, msg.role, msg.content); err != nil {
			t.Fatalf("AddMessage(%s): %v", msg.id, err)
		}
	}

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/compact",
		Origin:     origin,
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/compact was not handled")
	}
	requireCommandMessage(t, resp, "compact", "success")
	if fakeLLM.calls != 1 {
		t.Fatalf("summary calls = %d, want 1", fakeLLM.calls)
	}
	state, err := contextutil.LoadSessionState(ctx, db, sessionID, cfg)
	if err != nil {
		t.Fatalf("LoadSessionState: %v", err)
	}
	if state.RunningSummary.SessionGoal != "keep the compacted context" {
		t.Fatalf("running summary = %#v", state.RunningSummary)
	}
	messages, err := db.GetAllMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	if len(messages) != 4 {
		t.Fatalf("messages len = %d, want 4", len(messages))
	}
	assertInvocation(t, db, "builtin.compact", "success")
}

func TestCommandServiceForgetPreviewsMemoryCoreWithoutMessage(t *testing.T) {
	ctx := context.Background()
	_, db, commands := newTestCommandService(t)
	core := &commandForgetCore{
		preview: &memorycore.ForgetPreviewResult{
			PersonaID:      "default",
			RequestID:      "forget-preview-1",
			OperationID:    "operation-1",
			PreviewHash:    "hash-1",
			RequestedLevel: memorycore.ForgetLevelSoft,
			ScopeMode:      memorycore.ForgetScopeSemanticQuery,
			Targets: []memorycore.ForgetResolvedTarget{{
				NodeType:    memorycore.ForgetNodeFact,
				NodeID:      "fact-1",
				SafeSummary: "我喜欢手冲咖啡",
			}},
		},
	}
	commands.memory = &MemoryService{host: &memoryhost.Host{Core: core}}
	origin := conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"}
	sessionID := createTestSession(t, db, "session-forget-command", "default")
	if _, err := db.CreateMemorySegment(ctx, storage.CreateMemorySegmentParams{
		ID:              "segment-forget-command",
		ChatSessionID:   sessionID,
		PersonaID:       "default",
		MemorySessionID: "memory-session-forget-command",
	}); err != nil {
		t.Fatalf("CreateMemorySegment: %v", err)
	}

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/forget 我喜欢手冲咖啡",
		Origin:     origin,
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/forget was not handled")
	}
	requireCommandMessage(t, resp, "forget", "success")
	if len(core.previewCalls) != 1 {
		t.Fatalf("PreviewForget calls = %d, want 1", len(core.previewCalls))
	}
	req := core.previewCalls[0]
	if req.PersonaID != "default" ||
		req.ChatSessionID != sessionID ||
		req.SessionID != "memory-session-forget-command" ||
		req.ScopeMode != memorycore.ForgetScopeSemanticQuery ||
		req.RequestedLevel != memorycore.ForgetLevelSoft ||
		req.SemanticQuery == nil ||
		*req.SemanticQuery != "我喜欢手冲咖啡" ||
		!req.RequireConfirmation {
		t.Fatalf("PreviewForget request = %#v", req)
	}
	if !strings.Contains(resp.Messages[len(resp.Messages)-1].Content, "确认删除") {
		t.Fatalf("forget content = %q, want confirmation prompt", resp.Messages[len(resp.Messages)-1].Content)
	}
	assertNoMessages(t, db, sessionID)
	assertInvocation(t, db, "builtin.forget", "success")
}

func TestCommandServiceForgetFallsBackWhenMemoryHostUnavailable(t *testing.T) {
	ctx := context.Background()
	_, db, commands := newTestCommandService(t)
	sessionID := createTestSession(t, db, "session-forget-fallback", "default")

	resp, handled, err := commands.TryHandle(ctx, chat.CommandRequest{
		Content:    "/forget 我喜欢手冲咖啡",
		Origin:     conversation.Origin{OriginKey: "webui:local:main", SourceType: "webui", ChannelType: "web"},
		SessionID:  sessionID,
		PersonaKey: "default",
		ActorRole:  "member",
	})
	if err != nil {
		t.Fatalf("TryHandle: %v", err)
	}
	if !handled {
		t.Fatal("/forget was not handled")
	}
	requireCommandMessage(t, resp, "forget", "success")
	if !strings.Contains(resp.Messages[len(resp.Messages)-1].Content, "Forget Manager") {
		t.Fatalf("forget fallback content = %q", resp.Messages[len(resp.Messages)-1].Content)
	}
	assertNoMessages(t, db, sessionID)
	assertInvocation(t, db, "builtin.forget", "success")
}

type commandCompactFakeLLM struct {
	calls int
}

func (f *commandCompactFakeLLM) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	f.calls++
	return &llm.ChatResponse{
		Content: `{"running_summary":{"session_goal":"keep the compacted context","user_facts":[],"relationship_state":{"tone":"","recent_emotion":"","promises_made":[]},"open_loops":[],"decisions":[],"do_not_forget":[]}}`,
	}, nil
}

func (f *commandCompactFakeLLM) ChatStream(ctx context.Context, req llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	return f.Chat(ctx, req)
}

type commandForgetCore struct {
	memoryhost.CoreClient
	previewCalls []memorycore.ForgetPreviewRequest
	preview      *memorycore.ForgetPreviewResult
	err          error
}

func (f *commandForgetCore) PreviewForget(_ context.Context, req memorycore.ForgetPreviewRequest) (*memorycore.ForgetPreviewResult, error) {
	f.previewCalls = append(f.previewCalls, req)
	return f.preview, f.err
}

func newTestCommandService(t *testing.T) (*App, *storage.DB, *CommandService) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	app := newTestApp(config.DefaultConfig(), db, logger)
	if app.kernel.Services.Commands == nil {
		t.Fatal("CommandService = nil")
	}
	return app, db, app.kernel.Services.Commands
}

func createTestSession(t *testing.T, db *storage.DB, sessionID, persona string) string {
	t.Helper()
	if err := db.CreateSession(context.Background(), sessionID, persona); err != nil {
		t.Fatalf("CreateSession(%s): %v", sessionID, err)
	}
	return sessionID
}

func requireCommandMessage(t *testing.T, resp chat.CommandResponse, name string, status string) {
	t.Helper()
	for _, msg := range resp.Messages {
		if msg.Type == "command_result" && msg.CommandName == name && msg.Status == status {
			return
		}
	}
	t.Fatalf("missing command_result %s/%s in %#v", name, status, resp.Messages)
}

func requireWSMessage(t *testing.T, messages []chat.WSMessage, typ string) {
	t.Helper()
	for _, msg := range messages {
		if msg.Type == typ {
			return
		}
	}
	t.Fatalf("missing ws message %q in %#v", typ, messages)
}

func assertNoMessages(t *testing.T, db *storage.DB, sessionID string) {
	t.Helper()
	messages, err := db.GetAllMessages(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("session %s has %d messages, want 0", sessionID, len(messages))
	}
}

func assertInvocation(t *testing.T, db *storage.DB, commandID string, status string) {
	t.Helper()
	invocations, err := db.ListCommandInvocations(context.Background(), storage.CommandInvocationFilter{CommandID: commandID, Limit: 1})
	if err != nil {
		t.Fatalf("ListCommandInvocations: %v", err)
	}
	if len(invocations) != 1 {
		t.Fatalf("invocation count for %s = %d, want 1", commandID, len(invocations))
	}
	if invocations[0].Status != status {
		t.Fatalf("invocation status = %q, want %q", invocations[0].Status, status)
	}
}

func assertConversationEvent(t *testing.T, db *storage.DB, sessionID string, eventType string) {
	t.Helper()
	events, err := db.ListConversationEvents(context.Background(), sessionID, "", 20)
	if err != nil {
		t.Fatalf("ListConversationEvents: %v", err)
	}
	for _, event := range events {
		if event.EventType == eventType {
			return
		}
	}
	t.Fatalf("missing event %q in %#v", eventType, events)
}
