package onebotv11

import (
	"encoding/json"
	"testing"

	"github.com/longyisang/emoagent/internal/platform"
)

func TestSnowLumaPrivateMessageArray(t *testing.T) {
	event := mustSnowLumaEvent(t, `{
		"time": 1710000000,
		"self_id": 123456,
		"post_type": "message",
		"message_type": "private",
		"sub_type": "friend",
		"message_id": 24680,
		"user_id": 10001,
		"raw_message": "hello",
		"message": [{"type":"text","data":{"text":"hello"}}],
		"sender": {"user_id":10001,"nickname":"Alice"}
	}`)
	in, accepted, err := MapEvent(event, snowLumaTestConfig(), ProfileSnowLuma())
	if err != nil {
		t.Fatalf("MapEvent: %v", err)
	}
	if !accepted {
		t.Fatal("accepted = false, want true")
	}
	if in.Text != "hello" ||
		in.SourceType != "onebot" ||
		in.PlatformID != "qq" ||
		in.OriginScope != platform.OriginScopePrivate {
		t.Fatalf("inbound = %#v", in)
	}
}

func TestSnowLumaMessageSentIgnored(t *testing.T) {
	event := mustSnowLumaEvent(t, `{
		"time": 1710000000,
		"self_id": 123456,
		"post_type": "message_sent",
		"message_type": "private",
		"message_id": 24680,
		"user_id": 10001,
		"message": [{"type":"text","data":{"text":"hello"}}]
	}`)
	if _, accepted, err := MapEvent(event, snowLumaTestConfig(), ProfileSnowLuma()); err != nil || accepted {
		t.Fatalf("MapEvent accepted=%v err=%v, want ignored nil error", accepted, err)
	}
}

func TestSnowLumaGroupMessageIgnored(t *testing.T) {
	event := mustSnowLumaEvent(t, `{
		"time": 1710000000,
		"self_id": 123456,
		"post_type": "message",
		"message_type": "group",
		"group_id": 20002,
		"user_id": 10001,
		"message_id": 24680,
		"message": [{"type":"text","data":{"text":"hello group"}}]
	}`)
	if _, accepted, err := MapEvent(event, snowLumaTestConfig(), ProfileSnowLuma()); err != nil || accepted {
		t.Fatalf("MapEvent accepted=%v err=%v, want ignored nil error", accepted, err)
	}
}

func TestSnowLumaImagePlaceholder(t *testing.T) {
	event := mustSnowLumaEvent(t, `{
		"time": 1710000000,
		"self_id": 123456,
		"post_type": "message",
		"message_type": "private",
		"message_id": 24680,
		"user_id": 10001,
		"message": [{"type":"image","data":{"file":"a.jpg","url":"http://127.0.0.1/a.jpg"}}]
	}`)
	in, accepted, err := MapEvent(event, snowLumaTestConfig(), ProfileSnowLuma())
	if err != nil {
		t.Fatalf("MapEvent: %v", err)
	}
	if !accepted {
		t.Fatal("accepted = false, want true")
	}
	if in.Text != "[图片]" || len(in.Parts) != 0 {
		t.Fatalf("text/parts = %q/%d, want image placeholder and no parts", in.Text, len(in.Parts))
	}
}

func TestSnowLumaSendPrivateActionShape(t *testing.T) {
	req := sendPrivateMsgRequest("10001", outboundTextMessage("hello", MessageFormatArray), false)
	if req.Action != "send_private_msg" {
		t.Fatalf("action = %q", req.Action)
	}
	if req.Params["user_id"] != int64(10001) {
		t.Fatalf("user_id = %#v, want numeric int64", req.Params["user_id"])
	}
	segments, ok := req.Params["message"].([]Segment)
	if !ok || len(segments) != 1 || segments[0].Type != "text" || segments[0].Data["text"] != "hello" {
		t.Fatalf("message = %#v, want array text segment", req.Params["message"])
	}
}

func mustSnowLumaEvent(t *testing.T, body string) Event {
	t.Helper()
	var event Event
	if err := json.Unmarshal([]byte(body), &event); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return event
}

func snowLumaTestConfig() Config {
	cfg := testConfig()
	cfg.Implementation = "snowluma"
	return cfg
}
