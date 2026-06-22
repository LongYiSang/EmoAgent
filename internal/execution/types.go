package execution

import (
	"context"
	"encoding/json"
	"time"
)

type NetworkMode string

const (
	NetworkDeny  NetworkMode = "deny"
	NetworkAllow NetworkMode = "allow"
)

type CommandLimits struct {
	TimeoutSeconds int     `json:"timeout_seconds,omitempty"`
	CPUQuota       float64 `json:"cpu_quota,omitempty"`
	MemoryBytes    int64   `json:"memory_bytes,omitempty"`
	MaxProcesses   int     `json:"max_processes,omitempty"`
	MaxOutputBytes int     `json:"max_output_bytes,omitempty"`
}

type ManagedProcessProfile struct {
	WorkspaceMode string        `json:"workspace_mode"`
	TempMode      string        `json:"temp_mode,omitempty"`
	PersonalMode  string        `json:"personal_mode,omitempty"`
	NetworkMode   NetworkMode   `json:"network_mode"`
	EnvAllowlist  []string      `json:"env_allowlist,omitempty"`
	Limits        CommandLimits `json:"limits"`
}

type ProbeRequest struct {
	Driver string `json:"driver,omitempty"`
}

type ProbeResult struct {
	Available bool   `json:"available"`
	Driver    string `json:"driver,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type CommandRequest struct {
	InvocationID string                `json:"invocation_id,omitempty"`
	Command      []string              `json:"command"`
	WorkspaceDir string                `json:"workspace_dir"`
	Profile      ManagedProcessProfile `json:"profile"`
	Metadata     json.RawMessage       `json:"metadata,omitempty"`
}

type CommandResult struct {
	InvocationID      string        `json:"invocation_id,omitempty"`
	Stdout            string        `json:"stdout"`
	StdoutTruncated   bool          `json:"stdout_truncated"`
	Stderr            string        `json:"stderr"`
	StderrTruncated   bool          `json:"stderr_truncated"`
	ExitCode          int           `json:"exit_code"`
	TimedOut          bool          `json:"timed_out"`
	Unavailable       bool          `json:"unavailable,omitempty"`
	UnavailableReason string        `json:"unavailable_reason,omitempty"`
	ExecutionMode     string        `json:"execution_mode,omitempty"`
	Driver            string        `json:"driver,omitempty"`
	IsolationLevel    string        `json:"isolation_level,omitempty"`
	Sandboxed         bool          `json:"sandboxed,omitempty"`
	Unsafe            bool          `json:"unsafe,omitempty"`
	Duration          time.Duration `json:"-"`
}

type CommandExecutor interface {
	Probe(context.Context, ProbeRequest) (ProbeResult, error)
	Execute(context.Context, CommandRequest) (CommandResult, error)
	Cancel(context.Context, string) error
}
