package execution

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/processguard"
)

type fakeCommandExecutor struct {
	probe       ProbeResult
	probeErr    error
	execute     CommandResult
	executeErr  error
	probeCalls  int
	execCalls   int
	cancelCalls int
	lastReq     CommandRequest
}

func (f *fakeCommandExecutor) Probe(context.Context, ProbeRequest) (ProbeResult, error) {
	f.probeCalls++
	return f.probe, f.probeErr
}

func (f *fakeCommandExecutor) Execute(_ context.Context, req CommandRequest) (CommandResult, error) {
	f.execCalls++
	f.lastReq = req
	return f.execute, f.executeErr
}

func (f *fakeCommandExecutor) Cancel(context.Context, string) error {
	f.cancelCalls++
	return nil
}

func TestHostExecutionManagerManagedHostRunsWithoutPlatformSandboxProbe(t *testing.T) {
	sandbox := &fakeCommandExecutor{probe: ProbeResult{Available: false, Driver: "wsl2", Reason: "missing wsl"}}
	host := &fakeCommandExecutor{probe: ProbeResult{Available: true}, execute: CommandResult{Stdout: "ok", ExitCode: 0, Unsafe: true}}
	manager := NewHostExecutionManagerForTest(ManagerConfig{Mode: ModeManagedHost}, sandbox, host)

	result, err := manager.Execute(context.Background(), CommandRequest{
		InvocationID: "inv-managed",
		Command:      []string{"echo", "ok"},
		WorkspaceDir: "/repo",
		Profile: ManagedProcessProfile{
			WorkspaceMode: "rw",
			TempMode:      "rw",
			NetworkMode:   NetworkDeny,
			EnvAllowlist:  []string{"PATH"},
			Limits:        CommandLimits{TimeoutSeconds: 7, MaxOutputBytes: 9},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Stdout != "ok" ||
		result.ExecutionMode != ModeManagedHost ||
		result.IsolationLevel != "current_user_process" ||
		result.Unsafe ||
		result.Sandboxed {
		t.Fatalf("result = %#v, want managed host current-user process result", result)
	}
	if sandbox.probeCalls != 0 || sandbox.execCalls != 0 {
		t.Fatalf("managed host must not probe sandbox backend: probe=%d exec=%d", sandbox.probeCalls, sandbox.execCalls)
	}
	if host.execCalls != 1 {
		t.Fatalf("host execCalls = %d, want 1", host.execCalls)
	}
}

func TestHostExecutionManagerCancelManagedHostUsesHostExecutor(t *testing.T) {
	sandbox := &fakeCommandExecutor{}
	host := &fakeCommandExecutor{}
	manager := NewHostExecutionManagerForTest(ManagerConfig{Mode: ModeManagedHost}, sandbox, host)

	if err := manager.Cancel(context.Background(), "inv-managed"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if host.cancelCalls != 1 || sandbox.cancelCalls != 0 {
		t.Fatalf("cancel calls host=%d sandbox=%d, want 1/0", host.cancelCalls, sandbox.cancelCalls)
	}
}

func TestHostExecutionManagerCancelSandboxUsesSandboxExecutor(t *testing.T) {
	sandbox := &fakeCommandExecutor{}
	host := &fakeCommandExecutor{}
	manager := NewHostExecutionManagerForTest(ManagerConfig{Mode: ModeSandbox}, sandbox, host)

	if err := manager.Cancel(context.Background(), "inv-sandbox"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if sandbox.cancelCalls != 1 || host.cancelCalls != 0 {
		t.Fatalf("cancel calls sandbox=%d host=%d, want 1/0", sandbox.cancelCalls, host.cancelCalls)
	}
}

func TestHostExecutionManagerCancelLegacyHostRequiresUnsafeFlag(t *testing.T) {
	sandbox := &fakeCommandExecutor{}
	host := &fakeCommandExecutor{}
	manager := NewHostExecutionManagerForTest(ManagerConfig{Mode: ModeLegacyHost}, sandbox, host)

	if err := manager.Cancel(context.Background(), "inv-legacy"); err == nil {
		t.Fatal("Cancel should reject legacy host without unsafe flag")
	}
	if sandbox.cancelCalls != 0 || host.cancelCalls != 0 {
		t.Fatalf("cancel calls sandbox=%d host=%d, want 0/0", sandbox.cancelCalls, host.cancelCalls)
	}
}

func TestHostExecutionManagerCancelLegacyHostUsesHostExecutorWhenExplicitlyUnsafe(t *testing.T) {
	sandbox := &fakeCommandExecutor{}
	host := &fakeCommandExecutor{}
	manager := NewHostExecutionManagerForTest(ManagerConfig{
		Mode:                  ModeLegacyHost,
		UnsafeHostExecEnabled: true,
	}, sandbox, host)

	if err := manager.Cancel(context.Background(), "inv-legacy"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if host.cancelCalls != 1 || sandbox.cancelCalls != 0 {
		t.Fatalf("cancel calls host=%d sandbox=%d, want 1/0", host.cancelCalls, sandbox.cancelCalls)
	}
}

func TestHostExecutionManagerSandboxUnavailableDoesNotFallbackToUnsafeHost(t *testing.T) {
	sandbox := &fakeCommandExecutor{probe: ProbeResult{Available: false, Driver: "wsl2", Reason: "missing wsl"}}
	unsafe := &fakeCommandExecutor{probe: ProbeResult{Available: true}, execute: CommandResult{Stdout: "host"}}
	manager := NewHostExecutionManagerForTest(ManagerConfig{
		Mode:                  ModeSandbox,
		UnsafeHostExecEnabled: true,
		Driver:                "wsl2",
	}, sandbox, unsafe)

	result, err := manager.Execute(context.Background(), CommandRequest{
		InvocationID: "inv-1",
		Command:      []string{"echo", "hello"},
		WorkspaceDir: "/repo",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Unavailable || result.UnavailableReason != "missing wsl" || result.ExecutionMode != ModeSandbox {
		t.Fatalf("result = %#v, want sandbox unavailable", result)
	}
	if sandbox.probeCalls != 1 || sandbox.execCalls != 0 {
		t.Fatalf("sandbox calls probe=%d exec=%d, want 1/0", sandbox.probeCalls, sandbox.execCalls)
	}
	if unsafe.execCalls != 0 {
		t.Fatalf("unsafe host fallback executed %d times, want 0", unsafe.execCalls)
	}
}

func TestHostExecutionManagerPassesRequestToAvailableSandbox(t *testing.T) {
	sandbox := &fakeCommandExecutor{
		probe:   ProbeResult{Available: true, Driver: "bubblewrap"},
		execute: CommandResult{Stdout: "ok", ExitCode: 0},
	}
	manager := NewHostExecutionManagerForTest(ManagerConfig{Mode: ModeSandbox, Driver: "bubblewrap"}, sandbox, nil)
	req := CommandRequest{
		InvocationID: "inv-2",
		Command:      []string{"sh", "-c", "echo ok"},
		WorkspaceDir: "/repo",
		Profile: ManagedProcessProfile{
			WorkspaceMode: "rw",
			TempMode:      "rw",
			PersonalMode:  "ro",
			NetworkMode:   NetworkDeny,
			EnvAllowlist:  []string{"PATH"},
			Limits:        CommandLimits{TimeoutSeconds: 7, MaxProcesses: 8, MaxOutputBytes: 9},
		},
	}

	result, err := manager.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Stdout != "ok" || result.ExecutionMode != ModeSandbox || result.Driver != "bubblewrap" || result.Unsafe {
		t.Fatalf("result = %#v", result)
	}
	if sandbox.execCalls != 1 || sandbox.lastReq.Profile.NetworkMode != NetworkDeny || sandbox.lastReq.Profile.Limits.TimeoutSeconds != 7 {
		t.Fatalf("sandbox lastReq = %#v execCalls=%d", sandbox.lastReq, sandbox.execCalls)
	}
}

func TestHostExecutionManagerLegacyHostRequiresUnsafeFlag(t *testing.T) {
	unsafe := &fakeCommandExecutor{probe: ProbeResult{Available: true}, execute: CommandResult{Stdout: "host"}}
	manager := NewHostExecutionManagerForTest(ManagerConfig{Mode: ModeLegacyHost}, nil, unsafe)

	result, err := manager.Execute(context.Background(), CommandRequest{Command: []string{"echo", "host"}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Unavailable || result.ExecutionMode != ModeLegacyHost {
		t.Fatalf("result = %#v, want unavailable legacy host", result)
	}
	if unsafe.execCalls != 0 {
		t.Fatalf("unsafe execCalls = %d, want 0", unsafe.execCalls)
	}
}

func TestHostExecutionManagerLegacyHostMarksUnsafe(t *testing.T) {
	unsafe := &fakeCommandExecutor{probe: ProbeResult{Available: true}, execute: CommandResult{Stdout: "host", ExitCode: 0}}
	manager := NewHostExecutionManagerForTest(ManagerConfig{
		Mode:                  ModeLegacyHost,
		UnsafeHostExecEnabled: true,
	}, nil, unsafe)

	result, err := manager.Execute(context.Background(), CommandRequest{Command: []string{"echo", "host"}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Stdout != "host" || !result.Unsafe || result.ExecutionMode != ModeUnsafeHostExec {
		t.Fatalf("result = %#v, want unsafe host result", result)
	}
}

func TestHostExecutionManagerPropagatesProbeError(t *testing.T) {
	sandbox := &fakeCommandExecutor{probeErr: errors.New("probe failed")}
	manager := NewHostExecutionManagerForTest(ManagerConfig{Mode: ModeSandbox}, sandbox, nil)

	if _, err := manager.Execute(context.Background(), CommandRequest{Command: []string{"true"}}); err == nil {
		t.Fatal("Execute should return probe error")
	}
}

func TestNewHostExecutionManagerSandboxModeIsUnavailableWithoutBroker(t *testing.T) {
	manager := NewHostExecutionManager(ManagerConfig{Mode: ModeSandbox, GOOS: "linux", Driver: "bubblewrap"})
	result, err := manager.Execute(context.Background(), CommandRequest{
		InvocationID: "inv-sandbox-unavailable",
		Command:      []string{"sh", "-c", "echo should-not-run"},
		WorkspaceDir: "/repo",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Unavailable ||
		result.ExecutionMode != ModeSandbox ||
		!strings.Contains(result.UnavailableReason, "unavailable") {
		t.Fatalf("result = %#v, want explicit sandbox unavailable", result)
	}
}

type fakeProcessGuard struct {
	mu              sync.Mutex
	attachCalls     int
	terminateCalls  int
	closeCalls      int
	attachedPID     int
	killOnTerminate bool
}

func (g *fakeProcessGuard) Attach(pid int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.attachCalls++
	g.attachedPID = pid
	return nil
}

func (g *fakeProcessGuard) Terminate(uint32) error {
	g.mu.Lock()
	g.terminateCalls++
	pid := g.attachedPID
	kill := g.killOnTerminate
	g.mu.Unlock()
	if kill && pid > 0 {
		if process, err := os.FindProcess(pid); err == nil {
			_ = process.Kill()
		}
	}
	return nil
}

func (g *fakeProcessGuard) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closeCalls++
	return nil
}

func (g *fakeProcessGuard) Snapshot() processguard.Snapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	return processguard.Snapshot{Kind: "fake", Attached: g.attachCalls > 0}
}

func (g *fakeProcessGuard) counts() (attach, terminate, close, pid int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.attachCalls, g.terminateCalls, g.closeCalls, g.attachedPID
}

func TestHostProcessExecutorAttachesManagedProcessToGuard(t *testing.T) {
	guard := &fakeProcessGuard{}
	executor := newHostProcessExecutorWithGuardFactory(func() processguard.Guard {
		return guard
	})

	result, err := executor.Execute(context.Background(), CommandRequest{
		InvocationID: "inv-guard-attach",
		Command:      testEchoCommand("guard-ok"),
		WorkspaceDir: t.TempDir(),
		Profile: ManagedProcessProfile{
			EnvAllowlist: defaultCommandEnvAllowlist(),
			Limits:       CommandLimits{MaxOutputBytes: 1024},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Stdout, "guard-ok") {
		t.Fatalf("stdout = %q, want guard-ok", result.Stdout)
	}
	attachCalls, terminateCalls, closeCalls, attachedPID := guard.counts()
	if attachCalls != 1 || attachedPID <= 0 {
		t.Fatalf("guard attach calls=%d pid=%d, want one positive pid", attachCalls, attachedPID)
	}
	if closeCalls != 1 {
		t.Fatalf("guard close calls=%d, want 1", closeCalls)
	}
	if terminateCalls != 0 {
		t.Fatalf("guard terminate calls=%d, want 0 for successful command", terminateCalls)
	}
}

func TestHostProcessExecutorPassesLimitsToProcessGuard(t *testing.T) {
	guard := &fakeProcessGuard{}
	var gotLimits processguard.Limits
	executor := newHostProcessExecutorWithLimitGuardFactory(func(limits processguard.Limits) processguard.Guard {
		gotLimits = limits
		return guard
	})

	_, err := executor.Execute(context.Background(), CommandRequest{
		InvocationID: "inv-guard-limits",
		Command:      testEchoCommand("limits-ok"),
		WorkspaceDir: t.TempDir(),
		Profile: ManagedProcessProfile{
			EnvAllowlist: defaultCommandEnvAllowlist(),
			Limits: CommandLimits{
				MaxProcesses:   5,
				MemoryBytes:    128 << 20,
				CPUQuota:       0.5,
				MaxOutputBytes: 1024,
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotLimits.MaxProcesses != 5 || gotLimits.MemoryBytes != 128<<20 || gotLimits.CPUQuota != 0.5 {
		t.Fatalf("guard limits = %#v", gotLimits)
	}
}

func TestHostProcessExecutorTerminatesGuardOnTimeout(t *testing.T) {
	if os.Getenv("EMO_EXEC_SLEEP_HELPER") == "1" {
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}
	t.Setenv("EMO_EXEC_SLEEP_HELPER", "1")
	guard := &fakeProcessGuard{killOnTerminate: true}
	executor := newHostProcessExecutorWithGuardFactory(func() processguard.Guard {
		return guard
	})

	result, err := executor.Execute(context.Background(), CommandRequest{
		InvocationID: "inv-guard-timeout",
		Command:      []string{os.Args[0], "-test.run=TestHostProcessExecutorTerminatesGuardOnTimeout", "--"},
		WorkspaceDir: t.TempDir(),
		Profile: ManagedProcessProfile{
			EnvAllowlist: append(defaultCommandEnvAllowlist(), "EMO_EXEC_SLEEP_HELPER"),
			Limits:       CommandLimits{TimeoutSeconds: 1, MaxOutputBytes: 1024},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.TimedOut {
		t.Fatalf("result = %#v, want timed_out", result)
	}
	attachCalls, terminateCalls, closeCalls, _ := guard.counts()
	if attachCalls != 1 {
		t.Fatalf("guard attach calls=%d, want 1", attachCalls)
	}
	if terminateCalls == 0 {
		t.Fatalf("guard terminate calls=%d, want timeout to terminate process tree", terminateCalls)
	}
	if closeCalls != 1 {
		t.Fatalf("guard close calls=%d, want 1", closeCalls)
	}
}

func TestHostProcessExecutorTimeoutDoesNotDependOnGuardKillingProcess(t *testing.T) {
	if os.Getenv("EMO_EXEC_SLEEP_HELPER") == "1" {
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}
	t.Setenv("EMO_EXEC_SLEEP_HELPER", "1")
	guard := &fakeProcessGuard{}
	executor := newHostProcessExecutorWithGuardFactory(func() processguard.Guard {
		return guard
	})

	done := make(chan CommandResult, 1)
	errs := make(chan error, 1)
	go func() {
		result, err := executor.Execute(context.Background(), CommandRequest{
			InvocationID: "inv-guard-timeout-fallback",
			Command:      []string{os.Args[0], "-test.run=TestHostProcessExecutorTimeoutDoesNotDependOnGuardKillingProcess", "--"},
			WorkspaceDir: t.TempDir(),
			Profile: ManagedProcessProfile{
				EnvAllowlist: append(defaultCommandEnvAllowlist(), "EMO_EXEC_SLEEP_HELPER"),
				Limits:       CommandLimits{TimeoutSeconds: 1, MaxOutputBytes: 1024},
			},
		})
		if err != nil {
			errs <- err
			return
		}
		done <- result
	}()

	select {
	case err := <-errs:
		t.Fatalf("Execute: %v", err)
	case result := <-done:
		if !result.TimedOut {
			t.Fatalf("result = %#v, want timed_out", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Execute did not return after timeout when guard did not kill process")
	}
	_, terminateCalls, _, _ := guard.counts()
	if terminateCalls == 0 {
		t.Fatalf("guard terminate calls=%d, want timeout to request guard termination", terminateCalls)
	}
}

func TestHostProcessExecutorCancelDoesNotDependOnGuardKillingProcess(t *testing.T) {
	if os.Getenv("EMO_EXEC_SLEEP_HELPER") == "1" {
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
	if testing.Short() {
		t.Skip("skipping cancel test in short mode")
	}
	t.Setenv("EMO_EXEC_SLEEP_HELPER", "1")
	guard := &fakeProcessGuard{}
	executor := newHostProcessExecutorWithGuardFactory(func() processguard.Guard {
		return guard
	})

	done := make(chan CommandResult, 1)
	errs := make(chan error, 1)
	go func() {
		result, err := executor.Execute(context.Background(), CommandRequest{
			InvocationID: "inv-guard-cancel-fallback",
			Command:      []string{os.Args[0], "-test.run=TestHostProcessExecutorCancelDoesNotDependOnGuardKillingProcess", "--"},
			WorkspaceDir: t.TempDir(),
			Profile: ManagedProcessProfile{
				EnvAllowlist: append(defaultCommandEnvAllowlist(), "EMO_EXEC_SLEEP_HELPER"),
				Limits:       CommandLimits{MaxOutputBytes: 1024},
			},
		})
		if err != nil {
			errs <- err
			return
		}
		done <- result
	}()
	deadline := time.After(2 * time.Second)
	for {
		attachCalls, _, _, _ := guard.counts()
		if attachCalls > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("process was not attached to guard")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	if err := executor.Cancel(context.Background(), "inv-guard-cancel-fallback"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	select {
	case err := <-errs:
		t.Fatalf("Execute: %v", err)
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Execute did not return after cancel when guard did not kill process")
	}
	_, terminateCalls, _, _ := guard.counts()
	if terminateCalls == 0 {
		t.Fatalf("guard terminate calls=%d, want cancel to request guard termination", terminateCalls)
	}
}

func TestHostProcessExecutorUsesEnvAllowlistAndSensitiveFilter(t *testing.T) {
	if os.Getenv("EMO_EXEC_ENV_HELPER") == "1" {
		fmt.Printf("allowed=%s secret=%s", os.Getenv("EMO_EXEC_ALLOWED"), os.Getenv("EMO_EXEC_SECRET_TOKEN"))
		os.Exit(0)
	}
	t.Setenv("EMO_EXEC_ENV_HELPER", "1")
	t.Setenv("EMO_EXEC_ALLOWED", "visible")
	t.Setenv("EMO_EXEC_SECRET_TOKEN", "hidden")
	executor := newHostProcessExecutorWithGuardFactory(func() processguard.Guard {
		return &fakeProcessGuard{}
	})

	result, err := executor.Execute(context.Background(), CommandRequest{
		InvocationID: "inv-env-allowlist",
		Command:      []string{os.Args[0], "-test.run=TestHostProcessExecutorUsesEnvAllowlistAndSensitiveFilter", "--"},
		WorkspaceDir: t.TempDir(),
		Profile: ManagedProcessProfile{
			EnvAllowlist: append(defaultCommandEnvAllowlist(), "EMO_EXEC_ENV_HELPER", "EMO_EXEC_ALLOWED", "EMO_EXEC_SECRET_TOKEN"),
			Limits:       CommandLimits{MaxOutputBytes: 1024},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(result.Stdout, "allowed=visible") {
		t.Fatalf("stdout = %q, want allowed env value", result.Stdout)
	}
	if strings.Contains(result.Stdout, "hidden") || !strings.Contains(result.Stdout, "secret=") {
		t.Fatalf("stdout = %q, sensitive env should be filtered even when allowlisted", result.Stdout)
	}
}

func testEchoCommand(text string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", "echo " + text}
	}
	return []string{"sh", "-c", "echo " + text}
}
