package sidecar

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagedCommandArgsUseLoopbackAndGeneratedConfig(t *testing.T) {
	spec := DefaultSpec()
	spec.Enabled = true
	spec.Managed = true
	spec.Command = []string{`D:\EmoAgent\data\python-envs\memory-sidecar\Scripts\python.exe`, "-I", "-P", "-u", "-m", "memorycore_sidecar.server"}
	spec.ConfigPath = `D:\Dev\Project\Agent\EmoAgent\data\runtime\sidecar.generated.toml`

	args := spec.CommandArgs()
	got := strings.Join(args, " ")
	for _, want := range []string{
		`D:\EmoAgent\data\python-envs\memory-sidecar\Scripts\python.exe -I -P -u -m memorycore_sidecar.server`,
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

func TestManagedDefaultSpecHasNoProductionUVRunFallback(t *testing.T) {
	spec := DefaultSpec()
	spec.Enabled = true
	spec.Managed = true

	if args := spec.CommandArgs(); len(args) != 0 {
		t.Fatalf("CommandArgs = %#v, want empty until environment python is injected", args)
	}
}

func TestManagedSupervisorProcessSurvivesStartContextCancel(t *testing.T) {
	port := freeLoopbackPort(t)
	spec := DefaultSpec()
	spec.Enabled = true
	spec.Managed = true
	spec.Host = "127.0.0.1"
	spec.Port = port
	spec.URL = fmt.Sprintf("http://127.0.0.1:%d", port)
	spec.WorkingDir = t.TempDir()
	spec.ConfigPath = filepath.Join(t.TempDir(), "sidecar.generated.toml")
	spec.LogPath = filepath.Join(t.TempDir(), "sidecar.log")
	spec.StartupTimeout = 5 * time.Second
	spec.Command = []string{os.Args[0], "-test.run=TestSidecarHelperProcess", "--", "--helper-sidecar"}

	supervisor := NewSupervisor(spec, slog.Default())
	ctx, cancel := context.WithCancel(context.Background())
	status, err := supervisor.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if status.State != StateHealthy {
		t.Fatalf("status = %#v, want healthy", status)
	}
	cancel()
	time.Sleep(300 * time.Millisecond)

	resp, err := http.Get(spec.EffectiveURL() + "/health")
	if err != nil {
		t.Fatalf("health after canceling start context: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", resp.StatusCode)
	}
	if err := supervisor.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestSidecarHelperProcess(t *testing.T) {
	if !hasArg("--helper-sidecar") {
		return
	}
	host := "127.0.0.1"
	port := "8765"
	for i, arg := range os.Args {
		if arg == "--host" && i+1 < len(os.Args) {
			host = os.Args[i+1]
		}
		if arg == "--port" && i+1 < len(os.Args) {
			port = os.Args[i+1]
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	if err := http.ListenAndServe(net.JoinHostPort(host, port), mux); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen free port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func hasArg(want string) bool {
	for _, arg := range os.Args {
		if arg == want {
			return true
		}
	}
	return false
}

func TestResolveManagedPathsMakesChildInputsAbsolute(t *testing.T) {
	spec := DefaultSpec()
	spec.ConfigPath = `.\data\runtime\sidecar.generated.toml`
	spec.WorkingDir = `..\EmoAgent-MemoryCore\sidecar`
	spec.LogPath = `.\logs\sidecar.log`
	spec.TriviumDir = `.\data\trivium`
	spec.EmbeddingCacheDBPath = `.\data\embedding_cache.sqlite3`

	resolved, err := resolveManagedPaths(spec)
	if err != nil {
		t.Fatalf("resolveManagedPaths: %v", err)
	}
	for name, path := range map[string]string{
		"config_path":          resolved.ConfigPath,
		"working_dir":          resolved.WorkingDir,
		"log_path":             resolved.LogPath,
		"trivium_dir":          resolved.TriviumDir,
		"embedding_cache_path": resolved.EmbeddingCacheDBPath,
	} {
		if !filepath.IsAbs(path) {
			t.Fatalf("%s = %q, want absolute path", name, path)
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
