package execution

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/longyisang/emoagent/internal/processguard"
)

const (
	ModeManagedHost    = "managed_host"
	ModeSandbox        = "sandbox"
	ModeLegacyHost     = "legacy_host"
	ModeUnsafeHostExec = "unsafe_host_exec"
)

type ManagerConfig struct {
	Mode                  string
	UnsafeHostExecEnabled bool
	GOOS                  string
	Driver                string
}

type HostExecutionManager struct {
	cfg     ManagerConfig
	sandbox CommandExecutor
	host    CommandExecutor
}

func NewHostExecutionManager(cfg ManagerConfig) *HostExecutionManager {
	if cfg.GOOS == "" {
		cfg.GOOS = runtime.GOOS
	}
	return &HostExecutionManager{
		cfg:     cfg,
		sandbox: UnavailableExecutor{Driver: cfg.Driver, Reason: "sandbox runtime unavailable in managed local runtime"},
		host:    NewHostProcessExecutor(),
	}
}

func NewHostExecutionManagerForTest(cfg ManagerConfig, sandbox CommandExecutor, host CommandExecutor) *HostExecutionManager {
	return &HostExecutionManager{cfg: cfg, sandbox: sandbox, host: host}
}

func (m *HostExecutionManager) Execute(ctx context.Context, req CommandRequest) (CommandResult, error) {
	if m == nil {
		return unavailableResult(req, ModeSandbox, "", "execution manager unavailable"), nil
	}
	mode := strings.TrimSpace(m.cfg.Mode)
	if mode == "" {
		mode = ModeManagedHost
	}
	switch mode {
	case ModeManagedHost:
		return m.executeManagedHost(ctx, req)
	case ModeSandbox:
		return m.executeSandbox(ctx, req)
	case ModeLegacyHost:
		if !m.cfg.UnsafeHostExecEnabled {
			return unavailableResult(req, ModeLegacyHost, "", "legacy host execution requires unsafe_host_exec_enabled=true"), nil
		}
		res, err := m.host.Execute(ctx, req)
		res.ExecutionMode = ModeUnsafeHostExec
		res.Unsafe = true
		return res, err
	default:
		return unavailableResult(req, mode, "", fmt.Sprintf("unsupported execution mode %q", mode)), nil
	}
}

func (m *HostExecutionManager) executeManagedHost(ctx context.Context, req CommandRequest) (CommandResult, error) {
	if m.host == nil {
		return unavailableResult(req, ModeManagedHost, "", "managed host executor unavailable"), nil
	}
	res, err := m.host.Execute(ctx, req)
	res.ExecutionMode = ModeManagedHost
	res.Driver = ""
	res.IsolationLevel = "current_user_process"
	res.Sandboxed = false
	res.Unsafe = false
	return res, err
}

func (m *HostExecutionManager) Cancel(ctx context.Context, invocationID string) error {
	if m == nil {
		return fmt.Errorf("execution manager unavailable")
	}
	mode := strings.TrimSpace(m.cfg.Mode)
	if mode == "" {
		mode = ModeManagedHost
	}
	switch mode {
	case ModeManagedHost:
		if m.host == nil {
			return fmt.Errorf("managed host executor unavailable")
		}
		return m.host.Cancel(ctx, invocationID)
	case ModeSandbox:
		if m.sandbox == nil {
			return fmt.Errorf("sandbox driver unavailable")
		}
		return m.sandbox.Cancel(ctx, invocationID)
	case ModeLegacyHost:
		if !m.cfg.UnsafeHostExecEnabled {
			return fmt.Errorf("legacy host execution requires unsafe_host_exec_enabled=true")
		}
		if m.host == nil {
			return fmt.Errorf("legacy host executor unavailable")
		}
		return m.host.Cancel(ctx, invocationID)
	default:
		return fmt.Errorf("unsupported execution mode %q", mode)
	}
}

func (m *HostExecutionManager) executeSandbox(ctx context.Context, req CommandRequest) (CommandResult, error) {
	if m.sandbox == nil {
		return unavailableResult(req, ModeSandbox, "", "sandbox driver unavailable"), nil
	}
	probe, err := m.sandbox.Probe(ctx, ProbeRequest{Driver: m.cfg.Driver})
	if err != nil {
		return CommandResult{}, err
	}
	if !probe.Available {
		reason := probe.Reason
		if reason == "" {
			reason = "sandbox driver unavailable"
		}
		return unavailableResult(req, ModeSandbox, probe.Driver, reason), nil
	}
	res, err := m.sandbox.Execute(ctx, req)
	res.ExecutionMode = ModeSandbox
	if res.Driver == "" {
		res.Driver = probe.Driver
	}
	return res, err
}

func unavailableResult(req CommandRequest, mode, driver, reason string) CommandResult {
	return CommandResult{
		InvocationID:      req.InvocationID,
		ExitCode:          -1,
		Unavailable:       true,
		UnavailableReason: reason,
		ExecutionMode:     mode,
		Driver:            driver,
		IsolationLevel:    "unavailable",
	}
}

type UnavailableExecutor struct {
	Driver string
	Reason string
}

func (s UnavailableExecutor) Probe(context.Context, ProbeRequest) (ProbeResult, error) {
	reason := s.Reason
	if reason == "" {
		reason = "sandbox driver unavailable"
	}
	return ProbeResult{Available: false, Driver: s.Driver, Reason: reason}, nil
}

func (s UnavailableExecutor) Execute(context.Context, CommandRequest) (CommandResult, error) {
	return CommandResult{Unavailable: true, UnavailableReason: s.Reason, Driver: s.Driver, ExecutionMode: ModeSandbox}, nil
}

func (s UnavailableExecutor) Cancel(context.Context, string) error {
	return nil
}

type HostProcessExecutor struct {
	mu                 sync.Mutex
	running            map[string]runningCommand
	newGuard           func() processguard.Guard
	newGuardWithLimits func(processguard.Limits) processguard.Guard
}

type runningCommand struct {
	guard   processguard.Guard
	process *os.Process
}

func NewHostProcessExecutor() *HostProcessExecutor {
	return newHostProcessExecutorWithLimitGuardFactory(processguard.NewWithLimits)
}

func newHostProcessExecutorWithGuardFactory(factory func() processguard.Guard) *HostProcessExecutor {
	return &HostProcessExecutor{newGuard: factory}
}

func newHostProcessExecutorWithLimitGuardFactory(factory func(processguard.Limits) processguard.Guard) *HostProcessExecutor {
	return &HostProcessExecutor{newGuardWithLimits: factory}
}

func (*HostProcessExecutor) Probe(context.Context, ProbeRequest) (ProbeResult, error) {
	return ProbeResult{Available: true, Driver: ModeUnsafeHostExec}, nil
}

func (h *HostProcessExecutor) Execute(ctx context.Context, req CommandRequest) (CommandResult, error) {
	if h == nil {
		return CommandResult{}, fmt.Errorf("managed host executor unavailable")
	}
	return h.runHostCommand(ctx, req, ModeUnsafeHostExec, true)
}

func (h *HostProcessExecutor) Cancel(_ context.Context, invocationID string) error {
	if h == nil {
		return fmt.Errorf("managed host executor unavailable")
	}
	running := h.runningForInvocation(invocationID)
	if running.guard == nil && running.process == nil {
		return nil
	}
	var terminateErr error
	if running.guard != nil {
		terminateErr = running.guard.Terminate(1)
	}
	if running.process != nil {
		_ = running.process.Kill()
	}
	return terminateErr
}

func (h *HostProcessExecutor) runHostCommand(ctx context.Context, req CommandRequest, mode string, unsafe bool) (CommandResult, error) {
	if len(req.Command) == 0 || strings.TrimSpace(req.Command[0]) == "" {
		return CommandResult{}, fmt.Errorf("command is required")
	}
	if err := validateCommandLimits(req.Profile.Limits); err != nil {
		return CommandResult{}, err
	}
	timeout := req.Profile.Limits.TimeoutSeconds
	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
	}
	started := time.Now()
	cmd := exec.Command(req.Command[0], req.Command[1:]...)
	cmd.Dir = req.WorkspaceDir
	cmd.Env = filteredCommandEnv(os.Environ(), req.Profile.EnvAllowlist)

	cap := req.Profile.Limits.MaxOutputBytes
	var stdoutBuf, stderrBuf limitedBuffer
	stdoutBuf.cap = cap
	stderrBuf.cap = cap
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return CommandResult{}, fmt.Errorf("start failed: %w", err)
	}

	guard := h.createGuard(req.Profile.Limits)
	if guard != nil {
		if err := guard.Attach(cmd.Process.Pid); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			_ = guard.Close()
			return CommandResult{}, fmt.Errorf("process guard attach failed: %w", err)
		}
		defer guard.Close()
	}
	h.trackRunning(req.InvocationID, guard, cmd.Process)
	defer h.untrackRunning(req.InvocationID)

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	var runErr error
	timedOut := false
	select {
	case runErr = <-done:
	case <-runCtx.Done():
		timedOut = runCtx.Err() == context.DeadlineExceeded
		if guard != nil {
			_ = guard.Terminate(1)
		}
		_ = cmd.Process.Kill()
		runErr = <-done
	}
	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	} else if runErr != nil && !timedOut {
		return CommandResult{}, fmt.Errorf("start failed: %w", runErr)
	}
	return CommandResult{
		InvocationID:    req.InvocationID,
		Stdout:          stdoutBuf.String(),
		StdoutTruncated: stdoutBuf.truncated,
		Stderr:          stderrBuf.String(),
		StderrTruncated: stderrBuf.truncated,
		ExitCode:        exitCode,
		TimedOut:        timedOut,
		ExecutionMode:   mode,
		Unsafe:          unsafe,
		Duration:        time.Since(started),
	}, nil
}

func validateCommandLimits(limits CommandLimits) error {
	if limits.TimeoutSeconds < 0 {
		return fmt.Errorf("timeout_seconds must be >= 0")
	}
	if limits.CPUQuota < 0 {
		return fmt.Errorf("cpu_quota must be >= 0")
	}
	if limits.MemoryBytes < 0 {
		return fmt.Errorf("memory_bytes must be >= 0")
	}
	if limits.MaxProcesses < 0 {
		return fmt.Errorf("max_processes must be >= 0")
	}
	if limits.MaxOutputBytes < 0 {
		return fmt.Errorf("max_output_bytes must be >= 0")
	}
	return nil
}

func (h *HostProcessExecutor) createGuard(limits CommandLimits) processguard.Guard {
	guardLimits := processguard.Limits{
		MaxProcesses: limits.MaxProcesses,
		MemoryBytes:  limits.MemoryBytes,
		CPUQuota:     limits.CPUQuota,
	}
	if h.newGuardWithLimits != nil {
		return h.newGuardWithLimits(guardLimits)
	}
	if h.newGuard != nil {
		return h.newGuard()
	}
	return processguard.NewWithLimits(guardLimits)
}

func (h *HostProcessExecutor) trackRunning(invocationID string, guard processguard.Guard, process *os.Process) {
	if strings.TrimSpace(invocationID) == "" || (guard == nil && process == nil) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running == nil {
		h.running = map[string]runningCommand{}
	}
	h.running[invocationID] = runningCommand{guard: guard, process: process}
}

func (h *HostProcessExecutor) untrackRunning(invocationID string) {
	if strings.TrimSpace(invocationID) == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.running, invocationID)
}

func (h *HostProcessExecutor) runningForInvocation(invocationID string) runningCommand {
	if strings.TrimSpace(invocationID) == "" {
		return runningCommand{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.running[invocationID]
}

func DefaultCommandEnvAllowlist() []string {
	return append([]string(nil), defaultCommandEnvAllowlist()...)
}

func defaultCommandEnvAllowlist() []string {
	return []string{
		"PATH",
		"HOME",
		"TMPDIR",
		"TMP",
		"TEMP",
		"USERPROFILE",
		"HOMEDRIVE",
		"HOMEPATH",
		"SystemRoot",
		"WINDIR",
		"COMSPEC",
		"PATHEXT",
	}
}

func filteredCommandEnv(base []string, allowlist []string) []string {
	if len(allowlist) == 0 {
		allowlist = defaultCommandEnvAllowlist()
	}
	allowed := map[string]struct{}{}
	for _, name := range allowlist {
		name = strings.TrimSpace(name)
		if name != "" {
			allowed[envLookupKey(name)] = struct{}{}
		}
	}
	out := make([]string, 0, len(allowed))
	for _, item := range base {
		name, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if _, ok := allowed[envLookupKey(name)]; !ok {
			continue
		}
		if sensitiveCommandEnvName(name) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func envLookupKey(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(name)
	}
	return name
}

func sensitiveCommandEnvName(name string) bool {
	upper := strings.ToUpper(name)
	return strings.Contains(upper, "API_KEY") ||
		strings.Contains(upper, "SECRET") ||
		strings.Contains(upper, "TOKEN") ||
		strings.Contains(upper, "PASSWORD")
}

type limitedBuffer struct {
	buf       bytes.Buffer
	cap       int
	written   int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	origLen := len(p)
	if b.cap <= 0 {
		return origLen, nil
	}
	remaining := b.cap - b.written
	if remaining <= 0 {
		b.truncated = true
		return origLen, nil
	}
	if len(p) > remaining {
		b.truncated = true
		p = p[:remaining]
	}
	n, err := b.buf.Write(p)
	b.written += n
	return origLen, err
}

func (b *limitedBuffer) String() string {
	return b.buf.String()
}

var _ io.Writer = (*limitedBuffer)(nil)
