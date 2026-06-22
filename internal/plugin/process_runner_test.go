package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProcessRuntimeManagedPythonLaunchUsesHostBootstrapArgs(t *testing.T) {
	root := t.TempDir()
	cfg := ProcessLaunchConfig{
		PluginID:          "com.example.bootstrap",
		Version:           "0.1.0",
		WorkDir:           filepath.Join(root, "work"),
		Entry:             "main.py",
		PythonExecutable:  filepath.Join(root, "python"),
		StateDir:          filepath.Join(root, "state"),
		CacheDir:          filepath.Join(root, "cache"),
		RunDir:            filepath.Join(root, "run"),
		DependencyEnvDir:  filepath.Join(root, "deps"),
		StartupTimeout:    time.Second,
		ShutdownTimeout:   time.Second,
		MaxStderrBytes:    8192,
		ManagedPython:     true,
		AdditionalEnvVars: []string{"PYTHONPATH=" + filepath.Join(root, "sdk")},
	}
	launch, err := prepareProcessLaunch(cfg)
	if err != nil {
		t.Fatalf("prepareProcessLaunch: %v", err)
	}
	if len(launch.Args) < 4 || strings.Join(launch.Args[:3], " ") != "-I -P -u" {
		t.Fatalf("launch args = %#v, want -I -P -u host bootstrap", launch.Args)
	}
	if filepath.Base(launch.Args[3]) != "host_bootstrap.py" {
		t.Fatalf("bootstrap arg = %q, want host_bootstrap.py", launch.Args[3])
	}
	if strings.HasPrefix(filepath.Clean(launch.Args[3]), filepath.Clean(cfg.WorkDir)) {
		t.Fatalf("bootstrap arg = %q, must not live under plugin work dir %q", launch.Args[3], cfg.WorkDir)
	}
	if _, err := os.Stat(launch.Args[3]); err != nil {
		t.Fatalf("host bootstrap was not written: %v", err)
	}
	joinedEnv := strings.Join(launch.Config.AdditionalEnvVars, "\n")
	if strings.Contains(joinedEnv, "PYTHONPATH=") {
		t.Fatalf("managed bootstrap env should not pass PYTHONPATH: %s", joinedEnv)
	}
	if !strings.Contains(joinedEnv, "EMO_PLUGIN_BOOTSTRAP_EXTRA_PATHS="+filepath.Join(root, "sdk")) {
		t.Fatalf("bootstrap extra path missing SDK path: %s", joinedEnv)
	}
	if strings.Contains(joinedEnv, "EMO_PLUGIN_HOST_BOOTSTRAP=1") {
		t.Fatalf("host bootstrap marker must be set by Python bootstrap, not launch env: %s", joinedEnv)
	}
}

func TestProcessRuntimeIgnoresLaunchContextCancellationAfterStart(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"work", "state", "cache", "run"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", dir, err)
		}
	}
	launchCtx, cancelLaunch := context.WithCancel(context.Background())
	runtime, err := StartProcessRuntime(launchCtx, ProcessLaunchConfig{
		PluginID:          "com.example.ctx",
		Version:           "0.1.0",
		WorkDir:           filepath.Join(root, "work"),
		Entry:             "-test.run=TestProcessRuntimeJSONRPCHelper",
		PythonExecutable:  os.Args[0],
		StateDir:          filepath.Join(root, "state"),
		CacheDir:          filepath.Join(root, "cache"),
		RunDir:            filepath.Join(root, "run"),
		StartupTimeout:    time.Second,
		ShutdownTimeout:   time.Second,
		MaxStderrBytes:    8192,
		AdditionalEnvVars: []string{"EMO_TEST_JSONRPC_HELPER=1"},
	}, nil)
	if err != nil {
		cancelLaunch()
		t.Fatalf("StartProcessRuntime: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	var init InitializeResponse
	if err := runtime.Call(context.Background(), "initialize", InitializeRequest{}, &init); err != nil {
		cancelLaunch()
		t.Fatalf("initialize: %v", err)
	}

	cancelLaunch()
	select {
	case err := <-runtime.done:
		t.Fatalf("runtime exited after launch context cancellation: %v", err)
	case <-time.After(250 * time.Millisecond):
	}

	var result HookResult
	if err := runtime.Call(context.Background(), "invoke_hook", hookInvokeRequest{Hook: HookAfterTurnEnd}, &result); err != nil {
		t.Fatalf("invoke_hook after launch context cancellation: %v", err)
	}
	if result.Annotations["helper"] != "alive" {
		t.Fatalf("hook result = %#v", result)
	}
}

func TestProcessRuntimeJSONRPCHelper(t *testing.T) {
	if os.Getenv("EMO_TEST_JSONRPC_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      string          `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			os.Exit(2)
		}
		var result any
		switch req.Method {
		case "initialize":
			result = InitializeResponse{}
		case "invoke_hook":
			result = HookResult{Annotations: map[string]any{"helper": "alive"}}
		case "shutdown":
			writeJSONRPCResult(req.ID, nil)
			os.Exit(0)
		default:
			writeJSONRPCError(req.ID, "unknown method")
			continue
		}
		writeJSONRPCResult(req.ID, result)
	}
	os.Exit(0)
}

func writeJSONRPCResult(id string, result any) {
	raw, _ := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      string `json:"id"`
		Result  any    `json:"result"`
	}{JSONRPC: "2.0", ID: id, Result: result})
	_, _ = os.Stdout.Write(append(raw, '\n'))
}

func writeJSONRPCError(id, message string) {
	raw, _ := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      string `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{JSONRPC: "2.0", ID: id, Error: struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{Code: -32601, Message: message}})
	_, _ = os.Stdout.Write(append(raw, '\n'))
}
