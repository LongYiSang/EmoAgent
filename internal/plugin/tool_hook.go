package plugin

import (
	"context"
	"fmt"

	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/turn"
)

type ToolHook struct {
	host *PluginHost
}

func NewToolHook(host *PluginHost) *ToolHook {
	if host == nil || !host.Enabled() {
		return nil
	}
	return &ToolHook{host: host}
}

func (h *ToolHook) BeforeToolCall(ctx context.Context, view tool.CallHookView) (tool.CallHookDecision, error) {
	if h == nil || h.host == nil || h.host.bus == nil {
		return tool.CallHookDecision{}, nil
	}
	hc := hookContextFromCorrelation(ctx, HookBeforeToolCall)
	hc.Tool = &ToolCallView{
		CallID:             view.Call.ID,
		Name:               view.Call.Name,
		AgentScope:         string(view.AgentScope),
		RequiredPermission: string(view.Spec.Permission),
		InputBytes:         len(view.Call.Input),
		InputHash:          contentHash(string(view.Call.Input)),
	}
	result, err := h.host.bus.Dispatch(ctx, HookBeforeToolCall, HookContext{
		Envelope: hc.Envelope,
		Turn:     hc.Turn,
		Tool:     hc.Tool,
	})
	if err != nil {
		return tool.CallHookDecision{}, err
	}
	decision := tool.CallHookDecision{}
	for _, patch := range result.Patches {
		switch patch.Type {
		case PatchToolDowngradePermission:
			permission, ok := patch.Value.(string)
			if !ok {
				continue
			}
			next := tool.Permission(permission)
			if moreConservativePermission(next, view.MaxPermission) {
				decision.MaxPermission = next
			}
		case PatchToolRequireApproval:
			decision.RequireApproval = true
			if fields, ok := patch.Value.(map[string]any); ok {
				if kind, ok := fields["kind"].(string); ok {
					decision.ApprovalKind = tool.ApprovalKind(kind)
				}
				if reason, ok := fields["reason"].(string); ok {
					decision.ApprovalReason = reason
				}
			}
			if decision.Reason == "" {
				decision.Reason = fmt.Sprintf("approval required by plugin %s", patch.PluginID)
			}
		}
	}
	return decision, nil
}

func (h *ToolHook) AfterToolCall(ctx context.Context, view tool.CallHookView, result tool.Result) error {
	if h == nil || h.host == nil || h.host.bus == nil {
		return nil
	}
	status := "success"
	if result.NeedsApproval {
		status = "approval_required"
	} else if result.IsError {
		status = "error"
	}
	hc := hookContextFromCorrelation(ctx, HookAfterToolCall)
	hc.Tool = &ToolCallView{
		CallID:       view.Call.ID,
		Name:         view.Call.Name,
		AgentScope:   string(view.AgentScope),
		InputBytes:   len(view.Call.Input),
		InputHash:    contentHash(string(view.Call.Input)),
		ResultStatus: status,
		ResultBytes:  len(result.Content),
		ResultHash:   contentHash(string(result.Content)),
	}
	_, err := h.host.bus.Dispatch(ctx, HookAfterToolCall, hc)
	return err
}

func hookContextFromCorrelation(ctx context.Context, hook HookName) HookContext {
	correlation, _ := turn.CorrelationContextFromContext(ctx)
	return HookContext{
		Envelope: HookEnvelope{
			Hook:       hook,
			TurnID:     correlation.TurnID,
			Stage:      correlation.Stage,
			SessionID:  correlation.SessionID,
			PersonaKey: correlation.PersonaKey,
		},
		Turn: TurnView{
			TurnID:     correlation.TurnID,
			Kind:       correlation.Kind,
			SessionID:  correlation.SessionID,
			PersonaKey: correlation.PersonaKey,
			RequestID:  correlation.RequestID,
		},
	}
}

func moreConservativePermission(next, current tool.Permission) bool {
	return permissionRank(next) >= 0 && permissionRank(next) < permissionRank(current)
}

func permissionRank(permission tool.Permission) int {
	switch permission {
	case tool.PermReadOnly:
		return 0
	case tool.PermWorkspaceWrite:
		return 1
	case tool.PermApprovedDestructive:
		return 2
	default:
		return -1
	}
}
