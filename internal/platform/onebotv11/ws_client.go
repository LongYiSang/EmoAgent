package onebotv11

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

var echoCounter uint64

type WSClientTransport struct {
	cfg TransportConfig

	mu      sync.RWMutex
	conn    *universalWSConn
	cancel  context.CancelFunc
	status  TransportStatus
	handler EventHandler
}

func NewWSClientTransport(cfg TransportConfig) *WSClientTransport {
	return &WSClientTransport{cfg: cfg, status: TransportStatus{Mode: TransportModeWSClient, URL: cfg.URL, State: "stopped"}}
}

func (t *WSClientTransport) Start(ctx context.Context, handler EventHandler) error {
	t.mu.Lock()
	if t.cancel != nil {
		t.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel
	t.handler = handler
	t.status.State = "connecting"
	t.mu.Unlock()
	go t.connectLoop(runCtx)
	return nil
}

func (t *WSClientTransport) Stop(ctx context.Context) error {
	t.mu.Lock()
	cancel := t.cancel
	t.cancel = nil
	conn := t.conn
	t.conn = nil
	t.status.State = "stopped"
	t.status.Connected = false
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		return conn.conn.Close(websocket.StatusNormalClosure, "stop")
	}
	return nil
}

func (t *WSClientTransport) Call(ctx context.Context, req ActionRequest) (ActionResponse, error) {
	conn := t.activeConn()
	if conn == nil {
		return ActionResponse{}, fmt.Errorf("onebot ws_client is not connected")
	}
	callCtx, cancel := contextWithRequestTimeout(ctx, t.cfg.RequestTimeoutMS)
	defer cancel()
	return conn.Call(callCtx, req)
}

func (t *WSClientTransport) Status() TransportStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.status
}

func (t *WSClientTransport) connectLoop(ctx context.Context) {
	reconnect := time.Duration(t.cfg.ReconnectIntervalMS) * time.Millisecond
	if reconnect <= 0 {
		reconnect = 3 * time.Second
	}
	for ctx.Err() == nil {
		conn, err := t.dial(ctx)
		if err != nil {
			t.setStatus("disconnected", false)
			if !sleepContext(ctx, reconnect) {
				return
			}
			continue
		}
		uws := newUniversalWSConn(conn, t.handler)
		t.mu.Lock()
		t.conn = uws
		t.status.State = "connected"
		t.status.Connected = true
		t.mu.Unlock()
		_ = uws.run(ctx)
		t.mu.Lock()
		if t.conn == uws {
			t.conn = nil
		}
		t.status.State = "disconnected"
		t.status.Connected = false
		t.mu.Unlock()
		if !sleepContext(ctx, reconnect) {
			return
		}
	}
}

func (t *WSClientTransport) dial(ctx context.Context) (*websocket.Conn, error) {
	dialCtx := ctx
	cancel := func() {}
	if t.cfg.ConnectTimeoutMS > 0 {
		dialCtx, cancel = context.WithTimeout(ctx, time.Duration(t.cfg.ConnectTimeoutMS)*time.Millisecond)
	}
	defer cancel()
	options := &websocket.DialOptions{}
	if token := t.cfg.BearerToken(); token != "" {
		options.HTTPHeader = http.Header{"Authorization": []string{"Bearer " + token}}
	}
	conn, _, err := websocket.Dial(dialCtx, t.cfg.URL, options)
	return conn, err
}

func (t *WSClientTransport) activeConn() *universalWSConn {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.conn
}

func (t *WSClientTransport) setStatus(state string, connected bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.State = state
	t.status.Connected = connected
}

func nextEcho() string {
	id := atomic.AddUint64(&echoCounter, 1)
	return fmt.Sprintf("emoagent-%d", id)
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
