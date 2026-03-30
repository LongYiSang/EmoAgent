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
)

type fakeConversationEngine struct {
	startPersona string
	sessionID    string
	sendReply    string
	sendErr      error
	sendSession  string
	sendPersona  *config.Persona
	sendContent  string
	deltas       []string
}

func (f *fakeConversationEngine) StartSession(_ context.Context, personaName string) (string, error) {
	f.startPersona = personaName
	if f.sessionID == "" {
		f.sessionID = "session-test"
	}
	return f.sessionID, nil
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

type fakeAppProvider struct {
	defaultPersona string
	persona        *config.Persona
}

func (f *fakeAppProvider) GetPersona(name string) (*config.Persona, bool) {
	if f.persona == nil || name != f.persona.Name {
		return nil, false
	}
	return f.persona, true
}

func (f *fakeAppProvider) GetDefaultPersonaName() string {
	return f.defaultPersona
}

func TestHandlerSendsGreetingOnConnect(t *testing.T) {
	handler, _ := newTestHandler()
	conn := dialTestWS(t, handler)
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	var msg WSMessage
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

func newTestHandler() (*Handler, *fakeConversationEngine) {
	engine := &fakeConversationEngine{}
	app := &fakeAppProvider{
		defaultPersona: "default",
		persona: &config.Persona{
			Name:     "default",
			Greeting: "Hello from Emo",
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewHandler(engine, app, logger), engine
}

func dialTestWS(t *testing.T, handler *Handler) *websocket.Conn {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.Dial(context.Background(), url, nil)
	if err != nil {
		t.Fatalf("Dial(%s): %v", fmt.Sprintf("%s", url), err)
	}
	return conn
}
