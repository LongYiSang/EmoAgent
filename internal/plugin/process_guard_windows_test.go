//go:build windows

package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"golang.org/x/sys/windows"
)

func TestProcessRuntimeStopTerminatesWindowsChildProcessTree(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Windows Job Object process tree test in short mode")
	}
	python := findPythonForTest(t)
	store, manifest := writeProcessPluginPackage(t, childProcessPythonPluginSource())
	manifest.Runtime.Kind = RuntimeManagedPythonProcess
	supervisor := NewRuntimeSupervisor(store, config.PluginRuntimeConfig{
		ProcessEnabled:          true,
		PrivatePythonExecutable: python,
		StartupTimeoutMS:        3000,
		ShutdownTimeoutMS:       100,
		MaxStderrBytes:          8192,
	}, nil)
	supervisor.AddPlugin(manifest)

	if _, err := supervisor.EnsureReady(t.Context(), manifest.ID); err != nil {
		t.Fatalf("EnsureReady: %v", err)
	}
	childPID := waitForChildPID(t, filepath.Join(store.RootDir, "run", manifest.ID, "child.pid"))
	t.Cleanup(func() {
		if processRunning(childPID) {
			terminateProcess(childPID)
		}
	})

	err := supervisor.Stop(context.Background(), manifest.ID)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop error = %v, want shutdown deadline after ignored shutdown", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !processRunning(childPID) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("child process %d is still running after Job Object cleanup", childPID)
}

func waitForChildPID(t *testing.T, path string) int {
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

func processRunning(pid int) bool {
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

func terminateProcess(pid int) {
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(handle)
	_ = windows.TerminateProcess(handle, 1)
}

func childProcessPythonPluginSource() string {
	return pythonRPCPrelude() + `
child = None

def handle(method, params):
    global child
    if method == "initialize":
        import os, subprocess, sys
        child = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(60)"])
        with open(os.path.join(os.environ["EMO_PLUGIN_RUN_DIR"], "child.pid"), "w", encoding="utf-8") as fh:
            fh.write(str(child.pid))
        return {"tools": []}
    if method == "shutdown":
        time.sleep(60)
        return None
    return None

main(handle)
`
}
