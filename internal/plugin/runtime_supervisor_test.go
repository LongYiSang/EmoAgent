package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

func TestRuntimeSupervisorConcurrentEnsureReadyStartsOneProcess(t *testing.T) {
	python := findPythonForTest(t)
	startLog := filepath.Join(t.TempDir(), "starts.log")
	store, manifest := writeProcessPluginPackage(t, concurrentStartPythonPluginSource())
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:    true,
		PythonExecutable:  python,
		StartupTimeoutMS:  3000,
		ShutdownTimeoutMS: 1000,
		MaxStderrBytes:    8192,
	}, nil)
	supervisor.SetAdditionalEnvVars([]string{
		"EMO_TEST_START_LOG=" + startLog,
		"EMO_TEST_INIT_DELAY_MS=300",
		"EMO_TEST_AUTO_EXIT_MS=5000",
	})
	supervisor.AddPlugin(manifest)
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	const callers = 20
	start := make(chan struct{})
	runtimes := make(chan *ProcessRuntime, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			runtime, err := supervisor.EnsureReady(context.Background(), manifest.ID)
			runtimes <- runtime
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(runtimes)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("EnsureReady: %v", err)
		}
	}
	uniqueRuntimePIDs := map[int]struct{}{}
	for runtime := range runtimes {
		if runtime == nil {
			t.Fatal("runtime = nil")
		}
		uniqueRuntimePIDs[runtime.PID()] = struct{}{}
	}
	if len(uniqueRuntimePIDs) != 1 {
		t.Fatalf("returned runtime pids = %#v, want one shared runtime", uniqueRuntimePIDs)
	}
	if got := uniqueStartLogCount(t, startLog); got != 1 {
		t.Fatalf("process starts = %d, want 1", got)
	}
}

func TestRuntimeSupervisorConcurrentEnsureReadyStartFailureSharesError(t *testing.T) {
	python := findPythonForTest(t)
	startLog := filepath.Join(t.TempDir(), "starts.log")
	store, manifest := writeProcessPluginPackage(t, failingInitializePythonPluginSource())
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:    true,
		PythonExecutable:  python,
		StartupTimeoutMS:  3000,
		ShutdownTimeoutMS: 1000,
		MaxStderrBytes:    8192,
	}, nil)
	supervisor.SetAdditionalEnvVars([]string{
		"EMO_TEST_START_LOG=" + startLog,
		"EMO_TEST_INIT_DELAY_MS=300",
		"EMO_TEST_INIT_ERROR=init failed once",
	})
	supervisor.AddPlugin(manifest)

	const callers = 20
	start := make(chan struct{})
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := supervisor.EnsureReady(context.Background(), manifest.ID)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	var firstErr string
	for err := range errs {
		if err == nil {
			t.Fatal("EnsureReady error = nil, want initialize error")
		}
		if firstErr == "" {
			firstErr = err.Error()
		} else if err.Error() != firstErr {
			t.Fatalf("EnsureReady error = %q, want shared %q", err.Error(), firstErr)
		}
	}
	if !strings.Contains(firstErr, "init failed once") {
		t.Fatalf("EnsureReady error = %q, want init failed once", firstErr)
	}
	if got := uniqueStartLogCount(t, startLog); got != 1 {
		t.Fatalf("process starts = %d, want 1", got)
	}
}

func TestRuntimeSupervisorStopConcurrentStartDoesNotAllowOldGenerationToOverwriteNewRuntime(t *testing.T) {
	python := findPythonForTest(t)
	root := t.TempDir()
	startLog := filepath.Join(root, "starts.log")
	v1Started := filepath.Join(root, "v1-started")
	releaseV1 := filepath.Join(root, "release-v1")
	store, v1 := writeProcessPluginPackage(t, gatedInitializePythonPluginSource())
	v2 := writeProcessPluginPackageVersion(t, store, v1, "0.2.0", gatedInitializePythonPluginSource())
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:    true,
		PythonExecutable:  python,
		StartupTimeoutMS:  3000,
		ShutdownTimeoutMS: 1000,
		MaxStderrBytes:    8192,
	}, nil)
	supervisor.SetAdditionalEnvVars([]string{
		"EMO_TEST_START_LOG=" + startLog,
		"EMO_TEST_V1_STARTED=" + v1Started,
		"EMO_TEST_RELEASE_V1=" + releaseV1,
		"EMO_TEST_AUTO_EXIT_MS=5000",
	})
	supervisor.AddPlugin(v1)
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	v1Done := make(chan error, 1)
	go func() {
		_, err := supervisor.EnsureReady(context.Background(), v1.ID)
		v1Done <- err
	}()
	waitForFile(t, v1Started)

	supervisor.AddPlugin(v2)
	if err := supervisor.Stop(context.Background(), v1.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	v2Runtime, err := supervisor.EnsureReady(context.Background(), v2.ID)
	if err != nil {
		t.Fatalf("EnsureReady v2: %v", err)
	}
	if err := os.WriteFile(releaseV1, []byte("ok"), 0o644); err != nil {
		t.Fatalf("release v1: %v", err)
	}
	select {
	case <-v1Done:
	case <-time.After(3 * time.Second):
		t.Fatal("v1 EnsureReady did not return")
	}

	status := supervisor.Status(v2.ID)
	if status.Version != v2.Version || status.PID != v2Runtime.PID() {
		t.Fatalf("status = %#v, want v2 runtime pid %d", status, v2Runtime.PID())
	}
}

func TestRuntimeSupervisorConcurrentHookAndToolFirstUseStartsOneProcess(t *testing.T) {
	python := findPythonForTest(t)
	startLog := filepath.Join(t.TempDir(), "starts.log")
	store, manifest := writeProcessPluginPackage(t, concurrentStartPythonPluginSource())
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:    true,
		PythonExecutable:  python,
		StartupTimeoutMS:  3000,
		ShutdownTimeoutMS: 1000,
		MaxStderrBytes:    8192,
	}, nil)
	supervisor.SetAdditionalEnvVars([]string{
		"EMO_TEST_START_LOG=" + startLog,
		"EMO_TEST_INIT_DELAY_MS=300",
		"EMO_TEST_AUTO_EXIT_MS=5000",
	})
	supervisor.AddPlugin(manifest)
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	start := make(chan struct{})
	hookErr := make(chan error, 1)
	toolErr := make(chan error, 1)
	go func() {
		<-start
		result, err := supervisor.InvokeHook(context.Background(), manifest.ID, HookAfterTurnEnd, HookContext{})
		if err == nil && result.Annotations["hook"] != "ok" {
			err = fmt.Errorf("hook annotations = %#v, want hook ok", result.Annotations)
		}
		hookErr <- err
	}()
	go func() {
		<-start
		raw, err := supervisor.InvokeTool(context.Background(), manifest.ID, "echo", json.RawMessage(`{"text":"hello"}`))
		if err == nil && !strings.Contains(string(raw), "hello") {
			err = fmt.Errorf("tool raw = %s, want hello", raw)
		}
		toolErr <- err
	}()
	close(start)

	if err := <-hookErr; err != nil {
		t.Fatalf("InvokeHook: %v", err)
	}
	if err := <-toolErr; err != nil {
		t.Fatalf("InvokeTool: %v", err)
	}
	if got := uniqueStartLogCount(t, startLog); got != 1 {
		t.Fatalf("process starts = %d, want 1", got)
	}
}

func TestRuntimeSupervisorIdleReaperStopsIdleRuntimeAfterTimeout(t *testing.T) {
	python := findPythonForTest(t)
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	store, manifest := writeProcessPluginPackage(t, normalPythonPluginSource())
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:             true,
		PythonExecutable:           python,
		StartupTimeoutMS:           3000,
		ShutdownTimeoutMS:          1000,
		IdleTimeoutSeconds:         10,
		MaxStderrBytes:             8192,
		CrashBackoffInitialSeconds: 1,
		CrashBackoffMaxSeconds:     2,
		CrashQuarantineThreshold:   5,
	}, nil)
	supervisor.now = func() time.Time { return now }
	supervisor.AddPlugin(manifest)
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	if _, err := supervisor.InvokeHook(t.Context(), manifest.ID, HookAfterTurnEnd, HookContext{}); err != nil {
		t.Fatalf("InvokeHook: %v", err)
	}
	running := supervisor.Status(manifest.ID)
	if running.Status != "running" || running.PID == 0 || running.LastUsedAt == "" {
		t.Fatalf("running status = %#v, want running pid and last_used_at", running)
	}

	now = now.Add(9 * time.Second)
	supervisor.reapIdleRuntimes(context.Background())
	if status := supervisor.Status(manifest.ID); status.Status != "running" || status.PID != running.PID {
		t.Fatalf("status before idle timeout = %#v, want running pid %d", status, running.PID)
	}

	now = now.Add(1 * time.Second)
	supervisor.reapIdleRuntimes(context.Background())
	if status := supervisor.Status(manifest.ID); status.Status != "stopped" || status.PID != 0 {
		t.Fatalf("status after idle timeout = %#v, want stopped", status)
	}
}

func TestRuntimeSupervisorIdleReaperSkipsInFlightRuntime(t *testing.T) {
	python := findPythonForTest(t)
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	started := filepath.Join(root, "hook-started")
	release := filepath.Join(root, "release-hook")
	store, manifest := writeProcessPluginPackage(t, gatedHookPythonPluginSource())
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:             true,
		PythonExecutable:           python,
		StartupTimeoutMS:           3000,
		ShutdownTimeoutMS:          1000,
		IdleTimeoutSeconds:         1,
		MaxStderrBytes:             8192,
		CrashBackoffInitialSeconds: 1,
		CrashBackoffMaxSeconds:     2,
		CrashQuarantineThreshold:   5,
	}, nil)
	supervisor.now = func() time.Time { return now }
	supervisor.SetAdditionalEnvVars([]string{
		"EMO_TEST_HOOK_STARTED=" + started,
		"EMO_TEST_RELEASE_HOOK=" + release,
	})
	supervisor.AddPlugin(manifest)
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	errCh := make(chan error, 1)
	go func() {
		_, err := supervisor.InvokeHook(context.Background(), manifest.ID, HookAfterTurnEnd, HookContext{})
		errCh <- err
	}()
	waitForFile(t, started)

	now = now.Add(2 * time.Second)
	supervisor.reapIdleRuntimes(context.Background())
	if status := supervisor.Status(manifest.ID); status.Status != "running" || status.InFlight != 1 {
		t.Fatalf("status during in-flight hook = %#v, want running in_flight=1", status)
	}

	if err := os.WriteFile(release, []byte("ok"), 0o644); err != nil {
		t.Fatalf("release hook: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("InvokeHook: %v", err)
	}
	now = now.Add(time.Second)
	supervisor.reapIdleRuntimes(context.Background())
	if status := supervisor.Status(manifest.ID); status.Status != "stopped" {
		t.Fatalf("status after in-flight hook completed = %#v, want stopped", status)
	}
}

func TestRuntimeSupervisorStartFailureBackoffAndQuarantine(t *testing.T) {
	python := findPythonForTest(t)
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	startLog := filepath.Join(t.TempDir(), "starts.log")
	store, manifest := writeProcessPluginPackage(t, failingInitializePythonPluginSource())
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:             true,
		PythonExecutable:           python,
		StartupTimeoutMS:           3000,
		ShutdownTimeoutMS:          1000,
		MaxStderrBytes:             8192,
		CrashBackoffInitialSeconds: 2,
		CrashBackoffMaxSeconds:     5,
		CrashQuarantineThreshold:   2,
	}, nil)
	supervisor.now = func() time.Time { return now }
	supervisor.SetAdditionalEnvVars([]string{
		"EMO_TEST_START_LOG=" + startLog,
		"EMO_TEST_INIT_ERROR=init failed for backoff",
	})
	supervisor.AddPlugin(manifest)

	_, err := supervisor.EnsureReady(t.Context(), manifest.ID)
	if err == nil || !strings.Contains(err.Error(), "init failed for backoff") {
		t.Fatalf("EnsureReady error = %v, want init failure", err)
	}
	status := supervisor.Status(manifest.ID)
	if status.Status != "backoff" || status.ConsecutiveFailures != 1 || status.NextStartAt == "" {
		t.Fatalf("status after first failure = %#v, want backoff with next_start_at", status)
	}

	_, err = supervisor.EnsureReady(t.Context(), manifest.ID)
	var backoffErr *RuntimeBackoffError
	if !errors.As(err, &backoffErr) || !backoffErr.NextStartAt.Equal(now.Add(2*time.Second)) {
		t.Fatalf("EnsureReady backoff error = %#v, want next_start_at %s", err, now.Add(2*time.Second))
	}
	if got := uniqueStartLogCount(t, startLog); got != 1 {
		t.Fatalf("process starts in backoff = %d, want 1", got)
	}

	now = now.Add(2 * time.Second)
	_, err = supervisor.EnsureReady(t.Context(), manifest.ID)
	if err == nil || !strings.Contains(err.Error(), "init failed for backoff") {
		t.Fatalf("EnsureReady second failure error = %v, want init failure", err)
	}
	status = supervisor.Status(manifest.ID)
	if status.Status != "quarantined" || status.ConsecutiveFailures != 2 {
		t.Fatalf("status after second failure = %#v, want quarantined", status)
	}
	_, err = supervisor.EnsureReady(t.Context(), manifest.ID)
	if err == nil || !strings.Contains(err.Error(), "quarantined") {
		t.Fatalf("EnsureReady quarantined error = %v", err)
	}
	if got := uniqueStartLogCount(t, startLog); got != 2 {
		t.Fatalf("process starts after quarantine = %d, want 2", got)
	}
}

func TestRuntimeSupervisorStableRuntimeResetsConsecutiveFailures(t *testing.T) {
	python := findPythonForTest(t)
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	failInit := filepath.Join(root, "fail-init")
	crashHook := filepath.Join(root, "crash-hook")
	if err := os.WriteFile(failInit, []byte("fail once"), 0o644); err != nil {
		t.Fatalf("write fail marker: %v", err)
	}
	store, manifest := writeProcessPluginPackage(t, flakyRuntimePythonPluginSource())
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:             true,
		PythonExecutable:           python,
		StartupTimeoutMS:           3000,
		ShutdownTimeoutMS:          1000,
		MaxStderrBytes:             8192,
		CrashBackoffInitialSeconds: 1,
		CrashBackoffMaxSeconds:     5,
		CrashQuarantineThreshold:   2,
	}, nil)
	supervisor.now = func() time.Time { return now }
	supervisor.SetAdditionalEnvVars([]string{
		"EMO_TEST_FAIL_INIT_MARKER=" + failInit,
		"EMO_TEST_CRASH_HOOK_MARKER=" + crashHook,
	})
	supervisor.AddPlugin(manifest)
	t.Cleanup(func() { _ = supervisor.StopAll(context.Background()) })

	if _, err := supervisor.EnsureReady(t.Context(), manifest.ID); err == nil {
		t.Fatal("EnsureReady error = nil, want first initialize failure")
	}
	now = now.Add(1 * time.Second)
	if _, err := supervisor.EnsureReady(t.Context(), manifest.ID); err != nil {
		t.Fatalf("EnsureReady after backoff: %v", err)
	}
	now = now.Add(runtimeStableResetDuration + time.Second)
	if err := os.WriteFile(crashHook, []byte("crash"), 0o644); err != nil {
		t.Fatalf("write crash marker: %v", err)
	}
	_, err := supervisor.InvokeHook(t.Context(), manifest.ID, HookAfterTurnEnd, HookContext{})
	if err == nil {
		t.Fatal("InvokeHook error = nil, want crash")
	}
	status := supervisor.Status(manifest.ID)
	if status.Status != "backoff" || status.ConsecutiveFailures != 1 {
		t.Fatalf("status after stable crash = %#v, want backoff with consecutive_failures reset to 1", status)
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
	supervisor.SetAdditionalEnvVars([]string{"EMO_TEST_INHERITED_PYTHONPATH=" + inheritedPythonPath})
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

func TestRuntimeSupervisorManagedPythonDoesNotInheritHostSensitiveEnv(t *testing.T) {
	python := findPythonForTest(t)
	requirePythonIsolatedSafePathSupport(t, python)
	for _, name := range []string{"DATABASE_URL", "GITHUB_PAT", "MY_PRIVATE_VALUE", "TAVILY_API_KEY"} {
		t.Setenv(name, "secret")
	}
	store, manifest := writeProcessPluginPackage(t, envLeakProbePythonPluginSource())
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
	for _, name := range []string{"DATABASE_URL", "GITHUB_PAT", "MY_PRIVATE_VALUE", "TAVILY_API_KEY"} {
		if result.Annotations[name] != false {
			t.Fatalf("%s visible to managed plugin: annotations=%#v", name, result.Annotations)
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

func uniqueStartLogCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("read start log: %v", err)
	}
	unique := map[string]struct{}{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			unique[line] = struct{}{}
		}
	}
	return len(unique)
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("file %s was not created", path)
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

func concurrentStartPythonPluginSource() string {
	return pythonRPCPrelude() + pythonConcurrentTestHelpers() + `
def handle(method, params):
    if method == "initialize":
        log_start()
        arm_auto_exit()
        delay_ms = int_env("EMO_TEST_INIT_DELAY_MS")
        if delay_ms > 0:
            time.sleep(delay_ms / 1000)
        return {"tools": [{"name": "echo", "description": "Echo input", "parameters": {"type": "object"}}]}
    if method == "invoke_hook":
        return {"Annotations": {"hook": "ok"}}
    if method == "invoke_tool":
        return {"ok": True, "input": params.get("input")}
    if method == "shutdown":
        send({"jsonrpc": "2.0", "id": current_id, "result": None})
        sys.exit(0)
    return None

main(handle)
`
}

func failingInitializePythonPluginSource() string {
	return pythonRPCPrelude() + pythonConcurrentTestHelpers() + `
def handle(method, params):
    if method == "initialize":
        log_start()
        delay_ms = int_env("EMO_TEST_INIT_DELAY_MS")
        if delay_ms > 0:
            time.sleep(delay_ms / 1000)
        raise RuntimeError(os.environ.get("EMO_TEST_INIT_ERROR", "init failed"))
    if method == "shutdown":
        send({"jsonrpc": "2.0", "id": current_id, "result": None})
        sys.exit(0)
    return None

main(handle)
`
}

func gatedInitializePythonPluginSource() string {
	return pythonRPCPrelude() + pythonConcurrentTestHelpers() + `
def handle(method, params):
    if method == "initialize":
        log_start()
        arm_auto_exit()
        if os.environ.get("EMO_PLUGIN_VERSION") == "0.1.0":
            started = os.environ.get("EMO_TEST_V1_STARTED")
            if started:
                with open(started, "w", encoding="utf-8") as f:
                    f.write("started")
            release = os.environ.get("EMO_TEST_RELEASE_V1")
            while release and not os.path.exists(release):
                time.sleep(0.01)
        return {"tools": []}
    if method == "shutdown":
        send({"jsonrpc": "2.0", "id": current_id, "result": None})
        sys.exit(0)
    return None

main(handle)
`
}

func pythonConcurrentTestHelpers() string {
	return `
import os, threading

def int_env(name):
    try:
        return int(os.environ.get(name, "0"))
    except ValueError:
        return 0

def log_start():
    path = os.environ.get("EMO_TEST_START_LOG")
    if path:
        with open(path, "a", encoding="utf-8") as f:
            f.write(str(os.getpid()) + "\n")
            f.flush()

def arm_auto_exit():
    delay_ms = int_env("EMO_TEST_AUTO_EXIT_MS")
    if delay_ms <= 0:
        return
    def exit_later():
        time.sleep(delay_ms / 1000)
        os._exit(0)
    threading.Thread(target=exit_later, daemon=True).start()
`
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

func envLeakProbePythonPluginSource() string {
	return pythonRPCPrelude() + `
import os

def handle(method, params):
    if method == "initialize":
        return {"tools": []}
    if method == "invoke_hook":
        names = ["DATABASE_URL", "GITHUB_PAT", "MY_PRIVATE_VALUE", "TAVILY_API_KEY"]
        return {"Annotations": {name: os.environ.get(name) == "secret" for name in names}}
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

func gatedHookPythonPluginSource() string {
	return pythonRPCPrelude() + `
import os

def handle(method, params):
    if method == "initialize":
        return {"tools": []}
    if method == "invoke_hook":
        started = os.environ.get("EMO_TEST_HOOK_STARTED")
        if started:
            with open(started, "w", encoding="utf-8") as f:
                f.write("started")
        release = os.environ.get("EMO_TEST_RELEASE_HOOK")
        while release and not os.path.exists(release):
            time.sleep(0.01)
        return {"Annotations": {"hook": "done"}}
    if method == "shutdown":
        send({"jsonrpc": "2.0", "id": current_id, "result": None})
        sys.exit(0)
    return None

main(handle)
`
}

func flakyRuntimePythonPluginSource() string {
	return pythonRPCPrelude() + `
import os

def handle(method, params):
    if method == "initialize":
        marker = os.environ.get("EMO_TEST_FAIL_INIT_MARKER")
        if marker and os.path.exists(marker):
            os.remove(marker)
            raise RuntimeError("init failed once")
        return {"tools": []}
    if method == "invoke_hook":
        marker = os.environ.get("EMO_TEST_CRASH_HOOK_MARKER")
        if marker and os.path.exists(marker):
            sys.exit(7)
        return {"Annotations": {"hook": "ok"}}
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
