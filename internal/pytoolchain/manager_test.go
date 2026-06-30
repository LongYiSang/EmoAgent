package pytoolchain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

type fakeRunner struct {
	calls []Command
	fn    func(Command) CommandResult
}

func (r *fakeRunner) Run(_ context.Context, cmd Command) CommandResult {
	r.calls = append(r.calls, cmd)
	if r.fn != nil {
		return r.fn(cmd)
	}
	return CommandResult{}
}

func TestManagerProbeAcceptsCPython312AndUV(t *testing.T) {
	runner := &fakeRunner{fn: func(cmd Command) CommandResult {
		joined := strings.Join(append([]string{cmd.Path}, cmd.Args...), " ")
		switch {
		case strings.Contains(joined, "python.exe -I -P -c"):
			return CommandResult{Stdout: `{"implementation":"CPython","version":"3.12.11","major":3,"minor":12,"patch":11,"architecture":"AMD64","executable":"C:/Python312/python.exe","prefix":"C:/Python312","isolated":true,"safe_path":true}`, ExitCode: 0}
		case strings.Contains(joined, "uv.exe --version"):
			return CommandResult{Stdout: "uv 0.11.9 (7829a03b6 2026-05-05 x86_64-pc-windows-msvc)", ExitCode: 0}
		case strings.Contains(joined, "uv.exe venv"):
			if !envHas(cmd.Env, "UV_PYTHON=C:/Python312/python.exe") ||
				!envHas(cmd.Env, "UV_PYTHON_DOWNLOADS=never") ||
				!envHas(cmd.Env, "UV_NO_MANAGED_PYTHON=1") ||
				!envHas(cmd.Env, "UV_NO_ENV_FILE=1") {
				t.Fatalf("uv venv env = %#v", cmd.Env)
			}
			return CommandResult{Stdout: "Using CPython 3.12.11", ExitCode: 0}
		default:
			t.Fatalf("unexpected command: %#v", cmd)
			return CommandResult{ExitCode: 1}
		}
	}}
	manager := NewManager(config.PythonToolchainConfig{
		Enabled:               true,
		PythonExecutable:      "C:/Python312/python.exe",
		UVExecutable:          "C:/Users/me/.local/bin/uv.exe",
		RequiredPython:        "3.12",
		MinimumUVVersion:      "0.11.0",
		EnvironmentRoot:       "data/python-envs",
		CacheDir:              "data/uv-cache",
		DefaultIndex:          "https://pypi.org/simple",
		SyncTimeoutSeconds:    600,
		UseSystemCertificates: true,
	}, WithRunner(runner), WithProcessArchitecture("AMD64"), WithTempDir(t.TempDir()))

	result, err := manager.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if result.Python.Version != "3.12.11" || result.UV.Version != "0.11.9" {
		t.Fatalf("probe result = %#v", result)
	}
	if result.Fingerprint.PythonPath != "C:/Python312/python.exe" || result.Fingerprint.UVPath != "C:/Users/me/.local/bin/uv.exe" {
		t.Fatalf("fingerprint = %#v", result.Fingerprint)
	}
	if len(runner.calls) != 4 {
		t.Fatalf("calls = %d, want python probe, uv probe, uv venv, env python probe", len(runner.calls))
	}
}

func TestManagerProbeRejectsNon312Python(t *testing.T) {
	runner := &fakeRunner{fn: func(cmd Command) CommandResult {
		return CommandResult{Stdout: `{"implementation":"CPython","version":"3.11.9","major":3,"minor":11,"patch":9,"architecture":"AMD64","executable":"C:/Python311/python.exe","prefix":"C:/Python311","isolated":true,"safe_path":true}`, ExitCode: 0}
	}}
	manager := NewManager(config.PythonToolchainConfig{
		Enabled:          true,
		PythonExecutable: "C:/Python311/python.exe",
		UVExecutable:     "C:/Users/me/.local/bin/uv.exe",
		RequiredPython:   "3.12",
	}, WithRunner(runner), WithProcessArchitecture("AMD64"))

	_, err := manager.Probe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "CPython 3.12") {
		t.Fatalf("Probe error = %v, want 3.12 rejection", err)
	}
}

func TestManagerProbeRejectsPython313(t *testing.T) {
	runner := &fakeRunner{fn: func(cmd Command) CommandResult {
		return CommandResult{Stdout: `{"implementation":"CPython","version":"3.13.1","major":3,"minor":13,"patch":1,"architecture":"AMD64","executable":"C:/Python313/python.exe","prefix":"C:/Python313","isolated":true,"safe_path":true}`, ExitCode: 0}
	}}
	manager := NewManager(config.PythonToolchainConfig{
		Enabled:          true,
		PythonExecutable: "C:/Python313/python.exe",
		UVExecutable:     "C:/Users/me/.local/bin/uv.exe",
		RequiredPython:   "3.12",
	}, WithRunner(runner), WithProcessArchitecture("AMD64"))

	_, err := manager.Probe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "CPython 3.12") {
		t.Fatalf("Probe error = %v, want 3.12 rejection", err)
	}
}

func TestManagerProbeRejectsNonCPython(t *testing.T) {
	runner := &fakeRunner{fn: func(cmd Command) CommandResult {
		return CommandResult{Stdout: `{"implementation":"PyPy","version":"3.12.0","major":3,"minor":12,"patch":0,"architecture":"AMD64","executable":"C:/PyPy/python.exe","prefix":"C:/PyPy","isolated":true,"safe_path":true}`, ExitCode: 0}
	}}
	manager := NewManager(config.PythonToolchainConfig{
		Enabled:          true,
		PythonExecutable: "C:/PyPy/python.exe",
		UVExecutable:     "C:/Users/me/.local/bin/uv.exe",
		RequiredPython:   "3.12",
	}, WithRunner(runner), WithProcessArchitecture("AMD64"))

	_, err := manager.Probe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "implementation must be CPython") {
		t.Fatalf("Probe error = %v, want CPython rejection", err)
	}
}

func TestManagerProbeRejectsArchitectureMismatch(t *testing.T) {
	runner := &fakeRunner{fn: func(cmd Command) CommandResult {
		return CommandResult{Stdout: `{"implementation":"CPython","version":"3.12.11","major":3,"minor":12,"patch":11,"architecture":"ARM64","executable":"C:/Python312/python.exe","prefix":"C:/Python312","isolated":true,"safe_path":true}`, ExitCode: 0}
	}}
	manager := NewManager(config.PythonToolchainConfig{
		Enabled:          true,
		PythonExecutable: "C:/Python312/python.exe",
		UVExecutable:     "C:/Users/me/.local/bin/uv.exe",
		RequiredPython:   "3.12",
	}, WithRunner(runner), WithProcessArchitecture("AMD64"))

	_, err := manager.Probe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "architecture") {
		t.Fatalf("Probe error = %v, want architecture rejection", err)
	}
}

func TestManagerProbeRejectsOldUV(t *testing.T) {
	runner := &fakeRunner{fn: func(cmd Command) CommandResult {
		joined := strings.Join(append([]string{cmd.Path}, cmd.Args...), " ")
		if strings.Contains(joined, "python.exe") {
			return CommandResult{Stdout: `{"implementation":"CPython","version":"3.12.11","major":3,"minor":12,"patch":11,"architecture":"AMD64","executable":"C:/Python312/python.exe","prefix":"C:/Python312","isolated":true,"safe_path":true}`, ExitCode: 0}
		}
		return CommandResult{Stdout: "uv 0.10.0", ExitCode: 0}
	}}
	manager := NewManager(config.PythonToolchainConfig{
		Enabled:          true,
		PythonExecutable: "C:/Python312/python.exe",
		UVExecutable:     "C:/Users/me/.local/bin/uv.exe",
		RequiredPython:   "3.12",
		MinimumUVVersion: "0.11.0",
	}, WithRunner(runner), WithProcessArchitecture("AMD64"))

	_, err := manager.Probe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "minimum uv version") {
		t.Fatalf("Probe error = %v, want uv version rejection", err)
	}
}

func TestManagerProbeRejectsMissingUV(t *testing.T) {
	runner := &fakeRunner{fn: func(cmd Command) CommandResult {
		joined := strings.Join(append([]string{cmd.Path}, cmd.Args...), " ")
		if strings.Contains(joined, "python.exe") {
			return CommandResult{Stdout: `{"implementation":"CPython","version":"3.12.11","major":3,"minor":12,"patch":11,"architecture":"AMD64","executable":"C:/Python312/python.exe","prefix":"C:/Python312","isolated":true,"safe_path":true}`, ExitCode: 0}
		}
		return CommandResult{Err: os.ErrNotExist, ExitCode: 1}
	}}
	manager := NewManager(config.PythonToolchainConfig{
		Enabled:          true,
		PythonExecutable: "C:/Python312/python.exe",
		UVExecutable:     "C:/Users/me/.local/bin/uv.exe",
		RequiredPython:   "3.12",
		MinimumUVVersion: "0.11.0",
	}, WithRunner(runner), WithProcessArchitecture("AMD64"))

	_, err := manager.Probe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "uv probe failed") {
		t.Fatalf("Probe error = %v, want uv probe failure", err)
	}
}

func TestManagerProbeRejectsInvalidMinimumUVVersion(t *testing.T) {
	runner := &fakeRunner{fn: func(cmd Command) CommandResult {
		joined := strings.Join(append([]string{cmd.Path}, cmd.Args...), " ")
		if strings.Contains(joined, "python.exe") {
			return CommandResult{Stdout: `{"implementation":"CPython","version":"3.12.11","major":3,"minor":12,"patch":11,"architecture":"AMD64","executable":"C:/Python312/python.exe","prefix":"C:/Python312","isolated":true,"safe_path":true}`, ExitCode: 0}
		}
		return CommandResult{Stdout: "uv 0.11.9", ExitCode: 0}
	}}
	manager := NewManager(config.PythonToolchainConfig{
		Enabled:          true,
		PythonExecutable: "C:/Python312/python.exe",
		UVExecutable:     "C:/Users/me/.local/bin/uv.exe",
		RequiredPython:   "3.12",
		MinimumUVVersion: "latest",
	}, WithRunner(runner), WithProcessArchitecture("AMD64"))

	_, err := manager.Probe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not a valid version") {
		t.Fatalf("Probe error = %v, want invalid version", err)
	}
}

func TestManagerProbeRejectsRelativeExecutablePaths(t *testing.T) {
	manager := NewManager(config.PythonToolchainConfig{
		Enabled:          true,
		PythonExecutable: "python",
		UVExecutable:     "uv",
		RequiredPython:   "3.12",
	}, WithRunner(&fakeRunner{}), WithProcessArchitecture("AMD64"))

	_, err := manager.Probe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "python executable must be absolute") {
		t.Fatalf("Probe error = %v, want absolute path rejection", err)
	}
}

func TestManagerProbeAppliesTimeout(t *testing.T) {
	runner := &fakeRunner{fn: func(cmd Command) CommandResult {
		if cmd.Timeout != 2*time.Second {
			t.Fatalf("timeout = %s, want 2s", cmd.Timeout)
		}
		return CommandResult{Stdout: `{"implementation":"CPython","version":"3.12.11","major":3,"minor":12,"patch":11,"architecture":"AMD64","executable":"C:/Python312/python.exe","prefix":"C:/Python312","isolated":true,"safe_path":true}`, ExitCode: 0}
	}}
	manager := NewManager(config.PythonToolchainConfig{
		Enabled:            true,
		PythonExecutable:   "C:/Python312/python.exe",
		UVExecutable:       "C:/Users/me/.local/bin/uv.exe",
		RequiredPython:     "3.12",
		SyncTimeoutSeconds: 2,
		MinimumUVVersion:   "0.11.0",
		EnvironmentRoot:    "data/python-envs",
		CacheDir:           "data/uv-cache",
		DefaultIndex:       "https://pypi.org/simple",
	}, WithRunner(runner), WithProcessArchitecture("AMD64"))

	_, _ = manager.Probe(context.Background())
}

func TestManagerProbeUVBindingUsesUniqueTempEnvPerProbe(t *testing.T) {
	base := t.TempDir()
	runner := &concurrentProbeRunner{}
	cfg := config.PythonToolchainConfig{
		Enabled:            true,
		PythonExecutable:   "C:/Python312/python.exe",
		UVExecutable:       "C:/Users/me/.local/bin/uv.exe",
		RequiredPython:     "3.12",
		MinimumUVVersion:   "0.11.0",
		EnvironmentRoot:    "data/python-envs",
		CacheDir:           "data/uv-cache",
		DefaultIndex:       "https://pypi.org/simple",
		SyncTimeoutSeconds: 2,
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := NewManager(cfg, WithRunner(runner), WithProcessArchitecture("AMD64"), WithTempDir(base)).Probe(context.Background())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Probe: %v", err)
		}
	}

	targets := runner.venvTargets()
	if len(targets) != 2 {
		t.Fatalf("uv venv targets = %#v, want 2", targets)
	}
	if targets[0] == targets[1] {
		t.Fatalf("uv binding probe reused temp env %q", targets[0])
	}
}

func TestManagerProbeRetriesTransientUVBindingFailure(t *testing.T) {
	uvVenvAttempts := 0
	runner := &fakeRunner{fn: func(cmd Command) CommandResult {
		joined := strings.Join(append([]string{cmd.Path}, cmd.Args...), " ")
		switch {
		case strings.Contains(joined, "python.exe -I -P -c"):
			return CommandResult{Stdout: `{"implementation":"CPython","version":"3.12.11","major":3,"minor":12,"patch":11,"architecture":"AMD64","executable":"C:/Python312/python.exe","prefix":"C:/Python312","isolated":true,"safe_path":true}`, ExitCode: 0}
		case strings.Contains(joined, "uv.exe --version"):
			return CommandResult{Stdout: "uv 0.11.9", ExitCode: 0}
		case strings.Contains(joined, "uv.exe venv"):
			uvVenvAttempts++
			if uvVenvAttempts == 1 {
				return CommandResult{Stderr: "Failed to update Windows PE resources", ExitCode: 2}
			}
			return CommandResult{Stdout: "Using CPython 3.12.11", ExitCode: 0}
		default:
			t.Fatalf("unexpected command: %#v", cmd)
			return CommandResult{ExitCode: 1}
		}
	}}
	manager := NewManager(config.PythonToolchainConfig{
		Enabled:            true,
		PythonExecutable:   "C:/Python312/python.exe",
		UVExecutable:       "C:/Users/me/.local/bin/uv.exe",
		RequiredPython:     "3.12",
		MinimumUVVersion:   "0.11.0",
		EnvironmentRoot:    "data/python-envs",
		CacheDir:           "data/uv-cache",
		DefaultIndex:       "https://pypi.org/simple",
		SyncTimeoutSeconds: 600,
	}, WithRunner(runner), WithProcessArchitecture("AMD64"), WithTempDir(t.TempDir()))

	if _, err := manager.Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if uvVenvAttempts != 2 {
		t.Fatalf("uv venv attempts = %d, want 2", uvVenvAttempts)
	}
}

func TestManagerDefaultsPreserveDisabledSystemCertificates(t *testing.T) {
	manager := NewManager(config.PythonToolchainConfig{
		PythonExecutable: "C:/Python312/python.exe",
		UVExecutable:     "C:/Users/me/.local/bin/uv.exe",
	})

	for _, item := range manager.uvCommandEnv("C:/env") {
		if strings.HasPrefix(item, "UV_SYSTEM_CERTS=") {
			t.Fatalf("uv env = %#v, want no UV_SYSTEM_CERTS when config false", manager.uvCommandEnv("C:/env"))
		}
	}
}

func TestCommandEnvironmentDoesNotInheritPythonVirtualEnvState(t *testing.T) {
	t.Setenv("VIRTUAL_ENV", "C:/old-venv")
	t.Setenv("CONDA_PREFIX", "C:/conda")
	t.Setenv("PYTHONPATH", "C:/pythonpath")

	env := commandEnvironment([]string{"UV_PYTHON=C:/Python312/python.exe"})
	for _, item := range env {
		switch {
		case strings.HasPrefix(item, "VIRTUAL_ENV="),
			strings.HasPrefix(item, "CONDA_PREFIX="),
			strings.HasPrefix(item, "PYTHONPATH="):
			t.Fatalf("env inherited Python state: %#v", env)
		}
	}
}

func TestPythonFileIdentityUsesFileContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "python.exe")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatalf("write python: %v", err)
	}
	first := pythonFileIdentity(path)
	if err := os.WriteFile(path, []byte("two"), 0o644); err != nil {
		t.Fatalf("rewrite python: %v", err)
	}
	second := pythonFileIdentity(path)
	if first == second {
		t.Fatalf("python file identity did not change: %s", first)
	}
	if !strings.HasPrefix(first, "sha256:") || !strings.HasPrefix(second, "sha256:") {
		t.Fatalf("file identities = %q / %q", first, second)
	}
}

func TestManagerProbeRealToolchain(t *testing.T) {
	python := strings.TrimSpace(os.Getenv("EMO_TEST_PYTHON_TOOLCHAIN_PYTHON"))
	uv := strings.TrimSpace(os.Getenv("EMO_TEST_PYTHON_TOOLCHAIN_UV"))
	if python == "" || uv == "" {
		t.Skip("set EMO_TEST_PYTHON_TOOLCHAIN_PYTHON and EMO_TEST_PYTHON_TOOLCHAIN_UV to run real probe")
	}
	manager := NewManager(config.PythonToolchainConfig{
		Enabled:               true,
		PythonExecutable:      python,
		UVExecutable:          uv,
		RequiredPython:        "3.12",
		MinimumUVVersion:      "0.11.0",
		EnvironmentRoot:       "data/python-envs",
		CacheDir:              t.TempDir(),
		DefaultIndex:          "https://pypi.org/simple",
		SyncTimeoutSeconds:    120,
		UseSystemCertificates: true,
	}, WithTempDir(t.TempDir()))

	result, err := manager.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe real toolchain: %v", err)
	}
	if result.Python.Major != 3 || result.Python.Minor != 12 {
		t.Fatalf("python result = %#v", result.Python)
	}
	if compareVersion(result.UV.Version, "0.11.0") < 0 {
		t.Fatalf("uv result = %#v", result.UV)
	}
}

func envHas(env []string, want string) bool {
	for _, item := range env {
		if item == want {
			return true
		}
	}
	return false
}

type concurrentProbeRunner struct {
	mu      sync.Mutex
	targets []string
}

func (r *concurrentProbeRunner) Run(_ context.Context, cmd Command) CommandResult {
	joined := strings.Join(append([]string{cmd.Path}, cmd.Args...), " ")
	switch {
	case strings.Contains(joined, "python.exe -I -P -c"):
		return CommandResult{Stdout: `{"implementation":"CPython","version":"3.12.11","major":3,"minor":12,"patch":11,"architecture":"AMD64","executable":"C:/Python312/python.exe","prefix":"C:/Python312","isolated":true,"safe_path":true}`, ExitCode: 0}
	case strings.Contains(joined, "uv.exe --version"):
		return CommandResult{Stdout: "uv 0.11.9", ExitCode: 0}
	case strings.Contains(joined, "uv.exe venv"):
		if len(cmd.Args) < 2 {
			return CommandResult{Stderr: "missing venv target", ExitCode: 1}
		}
		r.mu.Lock()
		r.targets = append(r.targets, cmd.Args[1])
		r.mu.Unlock()
		return CommandResult{Stdout: "Using CPython 3.12.11", ExitCode: 0}
	default:
		return CommandResult{Stderr: "unexpected command: " + joined, ExitCode: 1}
	}
}

func (r *concurrentProbeRunner) venvTargets() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.targets...)
}
