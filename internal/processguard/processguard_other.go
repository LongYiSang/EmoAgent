//go:build !windows

package processguard

type noopGuard struct {
	limits Limits
}

func New() Guard {
	return NewWithLimits(Limits{})
}

func NewWithLimits(limits Limits) Guard {
	return noopGuard{limits: limits}
}

func (noopGuard) Attach(int) error {
	return nil
}

func (noopGuard) Terminate(uint32) error {
	return nil
}

func (noopGuard) Close() error {
	return nil
}

func (g noopGuard) Snapshot() Snapshot {
	return Snapshot{Kind: KindNone, MaxProcesses: g.limits.MaxProcesses, MemoryBytes: g.limits.MemoryBytes, CPUQuota: g.limits.CPUQuota}
}
