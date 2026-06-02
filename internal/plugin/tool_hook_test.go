package plugin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/turn"
)

func TestToolHookConvertsPluginPatchesToConservativeDecision(t *testing.T) {
	journal := turn.NewMemoryJournal()
	if err := journal.StartTurn(context.Background(), turn.TurnRecord{TurnID: "turn-tool", Kind: turn.InboundUserMessage}); err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	bus := NewHookBus(HookBusConfig{DefaultTimeout: 50 * time.Millisecond, MaxTimeout: time.Second}, NewTurnJournalAudit(journal))
	err := bus.Register(RegisteredHook{
		PluginID:      "com.example.policy",
		Hook:          HookBeforeToolCall,
		Mode:          HookModeTransform,
		FailurePolicy: FailurePolicyFailOpen,
		Handler: func(context.Context, HookContext) (HookResult, error) {
			return HookResult{Patches: []Patch{
				{Type: PatchToolDowngradePermission, Operation: PatchOpSecure, Value: string(tool.PermReadOnly), ReasonCode: "least_privilege"},
				{Type: PatchToolRequireApproval, Operation: PatchOpSecure, Value: map[string]any{"kind": string(tool.ApprovalKindSensitiveRead), "reason": "plugin requested approval"}, ReasonCode: "policy"},
			}}, nil
		},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	host := &PluginHost{enabled: true, bus: bus}
	hook := NewToolHook(host)
	ctx := turn.WithCorrelationContext(context.Background(), turn.CorrelationContext{
		TurnID:     "turn-tool",
		SessionID:  "session-1",
		PersonaKey: "default",
		RequestID:  "request-1",
		Kind:       turn.InboundUserMessage,
		Stage:      turn.StageEmotionLoop,
	})

	decision, err := hook.BeforeToolCall(ctx, tool.CallHookView{
		Call:          tool.Call{ID: "call-1", Name: "plugin.com.example.policy.echo"},
		MaxPermission: tool.PermWorkspaceWrite,
	})
	if err != nil {
		t.Fatalf("BeforeToolCall: %v", err)
	}
	if decision.MaxPermission != tool.PermReadOnly {
		t.Fatalf("MaxPermission = %q, want read-only", decision.MaxPermission)
	}
	if !decision.RequireApproval || decision.ApprovalKind != tool.ApprovalKindSensitiveRead || decision.ApprovalReason != "plugin requested approval" {
		t.Fatalf("decision = %#v, want sensitive approval", decision)
	}
	snapshot, ok := journal.GetTurn("turn-tool")
	if !ok {
		t.Fatal("journal missing turn-tool")
	}
	if !hasPluginInvocation(snapshot.Events, "com.example.policy", HookBeforeToolCall) {
		t.Fatalf("events = %#v, want before_tool_call plugin_invocation", snapshot.Events)
	}
}

func TestToolHookDispatchesAfterToolCall(t *testing.T) {
	bus := NewHookBus(HookBusConfig{DefaultTimeout: 50 * time.Millisecond, MaxTimeout: time.Second}, nil)
	var afterCount int
	err := bus.Register(RegisteredHook{
		PluginID:      "com.example.audit",
		Hook:          HookAfterToolCall,
		Mode:          HookModeObserve,
		FailurePolicy: FailurePolicyFailOpen,
		Handler: func(context.Context, HookContext) (HookResult, error) {
			afterCount++
			return HookResult{}, nil
		},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	host := &PluginHost{enabled: true, bus: bus}
	hook := NewToolHook(host)

	if err := hook.AfterToolCall(context.Background(), tool.CallHookView{Call: tool.Call{ID: "call-1", Name: "get_time"}}, tool.Result{CallID: "call-1"}); err != nil {
		t.Fatalf("AfterToolCall: %v", err)
	}
	if afterCount != 1 {
		t.Fatalf("afterCount = %d, want 1", afterCount)
	}
}

func TestToolHookRequiresCapabilityForRequireApprovalPatch(t *testing.T) {
	bus := NewHookBus(HookBusConfig{DefaultTimeout: 50 * time.Millisecond, MaxTimeout: time.Second}, nil)
	manifest := Manifest{
		ID:              "com.example.observe-only",
		Name:            "Observe Only",
		Version:         "0.1.0",
		Runtime:         RuntimeBuiltin,
		EmoAgentVersion: "^0.1.0",
		Capabilities:    []Capability{CapabilityToolObserve},
		Hooks: []HookSpec{{
			Name:          HookBeforeToolCall,
			Mode:          HookModeTransform,
			FailurePolicy: FailurePolicyFailClosed,
			Priority:      10,
			TimeoutMS:     50,
		}},
	}
	registrar := NewRegistrarForManifest(manifest, nil, bus)
	if err := registrar.Hooks.Register(manifest.Hooks[0], func(context.Context, HookContext) (HookResult, error) {
		return HookResult{Patches: []Patch{{
			Type:      PatchToolRequireApproval,
			Operation: PatchOpSecure,
			Value:     map[string]any{"kind": string(tool.ApprovalKindSensitiveRead)},
		}}}, nil
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	host := &PluginHost{enabled: true, bus: bus}
	hook := NewToolHook(host)

	_, err := hook.BeforeToolCall(context.Background(), tool.CallHookView{
		Call:          tool.Call{ID: "call-1", Name: "read_file"},
		MaxPermission: tool.PermReadOnly,
	})
	if err == nil || !errors.Is(err, ErrCapabilityDenied) {
		t.Fatalf("BeforeToolCall error = %v, want ErrCapabilityDenied", err)
	}
}

func hasPluginInvocation(events []turn.JournalEvent, pluginID string, hook HookName) bool {
	for _, event := range events {
		if event.Type == "plugin_invocation" &&
			event.Payload["plugin_id"] == pluginID &&
			event.Payload["hook"] == hook &&
			event.Payload["status"] == "done" {
			return true
		}
	}
	return false
}
