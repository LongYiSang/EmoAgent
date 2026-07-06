package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/platform"
)

func TestPlatformServiceConfigureDisabledNoop(t *testing.T) {
	service := newBarePlatformService()
	if err := service.Configure(context.Background(), config.PlatformsConfig{Enabled: false}); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if _, ok := service.Manager().Adapter("qq-main"); ok {
		t.Fatal("adapter registered for disabled platforms")
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := service.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestPlatformServiceConfiguresOneBotReverseRoute(t *testing.T) {
	service := newBarePlatformService()
	cfg := config.PlatformsConfig{
		Enabled: true,
		Adapters: map[string]config.PlatformAdapterConfig{
			"qq-main": {
				Enabled:    true,
				Kind:       "onebot_v11",
				InstanceID: "qq-main",
				PlatformID: "qq",
				ConfigJSON: map[string]any{
					"implementation": "napcat",
					"transport": map[string]any{
						"mode": "ws_reverse",
					},
				},
			},
		},
	}
	if err := service.Configure(context.Background(), cfg); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if _, ok := service.Manager().Adapter("qq-main"); !ok {
		t.Fatal("qq-main adapter was not registered")
	}
	mux := http.NewServeMux()
	service.InstallHTTPRoutes(mux)
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = service.Stop(context.Background()) })

	req := httptest.NewRequest(http.MethodGet, "/api/platforms/onebot/v11/qq-main/ws", nil)
	req.Header.Set("X-Client-Role", "Event")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("route status = %d body=%q, want onebot reverse handler rejection", rr.Code, rr.Body.String())
	}
}

func TestPlatformServiceReverseRouteSurvivesDisabledToEnabledReconfigure(t *testing.T) {
	service := newBarePlatformService()
	mux := http.NewServeMux()
	service.InstallHTTPRoutes(mux)
	if err := service.Configure(context.Background(), config.PlatformsConfig{Enabled: false}); err != nil {
		t.Fatalf("Configure disabled: %v", err)
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start disabled: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/platforms/onebot/v11/qq-main/ws", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("disabled route status = %d body=%q, want adapter not found", rr.Code, rr.Body.String())
	}

	if err := service.Configure(context.Background(), config.PlatformsConfig{
		Enabled: true,
		Adapters: map[string]config.PlatformAdapterConfig{
			"qq-main": {
				Enabled:    true,
				Kind:       "onebot_v11",
				InstanceID: "qq-main",
				PlatformID: "qq",
				ConfigJSON: map[string]any{
					"implementation": "snowluma",
					"transport": map[string]any{
						"mode": "ws_reverse",
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("Configure enabled: %v", err)
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start enabled: %v", err)
	}
	t.Cleanup(func() { _ = service.Stop(context.Background()) })

	req = httptest.NewRequest(http.MethodGet, "/api/platforms/onebot/v11/qq-main/ws", nil)
	req.Header.Set("X-Client-Role", "Event")
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("enabled route status = %d body=%q, want reverse handler rejection", rr.Code, rr.Body.String())
	}
}

func TestPlatformServiceConfigureRejectsInvalidOneBotConfig(t *testing.T) {
	service := newBarePlatformService()
	err := service.Configure(context.Background(), config.PlatformsConfig{
		Enabled: true,
		Adapters: map[string]config.PlatformAdapterConfig{
			"qq-main": {
				Enabled: true,
				Kind:    "onebot_v11",
				ConfigJSON: map[string]any{
					"implementation": "unknown",
					"transport": map[string]any{
						"mode": "ws_reverse",
					},
				},
			},
		},
	})
	if err == nil {
		t.Fatal("Configure err = nil, want invalid onebot config error")
	}
}

func TestPlatformServiceStatusDisabled(t *testing.T) {
	service := newBarePlatformService()
	if err := service.Configure(context.Background(), config.PlatformsConfig{Enabled: false}); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	status := service.Status()
	if status.Enabled || len(status.Adapters) != 0 {
		t.Fatalf("status = %#v, want disabled with no adapters", status)
	}
}

func TestPlatformServiceStatusOneBotReverseWaitingDoesNotLeakToken(t *testing.T) {
	service := newBarePlatformService()
	cfg := config.PlatformsConfig{
		Enabled: true,
		Adapters: map[string]config.PlatformAdapterConfig{
			"qq-main": {
				Enabled:    true,
				Kind:       "onebot_v11",
				InstanceID: "qq-main",
				PlatformID: "qq",
				ConfigJSON: map[string]any{
					"implementation": "snowluma",
					"transport": map[string]any{
						"mode":         "ws_reverse",
						"access_token": "dev-token",
					},
				},
			},
		},
	}
	if err := service.Configure(context.Background(), cfg); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = service.Stop(context.Background()) })

	status := service.Status()
	if !status.Enabled || len(status.Adapters) != 1 {
		t.Fatalf("status = %#v, want one enabled adapter", status)
	}
	adapter := status.Adapters[0]
	if adapter.ID != "qq-main" ||
		adapter.Kind != "onebot_v11" ||
		adapter.Implementation != "snowluma" ||
		adapter.SourceType != "onebot" ||
		adapter.PlatformID != "qq" ||
		adapter.InstanceID != "qq-main" {
		t.Fatalf("adapter status = %#v", adapter)
	}
	if adapter.Transport.Mode != "ws_reverse" ||
		adapter.Transport.State != "waiting" ||
		adapter.Transport.Connected {
		t.Fatalf("transport status = %#v, want waiting reverse ws", adapter.Transport)
	}
	if !adapter.Routing.PrivateEnabled || adapter.Routing.GroupEnabled || !adapter.Routing.IgnoreSelfMessages {
		t.Fatalf("routing status = %#v", adapter.Routing)
	}
	if !adapter.Auth.AccessTokenConfigured {
		t.Fatalf("auth status = %#v, want access_token_configured", adapter.Auth)
	}
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Marshal status: %v", err)
	}
	if strings.Contains(string(data), "dev-token") {
		t.Fatalf("status leaked token: %s", data)
	}
}

func TestPlatformServiceStatusOneBotReverseConnectedSelfID(t *testing.T) {
	service := newBarePlatformService()
	cfg := config.PlatformsConfig{
		Enabled: true,
		Adapters: map[string]config.PlatformAdapterConfig{
			"qq-main": {
				Enabled:    true,
				Kind:       "onebot_v11",
				InstanceID: "qq-main",
				PlatformID: "qq",
				ConfigJSON: map[string]any{
					"implementation": "snowluma",
					"transport": map[string]any{
						"mode": "ws_reverse",
					},
				},
			},
		},
	}
	if err := service.Configure(context.Background(), cfg); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	mux := http.NewServeMux()
	service.InstallHTTPRoutes(mux)
	if err := service.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = service.Stop(context.Background()) })

	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	headers := http.Header{
		"X-Client-Role": []string{"Universal"},
		"X-Self-ID":     []string{"123456"},
	}
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/platforms/onebot/v11/qq-main/ws", &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status := service.Status()
		if len(status.Adapters) == 1 && status.Adapters[0].Transport.Connected {
			if status.Adapters[0].Transport.SelfID != "123456" {
				t.Fatalf("transport status = %#v, want self_id", status.Adapters[0].Transport)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("status = %#v, want connected", service.Status())
}

func newBarePlatformService() *PlatformService {
	cfg := config.DefaultConfig()
	return NewPlatformService(&Infra{
		Config: cfg,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, nil, nil, nil, nil, nil, nil)
}

func TestServerShutdownStopsPlatforms(t *testing.T) {
	var stopped atomic.Int32
	service := newBarePlatformService()
	adapter := &countingPlatformAdapter{stopped: &stopped}
	service.adapters = []platform.Adapter{adapter}
	service.started = []platform.Adapter{adapter}
	server := &Server{
		httpServer: &http.Server{Handler: http.NewServeMux()},
		platforms:  service,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if stopped.Load() != 1 {
		t.Fatalf("platform stops = %d, want 1", stopped.Load())
	}
}

func TestServerShutdownStopsPlatformsBeforeHTTPShutdown(t *testing.T) {
	stopCh := make(chan struct{})
	handlerActive := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/block", func(w http.ResponseWriter, r *http.Request) {
		close(handlerActive)
		<-stopCh
		w.WriteHeader(http.StatusNoContent)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	httpServer := &http.Server{Handler: mux}
	go func() {
		_ = httpServer.Serve(listener)
	}()
	clientDone := make(chan struct{})
	go func() {
		resp, err := http.Get("http://" + listener.Addr().String() + "/block")
		if err == nil {
			_ = resp.Body.Close()
		}
		close(clientDone)
	}()
	select {
	case <-handlerActive:
	case <-time.After(time.Second):
		t.Fatal("blocking handler was not active")
	}

	service := newBarePlatformService()
	adapter := &channelStopPlatformAdapter{stopCh: stopCh}
	service.adapters = []platform.Adapter{adapter}
	service.started = []platform.Adapter{adapter}
	server := &Server{
		httpServer: httpServer,
		platforms:  service,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case <-clientDone:
	case <-time.After(time.Second):
		t.Fatal("blocking request did not finish after platform stop")
	}
}

func TestServerRunListenErrorStopsPlatforms(t *testing.T) {
	var stopped atomic.Int32
	service := newBarePlatformService()
	adapter := &countingPlatformAdapter{stopped: &stopped}
	service.adapters = []platform.Adapter{adapter}
	service.started = []platform.Adapter{adapter}
	server := &Server{
		httpServer: &http.Server{Addr: "127.0.0.1:bad-port", Handler: http.NewServeMux()},
		platforms:  service,
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	if err := server.Run(context.Background()); err == nil {
		t.Fatal("Run err = nil, want listen error")
	}
	if stopped.Load() != 1 {
		t.Fatalf("platform stops = %d, want 1 after listen error", stopped.Load())
	}
}

type countingPlatformAdapter struct {
	stopped *atomic.Int32
}

func (a *countingPlatformAdapter) Start(context.Context, platform.InboundHandler) error {
	return nil
}

func (a *countingPlatformAdapter) Stop(context.Context) error {
	a.stopped.Add(1)
	return nil
}

type channelStopPlatformAdapter struct {
	stopCh chan struct{}
	closed atomic.Bool
}

func (a *channelStopPlatformAdapter) Start(context.Context, platform.InboundHandler) error {
	return nil
}

func (a *channelStopPlatformAdapter) Stop(context.Context) error {
	if a.closed.CompareAndSwap(false, true) {
		close(a.stopCh)
	}
	return nil
}
