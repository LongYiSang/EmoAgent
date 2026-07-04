package onebotv11

import (
	"encoding/json"
	"strings"
	"testing"

	appconfig "github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/platform"
)

func TestParseConfigAppliesOneBotDefaults(t *testing.T) {
	cfg, err := ParseConfig("qq-main", appconfig.PlatformAdapterConfig{
		Enabled: true,
		Kind:    "onebot_v11",
		ConfigJSON: map[string]any{
			"implementation": "napcat",
			"transport": map[string]any{
				"mode": "ws_reverse",
			},
		},
	})
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.AdapterID != "qq-main" || cfg.InstanceID != "qq-main" {
		t.Fatalf("ids = %#v, want adapter and instance qq-main", cfg)
	}
	if cfg.PlatformID != "qq" || cfg.SourceType != "onebot" {
		t.Fatalf("source/platform = %q/%q, want onebot/qq", cfg.SourceType, cfg.PlatformID)
	}
	if cfg.Implementation != "napcat" {
		t.Fatalf("implementation = %q, want napcat", cfg.Implementation)
	}
	if cfg.Transport.Mode != TransportModeWSReverse {
		t.Fatalf("transport mode = %q, want ws_reverse", cfg.Transport.Mode)
	}
	if cfg.Transport.ReversePath != "/api/platforms/onebot/v11/qq-main/ws" {
		t.Fatalf("reverse path = %q", cfg.Transport.ReversePath)
	}
	if !cfg.Routing.PrivateEnabled || cfg.Routing.GroupEnabled {
		t.Fatalf("routing = %#v, want private on and group off", cfg.Routing)
	}
	if !cfg.Routing.IgnoreSelfMessages || cfg.Routing.PrivateScope != platform.OriginScopePrivate {
		t.Fatalf("routing defaults = %#v", cfg.Routing)
	}
	if cfg.Message.InputFormat != MessageFormatArray || cfg.Message.OutputFormat != MessageFormatArray {
		t.Fatalf("message defaults = %#v", cfg.Message)
	}
	if !cfg.Outbound.CoalesceCommandEvents || !cfg.Outbound.SplitLongMessages {
		t.Fatalf("outbound defaults = %#v", cfg.Outbound)
	}
}

func TestParseConfigRejectsUnknownImplementation(t *testing.T) {
	_, err := ParseConfig("qq-main", appconfig.PlatformAdapterConfig{
		Enabled: true,
		Kind:    "onebot_v11",
		ConfigJSON: map[string]any{
			"implementation": "other",
			"transport": map[string]any{
				"mode": "ws_reverse",
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "implementation") {
		t.Fatalf("ParseConfig error = %v, want implementation error", err)
	}
}

func TestParseConfigPreservesExplicitFalseBooleans(t *testing.T) {
	cfg, err := ParseConfig("qq-main", appconfig.PlatformAdapterConfig{
		Enabled: true,
		Kind:    "onebot_v11",
		ConfigJSON: map[string]any{
			"transport": map[string]any{
				"mode": "ws_reverse",
			},
			"routing": map[string]any{
				"private_enabled":      false,
				"ignore_self_messages": false,
			},
			"outbound": map[string]any{
				"coalesce_command_events": false,
				"split_long_messages":     false,
			},
		},
	})
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.Routing.PrivateEnabled || cfg.Routing.IgnoreSelfMessages || cfg.Outbound.CoalesceCommandEvents || cfg.Outbound.SplitLongMessages {
		t.Fatalf("explicit false values were overwritten: %#v", cfg)
	}
}

func TestSelectProfileKeepsVendorsAsProfiles(t *testing.T) {
	for _, name := range []string{"generic", "napcat", "snowluma"} {
		profile, err := SelectProfile(name)
		if err != nil {
			t.Fatalf("SelectProfile(%q): %v", name, err)
		}
		if profile.Name != name {
			t.Fatalf("profile = %#v, want name %q", profile, name)
		}
		if !profile.RetcodeSuccess(ActionResponse{Status: "ok", Retcode: 0}) {
			t.Fatalf("profile %q should accept status ok retcode 0", name)
		}
	}
}

func TestRenderInboundMessageUsesSafePlaceholders(t *testing.T) {
	text := RenderInboundMessage(RawMessageValue{Segments: Message{
		{Type: "text", Data: map[string]string{"text": "hello "}},
		{Type: "image", Data: map[string]string{"file": "a.jpg"}},
		{Type: "record", Data: map[string]string{"file": "a.mp3"}},
		{Type: "poke", Data: map[string]string{"id": "1"}},
	}}, MessageConfig{UnsupportedSegmentPolicy: UnsupportedSegmentPlaceholder})

	if text != "hello [图片][语音][poke]" {
		t.Fatalf("rendered = %q", text)
	}
}

func TestRenderInboundMessageKeepsSegmentsWithNonStringData(t *testing.T) {
	var event Event
	if err := json.Unmarshal([]byte(`{
		"time": 1710000000,
		"self_id": 123456,
		"post_type": "message",
		"message_type": "private",
		"message_id": 24682,
		"user_id": 11223344,
		"message": [
			{"type":"text","data":{"text":"hi "}},
			{"type":"at","data":{"qq":123456}},
			{"type":"image","data":{"file":"a.jpg","size":1024}}
		],
		"sender": {"user_id":11223344,"nickname":"Alice"}
	}`), &event); err != nil {
		t.Fatalf("Unmarshal event: %v", err)
	}
	in, accepted, err := MapEvent(event, testConfig(), ProfileGeneric())
	if err != nil {
		t.Fatalf("MapEvent: %v", err)
	}
	if !accepted {
		t.Fatal("accepted = false, want true")
	}
	if in.Text != "hi [at][图片]" {
		t.Fatalf("text = %q, want placeholders for non-string segment data", in.Text)
	}
}

func TestRenderCQStringFallbackUsesSafePlaceholders(t *testing.T) {
	text := RenderInboundMessage(RawMessageValue{
		IsString: true,
		String:   "hi [CQ:image,file=a.jpg] [CQ:at,qq=123]",
	}, MessageConfig{UnsupportedSegmentPolicy: UnsupportedSegmentPlaceholder})

	if text != "hi [图片] [at]" {
		t.Fatalf("rendered = %q", text)
	}
}

func TestMapPrivateEventToInboundMessage(t *testing.T) {
	var event Event
	if err := json.Unmarshal([]byte(`{
		"time": 1710000000,
		"self_id": 123456,
		"post_type": "message",
		"message_type": "private",
		"message_id": 24680,
		"user_id": 11223344,
		"message": [{"type":"text","data":{"text":"/sid"}}],
		"sender": {"user_id":11223344,"nickname":"Alice"}
	}`), &event); err != nil {
		t.Fatalf("Unmarshal event: %v", err)
	}
	cfg := testConfig()
	in, accepted, err := MapEvent(event, cfg, ProfileGeneric())
	if err != nil {
		t.Fatalf("MapEvent: %v", err)
	}
	if !accepted {
		t.Fatal("accepted = false, want true")
	}
	if in.SourceType != "onebot" || in.PlatformID != "qq" || in.AdapterInstanceID != "qq-main" {
		t.Fatalf("platform identity = %#v", in)
	}
	if in.ChannelType != "private" || in.ExternalConversationID != "11223344" || in.ExternalActorID != "11223344" {
		t.Fatalf("conversation mapping = %#v", in)
	}
	if in.ExternalMessageID != "123456:private:11223344:24680" {
		t.Fatalf("external message id = %q", in.ExternalMessageID)
	}
	if in.OriginScope != platform.OriginScopePrivate || in.AcceptedReason != "private" {
		t.Fatalf("origin fields = %#v", in)
	}
	if in.Text != "/sid" || len(in.Parts) != 0 {
		t.Fatalf("text/parts = %q/%d", in.Text, len(in.Parts))
	}
	if in.Actor.DisplayName != "Alice" || in.Actor.Role != platform.ActorRoleMember {
		t.Fatalf("actor = %#v", in.Actor)
	}
	if in.RawEventHash == "" || in.Raw == nil {
		t.Fatalf("raw fields missing: %#v", in)
	}
}

func TestMapEventIgnoresSelfAndGroupMessages(t *testing.T) {
	cfg := testConfig()
	groupEnabledCfg := cfg
	groupEnabledCfg.Routing.GroupEnabled = true
	tests := []struct {
		name string
		body string
		cfg  Config
	}{
		{
			name: "self private",
			body: `{"time":1710000000,"self_id":123456,"post_type":"message","message_type":"private","message_id":1,"user_id":123456,"message":[{"type":"text","data":{"text":"hello"}}]}`,
			cfg:  cfg,
		},
		{
			name: "group disabled",
			body: `{"time":1710000000,"self_id":123456,"post_type":"message","message_type":"group","message_id":1,"user_id":11223344,"group_id":9988,"message":[{"type":"text","data":{"text":"hello"}}]}`,
			cfg:  cfg,
		},
		{
			name: "group still ignored when configured",
			body: `{"time":1710000000,"self_id":123456,"post_type":"message","message_type":"group","message_id":1,"user_id":11223344,"group_id":9988,"message":[{"type":"text","data":{"text":"hello"}}]}`,
			cfg:  groupEnabledCfg,
		},
		{
			name: "notice ignored",
			body: `{"time":1710000000,"self_id":123456,"post_type":"notice","notice_type":"friend_recall","user_id":11223344}`,
			cfg:  cfg,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var event Event
			if err := json.Unmarshal([]byte(tt.body), &event); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if _, accepted, err := MapEvent(event, tt.cfg, ProfileGeneric()); err != nil || accepted {
				t.Fatalf("MapEvent accepted=%v err=%v, want ignored nil error", accepted, err)
			}
		})
	}
}

func TestMapActorRole(t *testing.T) {
	tests := map[string]platform.ActorRole{
		"owner": platform.ActorRoleOwner,
		"admin": platform.ActorRoleAdmin,
		"":      platform.ActorRoleMember,
		"other": platform.ActorRoleMember,
	}
	for input, want := range tests {
		if got := MapActorRole(input); got != want {
			t.Fatalf("MapActorRole(%q) = %q, want %q", input, got, want)
		}
	}
}

func testConfig() Config {
	cfg, err := ParseConfig("qq-main", appconfig.PlatformAdapterConfig{
		Enabled:    true,
		Kind:       "onebot_v11",
		InstanceID: "qq-main",
		PlatformID: "qq",
		ConfigJSON: map[string]any{
			"implementation": "generic",
			"transport": map[string]any{
				"mode": "ws_reverse",
			},
		},
	})
	if err != nil {
		panic(err)
	}
	return cfg
}
