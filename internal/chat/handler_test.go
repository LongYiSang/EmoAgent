package chat

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/longyisang/emoagent/internal/config"
	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/conversation"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/replydelivery"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/turn"
)

type fakeConversationEngine struct {
	startPersona   string
	resumeID       string
	resumeOK       bool
	resumePersona  string
	sessionID      string
	sendReply      string
	sendErr        error
	sendSession    string
	sendPersona    *config.Persona
	sendContent    string
	sendParts      int
	sendCount      int
	deltas         []string
	history        []storage.MessageRecord
	sendHook       func(context.Context)
	approvals      []protocol.ApprovalRequest
	lastAction     string
	lastActionReq  string
	lastActionOpt  string
	applyCount     int
	continueCount  int
	approvalReply  string
	approvalDeltas []string
	sendDone       chan struct{}
}

type fakeCommandHandler struct {
	requests []CommandRequest
	response CommandResponse
	handled  bool
}

func (f *fakeCommandHandler) TryHandle(_ context.Context, req CommandRequest) (CommandResponse, bool, error) {
	f.requests = append(f.requests, req)
	return f.response, f.handled, nil
}

type stopCommandHandler struct {
	runs    *conversation.RunRegistry
	stopped chan int
}

func (h *stopCommandHandler) TryHandle(_ context.Context, req CommandRequest) (CommandResponse, bool, error) {
	if strings.TrimSpace(req.Content) != "/stop" {
		return CommandResponse{}, false, nil
	}
	count := h.runs.Stop(conversation.StopSelector{OriginKey: req.Origin.OriginKey, SessionID: req.SessionID})
	if h.stopped != nil {
		h.stopped <- count
	}
	return CommandResponse{Messages: []WSMessage{{
		Type:        "command_result",
		Status:      "success",
		Content:     "stopped",
		CommandName: "stop",
		CommandID:   "builtin.stop",
	}}}, true, nil
}

type fakeBindingService struct {
	ensureOrigin  conversation.Origin
	ensurePersona string
	bindOrigin    conversation.Origin
	bindPersona   string
	bindSession   string
	binding       conversation.Binding
}

func (f *fakeBindingService) EnsureCurrent(_ context.Context, origin conversation.Origin, personaKey string) (conversation.Binding, error) {
	f.ensureOrigin = origin
	f.ensurePersona = personaKey
	if f.binding.SessionID == "" {
		f.binding = conversation.Binding{OriginKey: origin.OriginKey, PersonaKey: personaKey, SessionID: "bound-session"}
	}
	return f.binding, nil
}

func (f *fakeBindingService) BindSession(_ context.Context, origin conversation.Origin, personaKey string, sessionID string, isNew bool) (conversation.Binding, error) {
	f.bindOrigin = origin
	f.bindPersona = personaKey
	f.bindSession = sessionID
	f.binding = conversation.Binding{OriginKey: origin.OriginKey, PersonaKey: personaKey, SessionID: sessionID, IsNew: isNew}
	return f.binding, nil
}

func (f *fakeConversationEngine) StartSession(_ context.Context, personaName string) (string, error) {
	f.startPersona = personaName
	if f.sessionID == "" {
		f.sessionID = "session-test"
	}
	return f.sessionID, nil
}

func (f *fakeConversationEngine) ResumeSession(_ context.Context, sessionID string, personaName string) (string, bool, error) {
	f.resumeID = sessionID
	f.resumePersona = personaName
	if f.resumeOK {
		if f.sessionID == "" {
			f.sessionID = sessionID
		}
		return f.sessionID, true, nil
	}
	return "", false, nil
}

func (f *fakeConversationEngine) SendMessage(ctx context.Context, sessionID string, persona *config.Persona, userContent string, cb func(delta string)) (string, error) {
	f.sendCount++
	if f.sendHook != nil {
		f.sendHook(ctx)
	}
	f.sendSession = sessionID
	f.sendPersona = persona
	f.sendContent = userContent
	for _, delta := range f.deltas {
		cb(delta)
	}
	if f.sendDone != nil {
		select {
		case f.sendDone <- struct{}{}:
		default:
		}
	}
	return f.sendReply, f.sendErr
}

func (f *fakeConversationEngine) SendMessageParts(ctx context.Context, sessionID string, persona *config.Persona, parts []llm.ContentBlock, cb func(delta string)) (string, error) {
	f.sendParts = len(parts)
	return f.SendMessage(ctx, sessionID, persona, "", cb)
}

func (f *fakeConversationEngine) GetHistory(_ context.Context, sessionID string, limit int) ([]storage.MessageRecord, error) {
	if len(f.history) <= limit || limit <= 0 {
		return append([]storage.MessageRecord(nil), f.history...), nil
	}
	return append([]storage.MessageRecord(nil), f.history[len(f.history)-limit:]...), nil
}

func (f *fakeConversationEngine) ListSessionApprovals(_ context.Context, sessionID string) ([]protocol.ApprovalRequest, error) {
	return append([]protocol.ApprovalRequest(nil), f.approvals...), nil
}

func (f *fakeConversationEngine) ApplyApprovalAction(_ context.Context, sessionID, requestID, action, optionID string) (*protocol.ApprovalRequest, error) {
	f.applyCount++
	f.lastAction = action
	f.lastActionReq = requestID
	f.lastActionOpt = optionID
	for i := range f.approvals {
		if f.approvals[i].ID != requestID {
			continue
		}
		req := f.approvals[i]
		switch action {
		case "approve":
			req.Status = string(protocol.ApprovalStatusApproved)
			req.SelectedOptionID = optionID
		case "reject":
			req.Status = string(protocol.ApprovalStatusRejected)
			req.SelectedOptionID = req.RejectOptionID
		}
		f.approvals[i] = req
		return &req, nil
	}
	return nil, fmt.Errorf("approval not found")
}

func (f *fakeConversationEngine) ContinueAfterApproval(_ context.Context, sessionID string, persona *config.Persona, approval *protocol.ApprovalRequest, cb func(delta string)) (string, error) {
	f.continueCount++
	for _, delta := range f.approvalDeltas {
		cb(delta)
	}
	return f.approvalReply, nil
}

type fakeAppProvider struct {
	defaultPersona string
	personas       map[string]*config.Persona
}

func (f *fakeAppProvider) GetPersona(name string) (*config.Persona, bool) {
	persona, ok := f.personas[name]
	if !ok || persona == nil {
		return nil, false
	}
	return persona, true
}

func (f *fakeAppProvider) GetDefaultPersonaName() string {
	return f.defaultPersona
}

func TestHandlerSendsSessionReadyAndGreetingOnNewSession(t *testing.T) {
	handler, _ := newTestHandler()
	conn := dialTestWS(t, handler)
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if msg.Type != "session_ready" {
		t.Fatalf("Type = %q, want session_ready", msg.Type)
	}
	if msg.SessionID != "session-test" {
		t.Fatalf("SessionID = %q, want session-test", msg.SessionID)
	}
	if !msg.IsNew {
		t.Fatal("IsNew = false, want true")
	}

	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(greeting): %v", err)
	}
	if msg.Type != "greeting" {
		t.Fatalf("Type = %q, want greeting", msg.Type)
	}
	if msg.Content != "Hello from Emo" {
		t.Fatalf("Content = %q, want greeting text", msg.Content)
	}
}

func TestHandlerStreamsAssistantResponse(t *testing.T) {
	handler, engine := newTestHandler()
	engine.deltas = []string{"Hi", " there"}
	engine.sendReply = "Hi there"

	conn := dialTestWS(t, handler)
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var ready WSMessage
	if err := wsjson.Read(context.Background(), conn, &ready); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	var greeting WSMessage
	if err := wsjson.Read(context.Background(), conn, &greeting); err != nil {
		t.Fatalf("Read(greeting): %v", err)
	}

	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "message", Content: "How are you?"}); err != nil {
		t.Fatalf("Write(message): %v", err)
	}

	types := make([]string, 0, 4)
	contents := make([]string, 0, 4)
	for len(types) < 4 {
		var msg WSMessage
		if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
			t.Fatalf("Read(stream): %v", err)
		}
		types = append(types, msg.Type)
		contents = append(contents, msg.Content)
	}

	wantTypes := []string{"stream_start", "stream_delta", "stream_delta", "stream_end"}
	for i, want := range wantTypes {
		if types[i] != want {
			t.Fatalf("types[%d] = %q, want %q", i, types[i], want)
		}
	}
	if contents[1] != "Hi" || contents[2] != " there" {
		t.Fatalf("delta contents = %#v, want [Hi,  there]", contents)
	}
	if engine.sendSession != engine.sessionID {
		t.Fatalf("sendSession = %q, want %q", engine.sendSession, engine.sessionID)
	}
	if engine.sendPersona == nil || engine.sendPersona.Name != "default" {
		t.Fatalf("sendPersona = %#v, want default persona", engine.sendPersona)
	}
	if engine.sendContent != "How are you?" {
		t.Fatalf("sendContent = %q, want user message", engine.sendContent)
	}
}

func TestHandlerEmitsAssistantSegmentsForCasualReply(t *testing.T) {
	replyCfg := config.DefaultReplyDeliveryConfig()
	replyCfg.Enabled = true
	replyCfg.Timing.Enabled = false
	handler, engine := newTestHandlerWithOptions(WithReplyDeliveryConfig(replyCfg))
	engine.sendReply = "第一句。第二句。"
	engine.sendHook = func(ctx context.Context) {
		replydelivery.RecordPromptMode(ctx, contextutil.PromptModeCasualChat)
	}

	conn := dialTestWS(t, handler)
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(greeting): %v", err)
	}
	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "message", Content: "hello"}); err != nil {
		t.Fatalf("Write(message): %v", err)
	}

	var got []WSMessage
	for len(got) < 4 {
		msg = WSMessage{}
		if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
			t.Fatalf("Read(stream): %v", err)
		}
		got = append(got, msg)
	}
	wantTypes := []string{"stream_start", "assistant_segment", "assistant_segment", "stream_end"}
	for i, want := range wantTypes {
		if got[i].Type != want {
			t.Fatalf("types[%d] = %q, want %q (all=%#v)", i, got[i].Type, want, got)
		}
	}
	if got[1].Content != "第一句。" || got[2].Content != "第二句。" || got[1].SegmentIndex != 0 || got[2].SegmentTotal != 2 {
		t.Fatalf("segment messages = %#v, want indexed split segments", got)
	}
}

func TestHandlerFallsBackToStreamDeltaForWorkModeReplyDelivery(t *testing.T) {
	replyCfg := config.DefaultReplyDeliveryConfig()
	replyCfg.Enabled = true
	replyCfg.Timing.Enabled = false
	handler, engine := newTestHandlerWithOptions(WithReplyDeliveryConfig(replyCfg))
	engine.sendReply = "第一句。第二句。"
	engine.sendHook = func(ctx context.Context) {
		replydelivery.RecordPromptMode(ctx, contextutil.PromptModeWorkMode)
	}

	conn := dialTestWS(t, handler)
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(greeting): %v", err)
	}
	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "message", Content: "hello"}); err != nil {
		t.Fatalf("Write(message): %v", err)
	}

	want := []WSMessage{
		{Type: "stream_start"},
		{Type: "stream_delta", Content: "第一句。第二句。"},
		{Type: "stream_end"},
	}
	for i := range want {
		msg = WSMessage{}
		if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
			t.Fatalf("Read(stream %d): %v", i, err)
		}
		if msg.Type != want[i].Type || msg.Content != want[i].Content {
			t.Fatalf("message[%d] = %#v, want %#v", i, msg, want[i])
		}
	}
}

func TestHandlerFallsBackToStreamDeltaForRealtimeReplyDelivery(t *testing.T) {
	replyCfg := config.DefaultReplyDeliveryConfig()
	replyCfg.Enabled = true
	replyCfg.Timing.Enabled = false
	handler, engine := newTestHandlerWithOptions(WithReplyDeliveryConfig(replyCfg), WithRealtimeStreaming(true))
	engine.sendReply = "第一句。第二句。"
	engine.sendHook = func(ctx context.Context) {
		replydelivery.RecordPromptMode(ctx, contextutil.PromptModeCasualChat)
	}

	conn := dialTestWS(t, handler)
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(greeting): %v", err)
	}
	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "message", Content: "hello"}); err != nil {
		t.Fatalf("Write(message): %v", err)
	}

	want := []WSMessage{
		{Type: "stream_start"},
		{Type: "stream_delta", Content: "第一句。第二句。"},
		{Type: "stream_end"},
	}
	for i := range want {
		msg = WSMessage{}
		if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
			t.Fatalf("Read(stream %d): %v", i, err)
		}
		if msg.Type != want[i].Type || msg.Content != want[i].Content {
			t.Fatalf("message[%d] = %#v, want %#v", i, msg, want[i])
		}
	}
}

func TestHandlerRejectsUnsupportedUserParts(t *testing.T) {
	handler, engine := newTestHandler()
	conn := dialTestWS(t, handler)
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(greeting): %v", err)
	}

	if err := wsjson.Write(context.Background(), conn, WSMessage{
		Type:    "message",
		Content: "hello",
		Parts: []llm.ContentBlock{
			{Type: string(llm.PartText), Text: "hello"},
			{
				Type: string(llm.PartToolUse),
				ID:   "tool-call-1",
				Name: "spoofed_tool",
			},
		},
	}); err != nil {
		t.Fatalf("Write(message): %v", err)
	}

	readCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := wsjson.Read(readCtx, conn, &msg); err != nil {
		t.Fatalf("Read(error): %v", err)
	}
	if msg.Type != "error" || !strings.Contains(msg.Content, "unsupported user content part type") {
		t.Fatalf("message = %#v, want unsupported part error", msg)
	}
	if engine.sendCount != 0 {
		t.Fatalf("sendCount = %d, want invalid parts rejected before engine", engine.sendCount)
	}
}

func TestHandlerManualMemoryNoticeStreamsReturnedReply(t *testing.T) {
	handler, engine := newTestHandler()
	engine.sendReply = "Manual memory saved."

	conn := dialTestWS(t, handler)
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(greeting): %v", err)
	}

	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "message", Content: "remember this manually"}); err != nil {
		t.Fatalf("Write(message): %v", err)
	}

	want := []WSMessage{
		{Type: "stream_start"},
		{Type: "stream_delta", Content: "Manual memory saved."},
		{Type: "stream_end"},
	}
	for i := range want {
		msg = WSMessage{}
		if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
			t.Fatalf("Read(stream %d): %v", i, err)
		}
		if msg.Type != want[i].Type || msg.Content != want[i].Content {
			t.Fatalf("message[%d] = %#v, want %#v", i, msg, want[i])
		}
	}
}

func TestHandlerStreamsToolCallEvents(t *testing.T) {
	handler, engine := newTestHandler()
	engine.deltas = []string{"done"}
	engine.sendReply = "done"
	engine.sendHook = func(ctx context.Context) {
		writer := wsWriterFromContext(ctx)
		if writer == nil {
			t.Fatal("ws writer missing from message context")
		}
		writer(WSMessage{Type: "tool_call_start", Tool: &ToolActivity{ID: "call-1", Name: "get_time", Status: "running"}})
		writer(WSMessage{Type: "tool_call_end", Tool: &ToolActivity{ID: "call-1", Name: "get_time", Status: "success", Preview: `{"time":"17:00"}`, Size: 17, Hash: "abc123"}})
	}

	conn := dialTestWS(t, handler)
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(greeting): %v", err)
	}

	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "message", Content: "use a tool"}); err != nil {
		t.Fatalf("Write(message): %v", err)
	}

	var types []string
	var toolEvents []ToolActivity
	for len(types) < 5 {
		msg = WSMessage{}
		if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
			t.Fatalf("Read(stream): %v", err)
		}
		types = append(types, msg.Type)
		if msg.Tool != nil {
			toolEvents = append(toolEvents, *msg.Tool)
		}
	}

	want := []string{"stream_start", "tool_call_start", "tool_call_end", "stream_delta", "stream_end"}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("types[%d] = %q, want %q (all=%#v)", i, types[i], want[i], types)
		}
	}
	if len(toolEvents) != 2 {
		t.Fatalf("toolEvents = %#v, want start and end", toolEvents)
	}
	if toolEvents[0].Status != "running" || toolEvents[1].Status != "success" || toolEvents[1].Preview == "" {
		t.Fatalf("toolEvents = %#v, want running then successful preview", toolEvents)
	}
}

func TestHandlerStreamsReasoningEvents(t *testing.T) {
	handler, engine := newTestHandler()
	engine.deltas = []string{"done"}
	engine.sendReply = "done"
	engine.sendHook = func(ctx context.Context) {
		writer := wsWriterFromContext(ctx)
		if writer == nil {
			t.Fatal("ws writer missing from message context")
		}
		writer(WSMessage{Type: "reasoning_start", Reasoning: &ReasoningActivity{ID: "reasoning-1", Status: "running", Kind: "reasoning_content"}})
		writer(WSMessage{Type: "reasoning_delta", Reasoning: &ReasoningActivity{ID: "reasoning-1", Status: "running", Content: "thinking", Kind: "reasoning_content"}})
		writer(WSMessage{Type: "reasoning_end", Reasoning: &ReasoningActivity{ID: "reasoning-1", Status: "done", Content: "thinking", DurationMS: 42, Kind: "reasoning_content"}})
	}

	conn := dialTestWS(t, handler)
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(greeting): %v", err)
	}

	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "message", Content: "think"}); err != nil {
		t.Fatalf("Write(message): %v", err)
	}

	var types []string
	var reasoning []ReasoningActivity
	for len(types) < 6 {
		msg = WSMessage{}
		if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
			t.Fatalf("Read(stream): %v", err)
		}
		types = append(types, msg.Type)
		if msg.Reasoning != nil {
			reasoning = append(reasoning, *msg.Reasoning)
		}
	}

	want := []string{"stream_start", "reasoning_start", "reasoning_delta", "reasoning_end", "stream_delta", "stream_end"}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("types[%d] = %q, want %q (all=%#v)", i, types[i], want[i], types)
		}
	}
	if len(reasoning) != 3 {
		t.Fatalf("reasoning = %#v, want start/delta/end", reasoning)
	}
	if reasoning[1].Content != "thinking" || reasoning[2].DurationMS != 42 {
		t.Fatalf("reasoning = %#v, want delta content and end duration", reasoning)
	}
}

func TestHandlerRepliesToPing(t *testing.T) {
	handler, _ := newTestHandler()
	conn := dialTestWS(t, handler)
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var ready WSMessage
	if err := wsjson.Read(context.Background(), conn, &ready); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	var greeting WSMessage
	if err := wsjson.Read(context.Background(), conn, &greeting); err != nil {
		t.Fatalf("Read(greeting): %v", err)
	}

	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "ping"}); err != nil {
		t.Fatalf("Write(ping): %v", err)
	}

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(pong): %v", err)
	}
	if msg.Type != "pong" {
		t.Fatalf("Type = %q, want pong", msg.Type)
	}
}

func TestHandlerRoutesCommandBeforeEngine(t *testing.T) {
	commandHandler := &fakeCommandHandler{
		handled: true,
		response: CommandResponse{
			Messages: []WSMessage{{
				Type:        "command_result",
				CommandID:   "builtin.sid",
				CommandName: "sid",
				Status:      "success",
				Content:     "origin=webui:local:main session=session-test persona=default",
			}},
		},
	}
	handler, engine := newTestHandlerWithOptions(WithCommandHandler(commandHandler))
	conn := dialTestWS(t, handler, "/ws?skip_greeting=1")
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if msg.OriginKey != conversation.DefaultOriginKey {
		t.Fatalf("OriginKey = %q, want %q", msg.OriginKey, conversation.DefaultOriginKey)
	}

	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "message", Content: "/sid"}); err != nil {
		t.Fatalf("Write(command): %v", err)
	}
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(command_result): %v", err)
	}
	if msg.Type != "command_result" || msg.CommandID != "builtin.sid" || msg.CommandName != "sid" {
		t.Fatalf("message = %#v, want sid command_result", msg)
	}
	if engine.sendCount != 0 {
		t.Fatalf("sendCount = %d, want command to bypass engine", engine.sendCount)
	}
	if len(commandHandler.requests) != 1 || commandHandler.requests[0].SessionID != "session-test" || commandHandler.requests[0].Origin.OriginKey != conversation.DefaultOriginKey {
		t.Fatalf("command requests = %#v, want current binding context", commandHandler.requests)
	}
}

func TestHandlerRoutesCommandWithPlatformActor(t *testing.T) {
	commandHandler := &fakeCommandHandler{
		handled: true,
		response: CommandResponse{
			Messages: []WSMessage{{
				Type:        "command_result",
				CommandID:   "builtin.sid",
				CommandName: "sid",
				Status:      "success",
				Content:     "ok",
			}},
		},
	}
	handler, _ := newTestHandlerWithOptions(WithCommandHandler(commandHandler))
	conn := dialTestWS(t, handler, "/ws?skip_greeting=1&origin_key=napcat:main:group:20002&source=napcat&adapter_instance_id=main&platform_id=qq&channel_type=group&external_conversation_id=20002&external_actor_id=10001&display_name=Alice")
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "message", Content: "/sid"}); err != nil {
		t.Fatalf("Write(command): %v", err)
	}
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(command_result): %v", err)
	}
	if len(commandHandler.requests) != 1 {
		t.Fatalf("command requests = %#v, want one request", commandHandler.requests)
	}
	req := commandHandler.requests[0]
	if req.ActorID != "10001" || req.ActorName != "Alice" || req.ActorRole != "member" ||
		req.Origin.SourceType != "napcat" || req.Origin.ExternalConversationID != "20002" {
		t.Fatalf("command request = %#v, want platform actor and origin", req)
	}
}

func TestHandlerProcessesStopWhileReplyIsRunning(t *testing.T) {
	runs := conversation.NewRunRegistry()
	stopped := make(chan int, 1)
	handler, engine := newTestHandlerWithOptions(
		WithRunRegistry(runs),
		WithCommandHandler(&stopCommandHandler{runs: runs, stopped: stopped}),
	)
	started := make(chan struct{})
	engine.sendHook = func(ctx context.Context) {
		close(started)
		<-ctx.Done()
	}

	conn := dialTestWS(t, handler, "/ws?origin_key=webui:local:main&skip_greeting=1")
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "message", Content: "long reply"}); err != nil {
		t.Fatalf("Write(message): %v", err)
	}
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(stream_start): %v", err)
	}
	if msg.Type != "stream_start" {
		t.Fatalf("message = %#v, want stream_start", msg)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("reply did not start")
	}
	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "message", Content: "/stop"}); err != nil {
		t.Fatalf("Write(stop): %v", err)
	}
	select {
	case count := <-stopped:
		if count != 1 {
			t.Fatalf("stopped count = %d, want 1", count)
		}
	case <-time.After(time.Second):
		t.Fatal("/stop was not processed while reply was running")
	}
	for i := 0; i < 3; i++ {
		if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
			t.Fatalf("Read(after stop): %v", err)
		}
		if msg.Type == "command_result" && msg.CommandName == "stop" {
			return
		}
	}
	t.Fatalf("did not receive stop command_result, last=%#v", msg)
}

func TestHandlerUsesConversationBindingWhenConfigured(t *testing.T) {
	bindings := &fakeBindingService{
		binding: conversation.Binding{OriginKey: "webui:local:main", PersonaKey: "default", SessionID: "bound-session"},
	}
	handler, engine := newTestHandlerWithOptions(WithConversationBindings(bindings))
	conn := dialTestWS(t, handler, "/ws?origin_key=webui:local:main&skip_greeting=1")
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if msg.SessionID != "bound-session" || msg.OriginKey != "webui:local:main" || msg.IsNew {
		t.Fatalf("session_ready = %#v, want bound existing session", msg)
	}
	if bindings.ensurePersona != "default" || bindings.ensureOrigin.OriginKey != "webui:local:main" {
		t.Fatalf("binding ensure = %#v persona=%q", bindings.ensureOrigin, bindings.ensurePersona)
	}
	if engine.startPersona != "" || engine.resumeID != "" {
		t.Fatalf("engine bootstrap start/resume = %q/%q, want binding-only", engine.startPersona, engine.resumeID)
	}

	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "message", Content: "hello"}); err != nil {
		t.Fatalf("Write(message): %v", err)
	}
	for {
		if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
			t.Fatalf("Read(stream): %v", err)
		}
		if msg.Type == "stream_end" {
			break
		}
	}
	if engine.sendSession != "bound-session" {
		t.Fatalf("sendSession = %q, want bound-session", engine.sendSession)
	}
}

func TestHandlerUsesRequestedPersonaFromQuery(t *testing.T) {
	handler, engine := newTestHandlerWithApp(&fakeAppProvider{
		defaultPersona: "default",
		personas: map[string]*config.Persona{
			"default": {Name: "default", Greeting: "Hello from Emo"},
			"neko":    {Name: "neko", Greeting: "Meow hello"},
		},
	})

	conn := dialTestWS(t, handler, "/ws?persona=neko")
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if msg.Persona != "neko" {
		t.Fatalf("Persona = %q, want neko", msg.Persona)
	}
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(greeting): %v", err)
	}
	if msg.Content != "Meow hello" {
		t.Fatalf("Content = %q, want neko greeting", msg.Content)
	}
	if engine.startPersona != "neko" {
		t.Fatalf("startPersona = %q, want neko", engine.startPersona)
	}
}

func TestHandlerFallsBackToDefaultPersona(t *testing.T) {
	handler, engine := newTestHandler()

	conn := dialTestWS(t, handler, "/ws")
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(greeting): %v", err)
	}
	if msg.Content != "Hello from Emo" {
		t.Fatalf("Content = %q, want default greeting", msg.Content)
	}
	if engine.startPersona != "default" {
		t.Fatalf("startPersona = %q, want default", engine.startPersona)
	}
}

func TestHandlerReturnsErrorWhenRequestedPersonaMissing(t *testing.T) {
	handler, _ := newTestHandlerWithApp(&fakeAppProvider{
		defaultPersona: "default",
		personas: map[string]*config.Persona{
			"default": {Name: "default", Greeting: "Hello from Emo"},
		},
	})

	conn := dialTestWS(t, handler, "/ws?persona=missing")
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(error): %v", err)
	}
	if msg.Type != "error" {
		t.Fatalf("Type = %q, want error", msg.Type)
	}
	if !strings.Contains(msg.Content, "persona not found") {
		t.Fatalf("Content = %q, want persona not found", msg.Content)
	}
}

func TestHandlerResumedSessionSendsOnlySessionReady(t *testing.T) {
	handler, engine := newTestHandler()
	engine.resumeOK = true
	engine.sessionID = "session-restored"
	engine.history = []storage.MessageRecord{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}

	conn := dialTestWS(t, handler, "/ws?persona=default&session_id=session-restored")
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if msg.Type != "session_ready" || msg.IsNew {
		t.Fatalf("session_ready = %#v, want existing session", msg)
	}
	if engine.resumeID != "session-restored" {
		t.Fatalf("resumeID = %q, want session-restored", engine.resumeID)
	}

	// Resumed sessions no longer send history via WS (loaded via REST).
	// Verify we can send a ping and get pong (no history/greeting in between).
	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "ping"}); err != nil {
		t.Fatalf("Write(ping): %v", err)
	}
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(pong): %v", err)
	}
	if msg.Type != "pong" {
		t.Fatalf("Type = %q, want pong", msg.Type)
	}
}

func TestHandlerSkipsGreetingWhenRequested(t *testing.T) {
	handler, _ := newTestHandler()

	conn := dialTestWS(t, handler, "/ws?skip_greeting=1")
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if msg.Type != "session_ready" {
		t.Fatalf("Type = %q, want session_ready", msg.Type)
	}

	// No greeting should follow — verify with a ping/pong round-trip.
	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "ping"}); err != nil {
		t.Fatalf("Write(ping): %v", err)
	}
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(pong): %v", err)
	}
	if msg.Type != "pong" {
		t.Fatalf("Type = %q, want pong (no greeting expected)", msg.Type)
	}
}

func TestHandlerForwardsWorkProgressMessages(t *testing.T) {
	handler, engine := newTestHandler()
	engine.sendReply = "done"
	engine.sendHook = func(ctx context.Context) {
		writer := wsWriterFromContext(ctx)
		if writer == nil {
			t.Fatal("ws writer missing from context")
		}
		writer(WSMessage{Type: "work_progress", Content: "processing..."})
		writer(WSMessage{Type: "work_progress_end"})
	}

	conn := dialTestWS(t, handler)
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(greeting): %v", err)
	}

	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "message", Content: "progress please"}); err != nil {
		t.Fatalf("Write(message): %v", err)
	}

	var types []string
	var progressText string
	var deltaText string
	for len(types) < 5 {
		if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
			t.Fatalf("Read(stream): %v", err)
		}
		types = append(types, msg.Type)
		if msg.Type == "work_progress" {
			progressText = msg.Content
		}
		if msg.Type == "stream_delta" {
			deltaText = msg.Content
		}
	}

	want := []string{"stream_start", "work_progress", "work_progress_end", "stream_delta", "stream_end"}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("types[%d]=%q, want %q (all=%#v)", i, types[i], want[i], types)
		}
	}
	if progressText != "processing..." {
		t.Fatalf("progress text = %q, want processing...", progressText)
	}
	if deltaText != "done" {
		t.Fatalf("delta text = %q, want done", deltaText)
	}
}

func TestHandlerTurnPipelineEnabledForwardsWorkProgressViaOutboundSink(t *testing.T) {
	handler, engine := newTestHandlerWithOptions(WithTurnPipelineConfig(config.TurnPipelineConfig{Enabled: true}))
	engine.sendReply = "done"
	engine.sendHook = func(ctx context.Context) {
		sink := turn.OutboundSinkFromContext(ctx)
		if sink == nil {
			t.Fatal("outbound sink missing from context")
		}
		if err := sink.Emit(ctx, turn.OutboundEvent{Type: turn.EventWorkProgress, Content: "processing..."}); err != nil {
			t.Fatalf("Emit work_progress: %v", err)
		}
	}

	conn := dialTestWS(t, handler)
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(greeting): %v", err)
	}
	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "message", Content: "progress please", RequestID: "request-1"}); err != nil {
		t.Fatalf("Write(message): %v", err)
	}

	var types []string
	for len(types) < 4 {
		if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
			t.Fatalf("Read(stream): %v", err)
		}
		types = append(types, msg.Type)
	}
	want := []string{"stream_start", "work_progress", "stream_delta", "stream_end"}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("types[%d] = %q, want %q (all=%#v)", i, types[i], want[i], types)
		}
	}
}

func TestHandlerPluginsEnabledRejectsLegacyMessagePath(t *testing.T) {
	pluginHost := &fakePluginHost{enabled: true}
	handler, engine := newTestHandlerWithOptions(
		WithTurnPipelineConfig(config.TurnPipelineConfig{Enabled: true, RolloutPercent: 0}),
		WithPluginHost(pluginHost),
	)
	conn := dialTestWS(t, handler, "/ws?skip_greeting=1")
	defer conn.Close(websocket.StatusNormalClosure, "done")

	var ready WSMessage
	if err := wsjson.Read(context.Background(), conn, &ready); err != nil {
		t.Fatalf("read ready: %v", err)
	}
	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "message", Content: "hello", RequestID: "request-1"}); err != nil {
		t.Fatalf("write message: %v", err)
	}
	var got WSMessage
	if err := wsjson.Read(context.Background(), conn, &got); err != nil {
		t.Fatalf("read response: %v", err)
	}
	if got.Type != "error" || !strings.Contains(got.Content, "plugins.enabled requires Turn Pipeline") {
		t.Fatalf("message = %#v, want plugin turn pipeline error", got)
	}
	if engine.sendCount != 0 {
		t.Fatalf("sendCount = %d, want legacy path blocked before engine", engine.sendCount)
	}
}

func TestHandlerTurnPipelineContinuesAfterWSDisconnect(t *testing.T) {
	handler, engine := newTestHandlerWithOptions(WithTurnPipelineConfig(config.TurnPipelineConfig{Enabled: true, RolloutPercent: 100}))
	engine.sendReply = "answer"
	engine.sendDone = make(chan struct{}, 1)
	hookResult := make(chan error, 1)
	var conn *websocket.Conn
	engine.sendHook = func(ctx context.Context) {
		_ = conn.CloseNow()
		time.Sleep(10 * time.Millisecond)
		sink := turn.OutboundSinkFromContext(ctx)
		if sink == nil {
			hookResult <- fmt.Errorf("outbound sink missing from context")
			return
		}
		if err := sink.Emit(ctx, turn.OutboundEvent{Type: turn.EventReasoningStart}); err != nil {
			hookResult <- fmt.Errorf("Emit after WS disconnect: %w", err)
			return
		}
		select {
		case <-ctx.Done():
			hookResult <- fmt.Errorf("send context canceled after WS disconnect: %w", ctx.Err())
		default:
			hookResult <- nil
		}
	}

	conn = dialTestWS(t, handler, "/ws?skip_greeting=1")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "message", Content: "hello", RequestID: "request-1"}); err != nil {
		t.Fatalf("Write(message): %v", err)
	}
	select {
	case err := <-hookResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for send hook")
	}
	select {
	case <-engine.sendDone:
	case <-time.After(5 * time.Second):
		t.Fatal("SendMessage did not finish after WS disconnect")
	}
}

func TestWSOutboundSinkDetachesAfterFirstWriteFailure(t *testing.T) {
	handler, _ := newTestHandler()
	serverConn := make(chan *websocket.Conn, 1)
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		serverConn <- conn
		<-done
	}))
	defer srv.Close()
	defer close(done)

	target := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, _, err := websocket.Dial(context.Background(), target, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close(websocket.StatusNormalClosure, "done")

	var conn *websocket.Conn
	select {
	case conn = <-serverConn:
	case <-time.After(time.Second):
		t.Fatal("server websocket was not accepted")
	}
	_ = conn.Close(websocket.StatusInternalError, "force write failure")

	sink := handler.newWSOutboundSink(context.Background(), conn, &sync.Mutex{})
	if err := sink.Emit(context.Background(), turn.OutboundEvent{Type: turn.EventStreamStart}); err != nil {
		t.Fatalf("first Emit after write failure: %v", err)
	}
	if err := sink.Emit(context.Background(), turn.OutboundEvent{Type: turn.EventStreamEnd}); err != nil {
		t.Fatalf("second Emit after detach: %v", err)
	}
	if closer, ok := sink.(interface{ Close(context.Context) error }); ok {
		if err := closer.Close(context.Background()); err != nil {
			t.Fatalf("Close detached sink: %v", err)
		}
	}
}

func TestTurnRuntimeDeduplicatesApprovalAction(t *testing.T) {
	_, engine := newTestHandler()
	engine.approvals = []protocol.ApprovalRequest{
		{
			ID:             "approval-1",
			SessionID:      "session-test",
			TaskID:         "task-1",
			Status:         string(protocol.ApprovalStatusPending),
			RejectOptionID: "cancel",
			Options:        []protocol.DecisionOption{{ID: "delete", Summary: "Delete"}, {ID: "cancel", Summary: "Cancel"}},
		},
	}
	engine.approvalDeltas = []string{"ok"}

	runtime := newChatTurnRuntime(engine, config.TurnPipelineConfig{Enabled: true}, turn.NewMemoryJournal(), slog.Default())
	env := mustWSMessageToInbound(t, WSMessage{
		Type:      "approval_action",
		RequestID: "approval-1",
		Action:    "approve",
		OptionID:  "delete",
	}, "session-test", "default")
	persona := &config.Persona{Name: "default"}
	sink := turn.SinkFunc(func(context.Context, turn.OutboundEvent) error { return nil })

	if _, err := runtime.Execute(context.Background(), env, persona, sink); err != nil {
		t.Fatalf("Execute first: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), env, persona, sink); err != nil {
		t.Fatalf("Execute duplicate: %v", err)
	}
	if engine.applyCount != 1 || engine.continueCount != 1 {
		t.Fatalf("apply/continue counts = %d/%d, want 1/1", engine.applyCount, engine.continueCount)
	}
}

func TestHandlerProcessesApprovalActionAndStreamsContinuation(t *testing.T) {
	handler, engine := newTestHandler()
	engine.approvals = []protocol.ApprovalRequest{
		{
			ID:             "approval-1",
			SessionID:      "session-test",
			TaskID:         "task-1",
			Status:         string(protocol.ApprovalStatusPending),
			RejectOptionID: "cancel",
			Options:        []protocol.DecisionOption{{ID: "delete", Summary: "Delete"}, {ID: "cancel", Summary: "Cancel"}},
		},
	}
	engine.approvalDeltas = []string{"处理", "完成"}
	engine.approvalReply = "处理完成"

	conn := dialTestWS(t, handler)
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(greeting): %v", err)
	}

	if err := wsjson.Write(context.Background(), conn, WSMessage{
		Type:      "approval_action",
		RequestID: "approval-1",
		Action:    "approve",
		OptionID:  "delete",
	}); err != nil {
		t.Fatalf("Write(approval_action): %v", err)
	}

	var types []string
	var deltas []string
	for len(types) < 5 {
		if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
			t.Fatalf("Read(stream): %v", err)
		}
		types = append(types, msg.Type)
		if msg.Type == "stream_delta" {
			deltas = append(deltas, msg.Content)
		}
	}

	want := []string{"approval_updated", "stream_start", "stream_delta", "stream_delta", "stream_end"}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("types[%d] = %q, want %q (all=%#v)", i, types[i], want[i], types)
		}
	}
	if engine.lastAction != "approve" || engine.lastActionReq != "approval-1" || engine.lastActionOpt != "delete" {
		t.Fatalf("approval action = %q/%q/%q, want approve/approval-1/delete", engine.lastAction, engine.lastActionReq, engine.lastActionOpt)
	}
	if len(deltas) != 2 || deltas[0] != "处理" || deltas[1] != "完成" {
		t.Fatalf("deltas = %#v, want [处理 完成]", deltas)
	}
}

func TestHandlerEmitsApprovalRequiredWithBindingPreviewWithoutRawContent(t *testing.T) {
	handler, engine := newTestHandler()
	engine.sendErr = errApprovalPending
	engine.approvals = []protocol.ApprovalRequest{
		{
			ID:             "approval-1",
			SessionID:      "session-test",
			TaskID:         "task-1",
			Status:         string(protocol.ApprovalStatusPending),
			RejectOptionID: "deny",
			Options:        []protocol.DecisionOption{{ID: "allow", Summary: "允许执行"}, {ID: "deny", Summary: "拒绝"}},
			ToolApprovalBinding: &protocol.ToolApprovalBinding{
				ApprovalKind:        "destructive_write",
				ToolName:            "write_file",
				NormalizedInputHash: "sha256:input",
				PathDigest:          "sha256:path",
				InputPreview:        "path=config/.env, content_bytes=12",
			},
		},
	}

	conn := dialTestWS(t, handler)
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(session_ready): %v", err)
	}
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(greeting): %v", err)
	}
	if err := wsjson.Write(context.Background(), conn, WSMessage{Type: "message", Content: "请写入 SECRET=value"}); err != nil {
		t.Fatalf("Write(message): %v", err)
	}

	var approval *protocol.ApprovalRequest
	for i := 0; i < 3; i++ {
		if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
			t.Fatalf("Read(stream): %v", err)
		}
		if msg.Type == "approval_required" {
			approval = msg.Approval
			break
		}
	}
	if approval == nil || approval.ToolApprovalBinding == nil {
		t.Fatalf("approval event = %#v, want binding", msg)
	}
	if approval.ToolApprovalBinding.ApprovalKind != "destructive_write" || approval.ToolApprovalBinding.ToolName != "write_file" || approval.ToolApprovalBinding.NormalizedInputHash == "" || approval.ToolApprovalBinding.PathDigest == "" {
		t.Fatalf("binding = %#v, want tool/hash/path digest", approval.ToolApprovalBinding)
	}
	if strings.Contains(approval.ToolApprovalBinding.InputPreview, "SECRET=value") {
		t.Fatalf("input preview leaks raw content: %q", approval.ToolApprovalBinding.InputPreview)
	}
}

func newTestHandler() (*Handler, *fakeConversationEngine) {
	return newTestHandlerWithApp(&fakeAppProvider{
		defaultPersona: "default",
		personas: map[string]*config.Persona{
			"default": {
				Name:     "default",
				Greeting: "Hello from Emo",
			},
		},
	})
}

func newTestHandlerWithOptions(options ...HandlerOption) (*Handler, *fakeConversationEngine) {
	engine := &fakeConversationEngine{}
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	app := &fakeAppProvider{
		defaultPersona: "default",
		personas: map[string]*config.Persona{
			"default": {
				Name:     "default",
				Greeting: "Hello from Emo",
			},
		},
	}
	return NewHandler(engine, app, logger, options...), engine
}

func newTestHandlerWithApp(app *fakeAppProvider) (*Handler, *fakeConversationEngine) {
	engine := &fakeConversationEngine{}
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewHandler(engine, app, logger), engine
}

func dialTestWS(t *testing.T, handler *Handler, path ...string) *websocket.Conn {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	targetPath := "/"
	if len(path) > 0 && path[0] != "" {
		targetPath = path[0]
	}
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + targetPath
	conn, _, err := websocket.Dial(context.Background(), url, nil)
	if err != nil {
		t.Fatalf("Dial(%s): %v", fmt.Sprintf("%s", url), err)
	}
	return conn
}
