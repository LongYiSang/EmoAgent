package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	appconfig "github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/conversation"
	"github.com/longyisang/emoagent/internal/platform"
	"github.com/longyisang/emoagent/internal/platform/onebotv11"
)

func TestOneBotAdapterRoutesPrivateCommandsThroughPlatformGateway(t *testing.T) {
	ctx := context.Background()
	for _, command := range []string{"/sid", "/new", "/reset", "/clear", "/stop"} {
		t.Run(command, func(t *testing.T) {
			app, _, gateway, fakeLLM := newTestPlatformGateway(t)
			transport := &fakeOneBotTransport{}
			adapter := newTestOneBotAdapter(t, transport)
			if err := adapter.Start(ctx, gateway); err != nil {
				t.Fatalf("adapter.Start: %v", err)
			}
			defer adapter.Stop(ctx)

			if command == "/stop" {
				origin := conversation.Origin{
					OriginKey:              "onebot:qq-main:private:10001",
					SourceType:             "onebot",
					AdapterInstanceID:      "qq-main",
					PlatformID:             "qq",
					ChannelType:            "private",
					ExternalConversationID: "10001",
					ExternalActorID:        "10001",
				}
				binding, err := app.kernel.Services.Conversation.Bindings().EnsureCurrent(ctx, origin, "default")
				if err != nil {
					t.Fatalf("EnsureCurrent: %v", err)
				}
				unregister := app.kernel.Services.Conversation.RunRegistry().Register(conversation.RunRef{
					OriginKey: origin.OriginKey,
					SessionID: binding.SessionID,
					Kind:      "platform_text",
				}, func() {})
				t.Cleanup(unregister)
			}

			result, err := adapter.HandleEvent(ctx, onebotPrivateEvent(t, command, 24680))
			if err != nil {
				t.Fatalf("HandleEvent: %v", err)
			}
			if !result.Handled || result.Duplicate || result.SessionID == "" {
				t.Fatalf("result = %#v, want handled command", result)
			}
			if fakeLLM.calls != 0 {
				t.Fatalf("LLM calls = %d, want command to bypass chat engine", fakeLLM.calls)
			}
			if len(transport.requests) != 1 {
				t.Fatalf("onebot requests = %#v, want one visible command message", transport.requests)
			}
			if transport.requests[0].Action != "send_private_msg" || onebotRequestText(transport.requests[0]) == "" {
				t.Fatalf("onebot request = %#v", transport.requests[0])
			}
		})
	}
}

func TestOneBotAdapterRoutesPrivateTextThroughPlatformGateway(t *testing.T) {
	ctx := context.Background()
	_, _, gateway, fakeLLM := newTestPlatformGateway(t)
	transport := &fakeOneBotTransport{}
	adapter := newTestOneBotAdapter(t, transport)
	if err := adapter.Start(ctx, gateway); err != nil {
		t.Fatalf("adapter.Start: %v", err)
	}
	defer adapter.Stop(ctx)

	result, err := adapter.HandleEvent(ctx, onebotPrivateEvent(t, "hello", 24681))
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if !result.Handled || result.SessionID == "" {
		t.Fatalf("result = %#v, want handled text", result)
	}
	if fakeLLM.calls != 1 {
		t.Fatalf("LLM calls = %d, want one normal chat call", fakeLLM.calls)
	}
	if len(transport.requests) != 1 || onebotRequestText(transport.requests[0]) != "platform reply" {
		t.Fatalf("onebot requests = %#v, want platform reply", transport.requests)
	}
}

func TestOneBotAdapterIgnoresGroupMessagesBeforeGateway(t *testing.T) {
	ctx := context.Background()
	_, _, gateway, fakeLLM := newTestPlatformGateway(t)
	transport := &fakeOneBotTransport{}
	adapter := newTestOneBotAdapter(t, transport)
	if err := adapter.Start(ctx, gateway); err != nil {
		t.Fatalf("adapter.Start: %v", err)
	}
	defer adapter.Stop(ctx)

	var event onebotv11.Event
	if err := json.Unmarshal([]byte(`{
		"time": 1710000000,
		"self_id": 123456,
		"post_type": "message",
		"message_type": "group",
		"group_id": 9988,
		"user_id": 10001,
		"message_id": 1,
		"message": [{"type":"text","data":{"text":"hello group"}}]
	}`), &event); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	result, err := adapter.HandleEvent(ctx, event)
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if !result.Ignored || result.Handled {
		t.Fatalf("result = %#v, want ignored", result)
	}
	if fakeLLM.calls != 0 || len(transport.requests) != 0 {
		t.Fatalf("LLM calls=%d requests=%#v, want no gateway side effects", fakeLLM.calls, transport.requests)
	}
}

func newTestOneBotAdapter(t *testing.T, transport onebotv11.Transport) *onebotv11.Adapter {
	t.Helper()
	cfg, err := onebotv11.ParseConfig("qq-main", appconfig.PlatformAdapterConfig{
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
		t.Fatalf("ParseConfig: %v", err)
	}
	adapter, err := onebotv11.NewAdapterWithTransport("qq-main", cfg, transport)
	if err != nil {
		t.Fatalf("NewAdapterWithTransport: %v", err)
	}
	return adapter
}

func onebotPrivateEvent(t *testing.T, text string, messageID int) onebotv11.Event {
	t.Helper()
	body := strings.ReplaceAll(`{
		"time": 1710000000,
		"self_id": 123456,
		"post_type": "message",
		"message_type": "private",
		"user_id": 10001,
		"message_id": MESSAGE_ID,
		"message": [{"type":"text","data":{"text":MESSAGE_TEXT}}],
		"sender": {"user_id":10001,"nickname":"Alice"}
	}`, "MESSAGE_ID", jsonNumberString(messageID))
	body = strings.ReplaceAll(body, "MESSAGE_TEXT", jsonString(text))
	var event onebotv11.Event
	if err := json.Unmarshal([]byte(body), &event); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return event
}

func jsonNumberString(v int) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func jsonString(v string) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func onebotRequestText(req onebotv11.ActionRequest) string {
	switch message := req.Params["message"].(type) {
	case []onebotv11.Segment:
		if len(message) == 0 {
			return ""
		}
		return message[0].Data["text"]
	case string:
		return message
	default:
		return ""
	}
}

type fakeOneBotTransport struct {
	handler  onebotv11.EventHandler
	requests []onebotv11.ActionRequest
}

func (t *fakeOneBotTransport) Start(_ context.Context, handler onebotv11.EventHandler) error {
	t.handler = handler
	return nil
}

func (t *fakeOneBotTransport) Stop(context.Context) error {
	return nil
}

func (t *fakeOneBotTransport) Call(_ context.Context, req onebotv11.ActionRequest) (onebotv11.ActionResponse, error) {
	t.requests = append(t.requests, req)
	return onebotv11.ActionResponse{Status: "ok", Retcode: 0, Echo: req.Echo}, nil
}

func (t *fakeOneBotTransport) Status() onebotv11.TransportStatus {
	return onebotv11.TransportStatus{Mode: onebotv11.TransportModeWSReverse, State: "test", Connected: true}
}

var _ onebotv11.Transport = (*fakeOneBotTransport)(nil)
var _ platform.InboundHandler = (*PlatformGateway)(nil)
