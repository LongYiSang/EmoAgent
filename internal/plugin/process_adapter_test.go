package plugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/tool/resultv2"
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
			Name:        "echo",
			Description: "Echo test tool",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"],"additionalProperties":false}`),
			Scope:       tool.ScopeBoth,
			Permission:  tool.PermReadOnly,
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
	namespaced := "plugin.com.example.echo.echo"
	spec, ok := toolRegistry.GetSpec(namespaced)
	if !ok {
		t.Fatalf("namespaced tool %q not registered", namespaced)
	}
	if spec.Permission != tool.PermReadOnly {
		t.Fatalf("permission = %q, want read-only", spec.Permission)
	}
	if spec.ApprovalClassifier == nil {
		t.Fatal("ApprovalClassifier is nil, want plugin invocation approval gate")
	}
	if len(toolRegistry.ForScope(tool.ScopeEmotion)) != 0 {
		t.Fatalf("emotion tools = %#v, want default plugin exposure hidden from Emotion", toolRegistry.ForScope(tool.ScopeEmotion))
	}
	if len(toolRegistry.ForScope(tool.ScopeWork)) != 1 {
		t.Fatalf("work tools = %#v, want default plugin exposure visible to Work", toolRegistry.ForScope(tool.ScopeWork))
	}

	dispatcher := tool.NewDispatcher(toolRegistry, tool.MinimalSchemaValidator{}, nil)
	call := tool.Call{ID: "call-1", Name: namespaced, Input: json.RawMessage(`{"text":"hello"}`)}
	classification := dispatcher.ClassifyCall(t.Context(), call, tool.PermReadOnly)
	if classification.Action != tool.CallActionToolApprovalRequired {
		t.Fatalf("action = %q, want tool approval", classification.Action)
	}
	if classification.RequiredPermission != tool.PermReadOnly {
		t.Fatalf("required permission = %q, want read-only", classification.RequiredPermission)
	}
	if classification.ApprovalKind != tool.ApprovalKindPluginInvocation {
		t.Fatalf("approval kind = %q, want %q", classification.ApprovalKind, tool.ApprovalKindPluginInvocation)
	}
	result := dispatcher.ExecuteClassified(t.Context(), classification, tool.PermReadOnly)
	if !result.NeedsApproval || supervisor.toolCalls != 0 {
		t.Fatalf("without approval result = %#v toolCalls=%d, want approval and no execution", result, supervisor.toolCalls)
	}
	binding, err := tool.BuildApprovalBinding(call, "approval-1", tool.ApprovalKindPluginInvocation)
	if err != nil {
		t.Fatalf("BuildApprovalBinding: %v", err)
	}
	approvedCtx := tool.WithApproval(t.Context(), tool.ApprovalContext{
		RequestID:           binding.RequestID,
		ApprovalKind:        binding.ApprovalKind,
		AllowToolCall:       true,
		ToolName:            binding.ToolName,
		NormalizedInputHash: binding.NormalizedInputHash,
		PathDigest:          binding.PathDigest,
	})
	result = dispatcher.Execute(approvedCtx, call, tool.PermReadOnly)
	if result.IsError || supervisor.toolCalls != 1 {
		t.Fatalf("approved result = %#v toolCalls=%d, want execution", result, supervisor.toolCalls)
	}
	mutatedCall := tool.Call{ID: "call-2", Name: namespaced, Input: json.RawMessage(`{"text":"changed"}`)}
	result = dispatcher.Execute(approvedCtx, mutatedCall, tool.PermReadOnly)
	if !result.NeedsApproval || supervisor.toolCalls != 1 {
		t.Fatalf("mutated input result = %#v toolCalls=%d, want re-approval without execution", result, supervisor.toolCalls)
	}
}

func TestRegisterProcessPluginRejectsHookWithoutCapability(t *testing.T) {
	manifest := ManifestV2{
		SchemaVersion:   ManifestSchemaV02,
		ID:              "com.example.echo",
		Name:            "Echo",
		Version:         "0.1.0",
		EmoAgentVersion: ">=0.2.0",
		Runtime:         ManifestV2Runtime{Kind: RuntimePythonProcess, Entry: "main.py"},
		Access: ManifestV2Access{
			Tier:         AccessTierRuntimeSafe,
			Capabilities: []Capability{CapabilityTurnRead},
		},
		Hooks: []HookSpec{{Name: HookBeforeToolCall, Mode: HookModeObserve, FailurePolicy: FailurePolicyFailClosed, TimeoutMS: 100}},
	}
	supervisor := &fakeProcessSupervisor{}

	err := RegisterProcessPlugin(t.Context(), manifest, NewPluginRegistry(), tool.NewRegistry(), NewHookBus(HookBusConfig{}, nil), supervisor)
	if err == nil || !strings.Contains(err.Error(), string(CapabilityToolObserve)) {
		t.Fatalf("RegisterProcessPlugin error = %v, want missing tool.observe capability", err)
	}
	if supervisor.ensureReadyCalls != 0 {
		t.Fatalf("EnsureReady calls = %d, want hook rejection before runtime start", supervisor.ensureReadyCalls)
	}
}

func TestRegisterProcessPluginDeniesActiveHooksByDefault(t *testing.T) {
	manifest := ManifestV2{
		SchemaVersion:   ManifestSchemaV02,
		ID:              "com.example.active",
		Name:            "Active",
		Version:         "0.1.0",
		EmoAgentVersion: ">=0.2.0",
		Runtime:         ManifestV2Runtime{Kind: RuntimePythonProcess, Entry: "main.py"},
		Access: ManifestV2Access{
			Tier:         AccessTierRuntimeSafe,
			Capabilities: []Capability{CapabilityToolObserve},
		},
		Hooks: []HookSpec{{Name: HookBeforeToolCall, Mode: HookModeTransform, FailurePolicy: FailurePolicyFailClosed, TimeoutMS: 100}},
	}
	supervisor := &fakeProcessSupervisor{}

	err := RegisterProcessPlugin(t.Context(), manifest, NewPluginRegistry(), tool.NewRegistry(), NewHookBus(HookBusConfig{}, nil), supervisor)
	if err == nil || !strings.Contains(err.Error(), "active hooks are disabled") {
		t.Fatalf("RegisterProcessPlugin error = %v, want active hook policy denial", err)
	}
	if supervisor.ensureReadyCalls != 0 {
		t.Fatalf("EnsureReady calls = %d, want hook rejection before runtime start", supervisor.ensureReadyCalls)
	}
}

func TestRegisterProcessPluginAllowsActiveHooksWhenHostPolicyEnablesThem(t *testing.T) {
	manifest := ManifestV2{
		SchemaVersion:   ManifestSchemaV02,
		ID:              "com.example.active",
		Name:            "Active",
		Version:         "0.1.0",
		EmoAgentVersion: ">=0.2.0",
		Runtime:         ManifestV2Runtime{Kind: RuntimePythonProcess, Entry: "main.py"},
		Access: ManifestV2Access{
			Tier:         AccessTierRuntimeSafe,
			Capabilities: []Capability{CapabilityToolObserve},
		},
		Hooks: []HookSpec{{Name: HookBeforeToolCall, Mode: HookModeTransform, FailurePolicy: FailurePolicyFailClosed, TimeoutMS: 100}},
	}
	supervisor := &fakeProcessSupervisor{}
	bus := NewHookBus(HookBusConfig{}, nil)

	err := RegisterProcessPluginWithPolicy(t.Context(), manifest, NewPluginRegistry(), tool.NewRegistry(), bus, supervisor, ProcessPluginPolicy{AllowActiveHooks: true})
	if err != nil {
		t.Fatalf("RegisterProcessPluginWithPolicy: %v", err)
	}
	if supervisor.ensureReadyCalls != 1 {
		t.Fatalf("EnsureReady calls = %d, want process runtime start after policy allows active hook", supervisor.ensureReadyCalls)
	}
}

type fakeProcessSupervisor struct {
	tools            []ProcessToolSpec
	toolCalls        int
	ensureReadyCalls int
}

func (f *fakeProcessSupervisor) AddPlugin(ManifestV2) {}

func (f *fakeProcessSupervisor) EnsureReady(context.Context, string) (*ProcessRuntime, error) {
	f.ensureReadyCalls++
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
	spec := ProcessToolSpec{Name: "echo"}.ToToolSpec("com.example.echo", "0.1.0", RuntimeManagedPythonProcess)
	if spec.Scope != tool.ScopeWork || spec.Permission != tool.PermReadOnly {
		t.Fatalf("spec defaults = %#v", spec)
	}
	if spec.ApprovalClassifier == nil {
		t.Fatal("ApprovalClassifier is nil, want default plugin invocation ask policy")
	}
	if spec.Source.RuntimeKind != resultv2.RuntimeManagedPython {
		t.Fatalf("runtime kind = %q, want managed_python_process", spec.Source.RuntimeKind)
	}
	if strings.TrimSpace(spec.Name) != "echo" {
		t.Fatalf("spec name = %q", spec.Name)
	}
	if spec.Source.ProducerVersion != "0.1.0" {
		t.Fatalf("producer version = %q, want 0.1.0", spec.Source.ProducerVersion)
	}
}

func TestProcessToolSpecGetsHostDerivedUnverifiedDataOnlyLabels(t *testing.T) {
	spec := ProcessToolSpec{
		Name:       "echo",
		Scope:      tool.ScopeBoth,
		Permission: tool.PermApprovedDestructive,
		Trust: resultv2.ContentLabels{
			Executor:             resultv2.ExecutorHostBuiltin,
			Origin:               resultv2.OriginSystemGenerated,
			Integrity:            resultv2.IntegrityHostVerified,
			InstructionAuthority: resultv2.InstructionHostControl,
		},
	}.ToToolSpec("com.example.echo", "0.1.0", RuntimeManagedPythonProcess)

	if spec.Source.Kind != tool.ToolSourcePlugin {
		t.Fatalf("source kind = %q, want plugin", spec.Source.Kind)
	}
	if spec.Scope != tool.ScopeWork {
		t.Fatalf("scope = %q, want host-derived work exposure", spec.Scope)
	}
	if spec.Permission != tool.PermApprovedDestructive {
		t.Fatalf("permission = %q, want self-reported coarse permission", spec.Permission)
	}
	if spec.ApprovalClassifier == nil {
		t.Fatal("ApprovalClassifier is nil, want host-derived ask policy")
	}
	labels := spec.Source.DefaultLabels
	if labels.Executor != resultv2.ExecutorManagedPythonPlugin {
		t.Fatalf("executor = %q, want managed_python_plugin", labels.Executor)
	}
	if labels.Origin != resultv2.OriginPluginGenerated {
		t.Fatalf("origin = %q, want plugin_generated", labels.Origin)
	}
	if labels.Integrity != resultv2.IntegrityUnverified {
		t.Fatalf("integrity = %q, want unverified", labels.Integrity)
	}
	if labels.InstructionAuthority != resultv2.InstructionDataOnly {
		t.Fatalf("instruction_authority = %q, want data_only", labels.InstructionAuthority)
	}
}

func TestProcessToolSpecSelfReportedLowerPermissionsCannotBypassHostAskPolicy(t *testing.T) {
	for _, permission := range []tool.Permission{tool.PermReadOnly, tool.PermWorkspaceWrite} {
		t.Run(string(permission), func(t *testing.T) {
			spec := ProcessToolSpec{
				Name:       "echo",
				Scope:      tool.ScopeBoth,
				Permission: permission,
			}.ToToolSpec("com.example.echo", "0.1.0", RuntimeManagedPythonProcess)

			if spec.Scope != tool.ScopeWork || spec.Permission != permission {
				t.Fatalf("spec = %#v, want host-derived Work + self-reported coarse permission", spec)
			}
			if spec.ApprovalClassifier == nil {
				t.Fatal("ApprovalClassifier is nil, want host-derived ask policy")
			}
		})
	}
}

func TestProcessToolSpecInvocationPolicyAutoDoesNotAsk(t *testing.T) {
	spec := ProcessToolSpec{
		Name:             "echo",
		InvocationPolicy: InvocationAuto,
	}.ToToolSpec("com.example.echo", "0.1.0", RuntimeManagedPythonProcess)

	if spec.Permission != tool.PermReadOnly {
		t.Fatalf("permission = %q, want read-only", spec.Permission)
	}
	if spec.ApprovalClassifier != nil {
		t.Fatal("ApprovalClassifier should be nil for invocation=auto")
	}
}

func TestRegisterProcessPluginSkipsInvocationPolicyDenyTools(t *testing.T) {
	manifest := ManifestV2{
		SchemaVersion:   ManifestSchemaV02,
		ID:              "com.example.echo",
		Name:            "Echo",
		Version:         "0.1.0",
		EmoAgentVersion: ">=0.2.0",
		Runtime:         ManifestV2Runtime{Kind: RuntimePythonProcess, Entry: "main.py"},
		Access: ManifestV2Access{
			Tier:         AccessTierRuntimeSafe,
			Capabilities: []Capability{CapabilityToolRegister},
		},
	}
	supervisor := &fakeProcessSupervisor{
		tools: []ProcessToolSpec{{
			Name:             "echo",
			InvocationPolicy: InvocationDeny,
		}},
	}
	toolRegistry := tool.NewRegistry()
	if err := RegisterProcessPlugin(t.Context(), manifest, NewPluginRegistry(), toolRegistry, NewHookBus(HookBusConfig{}, nil), supervisor); err != nil {
		t.Fatalf("RegisterProcessPlugin: %v", err)
	}
	if _, ok := toolRegistry.GetSpec("plugin.com.example.echo.echo"); ok {
		t.Fatal("deny invocation tool was registered")
	}
}

func TestProcessToolSpecMarksLegacyPythonRuntimeHonestly(t *testing.T) {
	spec := ProcessToolSpec{Name: "echo"}.ToToolSpec("com.example.echo", "0.1.0", RuntimePythonProcess)
	if spec.Source.RuntimeKind != resultv2.RuntimeProcessDev {
		t.Fatalf("runtime kind = %q, want process_dev for legacy python_process", spec.Source.RuntimeKind)
	}
	if spec.Source.DefaultLabels.Executor != resultv2.ExecutorLegacyProcessPlugin {
		t.Fatalf("executor = %q, want legacy_process_plugin", spec.Source.DefaultLabels.Executor)
	}
}
