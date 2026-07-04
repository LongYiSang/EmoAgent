package onebotv11

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/conversation"
	"github.com/longyisang/emoagent/internal/platform"
)

func TestSinkSendsPrivateMessageAction(t *testing.T) {
	client := &recordingActionClient{}
	sink := NewSink(client, ProfileGeneric(), OutboundConfig{
		AutoEscape:            false,
		CoalesceCommandEvents: true,
		SplitLongMessages:     true,
		MaxMessageChars:       1800,
	})

	err := sink.Emit(context.Background(), platform.OutboundEvent{
		Type:    "message",
		Origin:  privateOrigin(),
		Content: "hello",
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %#v, want one", client.requests)
	}
	req := client.requests[0]
	if req.Action != "send_private_msg" {
		t.Fatalf("action = %q", req.Action)
	}
	if req.Params["user_id"] != "10001" {
		t.Fatalf("user_id param = %#v", req.Params["user_id"])
	}
	segments, ok := req.Params["message"].([]Segment)
	if !ok || len(segments) != 1 || segments[0].Type != "text" || segments[0].Data["text"] != "hello" {
		t.Fatalf("message param = %#v", req.Params["message"])
	}
	if req.Params["auto_escape"] != false {
		t.Fatalf("auto_escape = %#v", req.Params["auto_escape"])
	}
}

func TestSinkCoalescesDuplicateCommandEvents(t *testing.T) {
	client := &recordingActionClient{}
	sink := NewSink(client, ProfileGeneric(), OutboundConfig{
		CoalesceCommandEvents: true,
		SplitLongMessages:     true,
		MaxMessageChars:       1800,
	})
	origin := privateOrigin()
	first := platform.OutboundEvent{Type: "context_switched", Origin: origin, SessionID: "s1", Content: "已切换到新会话"}
	second := platform.OutboundEvent{Type: "command_result", Origin: origin, SessionID: "s1", Content: "已切换到新会话"}
	if err := sink.Emit(context.Background(), first); err != nil {
		t.Fatalf("Emit first: %v", err)
	}
	if err := sink.Emit(context.Background(), second); err != nil {
		t.Fatalf("Emit second: %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %#v, want one visible command message", client.requests)
	}
}

func TestSinkSplitsLongTextByRunes(t *testing.T) {
	client := &recordingActionClient{}
	sink := NewSink(client, ProfileGeneric(), OutboundConfig{
		SplitLongMessages: true,
		MaxMessageChars:   4,
	})
	err := sink.Emit(context.Background(), platform.OutboundEvent{
		Type:    "message",
		Origin:  privateOrigin(),
		Content: "你好世界测试",
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if len(client.requests) != 2 {
		t.Fatalf("requests = %d, want two", len(client.requests))
	}
	got := []string{
		client.requests[0].Params["message"].([]Segment)[0].Data["text"],
		client.requests[1].Params["message"].([]Segment)[0].Data["text"],
	}
	if strings.Join(got, "|") != "你好世界|测试" {
		t.Fatalf("split text = %#v", got)
	}
}

func TestSinkReturnsRetcodeError(t *testing.T) {
	client := &recordingActionClient{response: ActionResponse{Status: "failed", Retcode: 100, Wording: "bad request"}}
	sink := NewSink(client, ProfileGeneric(), OutboundConfig{MaxMessageChars: 1800})
	err := sink.Emit(context.Background(), platform.OutboundEvent{
		Type:    "message",
		Origin:  privateOrigin(),
		Content: "hello",
	})
	if err == nil || !strings.Contains(err.Error(), "retcode=100") {
		t.Fatalf("Emit error = %v, want retcode error", err)
	}
}

func TestSinkRejectsGroupOutboundWhenDisabled(t *testing.T) {
	client := &recordingActionClient{}
	sink := NewSink(client, ProfileGeneric(), OutboundConfig{MaxMessageChars: 1800})
	err := sink.Emit(context.Background(), platform.OutboundEvent{
		Type: "message",
		Origin: conversation.Origin{
			OriginKey:              "onebot:qq-main:group:20002",
			SourceType:             "onebot",
			AdapterInstanceID:      "qq-main",
			PlatformID:             "qq",
			ChannelType:            "group",
			ExternalConversationID: "20002",
			ExternalActorID:        "10001",
		},
		Content: "hello group",
	})
	if err == nil || !strings.Contains(err.Error(), "group outbound is disabled") {
		t.Fatalf("Emit error = %v, want group disabled error", err)
	}
	if len(client.requests) != 0 {
		t.Fatalf("requests = %#v, want no group action", client.requests)
	}
}

func TestEchoStoreTimeoutAndResponse(t *testing.T) {
	store := newEchoStore()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	wait := store.register("echo-1")
	if _, err := wait(ctx); err == nil {
		t.Fatal("wait err = nil, want timeout")
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	wait2 := store.register("echo-2")
	if !store.resolve(ActionResponse{Status: "ok", Retcode: 0, Echo: "echo-2"}) {
		t.Fatal("resolve returned false, want true")
	}
	resp, err := wait2(ctx2)
	if err != nil {
		t.Fatalf("wait2: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("resp = %#v", resp)
	}
	if store.resolve(ActionResponse{Status: "ok", Retcode: 0, Echo: "unknown"}) {
		t.Fatal("unknown echo resolved, want false")
	}
}

func privateOrigin() conversation.Origin {
	return conversation.Origin{
		OriginKey:              "onebot:qq-main:private:10001",
		SourceType:             "onebot",
		AdapterInstanceID:      "qq-main",
		PlatformID:             "qq",
		ChannelType:            "private",
		ExternalConversationID: "10001",
		ExternalActorID:        "10001",
	}
}

type recordingActionClient struct {
	requests []ActionRequest
	response ActionResponse
	err      error
}

func (c *recordingActionClient) Call(_ context.Context, req ActionRequest) (ActionResponse, error) {
	c.requests = append(c.requests, req)
	if c.err != nil {
		return ActionResponse{}, c.err
	}
	if c.response.Status != "" || c.response.Retcode != 0 {
		return c.response, nil
	}
	return ActionResponse{Status: "ok", Retcode: 0}, nil
}
