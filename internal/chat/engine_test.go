package chat

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
)

type fakeLLMClient struct {
	lastRequest llm.ChatRequest
	response    *llm.ChatResponse
	err         error
	deltas      []string
}

func (f *fakeLLMClient) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	panic("unexpected Chat call")
}

func (f *fakeLLMClient) ChatStream(_ context.Context, req llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	f.lastRequest = req
	for _, delta := range f.deltas {
		if cb != nil {
			cb(llm.StreamEvent{Content: delta})
		}
	}
	if cb != nil {
		cb(llm.StreamEvent{Done: true})
	}
	return f.response, f.err
}

func TestEngineStartSessionPersistsSession(t *testing.T) {
	engine, db, _ := newTestEngine(t, &fakeLLMClient{})

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if sessionID == "" {
		t.Fatal("StartSession returned empty session id")
	}

	session, err := db.GetSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session == nil {
		t.Fatal("session not persisted")
	}
	if session.Persona != "default" {
		t.Fatalf("session.Persona = %q, want default", session.Persona)
	}
}

func TestEngineSendMessageStreamsAndPersistsConversation(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		deltas: []string{"Hi", " there"},
		response: &llm.ChatResponse{
			ID:      "resp-1",
			Content: "Hi there",
			Model:   "test-model",
		},
	}
	engine, db, _ := newTestEngine(t, fakeLLM)

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := db.AddMessage(context.Background(), "earlier-user", sessionID, "user", "Earlier question"); err != nil {
		t.Fatalf("AddMessage(user): %v", err)
	}
	if err := db.AddMessage(context.Background(), "earlier-assistant", sessionID, "assistant", "Earlier answer"); err != nil {
		t.Fatalf("AddMessage(assistant): %v", err)
	}

	persona := &config.Persona{
		Name:         "default",
		SystemPrompt: "You are warm.",
	}

	var streamed []string
	reply, err := engine.SendMessage(context.Background(), sessionID, persona, "How are you?", func(delta string) {
		streamed = append(streamed, delta)
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if reply != "Hi there" {
		t.Fatalf("reply = %q, want %q", reply, "Hi there")
	}
	if len(streamed) != 2 || streamed[0] != "Hi" || streamed[1] != " there" {
		t.Fatalf("streamed = %#v, want [Hi \" there\"]", streamed)
	}

	if fakeLLM.lastRequest.System != "You are warm." {
		t.Fatalf("System = %q, want %q", fakeLLM.lastRequest.System, "You are warm.")
	}
	if fakeLLM.lastRequest.Model != "test-model" {
		t.Fatalf("Model = %q, want test-model", fakeLLM.lastRequest.Model)
	}
	if len(fakeLLM.lastRequest.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(fakeLLM.lastRequest.Messages))
	}
	if fakeLLM.lastRequest.Messages[0].Content != "Earlier question" {
		t.Fatalf("Messages[0] = %#v, want Earlier question first", fakeLLM.lastRequest.Messages[0])
	}
	if fakeLLM.lastRequest.Messages[2].Content != "How are you?" {
		t.Fatalf("Messages[2] = %#v, want current user message last", fakeLLM.lastRequest.Messages[2])
	}

	messages, err := db.GetRecentMessages(context.Background(), sessionID, 10)
	if err != nil {
		t.Fatalf("GetRecentMessages: %v", err)
	}
	if len(messages) != 4 {
		t.Fatalf("len(messages) = %d, want 4", len(messages))
	}
	if messages[2].Role != "user" || messages[2].Content != "How are you?" {
		t.Fatalf("messages[2] = %#v, want persisted user message", messages[2])
	}
	if messages[3].Role != "assistant" || messages[3].Content != "Hi there" {
		t.Fatalf("messages[3] = %#v, want persisted assistant message", messages[3])
	}
}

func newTestEngine(t *testing.T, client llm.Client) (*Engine, *storage.DB, *slog.Logger) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "chat.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	db, err := storage.Open(path, logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	engine := NewEngine(EngineConfig{
		LLM:          client,
		DB:           db,
		Logger:       logger,
		Model:        "test-model",
		MaxTokens:    256,
		Temperature:  0.2,
		HistoryLimit: 20,
	})

	return engine, db, logger
}
