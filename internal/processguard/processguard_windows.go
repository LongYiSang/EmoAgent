//go:build windows

package processguard

import (
	"fmt"
	"math"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	jobObjectCPURateControlEnable  = 0x1
	jobObjectCPURateControlHardCap = 0x4
)

type jobObjectCPURateControlInformation struct {
	ControlFlags uint32
	CPURate      uint32
}

type windowsJobGuard struct {
	mu       sync.Mutex
	job      windows.Handle
	attached bool
	lastErr  string
	closed   bool
	limits   Limits
}

func New() Guard {
	return NewWithLimits(Limits{})
}

func NewWithLimits(limits Limits) Guard {
	job, err := windows.CreateJobObject(nil, nil)
	g := &windowsJobGuard{job: job, limits: limits}
	if err != nil {
		g.lastErr = err.Error()
		return g
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if limits.MaxProcesses > 0 {
		info.BasicLimitInformation.LimitFlags |= windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS
		info.BasicLimitInformation.ActiveProcessLimit = uint32(limits.MaxProcesses)
	}
	if limits.MemoryBytes > 0 {
		info.BasicLimitInformation.LimitFlags |= windows.JOB_OBJECT_LIMIT_JOB_MEMORY
		info.JobMemoryLimit = uintptr(limits.MemoryBytes)
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		g.lastErr = err.Error()
		_ = windows.CloseHandle(job)
		g.job = 0
		return g
	}
	if limits.CPUQuota > 0 {
		cpuRate := cpuQuotaToWindowsRate(limits.CPUQuota)
		cpuInfo := jobObjectCPURateControlInformation{
			ControlFlags: jobObjectCPURateControlEnable | jobObjectCPURateControlHardCap,
			CPURate:      cpuRate,
		}
		if _, err := windows.SetInformationJobObject(
			job,
			windows.JobObjectCpuRateControlInformation,
			uintptr(unsafe.Pointer(&cpuInfo)),
			uint32(unsafe.Sizeof(cpuInfo)),
		); err != nil {
			g.lastErr = err.Error()
			_ = windows.CloseHandle(job)
			g.job = 0
			return g
		}
	}
	return g
}

func cpuQuotaToWindowsRate(quota float64) uint32 {
	if quota <= 0 {
		return 0
	}
	percent := quota
	if quota <= 1 {
		percent = quota * 100
	}
	if percent < 1 {
		percent = 1
	}
	if percent > 100 {
		percent = 100
	}
	return uint32(math.Round(percent * 100))
}

func (g *windowsJobGuard) Attach(pid int) error {
	if pid <= 0 {
		return g.setError(fmt.Errorf("invalid process pid %d", pid))
	}
	g.mu.Lock()
	job := g.job
	closed := g.closed
	g.mu.Unlock()
	if job == 0 || closed {
		return g.setError(fmt.Errorf("windows job object unavailable"))
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return g.setError(err)
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		return g.setError(err)
	}
	g.mu.Lock()
	g.attached = true
	g.lastErr = ""
	g.mu.Unlock()
	return nil
}

func (g *windowsJobGuard) Terminate(exitCode uint32) error {
	g.mu.Lock()
	job := g.job
	closed := g.closed
	g.mu.Unlock()
	if job == 0 || closed {
		return nil
	}
	if err := windows.TerminateJobObject(job, exitCode); err != nil {
		return g.setError(err)
	}
	return nil
}

func (g *windowsJobGuard) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.job == 0 || g.closed {
		g.closed = true
		return nil
	}
	err := windows.CloseHandle(g.job)
	g.job = 0
	g.closed = true
	if err != nil {
		g.lastErr = err.Error()
	}
	return err
}

func (g *windowsJobGuard) Snapshot() Snapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	return Snapshot{
		Kind:         KindWindowsJobObject,
		Attached:     g.attached,
		Error:        g.lastErr,
		MaxProcesses: g.limits.MaxProcesses,
		MemoryBytes:  g.limits.MemoryBytes,
		CPUQuota:     g.limits.CPUQuota,
	}
}

func (g *windowsJobGuard) setError(err error) error {
	g.mu.Lock()
	if err != nil {
		g.lastErr = err.Error()
	}
	g.mu.Unlock()
	return err
}
