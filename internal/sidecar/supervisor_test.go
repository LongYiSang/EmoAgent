package sidecar

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestManagedCommandArgsUseLoopbackAndGeneratedConfig(t *testing.T) {
	spec := DefaultSpec()
	spec.Enabled = true
	spec.Managed = true
	spec.ConfigPath = `D:\Dev\Project\Agent\EmoAgent\data\runtime\sidecar.generated.toml`

	args := spec.CommandArgs()
	got := strings.Join(args, " ")
	for _, want := range []string{
		"uv run python -m memorycore_sidecar.server",
		"--adapter trivium",
		`--config D:\Dev\Project\Agent\EmoAgent\data\runtime\sidecar.generated.toml`,
		"--host 127.0.0.1",
		"--port 8765",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("CommandArgs = %q, want %q", got, want)
		}
	}
}

func TestExternalSupervisorHealthCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("path = %q, want /health", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(server.Close)

	spec := DefaultSpec()
	spec.Enabled = true
	spec.Managed = false
	spec.URL = server.URL
	spec.StartupTimeout = 2 * time.Second

	supervisor := NewSupervisor(spec, slog.Default())
	status, err := supervisor.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if status.State != StateHealthy || status.URL != server.URL {
		t.Fatalf("status = %#v", status)
	}
}

func TestExternalSupervisorRejectsNonLoopbackURL(t *testing.T) {
	spec := DefaultSpec()
	spec.Enabled = true
	spec.Managed = false
	spec.URL = "http://192.168.1.2:8765"

	supervisor := NewSupervisor(spec, slog.Default())
	_, err := supervisor.Start(context.Background())
	if err == nil {
		t.Fatal("Start succeeded, want loopback error")
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("error = %q, want loopback", err.Error())
	}
}
