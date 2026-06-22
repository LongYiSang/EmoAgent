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
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/longyisang/emoagent/internal/processguard"
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
	DependencyEnvDir  string
	ManagedPython     bool
	StartupTimeout    time.Duration
	ShutdownTimeout   time.Duration
	MaxStderrBytes    int
	MaxProcesses      int
	MemoryBytes       int64
	CPUQuota          float64
	BlockedEnvNames   []string
	AdditionalEnvVars []string
	OnProtocolError   func(error)
}

type ProcessRuntime struct {
	cfg            ProcessLaunchConfig
	cmd            *exec.Cmd
	guard          processguard.Guard
	peer           *JSONRPCPeer
	done           chan error
	stderr         *boundedBuffer
	closeGuardOnce sync.Once
}

func StartProcessRuntime(ctx context.Context, cfg ProcessLaunchConfig, handler JSONRPCHandler) (*ProcessRuntime, error) {
	if err := validateProcessLaunchConfig(cfg); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	launch, err := prepareProcessLaunch(cfg)
	if err != nil {
		return nil, err
	}
	cfg = launch.Config
	cmd := exec.Command(cfg.PythonExecutable, launch.Args...)
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
	guard := processguard.NewWithLimits(processguard.Limits{
		MaxProcesses: cfg.MaxProcesses,
		MemoryBytes:  cfg.MemoryBytes,
		CPUQuota:     cfg.CPUQuota,
	})
	if err := guard.Attach(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = guard.Close()
		return nil, fmt.Errorf("process guard attach failed: %w", err)
	}
	runtime := &ProcessRuntime{
		cfg:    cfg,
		cmd:    cmd,
		guard:  guard,
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
		runtime.closeGuard()
		runtime.peer.CloseWithError(fmt.Errorf("plugin process exited: %w", err))
		runtime.done <- err
		close(runtime.done)
	}()
	return runtime, nil
}

type preparedProcessLaunch struct {
	Config ProcessLaunchConfig
	Args   []string
}

func prepareProcessLaunch(cfg ProcessLaunchConfig) (preparedProcessLaunch, error) {
	if cfg.ManagedPython {
		bootstrap, additional, err := withManagedPythonHostBootstrap(cfg)
		if err != nil {
			return preparedProcessLaunch{}, err
		}
		cfg.AdditionalEnvVars = additional
		return preparedProcessLaunch{
			Config: cfg,
			Args:   []string{"-I", "-P", "-u", bootstrap},
		}, nil
	}
	additional, err := withPythonAuditGuard(cfg)
	if err != nil {
		return preparedProcessLaunch{}, err
	}
	cfg.AdditionalEnvVars = additional
	return preparedProcessLaunch{
		Config: cfg,
		Args:   []string{filepath.FromSlash(cfg.Entry)},
	}, nil
}

func withManagedPythonHostBootstrap(cfg ProcessLaunchConfig) (string, []string, error) {
	bootstrapDir := filepath.Join(cfg.RunDir, "host_bootstrap")
	observerDir := filepath.Join(cfg.RunDir, "python_audit_observer")
	if err := os.MkdirAll(bootstrapDir, 0o755); err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(observerDir, 0o755); err != nil {
		return "", nil, err
	}
	observerPath := filepath.Join(observerDir, "audit_observer.py")
	if err := os.WriteFile(observerPath, []byte(pythonAuditGuardSource()), 0o644); err != nil {
		return "", nil, err
	}
	bootstrapPath := filepath.Join(bootstrapDir, "host_bootstrap.py")
	if err := os.WriteFile(bootstrapPath, []byte(pythonHostBootstrapSource()), 0o644); err != nil {
		return "", nil, err
	}
	extraPaths, additional := splitPythonPathEnv(cfg.AdditionalEnvVars, false)
	allowedRoots := pluginAllowedRoots(cfg)
	additional = append(additional,
		"EMO_PLUGIN_ALLOWED_ROOTS="+strings.Join(allowedRoots, string(os.PathListSeparator)),
		"EMO_PLUGIN_AUDIT_OBSERVER="+observerPath,
		"EMO_PLUGIN_BOOTSTRAP_EXTRA_PATHS="+strings.Join(extraPaths, string(os.PathListSeparator)),
		"EMO_PLUGIN_ENTRY="+filepath.FromSlash(cfg.Entry),
	)
	return bootstrapPath, additional, nil
}

func withPythonAuditGuard(cfg ProcessLaunchConfig) ([]string, error) {
	shimDir := filepath.Join(cfg.RunDir, "python_audit_guard")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(shimDir, "sitecustomize.py"), []byte(pythonAuditGuardSource()), 0o644); err != nil {
		return nil, err
	}
	pythonPathParts, additional := splitPythonPathEnv(cfg.AdditionalEnvVars, strings.TrimSpace(cfg.DependencyEnvDir) == "")
	prefix := []string{shimDir}
	if strings.TrimSpace(cfg.DependencyEnvDir) != "" {
		prefix = append(prefix, cfg.DependencyEnvDir)
	}
	pythonPathParts = append(prefix, pythonPathParts...)
	additional = append(additional,
		"EMO_PLUGIN_ALLOWED_ROOTS="+strings.Join(pluginAllowedRoots(cfg), string(os.PathListSeparator)),
		"PYTHONPATH="+strings.Join(pythonPathParts, string(os.PathListSeparator)),
	)
	return additional, nil
}

func splitPythonPathEnv(values []string, includeInherited bool) ([]string, []string) {
	paths := make([]string, 0, len(values)+2)
	additional := make([]string, 0, len(values)+2)
	for _, item := range values {
		name, value, ok := strings.Cut(item, "=")
		if ok && strings.EqualFold(name, "PYTHONPATH") {
			appendPathList(&paths, value)
			continue
		}
		additional = append(additional, item)
	}
	if includeInherited {
		appendPathList(&paths, os.Getenv("PYTHONPATH"))
	}
	return paths, additional
}

func appendPathList(paths *[]string, value string) {
	for _, item := range strings.Split(value, string(os.PathListSeparator)) {
		if strings.TrimSpace(item) != "" {
			*paths = append(*paths, item)
		}
	}
}

func pluginAllowedRoots(cfg ProcessLaunchConfig) []string {
	allowedRoots := []string{cfg.WorkDir, cfg.StateDir, cfg.CacheDir, cfg.RunDir}
	if strings.TrimSpace(cfg.DependencyEnvDir) != "" {
		allowedRoots = append(allowedRoots, cfg.DependencyEnvDir)
	}
	return allowedRoots
}

func pythonAuditGuardSource() string {
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

func pythonHostBootstrapSource() string {
	return `import os, runpy, sys

def _split_paths(value):
    return [item for item in (value or "").split(os.pathsep) if item]

def _prepend(path):
    if not path:
        return
    path = os.path.abspath(path)
    if path not in sys.path:
        sys.path.insert(0, path)

def _under(path, root):
    try:
        return os.path.commonpath([os.path.abspath(path), os.path.abspath(root)]) == os.path.abspath(root)
    except Exception:
        return False

plugin_root = os.path.abspath(os.environ["EMO_PLUGIN_ROOT"])
entry = os.environ["EMO_PLUGIN_ENTRY"]
entry_path = os.path.abspath(os.path.join(plugin_root, entry))
if not _under(entry_path, plugin_root):
    raise RuntimeError("plugin entry escapes plugin root")

paths = [plugin_root]
deps = os.environ.get("EMO_PLUGIN_DEPS_DIR")
if deps:
    paths.append(deps)
paths.extend(_split_paths(os.environ.get("EMO_PLUGIN_BOOTSTRAP_EXTRA_PATHS")))
for item in reversed(paths):
    _prepend(item)

observer = os.environ.get("EMO_PLUGIN_AUDIT_OBSERVER")
if observer:
    runpy.run_path(observer, run_name="_emo_plugin_audit_observer")

os.environ["EMO_PLUGIN_HOST_BOOTSTRAP"] = "1"
sys.argv = [entry_path]
os.chdir(plugin_root)
runpy.run_path(entry_path, run_name="__main__")
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
	if cfg.MaxProcesses < 0 {
		return fmt.Errorf("max processes must be >= 0")
	}
	if cfg.MemoryBytes < 0 {
		return fmt.Errorf("memory bytes must be >= 0")
	}
	if cfg.CPUQuota < 0 {
		return fmt.Errorf("cpu quota must be >= 0")
	}
	return nil
}

func (r *ProcessRuntime) Call(ctx context.Context, method string, params any, result any) error {
	if r == nil || r.peer == nil {
		return fmt.Errorf("plugin runtime is not started")
	}
	return r.peer.Call(ctx, method, params, result)
}

func (r *ProcessRuntime) PID() int {
	if r == nil || r.cmd == nil || r.cmd.Process == nil {
		return 0
	}
	return r.cmd.Process.Pid
}

func (r *ProcessRuntime) ProcessGuardSnapshot() processguard.Snapshot {
	if r == nil || r.guard == nil {
		return processguard.Snapshot{Kind: processguard.KindNone}
	}
	return r.guard.Snapshot()
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
		if r.guard != nil {
			_ = r.guard.Terminate(1)
		} else {
			_ = r.cmd.Process.Kill()
		}
		<-r.done
		return shutdownCtx.Err()
	}
}

func (r *ProcessRuntime) closeGuard() {
	if r == nil || r.guard == nil {
		return
	}
	r.closeGuardOnce.Do(func() {
		_ = r.guard.Close()
	})
}

func (r *ProcessRuntime) StderrTail() string {
	if r == nil || r.stderr == nil {
		return ""
	}
	return r.stderr.String()
}

func buildPluginProcessEnv(base []string, cfg ProcessLaunchConfig) []string {
	if cfg.ManagedPython {
		return buildManagedPythonProcessEnv(base, cfg)
	}
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
	if strings.TrimSpace(cfg.DependencyEnvDir) != "" {
		out = append(out, "EMO_PLUGIN_DEPS_DIR="+cfg.DependencyEnvDir)
	}
	out = append(out, cfg.AdditionalEnvVars...)
	return out
}

func buildManagedPythonProcessEnv(base []string, cfg ProcessLaunchConfig) []string {
	out := make([]string, 0, 16+len(cfg.AdditionalEnvVars))
	for _, name := range managedPythonAllowedHostEnvNames() {
		if value, ok := lookupEnv(base, name); ok {
			out = append(out, name+"="+value)
		}
	}
	for _, prefix := range managedPythonAllowedHostEnvPrefixes() {
		for _, item := range base {
			name, value, ok := strings.Cut(item, "=")
			if ok && strings.HasPrefix(strings.ToUpper(name), prefix) {
				out = append(out, name+"="+value)
			}
		}
	}
	if path := managedPythonPath(base, cfg.PythonExecutable); path != "" {
		out = append(out, "PATH="+path)
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
	if strings.TrimSpace(cfg.DependencyEnvDir) != "" {
		out = append(out, "EMO_PLUGIN_DEPS_DIR="+cfg.DependencyEnvDir)
	}
	out = append(out, managedPythonExplicitEnv(cfg.AdditionalEnvVars)...)
	return out
}

func managedPythonAllowedHostEnvNames() []string {
	return []string{
		"SystemRoot",
		"WINDIR",
		"ComSpec",
		"TEMP",
		"TMP",
		"PATHEXT",
		"LANG",
		"LC_ALL",
		"LC_CTYPE",
		"LC_MESSAGES",
	}
}

func managedPythonAllowedHostEnvPrefixes() []string {
	return []string{"EMO_PLUGIN_"}
}

func managedPythonExplicitEnv(values []string) []string {
	out := make([]string, 0, len(values))
	for _, item := range values {
		name, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if managedPythonControlledEnvName(name) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func managedPythonControlledEnvName(name string) bool {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "PATH", "PYTHONHOME", "PYTHONPATH", "PYTHONSTARTUP", "PYTHONUSERBASE", "VIRTUAL_ENV":
		return true
	default:
		return false
	}
}

func managedPythonPath(base []string, pythonExecutable string) string {
	parts := []string{}
	if dir := filepath.Dir(strings.TrimSpace(pythonExecutable)); dir != "." && dir != "" {
		parts = append(parts, dir)
	}
	if runtime.GOOS == "windows" {
		if root, ok := lookupEnv(base, "SystemRoot"); ok && strings.TrimSpace(root) != "" {
			parts = append(parts,
				filepath.Join(root, "System32"),
				root,
				filepath.Join(root, "System32", "Wbem"),
			)
		} else if root, ok := lookupEnv(base, "WINDIR"); ok && strings.TrimSpace(root) != "" {
			parts = append(parts,
				filepath.Join(root, "System32"),
				root,
				filepath.Join(root, "System32", "Wbem"),
			)
		}
	} else {
		parts = append(parts, "/usr/local/bin", "/usr/bin", "/bin")
	}
	return strings.Join(uniqueNonEmptyPaths(parts), string(os.PathListSeparator))
}

func uniqueNonEmptyPaths(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := value
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(value)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func lookupEnv(values []string, target string) (string, bool) {
	for _, item := range values {
		name, value, ok := strings.Cut(item, "=")
		if ok && strings.EqualFold(name, target) {
			return value, true
		}
	}
	return "", false
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
