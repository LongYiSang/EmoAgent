package transport

import (
	"context"
	"encoding/json"
	"testing"
)

type compileTransport struct{}

func (compileTransport) Start(context.Context, LaunchSpec) error {
	return nil
}

func (compileTransport) Call(context.Context, string, any, any) error {
	return nil
}

func (compileTransport) Health(context.Context) error {
	return nil
}

func (compileTransport) Stop(context.Context) error {
	return nil
}

func (compileTransport) LogTail() string {
	return ""
}

var _ PluginTransport = compileTransport{}

func TestPhase0PluginTransportContractCompile(t *testing.T) {
	spec := LaunchSpec{
		PluginID:      "com.example.echo",
		Version:       "0.3.0",
		RuntimeKind:   RuntimeKindManagedPythonProcess,
		InstanceID:    "inst-1",
		PlanHash:      "sha256:plan",
		StdioProtocol: "emoagent.plugin.stdio_jsonrpc.v0.2",
		Metadata:      json.RawMessage(`{"profile":"python-3.12-minimal"}`),
	}
	if spec.RuntimeKind != RuntimeKindManagedPythonProcess || spec.PlanHash == "" {
		t.Fatalf("transport contract mismatch: %#v", spec)
	}
}
