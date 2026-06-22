package transport

import (
	"context"
	"encoding/json"
)

type RuntimeKind string

const (
	RuntimeKindManagedPythonProcess RuntimeKind = "managed_python_process"
	RuntimeKindProcessDev           RuntimeKind = "process_dev"
	RuntimeKindContainer            RuntimeKind = "container"
	RuntimeKindFake                 RuntimeKind = "fake"
)

type LaunchSpec struct {
	PluginID      string          `json:"plugin_id"`
	Version       string          `json:"version"`
	RuntimeKind   RuntimeKind     `json:"runtime_kind"`
	InstanceID    string          `json:"instance_id,omitempty"`
	PlanHash      string          `json:"plan_hash,omitempty"`
	StdioProtocol string          `json:"stdio_protocol"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
}

type PluginTransport interface {
	Start(context.Context, LaunchSpec) error
	Call(context.Context, string, any, any) error
	Health(context.Context) error
	Stop(context.Context) error
	LogTail() string
}
