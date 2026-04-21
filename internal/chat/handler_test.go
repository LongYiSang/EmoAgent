package chat

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/storage"
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
	deltas         []string
	history        []storage.MessageRecord
	sendHook       func(context.Context)
	approvals      []protocol.ApprovalRequest
	lastAction     string
	lastActionReq  string
	lastActionOpt  string
	approvalReply  string
	approvalDeltas []string
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
	if f.sendHook != nil {
		f.sendHook(ctx)
	}
	f.sendSession = sessionID
	f.sendPersona = persona
	f.sendContent = userContent
	for _, delta := range f.deltas {
		cb(delta)
	}
	return f.sendReply, f.sendErr
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
	for len(types) < 4 {
		if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
			t.Fatalf("Read(stream): %v", err)
		}
		types = append(types, msg.Type)
		if msg.Type == "work_progress" {
			progressText = msg.Content
		}
	}

	want := []string{"stream_start", "work_progress", "work_progress_end", "stream_end"}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("types[%d]=%q, want %q (all=%#v)", i, types[i], want[i], types)
		}
	}
	if progressText != "processing..." {
		t.Fatalf("progress text = %q, want processing...", progressText)
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
