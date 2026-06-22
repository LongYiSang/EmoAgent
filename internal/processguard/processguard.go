package processguard

const (
	KindNone             = "none"
	KindWindowsJobObject = "windows_job_object"
)

type Snapshot struct {
	Kind         string
	Attached     bool
	Error        string
	MaxProcesses int
	MemoryBytes  int64
	CPUQuota     float64
}

type Limits struct {
	MaxProcesses int
	MemoryBytes  int64
	CPUQuota     float64
}

type Guard interface {
	Attach(pid int) error
	Terminate(exitCode uint32) error
	Close() error
	Snapshot() Snapshot
}
