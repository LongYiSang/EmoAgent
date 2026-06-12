package plugin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

func TestRuntimeSupervisorInvokesPythonHookAndTool(t *testing.T) {
	python := findPythonForTest(t)
	store, manifest := writeProcessPluginPackage(t, normalPythonPluginSource())
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:    true,
		PythonExecutable:  python,
		StartupTimeoutMS:  3000,
		ShutdownTimeoutMS: 1000,
		MaxStderrBytes:    8192,
	}, nil)
	supervisor.AddPlugin(manifest)
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	result, err := supervisor.InvokeHook(t.Context(), manifest.ID, HookAfterTurnEnd, HookContext{
		Turn: TurnView{TurnID: "turn-1"},
	})
	if err != nil {
		t.Fatalf("InvokeHook: %v", err)
	}
	if result.Annotations["echo_plugin"] != "observed:turn-1" {
		t.Fatalf("Hook annotations = %#v", result.Annotations)
	}
	tools := supervisor.Tools(manifest.ID)
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %#v", tools)
	}
	raw, err := supervisor.InvokeTool(t.Context(), manifest.ID, "echo", json.RawMessage(`{"text":"hello"}`))
	if err != nil {
		t.Fatalf("InvokeTool: %v", err)
	}
	if !strings.Contains(string(raw), "hello") {
		t.Fatalf("InvokeTool raw = %s", raw)
	}
}

func TestRuntimeSupervisorHookTimeoutMarksRuntimeFailed(t *testing.T) {
	python := findPythonForTest(t)
	store, manifest := writeProcessPluginPackage(t, sleepingPythonPluginSource())
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:    true,
		PythonExecutable:  python,
		StartupTimeoutMS:  3000,
		ShutdownTimeoutMS: 100,
		MaxStderrBytes:    8192,
	}, nil)
	supervisor.AddPlugin(manifest)
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := supervisor.InvokeHook(ctx, manifest.ID, HookAfterTurnEnd, HookContext{})
	if err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("InvokeHook error = %v, want deadline", err)
	}
	status := supervisor.Status(manifest.ID)
	if status.Status != "failed" {
		t.Fatalf("status = %#v, want failed", status)
	}
}

func TestRuntimeSupervisorCrashMarksRuntimeFailed(t *testing.T) {
	python := findPythonForTest(t)
	store, manifest := writeProcessPluginPackage(t, crashingPythonPluginSource())
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:    true,
		PythonExecutable:  python,
		StartupTimeoutMS:  3000,
		ShutdownTimeoutMS: 100,
		MaxStderrBytes:    8192,
	}, nil)
	supervisor.AddPlugin(manifest)
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	_, err := supervisor.InvokeHook(t.Context(), manifest.ID, HookAfterTurnEnd, HookContext{})
	if err == nil || !(strings.Contains(err.Error(), "plugin process exited") || strings.Contains(err.Error(), "EOF")) {
		t.Fatalf("InvokeHook error = %v, want process exited or EOF", err)
	}
	status := supervisor.Status(manifest.ID)
	if status.Status != "failed" {
		t.Fatalf("status = %#v, want failed", status)
	}
}

func TestRuntimeSupervisorProtocolErrorMarksRuntimeFailed(t *testing.T) {
	python := findPythonForTest(t)
	store, manifest := writeProcessPluginPackage(t, protocolErrorPythonPluginSource())
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:    true,
		PythonExecutable:  python,
		StartupTimeoutMS:  3000,
		ShutdownTimeoutMS: 100,
		MaxStderrBytes:    8192,
	}, nil)
	supervisor.AddPlugin(manifest)
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	if _, err := supervisor.EnsureReady(t.Context(), manifest.ID); err != nil {
		t.Fatalf("EnsureReady: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	status := supervisor.Status(manifest.ID)
	if status.Status != "failed" || !strings.Contains(status.LastError, "protocol") {
		t.Fatalf("status = %#v, want failed protocol error", status)
	}
}

func TestProcessRuntimeSecurityShimBlocksSocketAndDirectDatabase(t *testing.T) {
	python := findPythonForTest(t)
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	if err := os.WriteFile(dbPath, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write db fixture: %v", err)
	}
	store, manifest := writeProcessPluginPackage(t, securityProbePythonPluginSource())
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:    true,
		PythonExecutable:  python,
		StartupTimeoutMS:  3000,
		ShutdownTimeoutMS: 100,
		MaxStderrBytes:    8192,
	}, nil)
	supervisor.SetAdditionalEnvVars([]string{"HOST_DB_PATH=" + dbPath})
	supervisor.AddPlugin(manifest)
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	result, err := supervisor.InvokeHook(t.Context(), manifest.ID, HookAfterTurnEnd, HookContext{})
	if err != nil {
		t.Fatalf("InvokeHook: %v", err)
	}
	if result.Annotations["socket_blocked"] != true || result.Annotations["db_blocked"] != true || result.Annotations["sqlite_blocked"] != true {
		t.Fatalf("security annotations = %#v", result.Annotations)
	}
}

func TestBuildPluginProcessEnvRemovesProviderSecrets(t *testing.T) {
	env := buildPluginProcessEnv([]string{
		"PATH=/bin",
		"MOONSHOT_API_KEY=secret",
		"CUSTOM_TOKEN=secret",
		"NORMAL=value",
	}, ProcessLaunchConfig{
		PluginID:        "com.example.echo",
		Version:         "0.1.0",
		WorkDir:         "pkg",
		StateDir:        "state",
		CacheDir:        "cache",
		RunDir:          "run",
		BlockedEnvNames: []string{"NORMAL"},
	})
	joined := strings.Join(env, "\n")
	for _, forbidden := range []string{"MOONSHOT_API_KEY", "CUSTOM_TOKEN", "NORMAL=value"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("env leaked %s in %s", forbidden, joined)
		}
	}
	if !strings.Contains(joined, "EMO_PLUGIN_ID=com.example.echo") {
		t.Fatalf("env missing plugin id: %s", joined)
	}
}

func writeProcessPluginPackage(t *testing.T, source string) (*PluginStore, ManifestV2) {
	t.Helper()
	store, err := NewPluginStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("NewPluginStore: %v", err)
	}
	manifest := ManifestV2{
		SchemaVersion:   ManifestSchemaV02,
		ID:              "com.example.echo",
		Name:            "Echo",
		Version:         "0.1.0",
		EmoAgentVersion: ">=0.2.0",
		Runtime:         ManifestV2Runtime{Kind: RuntimePythonProcess, Entry: "main.py"},
		Access: ManifestV2Access{
			Tier:         AccessTierRuntimeSafe,
			Capabilities: []Capability{CapabilityTurnRead, CapabilityToolRegister},
		},
		Hooks: []HookSpec{{Name: HookAfterTurnEnd, Mode: HookModeObserve, FailurePolicy: FailurePolicyFailOpen, TimeoutMS: 200}},
	}
	dir, err := store.CreateImmutablePackageDir(manifest.ID, manifest.Version)
	if err != nil {
		t.Fatalf("CreateImmutablePackageDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestFileName), []byte(`
schema_version: emoagent.plugin.v0.2
id: com.example.echo
name: Echo
version: 0.1.0
emoagent_version: ">=0.2.0"
runtime:
  kind: python_process
  entry: main.py
access:
  tier: runtime_safe
  capabilities:
    - turn.read
    - tool.register
hooks:
  - name: after_turn_end
    mode: observe
    failure_policy: fail_open
    priority: 100
    timeout_ms: 200
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("write main.py: %v", err)
	}
	return store, manifest
}

func findPythonForTest(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python", "python3"} {
		path, err := exec.LookPath(name)
		if err == nil {
			return path
		}
	}
	t.Skip("python executable not found")
	return ""
}

func normalPythonPluginSource() string {
	return pythonRPCPrelude() + `
def handle(method, params):
    if method == "initialize":
        return {"tools": [{"name": "echo", "description": "Echo input", "parameters": {"type": "object"}, "scope": "both", "permission": "read-only"}]}
    if method == "invoke_hook":
        turn = params.get("context", {}).get("Turn", {})
        return {"Annotations": {"echo_plugin": "observed:" + turn.get("TurnID", "")}}
    if method == "invoke_tool":
        return {"ok": True, "input": params.get("input")}
    if method == "shutdown":
        send({"jsonrpc": "2.0", "id": current_id, "result": None})
        sys.exit(0)
    return None

main(handle)
`
}

func sleepingPythonPluginSource() string {
	return pythonRPCPrelude() + `
def handle(method, params):
    if method == "initialize":
        return {"tools": []}
    if method == "invoke_hook":
        time.sleep(2)
        return {"Annotations": {"late": True}}
    if method == "shutdown":
        send({"jsonrpc": "2.0", "id": current_id, "result": None})
        sys.exit(0)
    return None

main(handle)
`
}

func crashingPythonPluginSource() string {
	return pythonRPCPrelude() + `
def handle(method, params):
    if method == "initialize":
        return {"tools": []}
    if method == "invoke_hook":
        sys.exit(7)
    if method == "shutdown":
        sys.exit(0)
    return None

main(handle)
`
}

func protocolErrorPythonPluginSource() string {
	return pythonRPCPrelude() + `
def handle(method, params):
    if method == "initialize":
        import threading
        threading.Timer(0.1, lambda: (sys.stdout.write("not json\n"), sys.stdout.flush())).start()
        return {"tools": []}
    if method == "shutdown":
        send({"jsonrpc": "2.0", "id": current_id, "result": None})
        sys.exit(0)
    return None

main(handle)
`
}

func securityProbePythonPluginSource() string {
	return pythonRPCPrelude() + `
def handle(method, params):
    if method == "initialize":
        return {"tools": []}
    if method == "invoke_hook":
        import os, socket
        socket_blocked = False
        db_blocked = False
        sqlite_blocked = False
        try:
            s = socket.socket()
            s.bind(("127.0.0.1", 0))
            s.close()
        except PermissionError:
            socket_blocked = True
        try:
            open(os.environ["HOST_DB_PATH"], "rb").read()
        except PermissionError:
            db_blocked = True
        try:
            import sqlite3
            sqlite3.connect(os.environ["HOST_DB_PATH"])
        except PermissionError:
            sqlite_blocked = True
        return {"Annotations": {"socket_blocked": socket_blocked, "db_blocked": db_blocked, "sqlite_blocked": sqlite_blocked}}
    if method == "shutdown":
        send({"jsonrpc": "2.0", "id": current_id, "result": None})
        sys.exit(0)
    return None

main(handle)
`
}

func pythonRPCPrelude() string {
	return `import json, sys, time

current_id = None

def send(obj):
    sys.stdout.write(json.dumps(obj) + "\n")
    sys.stdout.flush()

def main(handle):
    global current_id
    for line in sys.stdin:
        req = json.loads(line)
        current_id = req.get("id")
        try:
            result = handle(req.get("method"), req.get("params") or {})
            if current_id is not None:
                send({"jsonrpc": "2.0", "id": current_id, "result": result})
        except Exception as exc:
            if current_id is not None:
                send({"jsonrpc": "2.0", "id": current_id, "error": {"code": -32000, "message": str(exc)}})
`
}
