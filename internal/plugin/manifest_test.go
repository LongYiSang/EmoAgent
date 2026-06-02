package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/turn"
)

func validManifest() Manifest {
	return Manifest{
		ID:              "com.example.audit",
		Name:            "Audit",
		Version:         "0.1.0",
		Runtime:         RuntimeBuiltin,
		EmoAgentVersion: "^0.1.0",
		Capabilities: []Capability{
			CapabilityTurnRead,
			CapabilityTurnAnnotate,
		},
		Hooks: []HookSpec{
			{
				Name:          HookAfterTurnEnd,
				Mode:          HookModeObserve,
				FailurePolicy: FailurePolicyFailOpen,
				Priority:      10,
				TimeoutMS:     50,
			},
		},
	}
}

func TestManifestValidateStrictContract(t *testing.T) {
	if err := validManifest().Validate(ManifestValidationOptions{MaxTimeoutMS: 1000}); err != nil {
		t.Fatalf("Validate valid manifest: %v", err)
	}

	tests := []struct {
		name string
		mut  func(*Manifest)
		want string
	}{
		{
			name: "invalid semver range",
			mut: func(m *Manifest) {
				m.EmoAgentVersion = "soon"
			},
			want: "emoagent_version",
		},
		{
			name: "unknown capability",
			mut: func(m *Manifest) {
				m.Capabilities = append(m.Capabilities, Capability("memory.raw_prompt"))
			},
			want: "unknown capability",
		},
		{
			name: "unknown hook",
			mut: func(m *Manifest) {
				m.Hooks[0].Name = HookName("before_session_magic")
			},
			want: "unknown hook",
		},
		{
			name: "timeout exceeds host max",
			mut: func(m *Manifest) {
				m.Hooks[0].TimeoutMS = 1001
			},
			want: "timeout_ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validManifest()
			tt.mut(&m)
			err := m.Validate(ManifestValidationOptions{MaxTimeoutMS: 1000})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestDecodeManifestYAMLRejectsUnknownFields(t *testing.T) {
	_, err := DecodeManifestYAML([]byte(`
id: com.example.audit
name: Audit
version: 0.1.0
runtime: builtin
emoagent_version: ^0.1.0
capabilities:
  - turn.read
hooks:
  - name: after_turn_end
    mode: observe
    failure_policy: fail_open
    priority: 10
    timeout_ms: 50
unexpected: true
`), ManifestValidationOptions{MaxTimeoutMS: 1000})
	if err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("DecodeManifestYAML error = %v, want unknown field error", err)
	}
}

func TestPluginRegistryRegistersAndRejectsDuplicate(t *testing.T) {
	registry := NewPluginRegistry()
	manifest := validManifest()
	if err := registry.Register(manifest, ManifestValidationOptions{MaxTimeoutMS: 1000}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := registry.Get(manifest.ID)
	if !ok || got.ID != manifest.ID {
		t.Fatalf("Get = %#v/%v, want manifest", got, ok)
	}
	if err := registry.Register(manifest, ManifestValidationOptions{MaxTimeoutMS: 1000}); err == nil {
		t.Fatal("expected duplicate Register error")
	}
	if list := registry.List(); len(list) != 1 || list[0].ID != manifest.ID {
		t.Fatalf("List = %#v, want one manifest", list)
	}
}

func TestHookRegistrarRequiresDeclaredHookAndCapability(t *testing.T) {
	bus := NewHookBus(HookBusConfig{DefaultTimeout: 50 * time.Millisecond, MaxTimeout: time.Second}, nil)
	manifest := validManifest()
	manifest.Hooks = []HookSpec{{
		Name:          HookBeforeToolCall,
		Mode:          HookModeTransform,
		FailurePolicy: FailurePolicyFailClosed,
		Priority:      10,
		TimeoutMS:     50,
	}}
	registrar := NewRegistrarForManifest(manifest, nil, bus)
	err := registrar.Hooks.Register(manifest.Hooks[0], func(context.Context, HookContext) (HookResult, error) {
		return HookResult{}, nil
	})
	if err == nil || !errors.Is(err, ErrCapabilityDenied) {
		t.Fatalf("Register missing capability error = %v, want ErrCapabilityDenied", err)
	}

	manifest.Capabilities = append(manifest.Capabilities, CapabilityToolObserve)
	registrar = NewRegistrarForManifest(manifest, nil, bus)
	if err := registrar.Hooks.Register(manifest.Hooks[0], func(context.Context, HookContext) (HookResult, error) {
		return HookResult{}, nil
	}); err != nil {
		t.Fatalf("Register declared hook with capability: %v", err)
	}
	err = registrar.Hooks.Register(HookSpec{Name: HookAfterToolCall, Mode: HookModeObserve, FailurePolicy: FailurePolicyFailOpen}, func(context.Context, HookContext) (HookResult, error) {
		return HookResult{}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "not declared") {
		t.Fatalf("Register undeclared hook error = %v, want not declared", err)
	}
}

func TestAuthorizerDeniesMissingCapability(t *testing.T) {
	auth := NewAuthorizer(validManifest())
	if err := auth.Require(CapabilityTurnRead); err != nil {
		t.Fatalf("Require existing capability: %v", err)
	}
	err := auth.Require(CapabilityMemoryReadSafe)
	if err == nil || !errors.Is(err, ErrCapabilityDenied) {
		t.Fatalf("Require missing capability error = %v, want ErrCapabilityDenied", err)
	}
}

func TestHookBusDispatchesPriorityAndAudits(t *testing.T) {
	journal := turn.NewMemoryJournal()
	if err := journal.StartTurn(context.Background(), turn.TurnRecord{TurnID: "turn-1", Kind: turn.InboundUserMessage}); err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	bus := NewHookBus(HookBusConfig{DefaultTimeout: 50 * time.Millisecond, MaxTimeout: time.Second}, NewTurnJournalAudit(journal))
	var order []string
	for _, registered := range []RegisteredHook{
		{
			PluginID:      "com.example.later",
			Hook:          HookBeforeIngressNormalize,
			Mode:          HookModeTransform,
			FailurePolicy: FailurePolicyFailOpen,
			Priority:      20,
			Handler: func(context.Context, HookContext) (HookResult, error) {
				order = append(order, "later")
				return HookResult{Patches: []Patch{{Type: PatchTurnAnnotate, Operation: PatchOpAppend, Path: "annotations", Value: "later", ReasonCode: "test"}}}, nil
			},
		},
		{
			PluginID:      "com.example.first",
			Hook:          HookBeforeIngressNormalize,
			Mode:          HookModeTransform,
			FailurePolicy: FailurePolicyFailOpen,
			Priority:      5,
			Handler: func(context.Context, HookContext) (HookResult, error) {
				order = append(order, "first")
				return HookResult{Patches: []Patch{{Type: PatchTurnAnnotate, Operation: PatchOpAppend, Path: "annotations", Value: "first", ReasonCode: "test"}}}, nil
			},
		},
	} {
		if err := bus.Register(registered); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}

	result, err := bus.Dispatch(context.Background(), HookBeforeIngressNormalize, HookContext{
		Envelope: HookEnvelope{TurnID: "turn-1", Hook: HookBeforeIngressNormalize, Stage: turn.StageNormalize},
		Turn:     TurnView{TurnID: "turn-1", SessionID: "session-1", PersonaKey: "default"},
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if strings.Join(order, ",") != "first,later" {
		t.Fatalf("order = %#v, want first,later", order)
	}
	if len(result.Patches) != 2 {
		t.Fatalf("patches = %#v, want 2 accepted append patches", result.Patches)
	}
	snapshot, ok := journal.GetTurn("turn-1")
	if !ok {
		t.Fatal("journal missing turn")
	}
	var audited bool
	for _, event := range snapshot.Events {
		if event.Type == "plugin_invocation" && event.Payload["plugin_id"] == "com.example.first" && event.Payload["status"] == "done" {
			if event.Payload["input_hash"] == "" {
				t.Fatalf("plugin_invocation input_hash is empty: %#v", event.Payload)
			}
			audited = true
		}
	}
	if !audited {
		t.Fatalf("journal events = %#v, want plugin_invocation done", snapshot.Events)
	}
}

func TestHookBusTimeoutFailOpenAndFailClosed(t *testing.T) {
	for _, tt := range []struct {
		name          string
		failurePolicy FailurePolicy
		wantErr       bool
	}{
		{name: "fail open", failurePolicy: FailurePolicyFailOpen},
		{name: "fail closed", failurePolicy: FailurePolicyFailClosed, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			bus := NewHookBus(HookBusConfig{DefaultTimeout: 10 * time.Millisecond, MaxTimeout: 100 * time.Millisecond}, nil)
			err := bus.Register(RegisteredHook{
				PluginID:      "com.example.slow",
				Hook:          HookBeforeToolCall,
				Mode:          HookModeTransform,
				FailurePolicy: tt.failurePolicy,
				Timeout:       5 * time.Millisecond,
				Handler: func(ctx context.Context, hc HookContext) (HookResult, error) {
					<-ctx.Done()
					return HookResult{}, ctx.Err()
				},
			})
			if err != nil {
				t.Fatalf("Register: %v", err)
			}
			_, err = bus.Dispatch(context.Background(), HookBeforeToolCall, HookContext{
				Envelope: HookEnvelope{TurnID: "turn-timeout", Hook: HookBeforeToolCall},
			})
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), ErrorKindPluginHookTimeout) {
					t.Fatalf("Dispatch error = %v, want timeout", err)
				}
			} else if err != nil {
				t.Fatalf("Dispatch error = %v, want nil fail-open", err)
			}
		})
	}
}

func TestHookBusRejectsLowerPriorityReplaceConflict(t *testing.T) {
	bus := NewHookBus(HookBusConfig{DefaultTimeout: 50 * time.Millisecond, MaxTimeout: time.Second}, nil)
	for _, registered := range []RegisteredHook{
		{
			PluginID:      "com.example.high",
			Hook:          HookBeforeOutbound,
			Mode:          HookModeTransform,
			FailurePolicy: FailurePolicyFailOpen,
			Priority:      1,
			Handler: func(context.Context, HookContext) (HookResult, error) {
				return HookResult{Patches: []Patch{{Type: PatchOutboundAddPayload, Operation: PatchOpReplace, Path: "payload.plugins.com_example.value", Value: "high", ReasonCode: "test"}}}, nil
			},
		},
		{
			PluginID:      "com.example.low",
			Hook:          HookBeforeOutbound,
			Mode:          HookModeTransform,
			FailurePolicy: FailurePolicyFailOpen,
			Priority:      9,
			Handler: func(context.Context, HookContext) (HookResult, error) {
				return HookResult{Patches: []Patch{{Type: PatchOutboundAddPayload, Operation: PatchOpReplace, Path: "payload.plugins.com_example.value", Value: "low", ReasonCode: "test"}}}, nil
			},
		},
	} {
		if err := bus.Register(registered); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}

	result, err := bus.Dispatch(context.Background(), HookBeforeOutbound, HookContext{Envelope: HookEnvelope{TurnID: "turn-patch", Hook: HookBeforeOutbound}})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(result.Patches) != 1 || result.Patches[0].Value != "high" {
		t.Fatalf("patches = %#v, want only high priority replace", result.Patches)
	}
	if len(result.RejectedPatches) != 1 || result.RejectedPatches[0].ErrorKind != ErrorKindPluginPatchConflict {
		t.Fatalf("rejected = %#v, want patch conflict", result.RejectedPatches)
	}
}

func TestToolFacadeRequiresCapabilityAndNamespacesTools(t *testing.T) {
	registry := tool.NewRegistry()
	manifest := validManifest()
	manifest.Capabilities = append(manifest.Capabilities, CapabilityToolRegister)
	registrar := NewRegistrar("com.example.audit", NewAuthorizer(manifest), registry, nil)

	err := registrar.Tools.Register(context.Background(), tool.Spec{
		Name:       "echo",
		Scope:      tool.ScopeWork,
		Permission: tool.PermReadOnly,
	}, func(context.Context, json.RawMessage) (json.RawMessage, error) { return nil, nil })
	if err != nil {
		t.Fatalf("Register plugin tool: %v", err)
	}
	if _, ok := registry.GetSpec("plugin.com.example.audit.echo"); !ok {
		t.Fatal("namespaced plugin tool not registered")
	}

	noCapability := validManifest()
	registrar = NewRegistrar("com.example.audit2", NewAuthorizer(noCapability), registry, nil)
	err = registrar.Tools.Register(context.Background(), tool.Spec{Name: "echo", Scope: tool.ScopeWork, Permission: tool.PermReadOnly}, nil)
	if err == nil || !errors.Is(err, ErrCapabilityDenied) {
		t.Fatalf("Register without capability error = %v, want ErrCapabilityDenied", err)
	}
}

func TestPluginToolStillUsesDispatcherApprovalGate(t *testing.T) {
	registry := tool.NewRegistry()
	manifest := validManifest()
	manifest.Capabilities = append(manifest.Capabilities, CapabilityToolRegister)
	registrar := NewRegistrar("com.example.audit", NewAuthorizer(manifest), registry, nil)
	var executed int

	err := registrar.Tools.Register(context.Background(), tool.Spec{
		Name:       "delete_note",
		Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`),
		Scope:      tool.ScopeWork,
		Permission: tool.PermWorkspaceWrite,
		DestructiveClassifier: func(json.RawMessage) (bool, string) {
			return true, "plugin destructive write"
		},
	}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		executed++
		return json.RawMessage(`{"ok":true}`), nil
	})
	if err != nil {
		t.Fatalf("Register plugin tool: %v", err)
	}
	dispatcher := tool.NewDispatcher(registry, tool.MinimalSchemaValidator{}, nil)
	call := tool.Call{
		ID:    "call-plugin-delete",
		Name:  "plugin.com.example.audit.delete_note",
		Input: json.RawMessage(`{"path":"tmp/note.txt"}`),
	}

	result := dispatcher.Execute(context.Background(), call, tool.PermApprovedDestructive)
	if !result.IsError || !result.NeedsApproval {
		t.Fatalf("result = %#v, want approval-required error", result)
	}
	if executed != 0 {
		t.Fatalf("plugin tool executed before approval: %d", executed)
	}

	binding, err := tool.BuildApprovalBinding(call, "approval-1", tool.ApprovalKindDestructiveWrite)
	if err != nil {
		t.Fatalf("BuildApprovalBinding: %v", err)
	}
	ctx := tool.WithApproval(context.Background(), tool.ApprovalContext{
		RequestID:           binding.RequestID,
		ApprovalKind:        binding.ApprovalKind,
		AllowToolCall:       true,
		AllowDestructive:    true,
		ToolName:            binding.ToolName,
		NormalizedInputHash: binding.NormalizedInputHash,
		PathDigest:          binding.PathDigest,
	})
	result = dispatcher.Execute(ctx, call, tool.PermApprovedDestructive)
	if result.IsError {
		t.Fatalf("approved plugin tool result = %#v, want success", result)
	}
	if executed != 1 {
		t.Fatalf("plugin tool executed %d times, want 1", executed)
	}
}

func TestAdvancedFacadesAreSafeStubs(t *testing.T) {
	manifest := validManifest()
	manifest.Capabilities = append(manifest.Capabilities,
		CapabilityMemoryCandidateSubmit,
		CapabilityMemoryForgetRequest,
		CapabilityWorkObserve,
		CapabilityWorkDispatchAnnotate,
		CapabilityApprovalObserve,
	)
	facades := NewFacades("com.example.audit", NewAuthorizer(manifest))

	candidate, err := facades.Memory.SubmitCandidate(context.Background(), PluginMemoryCandidate{
		Summary:       "user likes coffee",
		CandidateType: "fact",
		Confidence:    0.7,
	})
	if err != nil {
		t.Fatalf("SubmitCandidate: %v", err)
	}
	if candidate.Status != "queued" || candidate.ID == "" {
		t.Fatalf("candidate result = %#v, want queued id", candidate)
	}

	forget, err := facades.Memory.RequestForget(context.Background(), PluginForgetRequest{
		TargetSummary: "coffee",
		Level:         ForgetLevelSoft,
	})
	if err != nil {
		t.Fatalf("RequestForget soft: %v", err)
	}
	if forget.Status != "requested" || forget.FinalDecision != "pending_forget_manager" {
		t.Fatalf("forget result = %#v", forget)
	}

	_, err = facades.Memory.RequestForget(context.Background(), PluginForgetRequest{
		TargetSummary: "coffee",
		Level:         ForgetLevelPurge,
	})
	if err == nil || !errors.Is(err, ErrCapabilityDenied) {
		t.Fatalf("RequestForget purge error = %v, want ErrCapabilityDenied", err)
	}

	patch, err := facades.Work.AnnotateTaskBrief(context.Background(), WorkDispatchAnnotation{
		ConstraintHints: []string{"do not modify files outside workspace"},
	})
	if err != nil {
		t.Fatalf("AnnotateTaskBrief: %v", err)
	}
	if len(patch.ConstraintHints) != 1 || patch.PluginID != "com.example.audit" {
		t.Fatalf("work patch = %#v", patch)
	}

	if err := facades.Approval.Observe(context.Background(), ApprovalLifecycleView{RequestID: "approval-1", Status: "pending"}); err != nil {
		t.Fatalf("Approval.Observe: %v", err)
	}

	if err := facades.Work.ObserveDecisionPacket(context.Background(), DecisionPacketView{TaskID: "task-1", Category: "tool_approval", RiskLevel: "high"}); err != nil {
		t.Fatalf("Work.ObserveDecisionPacket: %v", err)
	}
}

func TestOutboundSinkIgnoresTextDecorationAndNamespacesPayload(t *testing.T) {
	bus := NewHookBus(HookBusConfig{DefaultTimeout: 50 * time.Millisecond, MaxTimeout: time.Second}, nil)
	if err := bus.Register(RegisteredHook{
		PluginID:      "com.example.outbound",
		Hook:          HookBeforeOutbound,
		Mode:          HookModeTransform,
		FailurePolicy: FailurePolicyFailOpen,
		Handler: func(context.Context, HookContext) (HookResult, error) {
			return HookResult{Patches: []Patch{
				{Type: PatchOutboundDecorateText, Operation: PatchOpReplace, Value: "[decorated]", ReasonCode: "disabled_in_v0"},
				{Type: PatchOutboundAddPayload, Operation: PatchOpAppend, Value: map[string]any{"status": "checked"}, ReasonCode: "safe_debug"},
			}}, nil
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	host := &PluginHost{enabled: true, bus: bus}
	var got turn.OutboundEvent
	sink := host.WrapOutboundSink(turn.SinkFunc(func(ctx context.Context, event turn.OutboundEvent) error {
		got = event
		return nil
	}))
	if err := sink.Emit(context.Background(), turn.OutboundEvent{TurnID: "turn-1", Type: turn.EventStreamDelta, Content: "answer"}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if got.Content != "answer" {
		t.Fatalf("Content = %q, want unchanged canonical content", got.Content)
	}
	plugins, _ := got.Payload["plugins"].(map[string]any)
	payload, _ := plugins["com.example.outbound"].(map[string]any)
	if payload["status"] != "checked" {
		t.Fatalf("payload = %#v, want namespaced plugin payload", got.Payload)
	}
}
