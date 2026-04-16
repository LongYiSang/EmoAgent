package chat

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tool"
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

func TestEngineUpdateConfigAffectsSubsequentMessages(t *testing.T) {
	firstClient := &fakeLLMClient{
		response: &llm.ChatResponse{ID: "resp-1", Content: "first", Model: "model-a"},
	}
	secondClient := &fakeLLMClient{
		response: &llm.ChatResponse{ID: "resp-2", Content: "second", Model: "model-b"},
	}
	engine, _, _ := newTestEngine(t, firstClient)

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	if _, err := engine.SendMessage(context.Background(), sessionID, persona, "before update", nil); err != nil {
		t.Fatalf("SendMessage(before): %v", err)
	}

	engine.UpdateConfig(secondClient, "openai", "model-b", 1024, 0.9)

	if _, err := engine.SendMessage(context.Background(), sessionID, persona, "after update", nil); err != nil {
		t.Fatalf("SendMessage(after): %v", err)
	}

	if secondClient.lastRequest.Model != "model-b" {
		t.Fatalf("lastRequest.Model = %q, want model-b", secondClient.lastRequest.Model)
	}
	if secondClient.lastRequest.MaxTokens != 1024 {
		t.Fatalf("lastRequest.MaxTokens = %d, want 1024", secondClient.lastRequest.MaxTokens)
	}
	if secondClient.lastRequest.Temperature != 0.9 {
		t.Fatalf("lastRequest.Temperature = %v, want 0.9", secondClient.lastRequest.Temperature)
	}
}

func TestEngineResumeSessionRequiresMatchingPersona(t *testing.T) {
	engine, db, _ := newTestEngine(t, &fakeLLMClient{})
	ctx := context.Background()

	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	resumedID, ok, err := engine.ResumeSession(ctx, sessionID, "default")
	if err != nil {
		t.Fatalf("ResumeSession(match): %v", err)
	}
	if !ok || resumedID != sessionID {
		t.Fatalf("ResumeSession(match) = (%q, %v), want (%q, true)", resumedID, ok, sessionID)
	}

	resumedID, ok, err = engine.ResumeSession(ctx, sessionID, "neko")
	if err != nil {
		t.Fatalf("ResumeSession(mismatch): %v", err)
	}
	if ok || resumedID != "" {
		t.Fatalf("ResumeSession(mismatch) = (%q, %v), want ('', false)", resumedID, ok)
	}

	if _, err := db.GetSession(ctx, sessionID); err != nil {
		t.Fatalf("GetSession: %v", err)
	}
}

func TestEngineGetHistoryReturnsRecentMessages(t *testing.T) {
	engine, db, _ := newTestEngine(t, &fakeLLMClient{})
	ctx := context.Background()

	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	for i, msg := range []struct {
		id      string
		role    string
		content string
	}{
		{id: "msg-1", role: "user", content: "hello"},
		{id: "msg-2", role: "assistant", content: "hi"},
		{id: "msg-3", role: "user", content: "again"},
	} {
		if err := db.AddMessage(ctx, msg.id, sessionID, msg.role, msg.content); err != nil {
			t.Fatalf("AddMessage(%d): %v", i, err)
		}
	}

	history, err := engine.GetHistory(ctx, sessionID, 2)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("len(history) = %d, want 2", len(history))
	}
	if history[0].Content != "hi" || history[1].Content != "again" {
		t.Fatalf("history = %#v, want [hi again]", history)
	}
}

func TestEngineSendMessageUsesAssemblerInsteadOfRecentMessagesOnly(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		response: &llm.ChatResponse{ID: "resp-asm", Content: "ok", StopReason: "end_turn"},
	}
	engine, db, _ := newTestEngine(t, fakeLLM)
	ctx := context.Background()

	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	for _, msg := range []struct {
		id      string
		role    string
		content string
	}{
		{id: "m1", role: "user", content: "first user"},
		{id: "m2", role: "assistant", content: "first answer"},
		{id: "m3", role: "user", content: "second user"},
		{id: "m4", role: "assistant", content: "second answer"},
	} {
		if err := db.AddMessage(ctx, msg.id, sessionID, msg.role, msg.content); err != nil {
			t.Fatalf("AddMessage(%s): %v", msg.id, err)
		}
	}

	engine.contextCfg.KeepRecentUserTurns = 1
	persona := &config.Persona{Name: "default", SystemPrompt: "You are warm."}
	if _, err := engine.SendMessage(ctx, sessionID, persona, "latest user", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if len(fakeLLM.lastRequest.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(fakeLLM.lastRequest.Messages))
	}
	if fakeLLM.lastRequest.Messages[0].Content != "latest user" {
		t.Fatalf("Messages[0] = %#v, want latest user only", fakeLLM.lastRequest.Messages[0])
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
		LLM:         client,
		DB:          db,
		Logger:      logger,
		Model:       "test-model",
		MaxTokens:   256,
		Temperature: 0.2,
		ContextConfig: config.ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  6,
			ToolResultSoftTokens: 1000,
			ToolResultHardTokens: 3000,
		},
	})

	return engine, db, logger
}

// --- Tool loop tests ---

// toolLoopLLMClient simulates an LLM that returns tool_use on the first call
// and then end_turn with the final reply on the second call.
type toolLoopLLMClient struct {
	callCount int
	requests  []llm.ChatRequest
}

func (c *toolLoopLLMClient) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	panic("unexpected Chat call")
}

func (c *toolLoopLLMClient) ChatStream(_ context.Context, req llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	c.callCount++
	c.requests = append(c.requests, req)

	if c.callCount == 1 {
		// First call: return tool_use.
		if cb != nil {
			cb(llm.StreamEvent{Content: "Let me check the current time."})
			cb(llm.StreamEvent{Done: true})
		}
		return &llm.ChatResponse{
			ID:         "resp-tool",
			Content:    "",
			StopReason: "tool_use",
			ContentBlocks: []llm.ContentBlock{
				{Type: "tool_use", ID: "call_1", Name: "get_current_time", Input: json.RawMessage(`{}`)},
			},
		}, nil
	}

	// Second call: return final text response.
	finalText := "It's 17:00 now!"
	if cb != nil {
		cb(llm.StreamEvent{Content: finalText})
		cb(llm.StreamEvent{Done: true})
	}
	return &llm.ChatResponse{
		ID:         "resp-final",
		Content:    finalText,
		StopReason: "end_turn",
		ContentBlocks: []llm.ContentBlock{
			{Type: "text", Text: finalText},
		},
	}, nil
}

func TestEngineToolLoopExecutesToolAndReturnsResponse(t *testing.T) {
	mockLLM := &toolLoopLLMClient{}

	// Set up registry with a simple get_current_time tool.
	registry := tool.NewRegistry()
	registry.Register(tool.Spec{
		Name:        "get_current_time",
		Description: "Get current time",
		Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Scope:       tool.ScopeBoth,
		Permission:  tool.PermReadOnly,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"current_time":"17:00:00","timezone":"CST"}`), nil
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "chat.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	db, err := storage.Open(path, logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	dispatcher := tool.NewDispatcher(registry, tool.MinimalSchemaValidator{}, logger)

	engine := NewEngine(EngineConfig{
		LLM:         mockLLM,
		DB:          db,
		Logger:      logger,
		Model:       "test-model",
		MaxTokens:   256,
		Temperature: 0.2,
		ContextConfig: config.ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  6,
			ToolResultSoftTokens: 1000,
			ToolResultHardTokens: 3000,
		},
		Provider:   "openai",
		Registry:   registry,
		Dispatcher: dispatcher,
	})

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	persona := &config.Persona{Name: "default", SystemPrompt: "You are warm."}

	var streamed []string
	reply, err := engine.SendMessage(context.Background(), sessionID, persona, "What time is it?", func(delta string) {
		streamed = append(streamed, delta)
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	// Verify final reply.
	if reply != "It's 17:00 now!" {
		t.Fatalf("reply = %q, want %q", reply, "It's 17:00 now!")
	}

	// Verify LLM was called twice (tool_use → end_turn).
	if mockLLM.callCount != 2 {
		t.Fatalf("LLM call count = %d, want 2", mockLLM.callCount)
	}

	// Verify streaming delivered only the final text.
	if len(streamed) != 1 || streamed[0] != "It's 17:00 now!" {
		t.Fatalf("streamed = %v, want [\"It's 17:00 now!\"]", streamed)
	}

	// Verify DB has only user message + final assistant text (not intermediate tool messages).
	messages, err := db.GetRecentMessages(context.Background(), sessionID, 10)
	if err != nil {
		t.Fatalf("GetRecentMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2 (user + assistant)", len(messages))
	}
	if messages[0].Role != "user" || messages[0].Content != "What time is it?" {
		t.Fatalf("messages[0] = %+v, want user message", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].Content != "It's 17:00 now!" {
		t.Fatalf("messages[1] = %+v, want final assistant message", messages[1])
	}
}

func TestEngineSendMessageDoesNotAdvertiseToolsWithoutDispatcher(t *testing.T) {
	mockLLM := &fakeLLMClient{
		response: &llm.ChatResponse{ID: "resp-plain", Content: "No tools", StopReason: "end_turn"},
	}
	registry := tool.NewRegistry()
	registry.Register(tool.Spec{
		Name:        "get_current_time",
		Description: "Get current time",
		Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Scope:       tool.ScopeBoth,
		Permission:  tool.PermReadOnly,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"current_time":"17:00:00","timezone":"CST"}`), nil
	})

	engine, _, _ := newTestEngine(t, mockLLM)
	engine.registry = registry
	engine.dispatcher = nil

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	persona := &config.Persona{Name: "default", SystemPrompt: "You are warm."}

	if _, err := engine.SendMessage(context.Background(), sessionID, persona, "What time is it?", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if len(mockLLM.lastRequest.Tools) != 0 {
		t.Fatalf("Tools = %#v, want none when dispatcher is nil", mockLLM.lastRequest.Tools)
	}
}

func TestEngineToolLoopSnipsLargeToolResult(t *testing.T) {
	mockLLM := &toolLoopLLMClient{}

	registry := tool.NewRegistry()
	registry.Register(tool.Spec{
		Name:        "get_current_time",
		Description: "Get current time",
		Parameters:  json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Scope:       tool.ScopeBoth,
		Permission:  tool.PermReadOnly,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"body":"` + strings.Repeat("x", 20000) + `"}`), nil
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "chat.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	db, err := storage.Open(path, logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	dispatcher := tool.NewDispatcher(registry, tool.MinimalSchemaValidator{}, logger)

	engine := NewEngine(EngineConfig{
		LLM:         mockLLM,
		DB:          db,
		Logger:      logger,
		Model:       "test-model",
		MaxTokens:   256,
		Temperature: 0.2,
		ContextConfig: config.ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  6,
			ToolResultSoftTokens: 10,
			ToolResultHardTokens: 20,
		},
		Provider:   "openai",
		Registry:   registry,
		Dispatcher: dispatcher,
	})

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	persona := &config.Persona{Name: "default", SystemPrompt: "You are warm."}
	if _, err := engine.SendMessage(context.Background(), sessionID, persona, "What time is it?", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if len(mockLLM.requests) != 2 {
		t.Fatalf("len(requests) = %d, want 2", len(mockLLM.requests))
	}
	second := mockLLM.requests[1]
	last := second.Messages[len(second.Messages)-1]
	if !strings.Contains(last.Content, `"is_truncated":true`) {
		t.Fatalf("tool result content = %q, want truncated digest JSON", last.Content)
	}
	if strings.Contains(last.Content, strings.Repeat("x", 1000)) {
		t.Fatal("tool result content still contains raw payload")
	}
}
