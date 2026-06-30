package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/longyisang/emoagent/internal/tool"
)

type Registrar struct {
	PluginID   string
	Authorizer *Authorizer
	Hooks      *HookRegistrar
	Tools      *ToolFacade
	Facades    Facades
}

func NewRegistrar(pluginID string, authorizer *Authorizer, registry *tool.Registry, bus *HookBus) *Registrar {
	return newRegistrar(pluginID, authorizer, registry, bus, nil, nil)
}

func NewRegistrarForManifest(manifest Manifest, registry *tool.Registry, bus *HookBus) *Registrar {
	return newRegistrar(manifest.ID, NewAuthorizer(manifest), registry, bus, manifest.Hooks, nil)
}

func newRegistrar(pluginID string, authorizer *Authorizer, registry *tool.Registry, bus *HookBus, hooks []HookSpec, agentAffect AgentAffectRuntime) *Registrar {
	declaredHooks := make(map[HookName]HookSpec, len(hooks))
	for _, hook := range hooks {
		declaredHooks[hook.Name] = hook
	}
	return &Registrar{
		PluginID:   pluginID,
		Authorizer: authorizer,
		Hooks:      &HookRegistrar{pluginID: pluginID, authorizer: authorizer, bus: bus, declaredHooks: declaredHooks},
		Tools:      &ToolFacade{pluginID: pluginID, authorizer: authorizer, registry: registry},
		Facades:    NewFacadesWithAgentAffect(pluginID, authorizer, agentAffect),
	}
}

type HookRegistrar struct {
	pluginID      string
	authorizer    *Authorizer
	bus           *HookBus
	declaredHooks map[HookName]HookSpec
}

func (r *HookRegistrar) Register(spec HookSpec, handler HookHandler) error {
	if r == nil || r.bus == nil {
		return nil
	}
	declared, ok := r.declaredHooks[spec.Name]
	if !ok {
		return fmt.Errorf("hook %q is not declared by plugin %s manifest", spec.Name, r.pluginID)
	}
	if err := r.authorizer.Require(capabilityForHook(spec.Name)); err != nil {
		return err
	}
	if spec.Mode == "" {
		spec.Mode = declared.Mode
	}
	if spec.FailurePolicy == "" {
		spec.FailurePolicy = declared.FailurePolicy
	}
	if spec.TimeoutMS == 0 {
		spec.TimeoutMS = declared.TimeoutMS
	}
	return r.bus.Register(RegisteredHook{
		PluginID:      r.pluginID,
		Authorizer:    r.authorizer,
		Hook:          spec.Name,
		Mode:          spec.Mode,
		FailurePolicy: spec.FailurePolicy,
		Priority:      spec.Priority,
		TimeoutMS:     spec.TimeoutMS,
		Handler:       handler,
	})
}

func capabilityForHook(hook HookName) Capability {
	switch hook {
	case HookBeforeMemoryPrepare,
		HookAfterMemoryPrepare,
		HookBeforeMemoryRetrieve,
		HookAfterMemoryRetrieve,
		HookBeforeMemoryCommit,
		HookAfterMemoryCommit:
		return CapabilityMemoryReadSafe
	case HookBeforeToolCall,
		HookAfterToolCall:
		return CapabilityToolObserve
	case HookWorkDispatchAnnotate:
		return CapabilityWorkDispatchAnnotate
	case HookOnDecisionPacket:
		return CapabilityWorkObserve
	case HookOnApprovalRequested,
		HookOnApprovalResolved,
		HookOnApprovalConsumed:
		return CapabilityApprovalObserve
	case HookMemoryCandidateSubmit:
		return CapabilityMemoryCandidateSubmit
	case HookMemoryForgetRequest:
		return CapabilityMemoryForgetRequest
	case HookBeforeOutbound,
		HookAfterOutbound:
		return CapabilityOutboundDecorate
	case HookBeforeAgentAffectEvaluate,
		HookAfterAgentAffectEvaluate,
		HookBeforeAgentAffectCommit,
		HookAfterAgentAffectCommit:
		return CapabilityAgentAffectObserve
	case HookAgentAffectGetState:
		return CapabilityAgentAffectRead
	default:
		return CapabilityTurnRead
	}
}

type ToolFacade struct {
	pluginID   string
	authorizer *Authorizer
	registry   *tool.Registry
}

func (f *ToolFacade) Register(ctx context.Context, spec tool.Spec, handler tool.Handler) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f.authorizer.Require(CapabilityToolRegister); err != nil {
		return err
	}
	if f == nil || f.registry == nil {
		return fmt.Errorf("tool registry is not configured")
	}
	spec.Name = f.namespacedToolName(spec.Name)
	if spec.Name == "" {
		return fmt.Errorf("plugin tool name is required")
	}
	if !strings.HasPrefix(spec.Name, pluginToolNamePrefix(f.pluginID)) {
		return fmt.Errorf("plugin tool %q must use namespace %s", spec.Name, pluginToolNamePrefix(f.pluginID))
	}
	return f.registry.TryRegister(spec, handler)
}

func (f *ToolFacade) namespacedToolName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	prefix := pluginToolNamePrefix(f.pluginID)
	if strings.HasPrefix(name, prefix) {
		return name
	}
	legacyPrefix := "plugin." + f.pluginID + "."
	if strings.HasPrefix(name, legacyPrefix) {
		return prefix + safePluginToolNameSegment(strings.TrimPrefix(name, legacyPrefix))
	}
	if strings.HasPrefix(name, "plugin.") || strings.HasPrefix(name, "plugin_") {
		return name
	}
	return prefix + safePluginToolNameSegment(name)
}

func pluginToolNamePrefix(pluginID string) string {
	return "plugin_" + safePluginToolNameSegment(pluginID) + "_"
}

func safePluginToolNameSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "tool"
	}
	var b strings.Builder
	b.Grow(len(value))
	lastUnderscore := false
	for _, r := range value {
		allowed := (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '_' ||
			r == '-'
		if allowed {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "tool"
	}
	return out
}
