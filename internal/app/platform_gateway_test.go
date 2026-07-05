package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	chatcore "github.com/longyisang/emoagent/internal/chat"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/conversation"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/platform"
	"github.com/longyisang/emoagent/internal/storage"
)

func TestPlatformGatewayHandlesCommandWithFullOriginAndActor(t *testing.T) {
	ctx := context.Background()
	_, db, gateway, fakeLLM := newTestPlatformGateway(t)
	sink := &platform.BufferedPlatformSink{}

	result, err := gateway.HandleInbound(ctx, platform.InboundMessage{
		ExternalMessageID:      "msg-sid",
		SourceType:             "napcat",
		AdapterInstanceID:      "main",
		PlatformID:             "qq",
		ChannelType:            "group",
		ExternalConversationID: "20002",
		ExternalActorID:        "10001",
		PersonaKey:             "default",
		Text:                   "/sid",
		Actor: platform.Actor{
			ID:          "10001",
			DisplayName: "Alice",
			Role:        platform.ActorRoleAdmin,
		},
	}, sink)
	if err != nil {
		t.Fatalf("HandleInbound: %v", err)
	}
	if !result.Handled || result.Duplicate || result.SessionID == "" {
		t.Fatalf("result = %#v, want handled command", result)
	}
	if fakeLLM.calls != 0 {
		t.Fatalf("LLM calls = %d, want command to bypass LLM", fakeLLM.calls)
	}
	if len(sink.Events) != 1 {
		t.Fatalf("sink events = %#v, want one command event", sink.Events)
	}
	event := sink.Events[0]
	if event.Type != "command_result" || event.Status != "success" || event.Payload["actor_id"] != "10001" || event.Payload["source_type"] != "napcat" {
		t.Fatalf("event = %#v, want command_result with actor/origin payload", event)
	}
	invocations, err := db.ListCommandInvocations(ctx, storage.CommandInvocationFilter{CommandID: "builtin.sid", Limit: 10})
	if err != nil {
		t.Fatalf("ListCommandInvocations: %v", err)
	}
	if len(invocations) != 1 || invocations[0].ActorID != "10001" || invocations[0].ActorRole != "admin" {
		t.Fatalf("invocations = %#v, want actor audit", invocations)
	}
}

func TestPlatformGatewayUsesBoundAgentForSID(t *testing.T) {
	ctx := context.Background()
	app, _, gateway, fakeLLM := newTestPlatformGateway(t)
	testConfig(app).Platforms.Common.DefaultAgentID = "Chat"
	upsertPlatformGatewayAgent(t, app, "Chat", "Xia", "http://127.0.0.1:1/v1")
	setTestPersonas(app, map[string]*config.Persona{
		"default": {Name: "default", SystemPrompt: "Default persona."},
		"Xia":     {Name: "Xia", SystemPrompt: "Xia persona."},
	})
	sink := &platform.BufferedPlatformSink{}

	result, err := gateway.HandleInbound(ctx, platform.InboundMessage{
		ExternalMessageID:      "msg-sid-bound-agent",
		SourceType:             "onebot",
		AdapterInstanceID:      "qq-main",
		PlatformID:             "qq",
		ChannelType:            "private",
		ExternalConversationID: "10001",
		ExternalActorID:        "10001",
		Text:                   "/sid",
		Actor:                  platform.Actor{ID: "10001", Role: platform.ActorRoleMember},
	}, sink)
	if err != nil {
		t.Fatalf("HandleInbound: %v", err)
	}
	if !result.Handled || result.SessionID == "" {
		t.Fatalf("result = %#v, want handled", result)
	}
	event := requirePlatformCommandEvent(t, sink.Events, "sid")
	for _, want := range []string{"agent_id=Chat", "persona=Xia"} {
		if !strings.Contains(event.Content, want) {
			t.Fatalf("/sid content missing %q:\n%s", want, event.Content)
		}
	}
	if fakeLLM.calls != 0 {
		t.Fatalf("LLM calls = %d, want /sid to bypass LLM", fakeLLM.calls)
	}
}

func TestPlatformGatewayUsesBoundAgentForText(t *testing.T) {
	ctx := context.Background()
	app, _, gateway, activeLLM := newTestPlatformGateway(t)
	var requestedModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("Decode request: %v", err)
		}
		requestedModel = req.Model
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"id":"chatcmpl-bound","model":"bound-model","choices":[{"delta":{"content":"bound reply"}}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"id":"chatcmpl-bound","model":"bound-model","choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n"))
	}))
	t.Cleanup(server.Close)
	testConfig(app).Platforms.Common.DefaultAgentID = "Chat"
	upsertPlatformGatewayAgent(t, app, "Chat", "Xia", server.URL)
	setTestPersonas(app, map[string]*config.Persona{
		"default": {Name: "default", SystemPrompt: "Default persona."},
		"Xia":     {Name: "Xia", SystemPrompt: "Xia persona."},
	})
	sink := &platform.BufferedPlatformSink{}

	result, err := gateway.HandleInbound(ctx, platform.InboundMessage{
		ExternalMessageID:      "msg-text-bound-agent",
		SourceType:             "onebot",
		AdapterInstanceID:      "qq-main",
		PlatformID:             "qq",
		ChannelType:            "private",
		ExternalConversationID: "10001",
		ExternalActorID:        "10001",
		Text:                   "hello",
		Actor:                  platform.Actor{ID: "10001", Role: platform.ActorRoleMember},
	}, sink)
	if err != nil {
		t.Fatalf("HandleInbound: %v", err)
	}
	if !result.Handled || result.SessionID == "" {
		t.Fatalf("result = %#v, want handled", result)
	}
	if activeLLM.calls != 0 {
		t.Fatalf("active LLM calls = %d, want bound agent runtime only", activeLLM.calls)
	}
	if requestedModel != "bound-model" {
		t.Fatalf("requested model = %q, want bound-model", requestedModel)
	}
	if len(sink.Events) != 1 || sink.Events[0].Type != "message" || sink.Events[0].PersonaKey != "Xia" || sink.Events[0].Content != "bound reply" {
		t.Fatalf("sink events = %#v, want bound reply for Xia", sink.Events)
	}
}

func TestPlatformGatewayDeduplicatesExternalMessageID(t *testing.T) {
	ctx := context.Background()
	_, db, gateway, _ := newTestPlatformGateway(t)
	inbound := platform.InboundMessage{
		ExternalMessageID:      "msg-new",
		SourceType:             "napcat",
		AdapterInstanceID:      "main",
		PlatformID:             "qq",
		ChannelType:            "private",
		ExternalConversationID: "10001",
		ExternalActorID:        "10001",
		PersonaKey:             "default",
		Text:                   "/new",
		Actor:                  platform.Actor{ID: "10001", Role: platform.ActorRoleMember},
	}
	firstSink := &platform.BufferedPlatformSink{}
	first, err := gateway.HandleInbound(ctx, inbound, firstSink)
	if err != nil {
		t.Fatalf("HandleInbound first: %v", err)
	}
	if !first.Handled || first.Duplicate || len(firstSink.Events) == 0 {
		t.Fatalf("first = %#v events=%#v, want handled with events", first, firstSink.Events)
	}

	secondSink := &platform.BufferedPlatformSink{}
	second, err := gateway.HandleInbound(ctx, inbound, secondSink)
	if err != nil {
		t.Fatalf("HandleInbound second: %v", err)
	}
	if !second.Duplicate || len(secondSink.Events) != 0 {
		t.Fatalf("second = %#v events=%#v, want duplicate without events", second, secondSink.Events)
	}
	invocations, err := db.ListCommandInvocations(ctx, storage.CommandInvocationFilter{CommandID: "builtin.new", Limit: 10})
	if err != nil {
		t.Fatalf("ListCommandInvocations: %v", err)
	}
	if len(invocations) != 1 {
		t.Fatalf("new invocations = %d, want exactly one", len(invocations))
	}
}

func TestPlatformGatewayHandlesBuiltinCommandMatrix(t *testing.T) {
	ctx := context.Background()
	commands := []string{"/sid", "/new", "/reset", "/clear", "/stop"}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			app, _, gateway, fakeLLM := newTestPlatformGateway(t)
			inbound := testPlatformInbound("msg-"+command[1:], command)
			var stopped bool
			if command == "/stop" {
				origin, err := platform.OriginFromInbound(inbound, "")
				if err != nil {
					t.Fatalf("OriginFromInbound: %v", err)
				}
				binding, err := app.kernel.Services.Conversation.Bindings().EnsureCurrent(ctx, origin, inbound.PersonaKey)
				if err != nil {
					t.Fatalf("EnsureCurrent: %v", err)
				}
				unregister := app.kernel.Services.Conversation.RunRegistry().Register(conversation.RunRef{
					OriginKey: origin.OriginKey,
					SessionID: binding.SessionID,
					Kind:      "platform_text",
				}, func() { stopped = true })
				t.Cleanup(unregister)
			}

			sink := &platform.BufferedPlatformSink{}
			result, err := gateway.HandleInbound(ctx, inbound, sink)
			if err != nil {
				t.Fatalf("HandleInbound: %v", err)
			}
			if !result.Handled || result.Duplicate || result.SessionID == "" {
				t.Fatalf("result = %#v, want handled command", result)
			}
			if fakeLLM.calls != 0 {
				t.Fatalf("LLM calls = %d, want command to bypass LLM", fakeLLM.calls)
			}
			event := requirePlatformCommandEvent(t, sink.Events, command[1:])
			if event.Status != "success" {
				t.Fatalf("event = %#v, want success", event)
			}
			switch command {
			case "/reset":
				if event.Payload["reload_memory"] != true {
					t.Fatalf("reset event = %#v, want reload_memory", event)
				}
			case "/clear":
				if event.Payload["reload_history"] != true {
					t.Fatalf("clear event = %#v, want reload_history", event)
				}
			case "/stop":
				if !stopped || event.Payload["stopped_count"] != float64(1) && event.Payload["stopped_count"] != 1 {
					t.Fatalf("stop event = %#v stopped=%v, want stopped_count=1", event, stopped)
				}
			}
		})
	}
}

func TestPlatformGatewayNormalTextEmitsFinalMessage(t *testing.T) {
	ctx := context.Background()
	_, db, gateway, fakeLLM := newTestPlatformGateway(t)
	sink := &platform.BufferedPlatformSink{}

	result, err := gateway.HandleInbound(ctx, platform.InboundMessage{
		ExternalMessageID:      "msg-text",
		SourceType:             "napcat",
		AdapterInstanceID:      "main",
		PlatformID:             "qq",
		ChannelType:            "private",
		ExternalConversationID: "10001",
		ExternalActorID:        "10001",
		PersonaKey:             "default",
		Text:                   "hello",
		Actor:                  platform.Actor{ID: "10001", Role: platform.ActorRoleMember},
	}, sink)
	if err != nil {
		t.Fatalf("HandleInbound: %v", err)
	}
	if !result.Handled || result.Duplicate || result.SessionID == "" {
		t.Fatalf("result = %#v, want handled text", result)
	}
	if fakeLLM.calls != 1 {
		t.Fatalf("LLM calls = %d, want 1", fakeLLM.calls)
	}
	if len(sink.Events) != 1 || sink.Events[0].Type != "message" || sink.Events[0].Content != "platform reply" {
		t.Fatalf("sink events = %#v, want final message", sink.Events)
	}
	messages, err := db.GetAllMessages(ctx, result.SessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want user and assistant persisted", len(messages))
	}
}

func TestPlatformGatewayRejectsPartsInput(t *testing.T) {
	ctx := context.Background()
	_, _, gateway, fakeLLM := newTestPlatformGateway(t)
	sink := &platform.BufferedPlatformSink{}

	_, err := gateway.HandleInbound(ctx, platform.InboundMessage{
		ExternalMessageID:      "msg-parts",
		SourceType:             "napcat",
		AdapterInstanceID:      "main",
		PlatformID:             "qq",
		ChannelType:            "private",
		ExternalConversationID: "10001",
		ExternalActorID:        "10001",
		PersonaKey:             "default",
		Text:                   "hello",
		Parts:                  []llm.ContentBlock{{Type: "text", Text: "hello"}},
		Actor:                  platform.Actor{ID: "10001", Role: platform.ActorRoleMember},
	}, sink)
	if err == nil {
		t.Fatal("HandleInbound err = nil, want parts rejection")
	}
	if fakeLLM.calls != 0 || len(sink.Events) != 0 {
		t.Fatalf("LLM calls=%d events=%#v, want no side effects", fakeLLM.calls, sink.Events)
	}
}

func newTestPlatformGateway(t *testing.T) (*App, *storage.DB, *PlatformGateway, *platformGatewayFakeLLM) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "platform.db"), logger)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	app := newTestApp(config.DefaultConfig(), db, logger)
	setTestPersonas(app, map[string]*config.Persona{
		"default": {Name: "default", SystemPrompt: "You are helpful."},
	})
	fakeLLM := &platformGatewayFakeLLM{response: &llm.ChatResponse{ID: "resp-platform", Content: "platform reply", StopReason: "end_turn"}}
	engine := chatcore.NewEngine(chatcore.EngineConfig{
		LLM:           fakeLLM,
		DB:            db,
		Logger:        logger,
		Model:         "test-model",
		ContextConfig: config.DefaultConfig().Context,
	})
	app.kernel.Services.Chat.engine = engine
	app.kernel.Services.Conversation.Bindings().SetSessionStarter(engine)
	gateway := NewPlatformGateway(app.kernel.Infra, app.kernel.Services.Conversation, app.kernel.Services.Commands, app.kernel.Services.Chat, app.kernel.Services.AgentRuntime, app.kernel.Services.Personas, NewStorageReceiptStore(db))
	return app, db, gateway, fakeLLM
}

func testPlatformInbound(messageID string, text string) platform.InboundMessage {
	return platform.InboundMessage{
		ExternalMessageID:      messageID,
		SourceType:             "napcat",
		AdapterInstanceID:      "main",
		PlatformID:             "qq",
		ChannelType:            "private",
		ExternalConversationID: "10001",
		ExternalActorID:        "10001",
		PersonaKey:             "default",
		Text:                   text,
		Actor:                  platform.Actor{ID: "10001", DisplayName: "Alice", Role: platform.ActorRoleMember},
	}
}

func requirePlatformCommandEvent(t *testing.T, events []platform.OutboundEvent, commandName string) platform.OutboundEvent {
	t.Helper()
	for _, event := range events {
		if event.Type == "command_result" && event.Payload["command_name"] == commandName {
			return event
		}
	}
	t.Fatalf("events = %#v, want command_result for %s", events, commandName)
	return platform.OutboundEvent{}
}

func upsertPlatformGatewayAgent(t *testing.T, app *App, agentID string, personaKey string, baseURL string) {
	t.Helper()
	db := app.kernel.Infra.DB
	if err := db.UpsertPersona(personaKey, personaKey, "", "system", "warm", nil, "", nil); err != nil {
		t.Fatalf("UpsertPersona: %v", err)
	}
	t.Setenv("TEST_PLATFORM_GATEWAY_KEY", "test-key")
	if err := db.UpsertLLMProvider(config.LLMProvider{ID: "test-provider", Name: "Test Provider", Protocol: "openai_compatible", BaseURL: baseURL, APIKeyEnv: "TEST_PLATFORM_GATEWAY_KEY", Enabled: true}); err != nil {
		t.Fatalf("UpsertLLMProvider: %v", err)
	}
	binding := config.ModelBinding{ProviderID: "test-provider", Model: "bound-model"}
	if err := db.UpsertAgentConfig(config.AgentConfig{
		ID:               agentID,
		Name:             agentID,
		PersonaKey:       personaKey,
		ContextOverrides: map[string]any{},
		Emotion: config.AgentModelGroup{
			Main:    binding,
			Summary: binding,
		},
		Work: config.AgentModelGroup{
			Main:    binding,
			Summary: binding,
		},
	}); err != nil {
		t.Fatalf("UpsertAgentConfig: %v", err)
	}
}

type platformGatewayFakeLLM struct {
	calls    int
	response *llm.ChatResponse
	err      error
}

func (f *platformGatewayFakeLLM) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	f.calls++
	return f.response, f.err
}

func (f *platformGatewayFakeLLM) ChatStream(_ context.Context, req llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	f.calls++
	return f.response, f.err
}
