package storage

import (
	"context"
	"testing"
)

func TestConversationOriginBindingAndClearMarkerRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	if err := db.CreateSession(ctx, "session-1", "default"); err != nil {
		t.Fatalf("CreateSession(session-1): %v", err)
	}
	if err := db.CreateSession(ctx, "session-2", "default"); err != nil {
		t.Fatalf("CreateSession(session-2): %v", err)
	}
	if err := db.UpsertConversationOrigin(ctx, ConversationOriginRecord{
		ID:          "origin-1",
		OriginKey:   "webui:local:main",
		SourceType:  "webui",
		ChannelType: "web",
		DisplayName: "WebUI",
	}); err != nil {
		t.Fatalf("UpsertConversationOrigin: %v", err)
	}

	origin, err := db.GetConversationOrigin(ctx, "webui:local:main")
	if err != nil {
		t.Fatalf("GetConversationOrigin: %v", err)
	}
	if origin == nil || origin.ID != "origin-1" || origin.SourceType != "webui" || origin.ChannelType != "web" {
		t.Fatalf("origin = %#v, want webui origin", origin)
	}

	if err := db.UpsertConversationBinding(ctx, ConversationBindingRecord{
		ID:                "binding-1",
		OriginKey:         "webui:local:main",
		PersonaKey:        "default",
		CurrentSessionID:  "session-1",
		DefaultPersonaKey: "default",
		UniqueScope:       "origin",
	}); err != nil {
		t.Fatalf("UpsertConversationBinding(session-1): %v", err)
	}
	if err := db.UpsertConversationBinding(ctx, ConversationBindingRecord{
		ID:                "binding-1-updated",
		OriginKey:         "webui:local:main",
		PersonaKey:        "default",
		CurrentSessionID:  "session-2",
		DefaultPersonaKey: "default",
		UniqueScope:       "origin",
	}); err != nil {
		t.Fatalf("UpsertConversationBinding(session-2): %v", err)
	}

	binding, err := db.GetConversationBinding(ctx, "webui:local:main", "default")
	if err != nil {
		t.Fatalf("GetConversationBinding: %v", err)
	}
	if binding == nil || binding.CurrentSessionID != "session-2" || binding.UniqueScope != "origin" {
		t.Fatalf("binding = %#v, want session-2 origin binding", binding)
	}

	if err := db.AddMessage(ctx, "msg-1", "session-2", "user", "hello"); err != nil {
		t.Fatalf("AddMessage(msg-1): %v", err)
	}
	if err := db.UpsertSessionClearMarker(ctx, SessionClearMarkerRecord{
		ID:             "marker-1",
		OriginKey:      "webui:local:main",
		SessionID:      "session-2",
		PersonaKey:     "default",
		AfterMessageID: "msg-1",
		Reason:         "command_clear",
	}); err != nil {
		t.Fatalf("UpsertSessionClearMarker: %v", err)
	}
	marker, err := db.GetSessionClearMarker(ctx, "webui:local:main", "session-2")
	if err != nil {
		t.Fatalf("GetSessionClearMarker: %v", err)
	}
	if marker == nil || marker.AfterMessageID != "msg-1" || marker.PersonaKey != "default" {
		t.Fatalf("marker = %#v, want clear marker after msg-1", marker)
	}
}

func TestConversationEventsAndCommandAuditRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	if err := db.CreateSession(ctx, "session-1", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := db.UpsertConversationOrigin(ctx, ConversationOriginRecord{
		ID:          "origin-1",
		OriginKey:   "webui:local:main",
		SourceType:  "webui",
		ChannelType: "web",
	}); err != nil {
		t.Fatalf("UpsertConversationOrigin: %v", err)
	}
	if err := db.AddConversationEvent(ctx, ConversationEventRecord{
		ID:             "event-1",
		OriginKey:      "webui:local:main",
		SessionID:      "session-1",
		PersonaKey:     "default",
		EventType:      "command_result",
		VisibleContent: "已重置当前会话的 LLM 上下文。",
		PayloadJSON:    `{"command":"reset"}`,
	}); err != nil {
		t.Fatalf("AddConversationEvent: %v", err)
	}
	events, err := db.ListConversationEvents(ctx, "session-1", "webui:local:main", 10)
	if err != nil {
		t.Fatalf("ListConversationEvents: %v", err)
	}
	if len(events) != 1 || events[0].ID != "event-1" || events[0].EventType != "command_result" {
		t.Fatalf("events = %#v, want command_result event", events)
	}

	if err := db.UpsertCommandConfig(ctx, CommandConfigRecord{
		CommandID:     "builtin.reset",
		ProviderKind:  "builtin",
		OriginalName:  "reset",
		EffectiveName: "reset",
		AliasesJSON:   `[]`,
		Enabled:       true,
		Permission:    "member",
		OutputMode:    "direct",
		ConfigJSON:    `{}`,
	}); err != nil {
		t.Fatalf("UpsertCommandConfig: %v", err)
	}
	config, err := db.GetCommandConfig(ctx, "builtin.reset")
	if err != nil {
		t.Fatalf("GetCommandConfig: %v", err)
	}
	if config == nil || !config.Enabled || config.EffectiveName != "reset" {
		t.Fatalf("config = %#v, want enabled reset command", config)
	}

	if err := db.AddCommandInvocation(ctx, CommandInvocationRecord{
		ID:           "invocation-1",
		CommandID:    "builtin.reset",
		CommandName:  "reset",
		ProviderKind: "builtin",
		OriginKey:    "webui:local:main",
		SourceType:   "webui",
		SessionID:    "session-1",
		PersonaKey:   "default",
		ActorRole:    "member",
		ArgvJSON:     `[]`,
		FlagsJSON:    `{}`,
		OutputMode:   "direct",
		Status:       "success",
		ResultText:   "已重置当前会话的 LLM 上下文。",
		PayloadJSON:  `{}`,
	}); err != nil {
		t.Fatalf("AddCommandInvocation: %v", err)
	}
	invocations, err := db.ListCommandInvocations(ctx, CommandInvocationFilter{SessionID: "session-1", Limit: 10})
	if err != nil {
		t.Fatalf("ListCommandInvocations: %v", err)
	}
	if len(invocations) != 1 || invocations[0].ID != "invocation-1" || invocations[0].Status != "success" {
		t.Fatalf("invocations = %#v, want successful reset invocation", invocations)
	}
}
