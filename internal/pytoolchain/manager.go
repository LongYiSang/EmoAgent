package pytoolchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/processguard"
)

type Command struct {
	Path    string
	Args    []string
	Env     []string
	Timeout time.Duration
}

type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

type Runner interface {
	Run(context.Context, Command) CommandResult
}

type Manager struct {
	cfg         config.PythonToolchainConfig
	runner      Runner
	processArch string
	tempDir     string
}

type Option func(*Manager)

func WithRunner(runner Runner) Option {
	return func(m *Manager) {
		if runner != nil {
			m.runner = runner
		}
	}
}

func WithProcessArchitecture(arch string) Option {
	return func(m *Manager) {
		m.processArch = strings.TrimSpace(arch)
	}
}

func WithTempDir(path string) Option {
	return func(m *Manager) {
		m.tempDir = strings.TrimSpace(path)
	}
}

func NewManager(cfg config.PythonToolchainConfig, opts ...Option) *Manager {
	applyManagerDefaults(&cfg)
	m := &Manager{
		cfg:         cfg,
		runner:      osExecRunner{},
		processArch: defaultProcessArchitecture(),
		tempDir:     "",
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func applyManagerDefaults(cfg *config.PythonToolchainConfig) {
	if cfg == nil {
		return
	}
	if cfg.RequiredPython == "" {
		cfg.RequiredPython = "3.12"
	}
	if cfg.MinimumUVVersion == "" {
		cfg.MinimumUVVersion = "0.11.0"
	}
	if cfg.EnvironmentRoot == "" {
		cfg.EnvironmentRoot = "data/python-envs"
	}
	if cfg.CacheDir == "" {
		cfg.CacheDir = "data/uv-cache"
	}
	if cfg.DefaultIndex == "" {
		cfg.DefaultIndex = "https://pypi.org/simple"
	}
	if cfg.SyncTimeoutSeconds == 0 {
		cfg.SyncTimeoutSeconds = 600
	}
}

type ProbeResult struct {
	Python      PythonProbeResult    `json:"python"`
	UV          UVProbeResult        `json:"uv"`
	Fingerprint ToolchainFingerprint `json:"fingerprint"`
}

type PythonProbeResult struct {
	Implementation string `json:"implementation"`
	Version        string `json:"version"`
	Major          int    `json:"major"`
	Minor          int    `json:"minor"`
	Patch          int    `json:"patch"`
	Architecture   string `json:"architecture"`
	Executable     string `json:"executable"`
	Prefix         string `json:"prefix"`
	Isolated       bool   `json:"isolated"`
	SafePath       bool   `json:"safe_path"`
}

type UVProbeResult struct {
	Version string `json:"version"`
	Raw     string `json:"raw"`
}

type ToolchainFingerprint struct {
	SchemaVersion  string `json:"schema_version"`
	PythonPath     string `json:"python_path"`
	PythonVersion  string `json:"python_version"`
	PythonArch     string `json:"python_arch"`
	PythonFileHash string `json:"python_file_hash"`
	UVPath         string `json:"uv_path"`
	UVVersion      string `json:"uv_version"`
}

func (m *Manager) Probe(ctx context.Context) (ProbeResult, error) {
	if m == nil {
		return ProbeResult{}, fmt.Errorf("python toolchain manager is nil")
	}
	if err := m.validatePaths(); err != nil {
		return ProbeResult{}, err
	}
	python, err := m.probePython(ctx, m.cfg.PythonExecutable)
	if err != nil {
		return ProbeResult{}, err
	}
	uv, err := m.probeUV(ctx)
	if err != nil {
		return ProbeResult{}, err
	}
	if err := m.probeUVBinding(ctx); err != nil {
		return ProbeResult{}, err
	}
	return ProbeResult{
		Python: python,
		UV:     uv,
		Fingerprint: ToolchainFingerprint{
			SchemaVersion:  "emoagent.python_toolchain.v1",
			PythonPath:     strings.TrimSpace(m.cfg.PythonExecutable),
			PythonVersion:  python.Version,
			PythonArch:     python.Architecture,
			PythonFileHash: pythonFileIdentity(strings.TrimSpace(m.cfg.PythonExecutable)),
			UVPath:         strings.TrimSpace(m.cfg.UVExecutable),
			UVVersion:      uv.Version,
		},
	}, nil
}

func (m *Manager) validatePaths() error {
	if strings.TrimSpace(m.cfg.PythonExecutable) == "" {
		return fmt.Errorf("python executable is required")
	}
	if !filepath.IsAbs(m.cfg.PythonExecutable) {
		return fmt.Errorf("python executable must be absolute")
	}
	if strings.TrimSpace(m.cfg.UVExecutable) == "" {
		return fmt.Errorf("uv executable is required")
	}
	if !filepath.IsAbs(m.cfg.UVExecutable) {
		return fmt.Errorf("uv executable must be absolute")
	}
	return nil
}

func (m *Manager) probePython(ctx context.Context, pythonPath string) (PythonProbeResult, error) {
	cmd := Command{
		Path:    strings.TrimSpace(pythonPath),
		Args:    []string{"-I", "-P", "-c", pythonProbeScript},
		Timeout: m.commandTimeout(),
	}
	result := m.runner.Run(ctx, cmd)
	if result.Err != nil {
		return PythonProbeResult{}, fmt.Errorf("python probe failed: %w", result.Err)
	}
	if result.ExitCode != 0 {
		return PythonProbeResult{}, fmt.Errorf("python probe failed with exit code %d: %s", result.ExitCode, sanitizeLogText(result.Stderr))
	}
	var parsed PythonProbeResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &parsed); err != nil {
		return PythonProbeResult{}, fmt.Errorf("python probe output is not valid JSON: %w", err)
	}
	if parsed.Implementation != "CPython" {
		return PythonProbeResult{}, fmt.Errorf("python implementation must be CPython, got %q", parsed.Implementation)
	}
	if parsed.Major != 3 || parsed.Minor != 12 {
		return PythonProbeResult{}, fmt.Errorf("python executable must be CPython 3.12, got %s", parsed.Version)
	}
	if !sameArchitecture(parsed.Architecture, m.processArch) {
		return PythonProbeResult{}, fmt.Errorf("python architecture %q does not match EmoAgent architecture %q", parsed.Architecture, m.processArch)
	}
	if !parsed.Isolated || !parsed.SafePath {
		return PythonProbeResult{}, fmt.Errorf("python probe did not run with isolated safe path flags")
	}
	return parsed, nil
}

func (m *Manager) probeUV(ctx context.Context) (UVProbeResult, error) {
	result := m.runner.Run(ctx, Command{
		Path:    strings.TrimSpace(m.cfg.UVExecutable),
		Args:    []string{"--version"},
		Timeout: m.commandTimeout(),
	})
	if result.Err != nil {
		return UVProbeResult{}, fmt.Errorf("uv probe failed: %w", result.Err)
	}
	if result.ExitCode != 0 {
		return UVProbeResult{}, fmt.Errorf("uv probe failed with exit code %d: %s", result.ExitCode, sanitizeLogText(result.Stderr))
	}
	version, err := parseUVVersion(result.Stdout)
	if err != nil {
		return UVProbeResult{}, err
	}
	minimum := strings.TrimSpace(m.cfg.MinimumUVVersion)
	if minimum == "" {
		minimum = "0.11.0"
	}
	if !validVersionString(minimum) {
		return UVProbeResult{}, fmt.Errorf("minimum uv version %q is not a valid version", minimum)
	}
	if compareVersion(version, minimum) < 0 {
		return UVProbeResult{}, fmt.Errorf("uv version %s is below minimum uv version %s", version, minimum)
	}
	return UVProbeResult{Version: version, Raw: strings.TrimSpace(result.Stdout)}, nil
}

func (m *Manager) probeUVBinding(ctx context.Context) error {
	envRoot := m.tempDir
	if envRoot == "" {
		envRoot = filepath.Join(filepath.Dir(strings.TrimSpace(m.cfg.PythonExecutable)), ".emoagent-probe")
	}
	if err := os.MkdirAll(envRoot, 0o755); err != nil {
		return fmt.Errorf("create uv python binding probe root: %w", err)
	}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		probeDir, err := os.MkdirTemp(envRoot, "toolchain-probe-*")
		if err != nil {
			return fmt.Errorf("create uv python binding probe dir: %w", err)
		}
		targetEnv := filepath.Join(probeDir, "venv")
		result := m.runner.Run(ctx, Command{
			Path:    strings.TrimSpace(m.cfg.UVExecutable),
			Args:    []string{"venv", targetEnv, "--python", strings.TrimSpace(m.cfg.PythonExecutable)},
			Env:     m.uvCommandEnv(targetEnv),
			Timeout: m.commandTimeout(),
		})
		if result.Err != nil {
			lastErr = fmt.Errorf("uv python binding probe failed: %w", result.Err)
		} else if result.ExitCode != 0 {
			lastErr = fmt.Errorf("uv python binding probe failed with exit code %d: %s", result.ExitCode, sanitizeLogText(result.Stderr))
		} else {
			defer os.RemoveAll(probeDir)
			envPython := filepath.Join(targetEnv, "Scripts", "python.exe")
			if runtime.GOOS != "windows" {
				envPython = filepath.Join(targetEnv, "bin", "python")
			}
			_, err := m.probePython(ctx, envPython)
			return err
		}
		_ = os.RemoveAll(probeDir)
		if attempt < 3 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
			}
		}
	}
	return lastErr
}

func (m *Manager) uvCommandEnv(targetEnv string) []string {
	out := []string{
		"UV_PYTHON=" + strings.TrimSpace(m.cfg.PythonExecutable),
		"UV_PYTHON_DOWNLOADS=never",
		"UV_NO_MANAGED_PYTHON=1",
		"UV_CACHE_DIR=" + strings.TrimSpace(m.cfg.CacheDir),
		"UV_NO_ENV_FILE=1",
		"UV_PROJECT_ENVIRONMENT=" + targetEnv,
		"UV_DEFAULT_INDEX=" + strings.TrimSpace(m.cfg.DefaultIndex),
	}
	if m.cfg.UseSystemCertificates {
		out = append(out, "UV_SYSTEM_CERTS=1")
	}
	return out
}

func (m *Manager) commandTimeout() time.Duration {
	if m != nil && m.cfg.SyncTimeoutSeconds > 0 {
		return time.Duration(m.cfg.SyncTimeoutSeconds) * time.Second
	}
	return 600 * time.Second
}

type osExecRunner struct{}

func (osExecRunner) Run(ctx context.Context, cmd Command) CommandResult {
	runCtx := ctx
	cancel := func() {}
	if cmd.Timeout > 0 {
		var cancelCtx context.CancelFunc
		runCtx, cancelCtx = context.WithTimeout(ctx, cmd.Timeout)
		cancel = cancelCtx
	}
	defer cancel()
	command := exec.CommandContext(runCtx, cmd.Path, cmd.Args...)
	command.Env = commandEnvironment(cmd.Env)
	stdoutPipe, err := command.StdoutPipe()
	if err != nil {
		return CommandResult{ExitCode: 1, Err: err}
	}
	stderrPipe, err := command.StderrPipe()
	if err != nil {
		return CommandResult{ExitCode: 1, Err: err}
	}
	if err := command.Start(); err != nil {
		return CommandResult{ExitCode: 1, Err: err}
	}
	guard := processguard.NewWithLimits(processguard.Limits{MaxProcesses: 64})
	if err := guard.Attach(command.Process.Pid); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		_ = guard.Close()
		return CommandResult{ExitCode: 1, Err: fmt.Errorf("process guard attach failed: %w", err)}
	}
	stdout := newTextLimitBuffer(64 * 1024)
	stderr := newTextLimitBuffer(64 * 1024)
	copyDone := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(stdout, stdoutPipe)
		copyDone <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(stderr, stderrPipe)
		copyDone <- struct{}{}
	}()
	waitDone := make(chan error, 1)
	go func() { waitDone <- command.Wait() }()

	var waitErr error
	select {
	case waitErr = <-waitDone:
	case <-runCtx.Done():
		_ = guard.Terminate(1)
		_ = command.Process.Kill()
		waitErr = <-waitDone
		_ = guard.Close()
		<-copyDone
		<-copyDone
		return CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 1, Err: runCtx.Err()}
	}
	_ = guard.Close()
	<-copyDone
	<-copyDone
	if waitErr != nil {
		exitCode := 1
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			return CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode, Err: nil}
		}
		return CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode, Err: waitErr}
	}
	return CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0}
}

type textLimitBuffer struct {
	limit int
	body  []byte
}

func newTextLimitBuffer(limit int) *textLimitBuffer {
	if limit <= 0 {
		limit = 64 * 1024
	}
	return &textLimitBuffer{limit: limit}
}

func (b *textLimitBuffer) Write(p []byte) (int, error) {
	b.body = append(b.body, p...)
	if len(b.body) > b.limit {
		b.body = append([]byte(nil), b.body[len(b.body)-b.limit:]...)
	}
	return len(p), nil
}

func (b *textLimitBuffer) String() string {
	if b == nil {
		return ""
	}
	return string(b.body)
}

const pythonProbeScript = `import json, platform, sys
print(json.dumps({
    "implementation": platform.python_implementation(),
    "version": platform.python_version(),
    "major": sys.version_info.major,
    "minor": sys.version_info.minor,
    "patch": sys.version_info.micro,
    "architecture": platform.machine(),
    "executable": sys.executable,
    "prefix": sys.prefix,
    "isolated": bool(getattr(sys.flags, "isolated", 0)),
    "safe_path": bool(getattr(sys.flags, "safe_path", False)),
}))`

var uvVersionRE = regexp.MustCompile(`\buv\s+([0-9]+(?:\.[0-9]+){1,2})\b`)

func parseUVVersion(raw string) (string, error) {
	match := uvVersionRE.FindStringSubmatch(strings.TrimSpace(raw))
	if len(match) != 2 {
		return "", fmt.Errorf("uv version output is not recognized: %q", strings.TrimSpace(raw))
	}
	return match[1], nil
}

func compareVersion(a, b string) int {
	aa := parseVersionParts(a)
	bb := parseVersionParts(b)
	for i := 0; i < 3; i++ {
		if aa[i] < bb[i] {
			return -1
		}
		if aa[i] > bb[i] {
			return 1
		}
	}
	return 0
}

func parseVersionParts(value string) [3]int {
	var out [3]int
	parts := strings.Split(value, ".")
	for i := 0; i < len(parts) && i < 3; i++ {
		n, _ := strconv.Atoi(parts[i])
		out[i] = n
	}
	return out
}

func validVersionString(value string) bool {
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) < 2 || len(parts) > 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}
	return true
}

func sameArchitecture(pythonArch, processArch string) bool {
	pythonArch = normalizeArchitecture(pythonArch)
	processArch = normalizeArchitecture(processArch)
	return pythonArch != "" && pythonArch == processArch
}

func normalizeArchitecture(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "amd64", "x86_64", "x64":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	case "386", "x86", "i386":
		return "386"
	default:
		return value
	}
}

func defaultProcessArchitecture() string {
	return normalizeArchitecture(runtime.GOARCH)
}

func pythonFileIdentity(path string) string {
	body, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		sum := sha256.Sum256([]byte("unavailable:" + strings.TrimSpace(path)))
		return "unavailable:sha256:" + hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sanitizeLogText(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 2048 {
		value = value[:2048]
	}
	return value
}

func commandEnvironment(explicit []string) []string {
	if explicit == nil {
		return nil
	}
	out := make([]string, 0, len(explicit)+12)
	seen := map[string]struct{}{}
	for _, item := range explicit {
		name, _, ok := strings.Cut(item, "=")
		if ok {
			seen[strings.ToUpper(name)] = struct{}{}
		}
		out = append(out, item)
	}
	for _, name := range inheritedCommandEnvNames() {
		key := strings.ToUpper(name)
		if _, ok := seen[key]; ok {
			continue
		}
		if value, ok := os.LookupEnv(name); ok {
			out = append(out, name+"="+value)
		}
	}
	return out
}

func inheritedCommandEnvNames() []string {
	return []string{
		"SystemRoot",
		"WINDIR",
		"TEMP",
		"TMP",
		"USERPROFILE",
		"LOCALAPPDATA",
		"APPDATA",
		"PROGRAMDATA",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"NO_PROXY",
		"SSL_CERT_FILE",
		"SSL_CERT_DIR",
	}
}
