package plugin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ProcessLaunchConfig struct {
	PluginID          string
	Version           string
	WorkDir           string
	Entry             string
	PythonExecutable  string
	StateDir          string
	CacheDir          string
	RunDir            string
	StartupTimeout    time.Duration
	ShutdownTimeout   time.Duration
	MaxStderrBytes    int
	BlockedEnvNames   []string
	AdditionalEnvVars []string
	OnProtocolError   func(error)
}

type ProcessRuntime struct {
	cfg    ProcessLaunchConfig
	cmd    *exec.Cmd
	peer   *JSONRPCPeer
	done   chan error
	stderr *boundedBuffer
}

func StartProcessRuntime(ctx context.Context, cfg ProcessLaunchConfig, handler JSONRPCHandler) (*ProcessRuntime, error) {
	if err := validateProcessLaunchConfig(cfg); err != nil {
		return nil, err
	}
	var err error
	cfg.AdditionalEnvVars, err = withPythonSecurityShim(cfg)
	if err != nil {
		return nil, err
	}
	entry := filepath.FromSlash(cfg.Entry)
	cmd := exec.CommandContext(ctx, cfg.PythonExecutable, entry)
	cmd.Dir = cfg.WorkDir
	cmd.Env = buildPluginProcessEnv(os.Environ(), cfg)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	runtime := &ProcessRuntime{
		cfg:    cfg,
		cmd:    cmd,
		peer:   NewJSONRPCPeer(stdin, handler),
		done:   make(chan error, 1),
		stderr: newBoundedBuffer(cfg.MaxStderrBytes),
	}
	go func() {
		if err := runtime.peer.Serve(context.Background(), stdout); err != nil && !errors.Is(err, io.EOF) && cfg.OnProtocolError != nil {
			cfg.OnProtocolError(err)
		}
	}()
	go func() {
		_, _ = io.Copy(runtime.stderr, stderr)
	}()
	go func() {
		err := cmd.Wait()
		runtime.peer.CloseWithError(fmt.Errorf("plugin process exited: %w", err))
		runtime.done <- err
		close(runtime.done)
	}()
	return runtime, nil
}

func withPythonSecurityShim(cfg ProcessLaunchConfig) ([]string, error) {
	shimDir := filepath.Join(cfg.RunDir, "python_security")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(shimDir, "sitecustomize.py"), []byte(pythonSecurityShimSource()), 0o644); err != nil {
		return nil, err
	}
	additional := make([]string, 0, len(cfg.AdditionalEnvVars)+2)
	pythonPath := shimDir
	for _, item := range cfg.AdditionalEnvVars {
		name, value, ok := strings.Cut(item, "=")
		if ok && strings.EqualFold(name, "PYTHONPATH") {
			if strings.TrimSpace(value) != "" {
				pythonPath += string(os.PathListSeparator) + value
			}
			continue
		}
		additional = append(additional, item)
	}
	if existing := os.Getenv("PYTHONPATH"); existing != "" {
		pythonPath += string(os.PathListSeparator) + existing
	}
	additional = append(additional,
		"EMO_PLUGIN_ALLOWED_ROOTS="+strings.Join([]string{cfg.WorkDir, cfg.StateDir, cfg.CacheDir, cfg.RunDir}, string(os.PathListSeparator)),
		"PYTHONPATH="+pythonPath,
	)
	return additional, nil
}

func pythonSecurityShimSource() string {
	return `import os, sys

_allowed_roots = []
for _item in os.environ.get("EMO_PLUGIN_ALLOWED_ROOTS", "").split(os.pathsep):
    if _item:
        try:
            _allowed_roots.append(os.path.abspath(_item))
        except Exception:
            pass

def _under(path, root):
    try:
        return os.path.commonpath([os.path.abspath(path), root]) == root
    except Exception:
        return False

def _protected_path(path):
    try:
        value = os.fspath(path)
    except Exception:
        return False
    if not value:
        return False
    absolute = os.path.abspath(value)
    if any(_under(absolute, root) for root in _allowed_roots):
        return False
    lower = absolute.lower()
    return lower.endswith((".db", ".sqlite", ".sqlite3")) or "memorycore" in lower or "trivium" in lower

def _from_asyncio():
    try:
        import inspect
        for _frame in inspect.stack()[1:8]:
            _file = (_frame.filename or "").replace("\\", "/").lower()
            if "/asyncio/" in _file:
                return True
    except Exception:
        return False
    return False

def _audit(event, args):
    if event == "socket.bind" and not _from_asyncio():
        raise PermissionError("plugin raw socket listeners are disabled")
    if event == "open" and args:
        if _protected_path(args[0]):
            raise PermissionError("plugin direct database or memory store access is disabled")
    if event == "sqlite3.connect" and args:
        if _protected_path(args[0]):
            raise PermissionError("plugin direct sqlite access is disabled")

sys.addaudithook(_audit)
`
}

func validateProcessLaunchConfig(cfg ProcessLaunchConfig) error {
	if !validPluginID(cfg.PluginID) {
		return fmt.Errorf("invalid plugin id %q", cfg.PluginID)
	}
	if !validSemver(cfg.Version) {
		return fmt.Errorf("invalid plugin version %q", cfg.Version)
	}
	if strings.TrimSpace(cfg.PythonExecutable) == "" {
		return fmt.Errorf("python executable is required")
	}
	if strings.TrimSpace(cfg.WorkDir) == "" {
		return fmt.Errorf("work dir is required")
	}
	if err := validateRelativeEntry(cfg.Entry); err != nil {
		return fmt.Errorf("entry: %w", err)
	}
	if cfg.MaxStderrBytes <= 0 {
		return fmt.Errorf("max stderr bytes must be > 0")
	}
	return nil
}

func (r *ProcessRuntime) Call(ctx context.Context, method string, params any, result any) error {
	if r == nil || r.peer == nil {
		return fmt.Errorf("plugin runtime is not started")
	}
	return r.peer.Call(ctx, method, params, result)
}

func (r *ProcessRuntime) Stop(ctx context.Context) error {
	if r == nil || r.cmd == nil || r.cmd.Process == nil {
		return nil
	}
	shutdownCtx := ctx
	if _, ok := shutdownCtx.Deadline(); !ok && r.cfg.ShutdownTimeout > 0 {
		var cancel context.CancelFunc
		shutdownCtx, cancel = context.WithTimeout(ctx, r.cfg.ShutdownTimeout)
		defer cancel()
	}
	_ = r.Call(shutdownCtx, "shutdown", map[string]any{}, nil)
	select {
	case err := <-r.done:
		return err
	case <-shutdownCtx.Done():
		_ = r.cmd.Process.Kill()
		<-r.done
		return shutdownCtx.Err()
	}
}

func (r *ProcessRuntime) StderrTail() string {
	if r == nil || r.stderr == nil {
		return ""
	}
	return r.stderr.String()
}

func buildPluginProcessEnv(base []string, cfg ProcessLaunchConfig) []string {
	blocked := map[string]struct{}{}
	for _, name := range cfg.BlockedEnvNames {
		name = strings.ToUpper(strings.TrimSpace(name))
		if name != "" {
			blocked[name] = struct{}{}
		}
	}
	out := make([]string, 0, len(base)+8)
	for _, item := range base {
		name, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(name)
		if _, deny := blocked[upper]; deny || sensitiveEnvName(upper) {
			continue
		}
		out = append(out, item)
	}
	out = append(out,
		"EMO_PLUGIN_ID="+cfg.PluginID,
		"EMO_PLUGIN_VERSION="+cfg.Version,
		"EMO_PLUGIN_ROOT="+cfg.WorkDir,
		"EMO_PLUGIN_STATE_DIR="+cfg.StateDir,
		"EMO_PLUGIN_CACHE_DIR="+cfg.CacheDir,
		"EMO_PLUGIN_RUN_DIR="+cfg.RunDir,
		"PYTHONUNBUFFERED=1",
	)
	out = append(out, cfg.AdditionalEnvVars...)
	return out
}

func sensitiveEnvName(name string) bool {
	return strings.Contains(name, "API_KEY") ||
		strings.Contains(name, "SECRET") ||
		strings.Contains(name, "TOKEN") ||
		strings.Contains(name, "PASSWORD")
}

type boundedBuffer struct {
	mu    sync.Mutex
	limit int
	buf   []byte
}

func newBoundedBuffer(limit int) *boundedBuffer {
	if limit <= 0 {
		limit = 262144
	}
	return &boundedBuffer{limit: limit}
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.limit {
		b.buf = append([]byte(nil), b.buf[len(b.buf)-b.limit:]...)
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(bytes.TrimSpace(b.buf))
}
