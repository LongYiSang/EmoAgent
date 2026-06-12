package plugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/tool"
)

func TestRegisterProcessPluginHooksAndToolsThroughExistingGates(t *testing.T) {
	manifest := ManifestV2{
		SchemaVersion:   ManifestSchemaV02,
		ID:              "com.example.echo",
		Name:            "Echo",
		Version:         "0.1.0",
		EmoAgentVersion: ">=0.2.0",
		Runtime:         ManifestV2Runtime{Kind: RuntimePythonProcess, Entry: "main.py"},
		Access: ManifestV2Access{
			Tier:         AccessTierRuntimeSafe,
			Capabilities: []Capability{CapabilityTurnRead, CapabilityToolRegister},
		},
		Hooks: []HookSpec{{Name: HookAfterTurnEnd, Mode: HookModeObserve, FailurePolicy: FailurePolicyFailOpen, TimeoutMS: 100}},
	}
	supervisor := &fakeProcessSupervisor{
		tools: []ProcessToolSpec{{
			Name:        "danger",
			Description: "Dangerous test tool",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`),
			Scope:       tool.ScopeBoth,
			Permission:  tool.PermApprovedDestructive,
		}},
	}
	pluginRegistry := NewPluginRegistry()
	toolRegistry := tool.NewRegistry()
	bus := NewHookBus(HookBusConfig{}, nil)

	if err := RegisterProcessPlugin(t.Context(), manifest, pluginRegistry, toolRegistry, bus, supervisor); err != nil {
		t.Fatalf("RegisterProcessPlugin: %v", err)
	}
	hookResult, err := bus.Dispatch(t.Context(), HookAfterTurnEnd, HookContext{})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if hookResult.Annotations["process_hook"] != "called" {
		t.Fatalf("hook annotations = %#v", hookResult.Annotations)
	}
	namespaced := "plugin.com.example.echo.danger"
	if _, ok := toolRegistry.GetSpec(namespaced); !ok {
		t.Fatalf("namespaced tool %q not registered", namespaced)
	}

	dispatcher := tool.NewDispatcher(toolRegistry, tool.MinimalSchemaValidator{}, nil)
	call := tool.Call{ID: "call-1", Name: namespaced, Input: json.RawMessage(`{"path":"target.txt"}`)}
	result := dispatcher.Execute(t.Context(), call, tool.PermApprovedDestructive)
	if !result.NeedsApproval || supervisor.toolCalls != 0 {
		t.Fatalf("without approval result = %#v toolCalls=%d, want approval and no execution", result, supervisor.toolCalls)
	}
	binding, err := tool.BuildApprovalBinding(call, "approval-1", tool.ApprovalKindDestructiveWrite)
	if err != nil {
		t.Fatalf("BuildApprovalBinding: %v", err)
	}
	approvedCtx := tool.WithApproval(t.Context(), tool.ApprovalContext{
		RequestID:           binding.RequestID,
		ApprovalKind:        binding.ApprovalKind,
		AllowDestructive:    true,
		ToolName:            binding.ToolName,
		NormalizedInputHash: binding.NormalizedInputHash,
		PathDigest:          binding.PathDigest,
	})
	result = dispatcher.Execute(approvedCtx, call, tool.PermApprovedDestructive)
	if result.IsError || supervisor.toolCalls != 1 {
		t.Fatalf("approved result = %#v toolCalls=%d, want execution", result, supervisor.toolCalls)
	}
}

type fakeProcessSupervisor struct {
	tools     []ProcessToolSpec
	toolCalls int
}

func (f *fakeProcessSupervisor) AddPlugin(ManifestV2) {}

func (f *fakeProcessSupervisor) EnsureReady(context.Context, string) (*ProcessRuntime, error) {
	return nil, nil
}

func (f *fakeProcessSupervisor) InvokeHook(context.Context, string, HookName, HookContext) (HookResult, error) {
	return HookResult{Annotations: map[string]any{"process_hook": "called"}}, nil
}

func (f *fakeProcessSupervisor) InvokeTool(_ context.Context, _ string, _ string, input json.RawMessage) (json.RawMessage, error) {
	f.toolCalls++
	return json.RawMessage(`{"ok":true,"input":` + string(input) + `}`), nil
}

func (f *fakeProcessSupervisor) Tools(string) []ProcessToolSpec {
	return append([]ProcessToolSpec(nil), f.tools...)
}

func TestProcessToolSpecDefaults(t *testing.T) {
	spec := ProcessToolSpec{Name: "echo"}.ToToolSpec("com.example.echo")
	if spec.Scope != tool.ScopeBoth || spec.Permission != tool.PermReadOnly {
		t.Fatalf("spec defaults = %#v", spec)
	}
	if strings.TrimSpace(spec.Name) != "echo" {
		t.Fatalf("spec name = %q", spec.Name)
	}
}
