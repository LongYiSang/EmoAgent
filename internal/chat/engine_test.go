package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/work"
)

type fakeLLMClient struct {
	lastRequest  llm.ChatRequest
	chatRequests []llm.ChatRequest
	chatResponse *llm.ChatResponse
	chatErr      error
	response     *llm.ChatResponse
	err          error
	deltas       []string
}

func (f *fakeLLMClient) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	f.chatRequests = append(f.chatRequests, req)
	if f.chatResponse != nil || f.chatErr != nil {
		return f.chatResponse, f.chatErr
	}
	return &llm.ChatResponse{
		ID:      "summary-1",
		Model:   req.Model,
		Content: `{"running_summary":{"session_goal":"summarized"}}`,
	}, nil
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

type reactiveRetryLLMClient struct {
	requests []llm.ChatRequest
	errs     []error
	response *llm.ChatResponse
}

func (c *reactiveRetryLLMClient) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	panic("unexpected summary Chat call")
}

func (c *reactiveRetryLLMClient) ChatStream(_ context.Context, req llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	c.requests = append(c.requests, req)
	call := len(c.requests) - 1
	if call < len(c.errs) && c.errs[call] != nil {
		return nil, c.errs[call]
	}
	if cb != nil {
		cb(llm.StreamEvent{Done: true})
	}
	return c.response, nil
}

type reactiveToolLoopLLMClient struct {
	requests []llm.ChatRequest
}

func (c *reactiveToolLoopLLMClient) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	panic("unexpected summary Chat call")
}

func (c *reactiveToolLoopLLMClient) ChatStream(_ context.Context, req llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	c.requests = append(c.requests, req)
	switch len(c.requests) {
	case 1:
		return nil, &llm.Error{Kind: llm.ErrorKindContextOverflow, Provider: "openai", Operation: "chat_stream", StatusCode: 400, Message: "prompt too long"}
	case 2:
		if cb != nil {
			cb(llm.StreamEvent{Done: true})
		}
		return &llm.ChatResponse{
			ID:         "resp-tool",
			StopReason: "tool_use",
			ContentBlocks: []llm.ContentBlock{
				{Type: "tool_use", ID: "call_1", Name: "get_current_time", Input: json.RawMessage(`{}`)},
			},
		}, nil
	default:
		if cb != nil {
			cb(llm.StreamEvent{Content: "final"})
			cb(llm.StreamEvent{Done: true})
		}
		return &llm.ChatResponse{
			ID:         "resp-final",
			Content:    "final",
			StopReason: "end_turn",
			ContentBlocks: []llm.ContentBlock{
				{Type: "text", Text: "final"},
			},
		}, nil
	}
}

type scriptedEngineClient struct {
	responses []*llm.ChatResponse
	requests  []llm.ChatRequest
	index     int
}

func (c *scriptedEngineClient) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	// Summary call
	c.requests = append(c.requests, req)
	return &llm.ChatResponse{
		ID:      "summary-1",
		Model:   req.Model,
		Content: `{"running_summary":{"session_goal":"summary"}}`,
	}, nil
}

func (c *scriptedEngineClient) ChatStream(_ context.Context, req llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	c.requests = append(c.requests, req)
	if c.index >= len(c.responses) {
		return nil, errors.New("scriptedEngineClient: no scripted response")
	}
	resp := c.responses[c.index]
	c.index++
	if cb != nil {
		if resp.Content != "" {
			cb(llm.StreamEvent{Content: resp.Content})
		}
		cb(llm.StreamEvent{Done: true})
	}
	return resp, nil
}

func toolUseResponse(callID, name, input string) *llm.ChatResponse {
	return &llm.ChatResponse{
		ID:         "resp-tool-" + callID,
		StopReason: "tool_use",
		ContentBlocks: []llm.ContentBlock{
			{
				Type:  "tool_use",
				ID:    callID,
				Name:  name,
				Input: json.RawMessage(input),
			},
		},
	}
}

func endTurnResponse(text string) *llm.ChatResponse {
	return &llm.ChatResponse{
		ID:         "resp-end",
		Content:    text,
		StopReason: "end_turn",
		ContentBlocks: []llm.ContentBlock{
			{Type: "text", Text: text},
		},
	}
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

	if !strings.HasPrefix(fakeLLM.lastRequest.System, "You are warm.") {
		t.Fatalf("System = %q, want prefix %q", fakeLLM.lastRequest.System, "You are warm.")
	}
	if !strings.Contains(fakeLLM.lastRequest.System, "Delegation Guideline") {
		t.Fatalf("System = %q, want Delegation Guideline section", fakeLLM.lastRequest.System)
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

	engine.UpdateConfig(secondClient, "openai", "model-b", "summary-b", 1024, 0.9, config.ContextConfig{
		InputBudgetTokens:    12000,
		SoftCompactRatio:     0.70,
		HardCompactRatio:     0.90,
		ReserveOutputTokens:  2048,
		KeepRecentUserTurns:  2,
		ToolResultSoftTokens: 50,
		ToolResultHardTokens: 100,
	})

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
	if engine.summaryModel != "summary-b" {
		t.Fatalf("summaryModel = %q, want summary-b", engine.summaryModel)
	}
	if engine.contextCfg.KeepRecentUserTurns != 2 {
		t.Fatalf("contextCfg.KeepRecentUserTurns = %d, want 2", engine.contextCfg.KeepRecentUserTurns)
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

	if len(fakeLLM.lastRequest.Messages) != 2 {
		t.Fatalf("len(Messages) = %d, want 2 (running summary + latest user)", len(fakeLLM.lastRequest.Messages))
	}
	if !strings.Contains(fakeLLM.lastRequest.Messages[0].Content, `"running_summary"`) {
		t.Fatalf("Messages[0] = %#v, want running summary envelope first", fakeLLM.lastRequest.Messages[0])
	}
	if fakeLLM.lastRequest.Messages[1].Content != "latest user" {
		t.Fatalf("Messages[1] = %#v, want latest user last", fakeLLM.lastRequest.Messages[1])
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
		SummaryModel: "summary-model",
		MaxTokens:    256,
		Temperature:  0.2,
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

func TestSummaryModelFallsBackToPrimaryModelWhenEmpty(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		response: &llm.ChatResponse{ID: "resp-1", Content: "ok", StopReason: "end_turn"},
	}
	engine, db, _ := newTestEngine(t, fakeLLM)
	engine.contextCfg.KeepRecentUserTurns = 1
	engine.summaryModel = ""

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
		{id: "m1", role: "user", content: "old user"},
		{id: "m2", role: "assistant", content: "old assistant"},
	} {
		if err := db.AddMessage(ctx, msg.id, sessionID, msg.role, msg.content); err != nil {
			t.Fatalf("AddMessage(%s): %v", msg.id, err)
		}
	}

	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	if _, err := engine.SendMessage(ctx, sessionID, persona, "latest user", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	if len(fakeLLM.chatRequests) == 0 {
		t.Fatal("summary Chat was not invoked")
	}
	if fakeLLM.chatRequests[0].Model != "test-model" {
		t.Fatalf("summary request model = %q, want fallback test-model", fakeLLM.chatRequests[0].Model)
	}
}

func TestSummaryFailureFallsBackWithoutBlockingChat(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		chatErr:  errors.New("summary unavailable"),
		response: &llm.ChatResponse{ID: "resp-1", Content: "ok", StopReason: "end_turn"},
	}
	engine, db, _ := newTestEngine(t, fakeLLM)
	engine.contextCfg.KeepRecentUserTurns = 1

	ctx := context.Background()
	sessionID, err := engine.StartSession(ctx, "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := db.AddMessage(ctx, "old-user", sessionID, "user", "old user"); err != nil {
		t.Fatalf("AddMessage(old-user): %v", err)
	}
	if err := db.AddMessage(ctx, "old-assistant", sessionID, "assistant", "old assistant"); err != nil {
		t.Fatalf("AddMessage(old-assistant): %v", err)
	}
	if _, err := db.SqlDB().ExecContext(ctx, `UPDATE sessions SET metadata = ? WHERE id = ?`, "{bad json", sessionID); err != nil {
		t.Fatalf("corrupt session metadata: %v", err)
	}

	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	reply, err := engine.SendMessage(ctx, sessionID, persona, "latest user", nil)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if reply != "ok" {
		t.Fatalf("reply = %q, want ok", reply)
	}
	if len(fakeLLM.chatRequests) == 0 {
		t.Fatal("summary Chat was not attempted")
	}
	if len(fakeLLM.lastRequest.Messages) != 1 || fakeLLM.lastRequest.Messages[0].Content != "latest user" {
		t.Fatalf("Messages = %#v, want chat to continue with recent turn only", fakeLLM.lastRequest.Messages)
	}
}

func TestSummaryDoesNotPolluteVisibleHistory(t *testing.T) {
	fakeLLM := &fakeLLMClient{
		chatResponse: &llm.ChatResponse{
			ID:      "summary-1",
			Model:   "summary-model",
			Content: `{"running_summary":{"session_goal":"summarized old history"}}`,
		},
		response: &llm.ChatResponse{ID: "resp-1", Content: "ok", StopReason: "end_turn"},
	}
	engine, db, _ := newTestEngine(t, fakeLLM)
	engine.contextCfg.KeepRecentUserTurns = 1
	engine.summaryModel = "summary-model"

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
		{id: "m1", role: "user", content: "old user"},
		{id: "m2", role: "assistant", content: "old assistant"},
	} {
		if err := db.AddMessage(ctx, msg.id, sessionID, msg.role, msg.content); err != nil {
			t.Fatalf("AddMessage(%s): %v", msg.id, err)
		}
	}

	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	if _, err := engine.SendMessage(ctx, sessionID, persona, "latest user", nil); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	history, err := db.GetAllMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	if len(history) != 4 {
		t.Fatalf("len(history) = %d, want 4 visible user/assistant messages only", len(history))
	}
	for _, msg := range history {
		if msg.Role != "user" && msg.Role != "assistant" {
			t.Fatalf("unexpected visible role %q in history", msg.Role)
		}
		if strings.Contains(msg.Content, "running_summary") {
			t.Fatalf("visible history polluted by summary message: %#v", msg)
		}
	}
}

func TestReactiveCompactRetriesOnceOnOverflow(t *testing.T) {
	client := &reactiveRetryLLMClient{
		errs: []error{
			&llm.Error{Kind: llm.ErrorKindContextOverflow, Provider: "openai", Operation: "chat_stream", StatusCode: 400, Message: "prompt too long"},
		},
		response: &llm.ChatResponse{ID: "resp-1", Content: "ok", StopReason: "end_turn"},
	}
	engine, _, _ := newTestEngine(t, client)

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	reply, err := engine.SendMessage(context.Background(), sessionID, persona, "latest user", nil)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if reply != "ok" {
		t.Fatalf("reply = %q, want ok", reply)
	}
	if len(client.requests) != 2 {
		t.Fatalf("len(requests) = %d, want 2", len(client.requests))
	}
}

func TestReactiveCompactDoesNotRetryOnTransportError(t *testing.T) {
	client := &reactiveRetryLLMClient{
		errs: []error{
			&llm.Error{Kind: llm.ErrorKindTransport, Provider: "openai", Operation: "chat_stream", Message: "timeout"},
		},
		response: &llm.ChatResponse{ID: "resp-1", Content: "ok", StopReason: "end_turn"},
	}
	engine, _, _ := newTestEngine(t, client)

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	if _, err := engine.SendMessage(context.Background(), sessionID, persona, "latest user", nil); err == nil {
		t.Fatal("SendMessage should fail on transport error")
	}
	if len(client.requests) != 1 {
		t.Fatalf("len(requests) = %d, want 1", len(client.requests))
	}
}

func TestReactiveCompactStopsAfterSingleRetry(t *testing.T) {
	client := &reactiveRetryLLMClient{
		errs: []error{
			&llm.Error{Kind: llm.ErrorKindContextOverflow, Provider: "openai", Operation: "chat_stream", StatusCode: 400, Message: "prompt too long"},
			&llm.Error{Kind: llm.ErrorKindContextOverflow, Provider: "openai", Operation: "chat_stream", StatusCode: 400, Message: "prompt still too long"},
		},
	}
	engine, _, _ := newTestEngine(t, client)

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	if _, err := engine.SendMessage(context.Background(), sessionID, persona, "latest user", nil); err == nil {
		t.Fatal("SendMessage should fail after the single reactive retry is exhausted")
	}
	if len(client.requests) != 2 {
		t.Fatalf("len(requests) = %d, want 2", len(client.requests))
	}
}

func TestReactiveCompactToolLoopUsesCompactedWorkingSetAfterRetry(t *testing.T) {
	client := &reactiveToolLoopLLMClient{}
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
		LLM:          client,
		DB:           db,
		Logger:       logger,
		Model:        "test-model",
		SummaryModel: "summary-model",
		MaxTokens:    256,
		Temperature:  0.2,
		ContextConfig: config.ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  2,
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
	if err := db.AddMessage(context.Background(), "old-user", sessionID, "user", "old user"); err != nil {
		t.Fatalf("AddMessage(old-user): %v", err)
	}
	if err := db.AddMessage(context.Background(), "old-assistant", sessionID, "assistant", "old assistant"); err != nil {
		t.Fatalf("AddMessage(old-assistant): %v", err)
	}

	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	reply, err := engine.SendMessage(context.Background(), sessionID, persona, "latest user", nil)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if reply != "final" {
		t.Fatalf("reply = %q, want final", reply)
	}
	if len(client.requests) != 3 {
		t.Fatalf("len(requests) = %d, want 3", len(client.requests))
	}
	if len(client.requests[1].Messages) != 1 {
		t.Fatalf("retry messages len = %d, want 1 compacted latest user only", len(client.requests[1].Messages))
	}
	if len(client.requests[2].Messages) != 3 {
		t.Fatalf("post-tool-loop messages len = %d, want 3 compacted working-set messages", len(client.requests[2].Messages))
	}
	if client.requests[2].Messages[0].Content != "latest user" {
		t.Fatalf("post-tool-loop first message = %#v, want latest user", client.requests[2].Messages[0])
	}
}

func TestReactiveCompactRetryFailureLogsCompactContext(t *testing.T) {
	client := &reactiveRetryLLMClient{
		errs: []error{
			&llm.Error{Kind: llm.ErrorKindContextOverflow, Provider: "openai", Operation: "chat_stream", StatusCode: 400, Message: "prompt too long"},
			&llm.Error{Kind: llm.ErrorKindContextOverflow, Provider: "openai", Operation: "chat_stream", StatusCode: 400, Message: "prompt still too long"},
		},
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	db, err := storage.Open(filepath.Join(t.TempDir(), "chat.db"), logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	engine := NewEngine(EngineConfig{
		LLM:          client,
		DB:           db,
		Logger:       logger,
		Model:        "test-model",
		SummaryModel: "summary-model",
		MaxTokens:    256,
		Temperature:  0.2,
		ContextConfig: config.ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  1,
			ToolResultSoftTokens: 1000,
			ToolResultHardTokens: 3000,
		},
	})

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	persona := &config.Persona{Name: "default", SystemPrompt: "system"}
	if _, err := engine.SendMessage(context.Background(), sessionID, persona, "latest user", nil); err == nil {
		t.Fatal("SendMessage should fail after overflow retry also fails")
	}

	logOutput := logs.String()
	if !strings.Contains(logOutput, "reactive compact retry failed") {
		t.Fatalf("logs = %q, want retry failure log message", logOutput)
	}
	if !strings.Contains(logOutput, "retry_attempt=1") {
		t.Fatalf("logs = %q, want retry_attempt field", logOutput)
	}
	if !strings.Contains(logOutput, "error_kind=context_overflow") {
		t.Fatalf("logs = %q, want error_kind=context_overflow", logOutput)
	}
	if !strings.Contains(logOutput, "compact_reason=reactive_overflow") {
		t.Fatalf("logs = %q, want compact_reason=reactive_overflow", logOutput)
	}
}

func TestEnginePendingDecisionChainAcrossTurns(t *testing.T) {
	llmClient := &scriptedEngineClient{
		responses: []*llm.ChatResponse{
			// Turn 1: delegate_to_work -> needs_emotion_decision, then ask user
			toolUseResponse("call_delegate", "delegate_to_work", `{"goal":"delete finish files","permission_scope":"workspace-write"}`),
			endTurnResponse("我需要你确认是否继续执行删除操作。"),
			// Turn 2: resume_work -> task report, then final assistant text
			toolUseResponse("call_resume", "resume_work", `{"task_id":"task-decision-1","decision":"confirm_delete","reason":"用户已确认"}`),
			endTurnResponse("已完成处理，目标文件已删除。"),
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "chat.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	db, err := storage.Open(path, logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	registry := tool.NewRegistry()
	dispatcher := tool.NewDispatcher(registry, tool.MinimalSchemaValidator{}, logger)
	pending := work.NewPendingRegistry(time.Hour)

	var delegateSessionID string
	var resumeSessionID string
	const pausedTaskID = "task-decision-1"

	registry.Register(tool.Spec{
		Name:        "delegate_to_work",
		Description: "test delegate",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"goal":{"type":"string"},"permission_scope":{"type":"string"}},"required":["goal","permission_scope"],"additionalProperties":false}`),
		Scope:       tool.ScopeEmotion,
		Permission:  tool.PermReadOnly,
	}, func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		delegateSessionID = work.SessionIDFromContext(ctx)
		packet := protocol.DecisionPacket{
			TaskID:               pausedTaskID,
			Category:             protocol.CatHighRisk,
			RiskLevel:            "medium",
			GoalSummary:          "删除 docs/todo 下 [finish] 文件",
			Question:             "是否确认执行删除？",
			WhyBlocked:           "这是高风险不可逆操作",
			Options:              []protocol.DecisionOption{{ID: "confirm_delete", Summary: "确认删除"}, {ID: "cancel", Summary: "取消"}},
			RelevantFindings:     []protocol.DecisionEvidence{{Finding: "已定位到 4 个待删除文件", Source: "list_dir"}},
			KeyTradeoffs:         []protocol.DecisionTradeoff{{Dimension: "风险", Note: "删除后不可恢复"}},
			RecommendedOption:    "confirm_delete",
			RecommendationReason: "用户请求清理已完成文件",
			SuggestsUserInput:    true,
			CreatedAt:            time.Now().UTC(),
		}
		pending.Put(delegateSessionID, pausedTaskID, &work.PausedWork{
			TaskID:    pausedTaskID,
			Packet:    packet,
			CreatedAt: time.Now().UTC(),
		})
		return json.Marshal(work.NeedsEmotionDecision{
			Status:         "needs_emotion_decision",
			TaskID:         pausedTaskID,
			DecisionPacket: packet,
		})
	})

	registry.Register(tool.Spec{
		Name:        "resume_work",
		Description: "test resume",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"},"decision":{"type":"string"},"reason":{"type":"string"}},"required":["task_id","decision"],"additionalProperties":false}`),
		Scope:       tool.ScopeEmotion,
		Permission:  tool.PermReadOnly,
	}, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		resumeSessionID = work.SessionIDFromContext(ctx)
		var req struct {
			TaskID   string `json:"task_id"`
			Decision string `json:"decision"`
		}
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, err
		}
		if req.TaskID != pausedTaskID {
			t.Fatalf("resume_work task_id = %q, want %q", req.TaskID, pausedTaskID)
		}
		if req.Decision != "confirm_delete" {
			t.Fatalf("resume_work decision = %q, want confirm_delete", req.Decision)
		}
		_ = pending.Take(resumeSessionID, req.TaskID)
		return json.Marshal(protocol.TaskReport{
			TaskID:    req.TaskID,
			Status:    "completed",
			Goal:      "删除 docs/todo 下 [finish] 文件",
			Summary:   "删除完成",
			CreatedAt: time.Now().UTC(),
		})
	})

	engine := NewEngine(EngineConfig{
		LLM:          llmClient,
		DB:           db,
		Logger:       logger,
		Model:        "test-model",
		SummaryModel: "summary-model",
		MaxTokens:    512,
		Temperature:  0.2,
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
		Pending:    pending,
	})

	sessionID, err := engine.StartSession(context.Background(), "default")
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	persona := &config.Persona{Name: "default", SystemPrompt: "You are warm."}

	reply1, err := engine.SendMessage(context.Background(), sessionID, persona, "帮我清理 [finish] 文件", nil)
	if err != nil {
		t.Fatalf("SendMessage turn1: %v", err)
	}
	if !strings.Contains(reply1, "确认") {
		t.Fatalf("turn1 reply = %q, want confirmation question", reply1)
	}

	reply2, err := engine.SendMessage(context.Background(), sessionID, persona, "确认删除", nil)
	if err != nil {
		t.Fatalf("SendMessage turn2: %v", err)
	}
	if !strings.Contains(reply2, "已完成") {
		t.Fatalf("turn2 reply = %q, want completion text", reply2)
	}

	if delegateSessionID != sessionID {
		t.Fatalf("delegate session id = %q, want %q", delegateSessionID, sessionID)
	}
	if resumeSessionID != sessionID {
		t.Fatalf("resume session id = %q, want %q", resumeSessionID, sessionID)
	}

	// The first ChatStream request of turn2 should include Resume Note.
	chatStreamReqCount := 0
	var turn2First llm.ChatRequest
	for _, req := range llmClient.requests {
		if req.Stream {
			chatStreamReqCount++
			if chatStreamReqCount == 3 {
				turn2First = req
				break
			}
		}
	}
	if turn2First.System == "" {
		t.Fatal("failed to capture turn2 first ChatStream request")
	}
	if !strings.Contains(turn2First.System, "Pending Decision(s) Resume Note") {
		t.Fatalf("turn2 system missing Resume Note: %s", turn2First.System)
	}
	if !strings.Contains(turn2First.System, pausedTaskID) {
		t.Fatalf("turn2 system missing pending task id %q: %s", pausedTaskID, turn2First.System)
	}

	history, err := db.GetAllMessages(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	if len(history) != 4 {
		t.Fatalf("visible history len = %d, want 4 (user/assistant/user/assistant)", len(history))
	}
	for _, msg := range history {
		if msg.Role != "user" && msg.Role != "assistant" {
			t.Fatalf("unexpected persisted role %q", msg.Role)
		}
		if strings.Contains(msg.Content, "needs_emotion_decision") || strings.Contains(msg.Content, "decision_packet") || strings.Contains(msg.Content, `"task_report"`) {
			t.Fatalf("persisted message leaks internal work traces: %#v", msg)
		}
	}

	if got := pending.List(sessionID); len(got) != 0 {
		t.Fatalf("pending decisions should be consumed after resume, got %d", len(got))
	}
}
