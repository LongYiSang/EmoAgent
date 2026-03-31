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
	"github.com/longyisang/emoagent/internal/storage"
)

type fakeConversationEngine struct {
	startPersona  string
	resumeID      string
	resumeOK      bool
	resumePersona string
	sessionID     string
	sendReply     string
	sendErr       error
	sendSession   string
	sendPersona   *config.Persona
	sendContent   string
	deltas        []string
	history       []storage.MessageRecord
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

func (f *fakeConversationEngine) SendMessage(_ context.Context, sessionID string, persona *config.Persona, userContent string, cb func(delta string)) (string, error) {
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

func TestHandlerRestoresHistoryWithoutGreetingWhenSessionResumes(t *testing.T) {
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
	if err := wsjson.Read(context.Background(), conn, &msg); err != nil {
		t.Fatalf("Read(history): %v", err)
	}
	if msg.Type != "history" {
		t.Fatalf("Type = %q, want history", msg.Type)
	}
	if len(msg.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want 2", len(msg.Messages))
	}
	if msg.Messages[0].Content != "hello" || msg.Messages[1].Content != "hi there" {
		t.Fatalf("Messages = %#v, want restored history", msg.Messages)
	}
	if engine.resumeID != "session-restored" {
		t.Fatalf("resumeID = %q, want session-restored", engine.resumeID)
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
