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

func TestPlatformGatewayHandlesCommandBeforeInvalidBoundAgentRuntime(t *testing.T) {
	ctx := context.Background()
	app, _, gateway, fakeLLM := newTestPlatformGateway(t)
	testConfig(app).Platforms.Common.DefaultAgentID = "missing-agent"
	sink := &platform.BufferedPlatformSink{}

	result, err := gateway.HandleInbound(ctx, platform.InboundMessage{
		ExternalMessageID:      "msg-sid-missing-agent",
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
		t.Fatalf("result = %#v, want handled command", result)
	}
	event := requirePlatformCommandEvent(t, sink.Events, "sid")
	if !strings.Contains(event.Content, "agent_id=missing-agent") {
		t.Fatalf("/sid content missing configured agent id:\n%s", event.Content)
	}
	if fakeLLM.calls != 0 {
		t.Fatalf("LLM calls = %d, want /sid to bypass invalid runtime", fakeLLM.calls)
	}
}

func TestPlatformGatewayUsesBoundAgentForText(t *testing.T) {
	ctx := context.Background()
	app, db, gateway, activeLLM := newTestPlatformGateway(t)
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
	cfg := testConfig(app)
	cfg.Platforms.Common.DefaultAgentID = "Chat"
	cfg.Chat.TurnPipeline.Enabled = true
	cfg.Chat.TurnPipeline.MemoryStages = true
	cfg.Chat.PromptRouter.Mode = config.PromptRouterModeAlwaysCasual
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
	turnRow := requirePlatformTurnRow(t, db, "msg-text-bound-agent")
	if turnRow.Source != "platform" || turnRow.PersonaKey != "Xia" || turnRow.Status != "done" {
		t.Fatalf("turn row = %#v, want platform Xia done turn", turnRow)
	}
	requireTurnStage(t, db, turnRow.ID, "memory_commit")
	requirePlatformReceiptResult(t, db, "onebot", "qq-main", "msg-text-bound-agent", "handled", "message")
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
	app, db, gateway, activeLLM := newTestPlatformGateway(t)
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
		_, _ = w.Write([]byte(`data: {"id":"chatcmpl-default","model":"bound-model","choices":[{"delta":{"content":"fallback reply"}}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"id":"chatcmpl-default","model":"bound-model","choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n"))
	}))
	t.Cleanup(server.Close)
	cfg := testConfig(app)
	cfg.Chat.TurnPipeline.Enabled = true
	cfg.Chat.TurnPipeline.MemoryStages = true
	cfg.Chat.PromptRouter.Mode = config.PromptRouterModeAlwaysCasual
	cfg.AgentConfigs = []config.AgentConfig{{ID: "default-agent", PersonaKey: "default"}}
	upsertPlatformGatewayAgent(t, app, "default-agent", "default", server.URL)
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
	if activeLLM.calls != 0 {
		t.Fatalf("active LLM calls = %d, want platform fallback agent only", activeLLM.calls)
	}
	if requestedModel != "bound-model" {
		t.Fatalf("requested model = %q, want bound-model", requestedModel)
	}
	if len(sink.Events) != 1 || sink.Events[0].Type != "message" || sink.Events[0].Content != "fallback reply" {
		t.Fatalf("sink events = %#v, want final message", sink.Events)
	}
	messages, err := db.GetAllMessages(ctx, result.SessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want user and assistant persisted", len(messages))
	}
	requirePlatformReceiptResult(t, db, "napcat", "main", "msg-text", "handled", "message")
}

func TestPlatformGatewayNormalTextEmitsReplyDeliverySegments(t *testing.T) {
	ctx := context.Background()
	app, db, gateway, activeLLM := newTestPlatformGateway(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"id":"chatcmpl-segmented","model":"bound-model","choices":[{"delta":{"content":"第一句。第二句。"}}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"id":"chatcmpl-segmented","model":"bound-model","choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n"))
	}))
	t.Cleanup(server.Close)
	cfg := testConfig(app)
	cfg.Chat.TurnPipeline.Enabled = true
	cfg.Chat.TurnPipeline.MemoryStages = true
	cfg.Chat.PromptRouter.Mode = config.PromptRouterModeAlwaysCasual
	cfg.Chat.ReplyDelivery.Enabled = true
	cfg.Chat.ReplyDelivery.Timing.Enabled = false
	cfg.AgentConfigs = []config.AgentConfig{{ID: "default-agent", PersonaKey: "default"}}
	upsertPlatformGatewayAgent(t, app, "default-agent", "default", server.URL)
	sink := &platform.BufferedPlatformSink{}

	result, err := gateway.HandleInbound(ctx, platform.InboundMessage{
		ExternalMessageID:      "msg-text-segmented",
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
	if activeLLM.calls != 0 {
		t.Fatalf("active LLM calls = %d, want platform fallback agent only", activeLLM.calls)
	}
	if len(sink.Events) != 2 {
		t.Fatalf("sink events = %#v, want two segmented messages", sink.Events)
	}
	if sink.Events[0].Type != "message" || sink.Events[0].Content != "第一句。" {
		t.Fatalf("first sink event = %#v, want first segment", sink.Events[0])
	}
	if sink.Events[1].Type != "message" || sink.Events[1].Content != "第二句。" {
		t.Fatalf("second sink event = %#v, want second segment", sink.Events[1])
	}
	messages, err := db.GetAllMessages(ctx, result.SessionID)
	if err != nil {
		t.Fatalf("GetAllMessages: %v", err)
	}
	if len(messages) != 2 || messages[1].Content != "第一句。第二句。" {
		t.Fatalf("messages = %#v, want one persisted assistant reply", messages)
	}
	requirePlatformReceiptResult(t, db, "napcat", "main", "msg-text-segmented", "handled", "message")
}

func TestPlatformGatewayTextRequiresTurnPipeline(t *testing.T) {
	ctx := context.Background()
	app, db, gateway, activeLLM := newTestPlatformGateway(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"id":"chatcmpl-disabled","model":"bound-model","choices":[{"delta":{"content":"should not send"}}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"id":"chatcmpl-disabled","model":"bound-model","choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n\n"))
	}))
	t.Cleanup(server.Close)
	cfg := testConfig(app)
	cfg.Platforms.Common.DefaultAgentID = "Chat"
	cfg.Chat.TurnPipeline.Enabled = false
	cfg.Chat.TurnPipeline.MemoryStages = true
	cfg.Chat.PromptRouter.Mode = config.PromptRouterModeAlwaysCasual
	upsertPlatformGatewayAgent(t, app, "Chat", "Xia", server.URL)
	setTestPersonas(app, map[string]*config.Persona{
		"default": {Name: "default", SystemPrompt: "Default persona."},
		"Xia":     {Name: "Xia", SystemPrompt: "Xia persona."},
	})
	sink := &platform.BufferedPlatformSink{}

	result, err := gateway.HandleInbound(ctx, platform.InboundMessage{
		ExternalMessageID:      "msg-no-output",
		SourceType:             "onebot",
		AdapterInstanceID:      "qq-main",
		PlatformID:             "qq",
		ChannelType:            "private",
		ExternalConversationID: "10001",
		ExternalActorID:        "10001",
		Text:                   "hello",
		Actor:                  platform.Actor{ID: "10001", Role: platform.ActorRoleMember},
	}, sink)
	if err == nil {
		t.Fatalf("HandleInbound err = nil, want turn pipeline requirement failure; result=%#v", result)
	}
	if activeLLM.calls != 0 {
		t.Fatalf("active LLM calls = %d, want bound agent runtime only", activeLLM.calls)
	}
	if len(sink.Events) != 1 || sink.Events[0].Type != "error" || sink.Events[0].Content == "" {
		t.Fatalf("sink events = %#v, want one platform error event when pipeline is disabled", sink.Events)
	}
	requirePlatformReceiptResult(t, db, "onebot", "qq-main", "msg-no-output", "failed", "")
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

type platformTurnRow struct {
	ID         string
	Source     string
	PersonaKey string
	Status     string
}

func requirePlatformTurnRow(t *testing.T, db *storage.DB, sourceEventID string) platformTurnRow {
	t.Helper()
	var row platformTurnRow
	err := db.SqlDB().QueryRow(`
		SELECT id, source, persona_key, status
		FROM turns
		WHERE source_event_id = ?
	`, sourceEventID).Scan(&row.ID, &row.Source, &row.PersonaKey, &row.Status)
	if err != nil {
		t.Fatalf("query turn source_event_id=%q: %v", sourceEventID, err)
	}
	return row
}

func requireTurnStage(t *testing.T, db *storage.DB, turnID string, stage string) {
	t.Helper()
	var count int
	err := db.SqlDB().QueryRow(`
		SELECT COUNT(1)
		FROM turn_events
		WHERE turn_id = ? AND stage = ?
	`, turnID, stage).Scan(&count)
	if err != nil {
		t.Fatalf("query turn stage %s/%s: %v", turnID, stage, err)
	}
	if count == 0 {
		t.Fatalf("turn %s missing stage %s", turnID, stage)
	}
}

func requirePlatformReceiptResult(t *testing.T, db *storage.DB, sourceType string, adapterID string, externalMessageID string, wantStatus string, wantResultType string) {
	t.Helper()
	var status, resultType string
	err := db.SqlDB().QueryRow(`
		SELECT status, result_type
		FROM platform_message_receipts
		WHERE source_type = ? AND adapter_instance_id = ? AND external_message_id = ?
	`, sourceType, adapterID, externalMessageID).Scan(&status, &resultType)
	if err != nil {
		t.Fatalf("query receipt %s/%s/%s: %v", sourceType, adapterID, externalMessageID, err)
	}
	if status != wantStatus || resultType != wantResultType {
		t.Fatalf("receipt status/result_type = %q/%q, want %q/%q", status, resultType, wantStatus, wantResultType)
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
