package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/longyisang/emoagent/internal/tool"
)

const (
	TurnAuditPluginID          = "com.emoagent.plugins.turn-audit"
	MemoryContextDebugPluginID = "com.emoagent.plugins.memory-context-debug"
	OutboundGuardPluginID      = "com.emoagent.plugins.outbound-guard"
)

type BuiltinPlugin interface {
	Manifest() Manifest
	Register(context.Context, *Registrar) error
	Shutdown(context.Context) error
}

type BuiltinRunner struct {
	host        *PluginHost
	registry    *tool.Registry
	agentAffect AgentAffectRuntime
	loaded      map[string]BuiltinPlugin
}

func NewBuiltinRunner(host *PluginHost, registry *tool.Registry) *BuiltinRunner {
	if host != nil && host.registry == nil {
		host.registry = NewPluginRegistry()
	}
	return &BuiltinRunner{host: host, registry: registry, loaded: map[string]BuiltinPlugin{}}
}

func (r *BuiltinRunner) SetAgentAffectRuntime(runtime AgentAffectRuntime) {
	if r != nil {
		r.agentAffect = runtime
	}
}

func (r *BuiltinRunner) Load(ctx context.Context, plugins []BuiltinPlugin, enabledIDs []string) error {
	if r == nil || r.host == nil || !r.host.Enabled() {
		return nil
	}
	enabled := map[string]struct{}{}
	for _, id := range enabledIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			enabled[id] = struct{}{}
		}
	}
	for _, builtin := range plugins {
		if builtin == nil {
			continue
		}
		manifest := builtin.Manifest()
		if _, ok := enabled[manifest.ID]; !ok {
			continue
		}
		if err := manifest.Validate(ManifestValidationOptions{MaxTimeoutMS: r.host.config.MaxTimeoutMS}); err != nil {
			return fmt.Errorf("validate builtin plugin %s: %w", manifest.ID, err)
		}
		if err := r.host.registry.Register(manifest, ManifestValidationOptions{MaxTimeoutMS: r.host.config.MaxTimeoutMS}); err != nil {
			return fmt.Errorf("register builtin plugin manifest %s: %w", manifest.ID, err)
		}
		registrar := newRegistrar(manifest.ID, NewAuthorizer(manifest), r.registry, r.host.bus, manifest.Hooks, r.agentAffect)
		if err := builtin.Register(ctx, registrar); err != nil {
			return fmt.Errorf("register builtin plugin %s: %w", manifest.ID, err)
		}
		r.loaded[manifest.ID] = builtin
	}
	return nil
}

func (r *BuiltinRunner) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}
	var closeErr error
	for id, builtin := range r.loaded {
		if err := builtin.Shutdown(ctx); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("shutdown builtin plugin %s: %w", id, err))
		}
	}
	return closeErr
}

func DefaultBuiltinPlugins() []BuiltinPlugin {
	return []BuiltinPlugin{
		NewTurnAuditPlugin(),
		NewMemoryContextDebugPlugin(),
		NewOutboundGuardPlugin(),
	}
}

type turnAuditPlugin struct{}

func NewTurnAuditPlugin() BuiltinPlugin {
	return turnAuditPlugin{}
}

func (p turnAuditPlugin) Manifest() Manifest {
	return Manifest{
		ID:              TurnAuditPluginID,
		Name:            "Turn Audit",
		Version:         "0.1.0",
		Runtime:         RuntimeBuiltin,
		EmoAgentVersion: "^0.1.0",
		Capabilities:    []Capability{CapabilityTurnRead, CapabilityTurnAnnotate},
		Hooks: []HookSpec{
			{Name: HookAfterTurnEnd, Mode: HookModeObserve, FailurePolicy: FailurePolicyFailOpen, Priority: 10, TimeoutMS: 50},
			{Name: HookOnTurnError, Mode: HookModeObserve, FailurePolicy: FailurePolicyFailOpen, Priority: 10, TimeoutMS: 50},
		},
	}
}

func (p turnAuditPlugin) Register(ctx context.Context, registrar *Registrar) error {
	for _, spec := range p.Manifest().Hooks {
		if err := registrar.Hooks.Register(spec, func(context.Context, HookContext) (HookResult, error) {
			return HookResult{Annotations: map[string]any{"turn_audit": "observed"}}, nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func (p turnAuditPlugin) Shutdown(context.Context) error { return nil }

type memoryContextDebugPlugin struct{}

func NewMemoryContextDebugPlugin() BuiltinPlugin {
	return memoryContextDebugPlugin{}
}

func (p memoryContextDebugPlugin) Manifest() Manifest {
	return Manifest{
		ID:              MemoryContextDebugPluginID,
		Name:            "Memory Context Debug",
		Version:         "0.1.0",
		Runtime:         RuntimeBuiltin,
		EmoAgentVersion: "^0.1.0",
		Capabilities:    []Capability{CapabilityMemoryReadSafe, CapabilityOutboundSafeDebug},
		Hooks: []HookSpec{
			{Name: HookAfterMemoryRetrieve, Mode: HookModeObserve, FailurePolicy: FailurePolicyFailOpen, Priority: 20, TimeoutMS: 50},
		},
	}
}

func (p memoryContextDebugPlugin) Register(ctx context.Context, registrar *Registrar) error {
	return registrar.Hooks.Register(p.Manifest().Hooks[0], func(_ context.Context, hc HookContext) (HookResult, error) {
		count := 0
		if hc.Memory != nil {
			count = len(hc.Memory.Blocks)
		}
		return HookResult{Annotations: map[string]any{"safe_memory_blocks": count}}, nil
	})
}

func (p memoryContextDebugPlugin) Shutdown(context.Context) error { return nil }

type outboundGuardPlugin struct{}

func NewOutboundGuardPlugin() BuiltinPlugin {
	return outboundGuardPlugin{}
}

func (p outboundGuardPlugin) Manifest() Manifest {
	return Manifest{
		ID:              OutboundGuardPluginID,
		Name:            "Outbound Guard",
		Version:         "0.1.0",
		Runtime:         RuntimeBuiltin,
		EmoAgentVersion: "^0.1.0",
		Capabilities:    []Capability{CapabilityOutboundDecorate, CapabilityOutboundSafeDebug},
		Hooks: []HookSpec{
			{Name: HookBeforeOutbound, Mode: HookModeTransform, FailurePolicy: FailurePolicyFailOpen, Priority: 30, TimeoutMS: 50},
		},
	}
}

func (p outboundGuardPlugin) Register(ctx context.Context, registrar *Registrar) error {
	return registrar.Hooks.Register(p.Manifest().Hooks[0], func(_ context.Context, hc HookContext) (HookResult, error) {
		return HookResult{Annotations: map[string]any{"outbound_guard": "checked"}}, nil
	})
}

func (p outboundGuardPlugin) Shutdown(context.Context) error { return nil }
