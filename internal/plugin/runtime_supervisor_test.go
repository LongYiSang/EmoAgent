package plugin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestRuntimeSupervisorReplacesRunningRuntimeWhenManifestVersionChanges(t *testing.T) {
	python := findPythonForTest(t)
	store, v1 := writeProcessPluginPackage(t, versionEchoPythonPluginSource())
	v2 := writeProcessPluginPackageVersion(t, store, v1, "0.2.0", versionEchoPythonPluginSource())
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:    true,
		PythonExecutable:  python,
		StartupTimeoutMS:  3000,
		ShutdownTimeoutMS: 1000,
		MaxStderrBytes:    8192,
	}, nil)
	supervisor.AddPlugin(v1)
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	result, err := supervisor.InvokeHook(t.Context(), v1.ID, HookAfterTurnEnd, HookContext{})
	if err != nil {
		t.Fatalf("InvokeHook v1: %v", err)
	}
	if result.Annotations["runtime_version"] != "0.1.0" {
		t.Fatalf("v1 annotations = %#v", result.Annotations)
	}
	statusV1 := supervisor.Status(v1.ID)
	if statusV1.PID == 0 || statusV1.Version != "0.1.0" {
		t.Fatalf("v1 status = %#v", statusV1)
	}

	supervisor.AddPlugin(v2)
	result, err = supervisor.InvokeHook(t.Context(), v2.ID, HookAfterTurnEnd, HookContext{})
	if err != nil {
		t.Fatalf("InvokeHook v2: %v", err)
	}
	if result.Annotations["runtime_version"] != "0.2.0" {
		t.Fatalf("v2 annotations = %#v", result.Annotations)
	}
	statusV2 := supervisor.Status(v2.ID)
	if statusV2.Version != "0.2.0" || statusV2.PID == 0 || statusV2.PID == statusV1.PID {
		t.Fatalf("v2 status = %#v, v1 status = %#v", statusV2, statusV1)
	}
}

func TestRuntimeSupervisorRejectsInvokeWhenRunningPluginBecomesDisabled(t *testing.T) {
	python := findPythonForTest(t)
	store, manifest := writeProcessPluginPackage(t, normalPythonPluginSource())
	enabled := true
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:    true,
		PythonExecutable:  python,
		StartupTimeoutMS:  3000,
		ShutdownTimeoutMS: 1000,
		MaxStderrBytes:    8192,
	}, nil)
	supervisor.SetEnabledChecker(func(context.Context, string) bool {
		return enabled
	})
	supervisor.AddPlugin(manifest)
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	if _, err := supervisor.EnsureReady(t.Context(), manifest.ID); err != nil {
		t.Fatalf("EnsureReady: %v", err)
	}
	enabled = false

	if _, err := supervisor.InvokeHook(t.Context(), manifest.ID, HookAfterTurnEnd, HookContext{}); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("InvokeHook error = %v, want disabled", err)
	}
	if _, err := supervisor.InvokeTool(t.Context(), manifest.ID, "echo", json.RawMessage(`{"text":"hello"}`)); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("InvokeTool error = %v, want disabled", err)
	}
}

func TestRuntimeSupervisorAcceptsManagedPythonProcess(t *testing.T) {
	python := findPythonForTest(t)
	store, manifest := writeProcessPluginPackage(t, normalPythonPluginSource())
	manifest.Runtime.Kind = RuntimeManagedPythonProcess
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:          true,
		PrivatePythonExecutable: python,
		StartupTimeoutMS:        3000,
		ShutdownTimeoutMS:       1000,
		MaxStderrBytes:          8192,
	}, nil)
	supervisor.AddPlugin(manifest)
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	if _, err := supervisor.EnsureReady(t.Context(), manifest.ID); err != nil {
		t.Fatalf("EnsureReady: %v", err)
	}
	tools := supervisor.Tools(manifest.ID)
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %#v", tools)
	}
}

func TestRuntimeSupervisorManagedPythonUsesPerPluginDependencyEnv(t *testing.T) {
	python := findPythonForTest(t)
	store, manifest := writeProcessPluginPackage(t, dependencyImportPythonPluginSource())
	manifest.Runtime.Kind = RuntimeManagedPythonProcess
	depDir := filepath.Join(store.RootDir, "dependencies", manifest.ID, manifest.Version)
	if err := os.MkdirAll(depDir, 0o755); err != nil {
		t.Fatalf("MkdirAll dependency env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(depDir, "depmod.py"), []byte(`VALUE = "dependency-ready"`), 0o644); err != nil {
		t.Fatalf("write dependency module: %v", err)
	}
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:          true,
		PrivatePythonExecutable: python,
		StartupTimeoutMS:        3000,
		ShutdownTimeoutMS:       1000,
		MaxStderrBytes:          8192,
	}, nil)
	supervisor.AddPlugin(manifest)
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	if _, err := supervisor.EnsureReady(t.Context(), manifest.ID); err != nil {
		t.Fatalf("EnsureReady: %v", err)
	}
	result, err := supervisor.InvokeHook(t.Context(), manifest.ID, HookAfterTurnEnd, HookContext{})
	if err != nil {
		t.Fatalf("InvokeHook: %v", err)
	}
	if result.Annotations["dependency"] != "dependency-ready" {
		t.Fatalf("hook annotations = %#v, want dependency-ready", result.Annotations)
	}
}

func TestRuntimeSupervisorManagedPythonUsesIsolatedHostBootstrap(t *testing.T) {
	python := findPythonForTest(t)
	requirePythonIsolatedSafePathSupport(t, python)
	inheritedPythonPath := filepath.Join(t.TempDir(), "inherited-pythonpath")
	t.Setenv("PYTHONPATH", inheritedPythonPath)
	t.Setenv("EMO_TEST_INHERITED_PYTHONPATH", inheritedPythonPath)
	store, manifest := writeProcessPluginPackage(t, bootstrapProbePythonPluginSource())
	manifest.Runtime.Kind = RuntimeManagedPythonProcess
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:          true,
		PrivatePythonExecutable: python,
		StartupTimeoutMS:        3000,
		ShutdownTimeoutMS:       1000,
		MaxStderrBytes:          8192,
	}, nil)
	supervisor.AddPlugin(manifest)
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	result, err := supervisor.InvokeHook(t.Context(), manifest.ID, HookAfterTurnEnd, HookContext{})
	if err != nil {
		t.Fatalf("InvokeHook: %v", err)
	}
	for key, value := range map[string]any{
		"isolated":               true,
		"safe_path":              true,
		"no_user_site":           true,
		"host_bootstrap_seen":    true,
		"inherited_pythonpath":   false,
		"plugin_root_importable": true,
	} {
		if result.Annotations[key] != value {
			t.Fatalf("annotation %s = %#v, want %#v in %#v", key, result.Annotations[key], value, result.Annotations)
		}
	}
}

func TestRuntimeSupervisorManagedPythonRequiresPrivatePythonExecutable(t *testing.T) {
	store, manifest := writeProcessPluginPackage(t, normalPythonPluginSource())
	manifest.Runtime.Kind = RuntimeManagedPythonProcess
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:    true,
		PythonExecutable:  "python",
		StartupTimeoutMS:  3000,
		ShutdownTimeoutMS: 1000,
		MaxStderrBytes:    8192,
	}, nil)
	supervisor.AddPlugin(manifest)

	_, err := supervisor.EnsureReady(t.Context(), manifest.ID)
	if err == nil || !strings.Contains(err.Error(), "managed python runtime unavailable") {
		t.Fatalf("EnsureReady error = %v, want managed python runtime unavailable", err)
	}
	status := supervisor.Status(manifest.ID)
	if status.Status != "failed" || !strings.Contains(status.LastError, "managed python runtime unavailable") {
		t.Fatalf("status = %#v, want failed private python status", status)
	}
}

func TestRuntimeSupervisorStatusReportsManagedPythonDiagnostics(t *testing.T) {
	store, manifest := writeProcessPluginPackage(t, normalPythonPluginSource())
	manifest.Runtime.Kind = RuntimeManagedPythonProcess
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:    true,
		PythonExecutable:  "python",
		StartupTimeoutMS:  3000,
		ShutdownTimeoutMS: 1000,
		MaxStderrBytes:    8192,
	}, nil)
	supervisor.AddPlugin(manifest)

	_, err := supervisor.EnsureReady(t.Context(), manifest.ID)
	if err == nil {
		t.Fatal("EnsureReady error = nil, want missing managed private Python")
	}
	status := supervisor.Status(manifest.ID)
	if status.RuntimeKind != RuntimeManagedPythonProcess {
		t.Fatalf("runtime kind = %q, want %q", status.RuntimeKind, RuntimeManagedPythonProcess)
	}
	if status.PythonExecutableSource != "store_private_runtime" {
		t.Fatalf("python source = %q, want store_private_runtime", status.PythonExecutableSource)
	}
	expectedPath := filepath.Join(store.RootDir, "runtime", "python", privatePythonExecutableName(runtime.GOOS))
	if status.PythonExecutablePath != expectedPath {
		t.Fatalf("python path = %q, want %q", status.PythonExecutablePath, expectedPath)
	}
	if status.PythonExecutableAvailable == nil || *status.PythonExecutableAvailable {
		t.Fatalf("python available = %#v, want false", status.PythonExecutableAvailable)
	}
}

func TestRuntimeSupervisorStatusReportsDependencyEnvBeforeStart(t *testing.T) {
	store, manifest := writeProcessPluginPackage(t, normalPythonPluginSource())
	manifest.Runtime.Kind = RuntimeManagedPythonProcess
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:    true,
		StartupTimeoutMS:  3000,
		ShutdownTimeoutMS: 1000,
		MaxStderrBytes:    8192,
	}, nil)
	supervisor.AddPlugin(manifest)

	status := supervisor.Status(manifest.ID)
	expected := filepath.Join(store.RootDir, "dependencies", manifest.ID, manifest.Version)
	if status.DependencyEnvDir != expected {
		t.Fatalf("dependency env dir = %q, want %q", status.DependencyEnvDir, expected)
	}
}

func TestRuntimeSupervisorDependencyEnvAppliesOnlyToManagedPython(t *testing.T) {
	store, manifest := writeProcessPluginPackage(t, normalPythonPluginSource())
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:    true,
		StartupTimeoutMS:  3000,
		ShutdownTimeoutMS: 1000,
		MaxStderrBytes:    8192,
	}, nil)
	supervisor.AddPlugin(manifest)

	status := supervisor.Status(manifest.ID)
	if status.DependencyEnvDir != "" {
		t.Fatalf("legacy dependency env dir = %q, want empty", status.DependencyEnvDir)
	}
}

func TestRuntimeSupervisorStatusReportsProcessGuardDiagnostics(t *testing.T) {
	python := findPythonForTest(t)
	store, manifest := writeProcessPluginPackage(t, normalPythonPluginSource())
	manifest.Runtime.Kind = RuntimeManagedPythonProcess
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:          true,
		PrivatePythonExecutable: python,
		StartupTimeoutMS:        3000,
		ShutdownTimeoutMS:       1000,
		MaxStderrBytes:          8192,
	}, nil)
	supervisor.AddPlugin(manifest)
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	if _, err := supervisor.EnsureReady(t.Context(), manifest.ID); err != nil {
		t.Fatalf("EnsureReady: %v", err)
	}
	status := supervisor.Status(manifest.ID)
	if status.PID <= 0 {
		t.Fatalf("pid = %d, want process pid", status.PID)
	}
	if status.DependencyEnvDir == "" {
		t.Fatalf("dependency env dir = empty in %#v", status)
	}
	if strings.TrimSpace(status.ProcessGuardKind) == "" {
		t.Fatalf("process guard kind = empty in %#v", status)
	}
	if status.ProcessGuardKind == "windows_job_object" && !status.ProcessGuardAttached {
		t.Fatalf("process guard attached = false for windows job object: %#v", status)
	}
}

func TestRuntimeSupervisorManagedPythonRejectsPathSearchExecutable(t *testing.T) {
	store, manifest := writeProcessPluginPackage(t, normalPythonPluginSource())
	manifest.Runtime.Kind = RuntimeManagedPythonProcess
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:          true,
		PrivatePythonExecutable: "python",
		StartupTimeoutMS:        3000,
		ShutdownTimeoutMS:       1000,
		MaxStderrBytes:          8192,
	}, nil)
	supervisor.AddPlugin(manifest)

	_, err := supervisor.EnsureReady(t.Context(), manifest.ID)
	if err == nil || !strings.Contains(err.Error(), "private_python_executable must be an absolute path") {
		t.Fatalf("EnsureReady error = %v, want absolute private python path required", err)
	}
}

func TestRuntimeSupervisorManagedPythonUsesStorePrivateRuntimeLocator(t *testing.T) {
	python := findPythonForTest(t)
	store, _ := writeProcessPluginPackage(t, normalPythonPluginSource())
	exeName := "python"
	if runtime.GOOS == "windows" {
		exeName = "python.exe"
	}
	privatePython := filepath.Join(store.RootDir, "runtime", "python", exeName)
	if err := os.MkdirAll(filepath.Dir(privatePython), 0o755); err != nil {
		t.Fatalf("MkdirAll private python: %v", err)
	}
	if err := os.WriteFile(privatePython, []byte("placeholder"), 0o644); err != nil {
		t.Fatalf("write private python placeholder: %v", err)
	}
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{ProcessEnabled: true}, nil)

	got, err := supervisor.pythonExecutableFor(RuntimeManagedPythonProcess)
	if err != nil {
		t.Fatalf("pythonExecutableFor: %v", err)
	}
	if got != privatePython {
		t.Fatalf("python executable = %q, want store private runtime %q", got, privatePython)
	}

	supervisor.cfg.PrivatePythonExecutable = python
	got, err = supervisor.pythonExecutableFor(RuntimeManagedPythonProcess)
	if err != nil {
		t.Fatalf("pythonExecutableFor override: %v", err)
	}
	if got != python {
		t.Fatalf("override executable = %q, want %q", got, python)
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

func TestProcessRuntimeAuditGuardReportsSocketAndDirectDatabaseDenials(t *testing.T) {
	python := findPythonForTest(t)
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	if err := os.WriteFile(dbPath, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write db fixture: %v", err)
	}
	store, manifest := writeProcessPluginPackage(t, auditGuardProbePythonPluginSource())
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

func TestRuntimeSupervisorManagedPythonStorePrivateRuntimeAuditGuardSmoke(t *testing.T) {
	artifactPath := strings.TrimSpace(os.Getenv("EMO_TEST_PRIVATE_PYTHON_ARTIFACT"))
	if artifactPath == "" {
		t.Skip("set EMO_TEST_PRIVATE_PYTHON_ARTIFACT and EMO_TEST_PRIVATE_PYTHON_SHA256 to smoke a real store-private Python runtime")
	}
	expectedDigest := strings.TrimSpace(os.Getenv("EMO_TEST_PRIVATE_PYTHON_SHA256"))
	if expectedDigest == "" {
		t.Fatal("EMO_TEST_PRIVATE_PYTHON_SHA256 is required when EMO_TEST_PRIVATE_PYTHON_ARTIFACT is set")
	}
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	if err := os.WriteFile(dbPath, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write db fixture: %v", err)
	}
	store, manifest := writeProcessPluginPackage(t, auditGuardProbePythonPluginSource())
	manifest.Runtime.Kind = RuntimeManagedPythonProcess
	if _, err := ProvisionPrivatePythonRuntime(store, config.PluginRuntimeConfig{
		PrivatePythonArtifactPath:   artifactPath,
		PrivatePythonArtifactSHA256: expectedDigest,
	}); err != nil {
		t.Fatalf("ProvisionPrivatePythonRuntime: %v", err)
	}
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:    true,
		PythonExecutable:  "legacy-python-must-not-be-used",
		StartupTimeoutMS:  3000,
		ShutdownTimeoutMS: 100,
		MaxStderrBytes:    8192,
	}, nil)
	supervisor.SetBlockedEnvNames([]string{"PATH", "PYTHONPATH", "PYTHONHOME", "VIRTUAL_ENV"})
	supervisor.SetAdditionalEnvVars([]string{"HOST_DB_PATH=" + dbPath})
	supervisor.AddPlugin(manifest)
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	result, err := supervisor.InvokeHook(t.Context(), manifest.ID, HookAfterTurnEnd, HookContext{})
	if err != nil {
		t.Fatalf("InvokeHook: %v status=%#v", err, supervisor.Status(manifest.ID))
	}
	if result.Annotations["socket_blocked"] != true || result.Annotations["db_blocked"] != true || result.Annotations["sqlite_blocked"] != true {
		t.Fatalf("security annotations = %#v", result.Annotations)
	}
	status := supervisor.Status(manifest.ID)
	expectedPython := filepath.Join(store.RootDir, "runtime", "python", privatePythonExecutableName(runtime.GOOS))
	if status.PythonExecutableSource != PythonExecutableSourceStorePrivate || status.PythonExecutablePath != expectedPython {
		t.Fatalf("runtime python diagnostics = %#v, want store-private %s", status, expectedPython)
	}
	if status.PythonExecutableAvailable == nil || !*status.PythonExecutableAvailable {
		t.Fatalf("python available = %#v, want true", status.PythonExecutableAvailable)
	}
}

func TestSelfTestPrivatePythonRuntimeUsesIsolatedSafePathAndProcessGuard(t *testing.T) {
	python := findPythonForTest(t)
	requirePythonIsolatedSafePathSupport(t, python)
	t.Setenv("EMO_SELFTEST_API_KEY", "secret")

	result, err := SelfTestPrivatePythonRuntime(t.Context(), python, 3*time.Second)
	if err != nil {
		t.Fatalf("SelfTestPrivatePythonRuntime: %v", err)
	}
	if !result.Isolated || !result.SafePath {
		t.Fatalf("self-test flags = %#v, want isolated safe path", result)
	}
	if result.SecretEnvSeen {
		t.Fatalf("self-test inherited sensitive env: %#v", result)
	}
	if strings.TrimSpace(result.ProcessGuardKind) == "" {
		t.Fatalf("process guard kind = empty in %#v", result)
	}
	if runtime.GOOS == "windows" && !result.ProcessGuardAttached {
		t.Fatalf("process guard attached = false in %#v", result)
	}
}

func TestRuntimeSupervisorRefreshesBlockedEnvNamesBeforeLaunch(t *testing.T) {
	python := findPythonForTest(t)
	t.Setenv("RUNTIME_ONLY_KEY", "secret")
	store, manifest := writeProcessPluginPackage(t, envProbePythonPluginSource())
	var blocked []string
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:    true,
		PythonExecutable:  python,
		StartupTimeoutMS:  3000,
		ShutdownTimeoutMS: 1000,
		MaxStderrBytes:    8192,
	}, nil)
	supervisor.SetBlockedEnvNamesProvider(func() []string {
		return append([]string(nil), blocked...)
	})
	blocked = []string{"RUNTIME_ONLY_KEY"}
	supervisor.AddPlugin(manifest)
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	result, err := supervisor.InvokeHook(t.Context(), manifest.ID, HookAfterTurnEnd, HookContext{})
	if err != nil {
		t.Fatalf("InvokeHook: %v", err)
	}
	if result.Annotations["runtime_only_key_seen"] != false {
		t.Fatalf("dynamic blocked env leaked: %#v", result.Annotations)
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

func TestWithPythonAuditGuardAddsDependencyEnvAfterAuditGuardWithoutInheritedPythonPath(t *testing.T) {
	runDir := filepath.Join(t.TempDir(), "run")
	depDir := filepath.Join(t.TempDir(), "deps")
	configuredPath := filepath.Join(t.TempDir(), "configured-pythonpath")
	inheritedPath := filepath.Join(t.TempDir(), "inherited-pythonpath")
	t.Setenv("PYTHONPATH", inheritedPath)

	additional, err := withPythonAuditGuard(ProcessLaunchConfig{
		PluginID:          "com.example.echo",
		Version:           "0.1.0",
		WorkDir:           "pkg",
		StateDir:          "state",
		CacheDir:          "cache",
		RunDir:            runDir,
		DependencyEnvDir:  depDir,
		AdditionalEnvVars: []string{"PYTHONPATH=" + configuredPath, "CUSTOM=value"},
	})
	if err != nil {
		t.Fatalf("withPythonAuditGuard: %v", err)
	}
	values := envMap(additional)
	pythonPath := strings.Split(values["PYTHONPATH"], string(os.PathListSeparator))
	shimDir := filepath.Join(runDir, "python_audit_guard")
	want := []string{shimDir, depDir, configuredPath}
	if strings.Join(pythonPath, "\n") != strings.Join(want, "\n") {
		t.Fatalf("PYTHONPATH = %#v, want %#v", pythonPath, want)
	}
	allowedRoots := strings.Split(values["EMO_PLUGIN_ALLOWED_ROOTS"], string(os.PathListSeparator))
	if allowedRoots[len(allowedRoots)-1] != depDir {
		t.Fatalf("allowed roots = %#v, want dependency env last", allowedRoots)
	}
	if values["CUSTOM"] != "value" {
		t.Fatalf("CUSTOM env = %q, want value", values["CUSTOM"])
	}
}

func TestWithPythonAuditGuardKeepsInheritedPythonPathForLegacyRuntime(t *testing.T) {
	runDir := filepath.Join(t.TempDir(), "run")
	configuredPath := filepath.Join(t.TempDir(), "configured-pythonpath")
	inheritedPath := filepath.Join(t.TempDir(), "inherited-pythonpath")
	t.Setenv("PYTHONPATH", inheritedPath)

	additional, err := withPythonAuditGuard(ProcessLaunchConfig{
		PluginID:          "com.example.echo",
		Version:           "0.1.0",
		WorkDir:           "pkg",
		StateDir:          "state",
		CacheDir:          "cache",
		RunDir:            runDir,
		AdditionalEnvVars: []string{"PYTHONPATH=" + configuredPath},
	})
	if err != nil {
		t.Fatalf("withPythonAuditGuard: %v", err)
	}
	values := envMap(additional)
	pythonPath := strings.Split(values["PYTHONPATH"], string(os.PathListSeparator))
	shimDir := filepath.Join(runDir, "python_audit_guard")
	want := []string{shimDir, configuredPath, inheritedPath}
	if strings.Join(pythonPath, "\n") != strings.Join(want, "\n") {
		t.Fatalf("PYTHONPATH = %#v, want %#v", pythonPath, want)
	}
}

func envMap(values []string) map[string]string {
	out := map[string]string{}
	for _, item := range values {
		name, value, ok := strings.Cut(item, "=")
		if ok {
			out[name] = value
		}
	}
	return out
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

func writeProcessPluginPackageVersion(t *testing.T, store *PluginStore, base ManifestV2, version string, source string) ManifestV2 {
	t.Helper()
	manifest := base
	manifest.Version = version
	dir, err := store.CreateImmutablePackageDir(manifest.ID, manifest.Version)
	if err != nil {
		t.Fatalf("CreateImmutablePackageDir %s: %v", version, err)
	}
	manifestYAML := strings.ReplaceAll(`
schema_version: emoagent.plugin.v0.2
id: com.example.echo
name: Echo
version: VERSION
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
`, "VERSION", version)
	if err := os.WriteFile(filepath.Join(dir, manifestFileName), []byte(manifestYAML), 0o644); err != nil {
		t.Fatalf("write manifest %s: %v", version, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(source), 0o644); err != nil {
		t.Fatalf("write main.py %s: %v", version, err)
	}
	return manifest
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

func requirePythonIsolatedSafePathSupport(t *testing.T, python string) {
	t.Helper()
	cmd := exec.Command(python, "-I", "-P", "-c", "import sys")
	if err := cmd.Run(); err != nil {
		t.Skipf("%s does not support -I -P: %v", python, err)
	}
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

func bootstrapProbePythonPluginSource() string {
	return pythonRPCPrelude() + `
def handle(method, params):
    if method == "initialize":
        return {"tools": []}
    if method == "invoke_hook":
        import os, sys
        return {"Annotations": {
            "isolated": bool(sys.flags.isolated),
            "safe_path": bool(getattr(sys.flags, "safe_path", False)),
            "no_user_site": bool(sys.flags.no_user_site),
            "host_bootstrap_seen": os.environ.get("EMO_PLUGIN_HOST_BOOTSTRAP") == "1",
            "inherited_pythonpath": os.environ.get("PYTHONPATH") == os.environ.get("EMO_TEST_INHERITED_PYTHONPATH"),
            "plugin_root_importable": any(os.path.abspath(item) == os.path.abspath(os.environ.get("EMO_PLUGIN_ROOT", "")) for item in sys.path),
        }}
    if method == "shutdown":
        send({"jsonrpc": "2.0", "id": current_id, "result": None})
        sys.exit(0)
    return None

main(handle)
`
}

func versionEchoPythonPluginSource() string {
	return pythonRPCPrelude() + `
import os

def handle(method, params):
    if method == "initialize":
        return {"tools": [{"name": "echo", "description": "Echo input", "parameters": {"type": "object"}}]}
    if method == "invoke_hook":
        return {"Annotations": {"runtime_version": os.environ.get("EMO_PLUGIN_VERSION", "")}}
    if method == "shutdown":
        send({"jsonrpc": "2.0", "id": current_id, "result": None})
        sys.exit(0)
    return None

main(handle)
`
}

func envProbePythonPluginSource() string {
	return pythonRPCPrelude() + `
import os

def handle(method, params):
    if method == "initialize":
        return {"tools": []}
    if method == "invoke_hook":
        return {"Annotations": {"runtime_only_key_seen": os.environ.get("RUNTIME_ONLY_KEY") == "secret"}}
    if method == "shutdown":
        send({"jsonrpc": "2.0", "id": current_id, "result": None})
        sys.exit(0)
    return None

main(handle)
`
}

func dependencyImportPythonPluginSource() string {
	return pythonRPCPrelude() + `
def handle(method, params):
    if method == "initialize":
        import depmod
        return {"tools": [{"name": "echo", "description": depmod.VALUE, "parameters": {"type": "object"}}]}
    if method == "invoke_hook":
        import depmod
        return {"Annotations": {"dependency": depmod.VALUE}}
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

func auditGuardProbePythonPluginSource() string {
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
