package onebotv11

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"
)

type ReverseTransport struct {
	cfg TransportConfig

	mu      sync.RWMutex
	conn    *universalWSConn
	handler EventHandler
	status  TransportStatus
}

func NewReverseTransport(cfg TransportConfig) *ReverseTransport {
	return &ReverseTransport{cfg: cfg, status: TransportStatus{Mode: TransportModeWSReverse, State: "waiting"}}
}

func (t *ReverseTransport) Start(_ context.Context, handler EventHandler) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.handler = handler
	t.status.State = "waiting"
	return nil
}

func (t *ReverseTransport) Stop(ctx context.Context) error {
	t.mu.Lock()
	conn := t.conn
	t.conn = nil
	t.status.State = "stopped"
	t.status.Connected = false
	t.mu.Unlock()
	if conn != nil {
		return conn.conn.Close(websocket.StatusNormalClosure, "stop")
	}
	return nil
}

func (t *ReverseTransport) Call(ctx context.Context, req ActionRequest) (ActionResponse, error) {
	t.mu.RLock()
	conn := t.conn
	t.mu.RUnlock()
	if conn == nil {
		return ActionResponse{}, fmt.Errorf("onebot reverse websocket is not connected")
	}
	callCtx, cancel := contextWithRequestTimeout(ctx, t.cfg.RequestTimeoutMS)
	defer cancel()
	return conn.Call(callCtx, req)
}

func (t *ReverseTransport) Status() TransportStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.status
}

func (t *ReverseTransport) Attach(ctx context.Context, conn *websocket.Conn, selfID string) error {
	t.mu.RLock()
	handler := t.handler
	t.mu.RUnlock()
	uws := newUniversalWSConn(conn, handler)
	t.mu.Lock()
	if t.conn != nil {
		_ = t.conn.conn.Close(websocket.StatusNormalClosure, "replaced")
	}
	t.conn = uws
	t.status.State = "connected"
	t.status.Connected = true
	t.status.SelfID = selfID
	t.mu.Unlock()
	err := uws.run(ctx)
	t.mu.Lock()
	if t.conn == uws {
		t.conn = nil
		t.status.State = "waiting"
		t.status.Connected = false
	}
	t.mu.Unlock()
	return err
}

type ReverseServer struct {
	mu       sync.RWMutex
	adapters map[string]*Adapter
}

func NewReverseServer() *ReverseServer {
	return &ReverseServer{adapters: map[string]*Adapter{}}
}

func (s *ReverseServer) RegisterAdapter(id string, adapter *Adapter) {
	if s == nil || strings.TrimSpace(id) == "" || adapter == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adapters[id] = adapter
}

func (s *ReverseServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	adapterID := r.PathValue("adapter_id")
	if adapterID == "" {
		adapterID = adapterIDFromPath(r.URL.Path)
	}
	adapter := s.adapter(adapterID)
	if adapter == nil {
		http.Error(w, "onebot adapter not found", http.StatusNotFound)
		return
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Client-Role")), "Universal") {
		http.Error(w, "onebot reverse ws only supports X-Client-Role=Universal", http.StatusBadRequest)
		return
	}
	if token := adapter.cfg.Transport.BearerToken(); token != "" {
		if got := bearerToken(r.Header.Get("Authorization")); got != token {
			http.Error(w, "onebot reverse ws unauthorized", http.StatusUnauthorized)
			return
		}
	}
	transport, ok := adapter.ReverseTransport()
	if !ok {
		http.Error(w, "onebot adapter is not configured for reverse ws", http.StatusBadRequest)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")
	_ = transport.Attach(r.Context(), conn, strings.TrimSpace(r.Header.Get("X-Self-ID")))
}

func (s *ReverseServer) adapter(id string) *Adapter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.adapters[id]
}

func adapterIDFromPath(path string) string {
	const prefix = "/api/platforms/onebot/v11/"
	path = strings.TrimPrefix(path, prefix)
	path = strings.TrimSuffix(path, "/ws")
	return strings.Trim(path, "/")
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[len("bearer "):])
	}
	return ""
}
