//go:build windows

package execution

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestHostProcessExecutorTimeoutTerminatesWindowsChildProcessTree(t *testing.T) {
	if os.Getenv("EMO_EXEC_TREE_CHILD") == "1" {
		time.Sleep(60 * time.Second)
		os.Exit(0)
	}
	if os.Getenv("EMO_EXEC_TREE_PARENT") == "1" {
		child := exec.Command(os.Args[0], "-test.run=TestHostProcessExecutorTimeoutTerminatesWindowsChildProcessTree", "--")
		child.Env = append(os.Environ(), "EMO_EXEC_TREE_CHILD=1")
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile(os.Getenv("EMO_EXEC_TREE_PID_FILE"), []byte(strconv.Itoa(child.Process.Pid)), 0o644); err != nil {
			_ = child.Process.Kill()
			os.Exit(3)
		}
		time.Sleep(60 * time.Second)
		os.Exit(0)
	}
	if testing.Short() {
		t.Skip("skipping Windows Job Object process tree test in short mode")
	}

	pidFile := t.TempDir() + `\child.pid`
	t.Setenv("EMO_EXEC_TREE_PARENT", "1")
	t.Setenv("EMO_EXEC_TREE_PID_FILE", pidFile)
	executor := NewHostProcessExecutor()
	result, err := executor.Execute(context.Background(), CommandRequest{
		InvocationID: "inv-windows-tree-timeout",
		Command:      []string{os.Args[0], "-test.run=TestHostProcessExecutorTimeoutTerminatesWindowsChildProcessTree", "--"},
		WorkspaceDir: t.TempDir(),
		Profile: ManagedProcessProfile{
			EnvAllowlist: append(DefaultCommandEnvAllowlist(), "EMO_EXEC_TREE_PARENT", "EMO_EXEC_TREE_PID_FILE"),
			Limits:       CommandLimits{TimeoutSeconds: 1, MaxOutputBytes: 1024},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.TimedOut {
		t.Fatalf("result = %#v, want timed_out", result)
	}

	childPID := waitForExecutionChildPID(t, pidFile)
	t.Cleanup(func() {
		if processRunningForExecutionTest(childPID) {
			terminateProcessForExecutionTest(childPID)
		}
	})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !processRunningForExecutionTest(childPID) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("child process %d is still running after managed host ProcessGuard timeout", childPID)
}

func waitForExecutionChildPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw)))
			if convErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("child pid file %s was not written", path)
	return 0
}

func processRunningForExecutionTest(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false
	}
	return exitCode == 259
}

func terminateProcessForExecutionTest(pid int) {
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(handle)
	_ = windows.TerminateProcess(handle, 1)
}
