package pytoolchain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

func TestEnvironmentManagerFirstSyncWritesMarker(t *testing.T) {
	project := writeUVProject(t, "triviumdb==0.7.1")
	envDir := filepath.Join(t.TempDir(), "memory-sidecar")
	runner := &environmentFakeRunner{}
	manager := NewEnvironmentManager(environmentTestConfig(t), EnvironmentManagerOptions{
		Runner:    runner,
		Toolchain: environmentTestProbe(),
	})

	status, err := manager.Ensure(context.Background(), EnvironmentOwner{
		Kind:       OwnerMemorySidecar,
		ID:         "memorycore_sidecar",
		Version:    "0.1.0",
		ProjectDir: project,
		EnvDir:     envDir,
	})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if status.State != EnvReady {
		t.Fatalf("status = %#v", status)
	}
	if runner.Count("lock --check") != 1 || runner.Count("sync --locked --no-dev") != 1 {
		t.Fatalf("calls = %#v", runner.CommandLines())
	}
	marker, err := ReadEnvironmentMarker(envDir)
	if err != nil {
		t.Fatalf("ReadEnvironmentMarker: %v", err)
	}
	if marker.OwnerKind != string(OwnerMemorySidecar) || marker.OwnerID != "memorycore_sidecar" || marker.SyncStatus != string(EnvReady) {
		t.Fatalf("marker = %#v", marker)
	}
	if marker.UVLockHash == "" || marker.PyprojectHash == "" || marker.EnvironmentPython == "" {
		t.Fatalf("marker missing hashes/python: %#v", marker)
	}
	if strings.Contains(marker.EnvironmentPython, ".sync-") || !strings.HasPrefix(marker.EnvironmentPython, envDir) {
		t.Fatalf("marker environment python = %q, want final env dir %q", marker.EnvironmentPython, envDir)
	}
}

func TestEnvironmentManagerReadyEnvironmentDoesNotRunUV(t *testing.T) {
	project := writeUVProject(t, "triviumdb==0.7.1")
	envDir := filepath.Join(t.TempDir(), "memory-sidecar")
	runner := &environmentFakeRunner{}
	manager := NewEnvironmentManager(environmentTestConfig(t), EnvironmentManagerOptions{
		Runner:    runner,
		Toolchain: environmentTestProbe(),
	})
	owner := EnvironmentOwner{Kind: OwnerMemorySidecar, ID: "memorycore_sidecar", Version: "0.1.0", ProjectDir: project, EnvDir: envDir}
	if _, err := manager.Ensure(context.Background(), owner); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	runner.Reset()

	status, err := manager.Ensure(context.Background(), owner)
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if status.State != EnvReady {
		t.Fatalf("status = %#v", status)
	}
	if len(runner.calls) != 1 || !strings.Contains(runner.CommandLines()[0], "python.exe -I -P -c") {
		t.Fatalf("calls after ready = %#v, want only env python probe", runner.CommandLines())
	}
}

func TestEnvironmentManagerLockChangeTriggersSync(t *testing.T) {
	project := writeUVProject(t, "triviumdb==0.7.1")
	envDir := filepath.Join(t.TempDir(), "memory-sidecar")
	runner := &environmentFakeRunner{}
	manager := NewEnvironmentManager(environmentTestConfig(t), EnvironmentManagerOptions{
		Runner:    runner,
		Toolchain: environmentTestProbe(),
	})
	owner := EnvironmentOwner{Kind: OwnerMemorySidecar, ID: "memorycore_sidecar", Version: "0.1.0", ProjectDir: project, EnvDir: envDir}
	if _, err := manager.Ensure(context.Background(), owner); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project, "uv.lock"), []byte("changed-lock"), 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	runner.Reset()

	status, err := manager.Ensure(context.Background(), owner)
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if status.State != EnvReady {
		t.Fatalf("status = %#v", status)
	}
	if runner.Count("sync --locked --no-dev") != 1 {
		t.Fatalf("calls = %#v, want resync", runner.CommandLines())
	}
}

func TestEnvironmentManagerStaleLockPreservesReadyEnvironment(t *testing.T) {
	project := writeUVProject(t, "triviumdb==0.7.1")
	envDir := filepath.Join(t.TempDir(), "memory-sidecar")
	runner := &environmentFakeRunner{}
	manager := NewEnvironmentManager(environmentTestConfig(t), EnvironmentManagerOptions{
		Runner:    runner,
		Toolchain: environmentTestProbe(),
	})
	owner := EnvironmentOwner{Kind: OwnerMemorySidecar, ID: "memorycore_sidecar", Version: "0.1.0", ProjectDir: project, EnvDir: envDir}
	if _, err := manager.Ensure(context.Background(), owner); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	oldMarker, err := ReadEnvironmentMarker(envDir)
	if err != nil {
		t.Fatalf("old marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project, "uv.lock"), []byte("changed-lock"), 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	runner.Reset()
	runner.failLockCheck = true

	status, err := manager.Ensure(context.Background(), owner)
	if err == nil || !strings.Contains(err.Error(), "uv lock --check") {
		t.Fatalf("Ensure error = %v, want lock check failure", err)
	}
	if status.State != EnvBroken {
		t.Fatalf("status = %#v, want broken", status)
	}
	currentMarker, err := ReadEnvironmentMarker(envDir)
	if err != nil {
		t.Fatalf("current marker: %v", err)
	}
	if currentMarker.UVLockHash != oldMarker.UVLockHash {
		t.Fatalf("old marker was not preserved: old=%s current=%s", oldMarker.UVLockHash, currentMarker.UVLockHash)
	}
}

func TestEnvironmentManagerRealMemoryCoreSidecarProject(t *testing.T) {
	python := strings.TrimSpace(os.Getenv("EMO_TEST_PYTHON_TOOLCHAIN_PYTHON"))
	uv := strings.TrimSpace(os.Getenv("EMO_TEST_PYTHON_TOOLCHAIN_UV"))
	project := strings.TrimSpace(os.Getenv("EMO_TEST_MEMORYCORE_SIDECAR_PROJECT"))
	if python == "" || uv == "" || project == "" {
		t.Skip("set EMO_TEST_PYTHON_TOOLCHAIN_PYTHON, EMO_TEST_PYTHON_TOOLCHAIN_UV, and EMO_TEST_MEMORYCORE_SIDECAR_PROJECT to run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cfg := config.PythonToolchainConfig{
		Enabled:               true,
		PythonExecutable:      python,
		UVExecutable:          uv,
		RequiredPython:        "3.12",
		MinimumUVVersion:      "0.11.0",
		EnvironmentRoot:       filepath.Join(t.TempDir(), "python-envs"),
		CacheDir:              filepath.Join(t.TempDir(), "uv-cache"),
		DefaultIndex:          "https://pypi.org/simple",
		SyncTimeoutSeconds:    240,
		UseSystemCertificates: true,
	}
	probe, err := NewManager(cfg).Probe(ctx)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	envDir := filepath.Join(cfg.EnvironmentRoot, "memory-sidecar")
	manager := NewEnvironmentManager(cfg, EnvironmentManagerOptions{Toolchain: probe})
	status, err := manager.Ensure(ctx, EnvironmentOwner{
		Kind:       OwnerMemorySidecar,
		ID:         "memorycore_sidecar",
		Version:    "0.1.0",
		ProjectDir: project,
		EnvDir:     envDir,
	})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if status.State != EnvReady || status.Marker == nil || status.Marker.EnvironmentPython == "" {
		t.Fatalf("status = %#v", status)
	}
	cmd := exec.CommandContext(ctx, status.Marker.EnvironmentPython, "-I", "-P", "-m", "memorycore_sidecar.server", "--help")
	cmd.Dir = project
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sidecar help: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "memorycore") && !strings.Contains(string(out), "usage") {
		t.Fatalf("unexpected help output: %s", out)
	}
}

type environmentFakeRunner struct {
	calls         []Command
	failLockCheck bool
}

func (r *environmentFakeRunner) Run(_ context.Context, cmd Command) CommandResult {
	r.calls = append(r.calls, cmd)
	line := strings.Join(append([]string{filepath.Base(cmd.Path)}, cmd.Args...), " ")
	switch {
	case strings.Contains(line, "python.exe -I -P -c"):
		return CommandResult{Stdout: `{"implementation":"CPython","version":"3.12.11","major":3,"minor":12,"patch":11,"architecture":"AMD64","executable":"C:/env/Scripts/python.exe","prefix":"C:/env","isolated":true,"safe_path":true}`, ExitCode: 0}
	case strings.Contains(line, "uv.exe lock --check"):
		if r.failLockCheck {
			return CommandResult{Stderr: "lock needs update", ExitCode: 1}
		}
		return CommandResult{Stdout: "Resolved 1 package", ExitCode: 0}
	case strings.Contains(line, "uv.exe sync --locked --no-dev"):
		envDir := envValue(cmd.Env, "UV_PROJECT_ENVIRONMENT")
		if envDir == "" {
			return CommandResult{Stderr: "missing UV_PROJECT_ENVIRONMENT", ExitCode: 2}
		}
		pythonDir := filepath.Join(envDir, "Scripts")
		if err := os.MkdirAll(pythonDir, 0o755); err != nil {
			return CommandResult{Err: err, ExitCode: 1}
		}
		if err := os.WriteFile(filepath.Join(pythonDir, "python.exe"), []byte("python"), 0o755); err != nil {
			return CommandResult{Err: err, ExitCode: 1}
		}
		return CommandResult{Stdout: "Installed", ExitCode: 0}
	default:
		return CommandResult{Stderr: "unexpected command " + line, ExitCode: 99}
	}
}

func (r *environmentFakeRunner) Reset() {
	r.calls = nil
	r.failLockCheck = false
}

func (r *environmentFakeRunner) Count(needle string) int {
	count := 0
	for _, line := range r.CommandLines() {
		if strings.Contains(line, needle) {
			count++
		}
	}
	return count
}

func (r *environmentFakeRunner) CommandLines() []string {
	out := make([]string, 0, len(r.calls))
	for _, call := range r.calls {
		out = append(out, strings.Join(append([]string{filepath.Base(call.Path)}, call.Args...), " "))
	}
	return out
}

func writeUVProject(t *testing.T, dep string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[project]\nname=\"sidecar\"\nversion=\"0.1.0\"\nrequires-python=\">=3.12,<3.13\"\ndependencies=[\""+dep+"\"]\n"), 0o644); err != nil {
		t.Fatalf("write pyproject: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "uv.lock"), []byte("lock"), 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	return dir
}

func environmentTestConfig(t *testing.T) config.PythonToolchainConfig {
	t.Helper()
	return config.PythonToolchainConfig{
		Enabled:               true,
		PythonExecutable:      "C:/Python312/python.exe",
		UVExecutable:          "C:/Users/me/.local/bin/uv.exe",
		RequiredPython:        "3.12",
		MinimumUVVersion:      "0.11.0",
		EnvironmentRoot:       filepath.Join(t.TempDir(), "python-envs"),
		CacheDir:              filepath.Join(t.TempDir(), "uv-cache"),
		DefaultIndex:          "https://pypi.org/simple",
		SyncTimeoutSeconds:    30,
		UseSystemCertificates: true,
	}
}

func environmentTestProbe() ProbeResult {
	return ProbeResult{
		Python: PythonProbeResult{Version: "3.12.11", Architecture: "AMD64"},
		UV:     UVProbeResult{Version: "0.11.9"},
		Fingerprint: ToolchainFingerprint{
			SchemaVersion:  "emoagent.python_toolchain.v1",
			PythonPath:     "C:/Python312/python.exe",
			PythonVersion:  "3.12.11",
			PythonArch:     "AMD64",
			PythonFileHash: "sha256:python",
			UVPath:         "C:/Users/me/.local/bin/uv.exe",
			UVVersion:      "0.11.9",
		},
	}
}

func envValue(env []string, name string) string {
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok && key == name {
			return value
		}
	}
	return ""
}

var _ = time.Second
