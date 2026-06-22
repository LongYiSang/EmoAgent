//go:build windows

package processguard

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestWindowsJobGuardAppliesProcessAndMemoryLimits(t *testing.T) {
	guard := NewWithLimits(Limits{MaxProcesses: 1, MemoryBytes: 256 << 20, CPUQuota: 0.5})
	t.Cleanup(func() { _ = guard.Close() })
	snapshot := guard.Snapshot()
	if snapshot.Kind != KindWindowsJobObject || snapshot.Error != "" {
		t.Fatalf("snapshot = %#v, want available windows job object", snapshot)
	}
	if snapshot.MaxProcesses != 1 || snapshot.MemoryBytes != 256<<20 || snapshot.CPUQuota != 0.5 {
		t.Fatalf("snapshot limits = %#v", snapshot)
	}

	first := startProcessGuardHelper(t)
	if err := guard.Attach(first.Process.Pid); err != nil {
		t.Fatalf("Attach first: %v snapshot=%#v", err, guard.Snapshot())
	}
	second := startProcessGuardHelper(t)
	err := guard.Attach(second.Process.Pid)
	_ = second.Process.Kill()
	_ = second.Wait()
	if err == nil {
		t.Fatal("Attach second process succeeded, want active process limit rejection")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "quota") && !strings.Contains(strings.ToLower(err.Error()), "limit") {
		t.Fatalf("Attach second error = %v, want quota/limit rejection", err)
	}
}

func startProcessGuardHelper(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestProcessGuardHelperProcess")
	cmd.Env = append(os.Environ(), "EMO_PROCESS_GUARD_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

func TestProcessGuardHelperProcess(t *testing.T) {
	if os.Getenv("EMO_PROCESS_GUARD_HELPER") != "1" {
		return
	}
	time.Sleep(30 * time.Second)
	os.Exit(0)
}
