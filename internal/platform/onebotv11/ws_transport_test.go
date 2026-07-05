package onebotv11

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/longyisang/emoagent/internal/platform"
)

func TestWSClientUniversalFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	actionSeen := make(chan ActionRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q", got)
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "bye")
		if err := wsjson.Write(ctx, conn, map[string]any{
			"time":         1710000000,
			"self_id":      123456,
			"post_type":    "message",
			"message_type": "private",
			"message_id":   24680,
			"user_id":      10001,
			"message":      []map[string]any{{"type": "text", "data": map[string]string{"text": "hello"}}},
		}); err != nil {
			t.Errorf("write event: %v", err)
			return
		}
		var req ActionRequest
		if err := wsjson.Read(ctx, conn, &req); err != nil {
			t.Errorf("read action: %v", err)
			return
		}
		actionSeen <- req
		_ = wsjson.Write(ctx, conn, ActionResponse{Status: "ok", Retcode: 0, Echo: req.Echo})
		<-ctx.Done()
	}))
	defer srv.Close()

	cfg := testConfig()
	cfg.Transport.Mode = TransportModeWSClient
	cfg.Transport.URL = "ws" + strings.TrimPrefix(srv.URL, "http")
	cfg.Transport.AccessToken = "secret"
	adapter, err := NewAdapterWithTransport("qq-main", cfg, nil)
	if err != nil {
		t.Fatalf("NewAdapterWithTransport: %v", err)
	}
	handler := inboundHandlerFunc(func(ctx context.Context, in platform.InboundMessage, sink platform.OutboundSink) (platform.HandleResult, error) {
		if in.Text != "hello" || in.SourceType != "onebot" {
			t.Fatalf("inbound = %#v", in)
		}
		if err := sink.Emit(ctx, platform.OutboundEvent{Type: "message", Origin: privateOrigin(), Content: "reply"}); err != nil {
			return platform.HandleResult{}, err
		}
		return platform.HandleResult{Handled: true, SessionID: "s1"}, nil
	})
	if err := adapter.Start(ctx, handler); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer adapter.Stop(context.Background())

	select {
	case req := <-actionSeen:
		if req.Action != "send_private_msg" || req.Params["user_id"] != float64(10001) {
			t.Fatalf("action = %#v", req)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for action")
	}
}

func TestWSClientTransportCallUsesRequestTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	actionSeen := make(chan ActionRequest, 1)
	releaseServer := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "bye")
		var req ActionRequest
		if err := wsjson.Read(ctx, conn, &req); err != nil {
			t.Errorf("read action: %v", err)
			return
		}
		actionSeen <- req
		<-releaseServer
	}))
	defer func() {
		close(releaseServer)
		srv.Close()
	}()

	transport := NewWSClientTransport(TransportConfig{
		Mode:                TransportModeWSClient,
		URL:                 "ws" + strings.TrimPrefix(srv.URL, "http"),
		RequestTimeoutMS:    30,
		ConnectTimeoutMS:    500,
		ReconnectIntervalMS: 1000,
	})
	if err := transport.Start(ctx, onebotEventHandlerFunc(func(context.Context, Event) error { return nil })); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer transport.Stop(context.Background())
	waitForWSClientConnected(t, transport)

	start := time.Now()
	callCtx, callCancel := context.WithTimeout(context.Background(), time.Second)
	defer callCancel()
	_, err := transport.Call(callCtx, ActionRequest{Action: "send_private_msg", Params: map[string]any{"user_id": "10001", "message": "hello"}})
	if err == nil {
		t.Fatal("Call err = nil, want timeout")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("Call elapsed = %s, want request_timeout_ms to bound wait", elapsed)
	}
	select {
	case req := <-actionSeen:
		if req.Echo == "" {
			t.Fatalf("request echo was empty: %#v", req)
		}
	case <-ctx.Done():
		t.Fatal("server did not receive action request")
	}
}

func TestReverseWSUniversalFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := testConfig()
	cfg.Transport.Mode = TransportModeWSReverse
	cfg.Transport.AccessToken = "secret"
	adapter, err := NewAdapterWithTransport("qq-main", cfg, nil)
	if err != nil {
		t.Fatalf("NewAdapterWithTransport: %v", err)
	}
	handlerCalled := make(chan struct{}, 1)
	handler := inboundHandlerFunc(func(ctx context.Context, in platform.InboundMessage, sink platform.OutboundSink) (platform.HandleResult, error) {
		handlerCalled <- struct{}{}
		if err := sink.Emit(ctx, platform.OutboundEvent{Type: "message", Origin: privateOrigin(), Content: "reverse reply"}); err != nil {
			return platform.HandleResult{}, err
		}
		return platform.HandleResult{Handled: true, SessionID: "s1"}, nil
	})
	if err := adapter.Start(ctx, handler); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer adapter.Stop(context.Background())

	reverse := NewReverseServer()
	reverse.RegisterAdapter("qq-main", adapter)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/platforms/onebot/v11/{adapter_id}/ws", reverse.ServeHTTP)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	headers := http.Header{}
	headers.Set("X-Client-Role", "Universal")
	headers.Set("X-Self-ID", "123456")
	headers.Set("Authorization", "Bearer secret")
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/platforms/onebot/v11/qq-main/ws", &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatalf("Dial reverse: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	if err := wsjson.Write(ctx, conn, map[string]any{
		"time":         1710000000,
		"self_id":      123456,
		"post_type":    "message",
		"message_type": "private",
		"message_id":   24680,
		"user_id":      10001,
		"message":      []map[string]any{{"type": "text", "data": map[string]string{"text": "hello"}}},
	}); err != nil {
		t.Fatalf("write event: %v", err)
	}
	var req ActionRequest
	if err := wsjson.Read(ctx, conn, &req); err != nil {
		t.Fatalf("read action: %v", err)
	}
	if req.Action != "send_private_msg" {
		t.Fatalf("action = %#v", req)
	}
	if err := wsjson.Write(ctx, conn, ActionResponse{Status: "ok", Retcode: 0, Echo: req.Echo}); err != nil {
		t.Fatalf("write response: %v", err)
	}
	select {
	case <-handlerCalled:
	case <-ctx.Done():
		t.Fatal("handler not called")
	}
}

func TestReverseWSRejectsUnsupportedRole(t *testing.T) {
	cfg := testConfig()
	adapter, err := NewAdapterWithTransport("qq-main", cfg, nil)
	if err != nil {
		t.Fatalf("NewAdapterWithTransport: %v", err)
	}
	if err := adapter.Start(context.Background(), inboundHandlerFunc(func(context.Context, platform.InboundMessage, platform.OutboundSink) (platform.HandleResult, error) {
		return platform.HandleResult{}, nil
	})); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer adapter.Stop(context.Background())

	reverse := NewReverseServer()
	reverse.RegisterAdapter("qq-main", adapter)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/platforms/onebot/v11/{adapter_id}/ws", reverse.ServeHTTP)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	headers := http.Header{}
	headers.Set("X-Client-Role", "Event")
	_, resp, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/platforms/onebot/v11/qq-main/ws", &websocket.DialOptions{HTTPHeader: headers})
	if err == nil {
		t.Fatal("Dial err = nil, want unsupported role rejection")
	}
	if resp == nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("response = %#v, want 400", resp)
	}
}

func TestReverseTransportCallUsesRequestTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	transport := NewReverseTransport(TransportConfig{RequestTimeoutMS: 30})
	if err := transport.Start(ctx, onebotEventHandlerFunc(func(context.Context, Event) error {
		return nil
	})); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer transport.Stop(context.Background())

	accepted := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("Accept: %v", err)
			return
		}
		close(accepted)
		_ = transport.Attach(r.Context(), conn, "123456")
	}))
	defer srv.Close()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")
	select {
	case <-accepted:
	case <-ctx.Done():
		t.Fatal("reverse websocket was not accepted")
	}

	start := time.Now()
	callCtx, callCancel := context.WithTimeout(context.Background(), time.Second)
	defer callCancel()
	_, err = transport.Call(callCtx, ActionRequest{Action: "send_private_msg", Params: map[string]any{"user_id": "10001", "message": "hello"}})
	if err == nil {
		t.Fatal("Call err = nil, want timeout")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("Call elapsed = %s, want request_timeout_ms to bound wait", elapsed)
	}
}

type inboundHandlerFunc func(context.Context, platform.InboundMessage, platform.OutboundSink) (platform.HandleResult, error)

func (f inboundHandlerFunc) HandleInbound(ctx context.Context, in platform.InboundMessage, sink platform.OutboundSink) (platform.HandleResult, error) {
	return f(ctx, in, sink)
}

type onebotEventHandlerFunc func(context.Context, Event) error

func (f onebotEventHandlerFunc) HandleOneBotEvent(ctx context.Context, event Event) error {
	return f(ctx, event)
}

func readJSONRaw(t *testing.T, conn *websocket.Conn) map[string]json.RawMessage {
	t.Helper()
	var mu sync.Mutex
	mu.Lock()
	defer mu.Unlock()
	var raw map[string]json.RawMessage
	if err := wsjson.Read(context.Background(), conn, &raw); err != nil {
		t.Fatalf("read raw: %v", err)
	}
	return raw
}

func waitForWSClientConnected(t *testing.T, transport *WSClientTransport) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if transport.Status().Connected {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("transport status = %#v, want connected", transport.Status())
}
