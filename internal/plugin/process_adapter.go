package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/longyisang/emoagent/internal/tool"
)

type ProcessSupervisor interface {
	AddPlugin(ManifestV2)
	EnsureReady(context.Context, string) (*ProcessRuntime, error)
	InvokeHook(context.Context, string, HookName, HookContext) (HookResult, error)
	InvokeTool(context.Context, string, string, json.RawMessage) (json.RawMessage, error)
	Tools(string) []ProcessToolSpec
}

func RegisterProcessPlugin(ctx context.Context, manifest ManifestV2, pluginRegistry *PluginRegistry, toolRegistry *tool.Registry, bus *HookBus, supervisor ProcessSupervisor) error {
	if supervisor == nil {
		return fmt.Errorf("process supervisor is not configured")
	}
	compat := manifest.CompatManifest()
	if err := compat.Validate(ManifestValidationOptions{}); err != nil {
		return err
	}
	if pluginRegistry != nil {
		if err := pluginRegistry.Register(compat, ManifestValidationOptions{}); err != nil {
			return err
		}
	}
	supervisor.AddPlugin(manifest)
	authorizer := NewAuthorizer(compat)
	for _, spec := range manifest.Hooks {
		spec := spec
		if bus == nil {
			continue
		}
		if err := bus.Register(RegisteredHook{
			PluginID:      manifest.ID,
			Authorizer:    authorizer,
			Hook:          spec.Name,
			Mode:          spec.Mode,
			FailurePolicy: spec.FailurePolicy,
			Priority:      spec.Priority,
			TimeoutMS:     spec.TimeoutMS,
			Handler: func(ctx context.Context, hc HookContext) (HookResult, error) {
				return supervisor.InvokeHook(ctx, manifest.ID, spec.Name, hc)
			},
		}); err != nil {
			return err
		}
	}
	if _, err := supervisor.EnsureReady(ctx, manifest.ID); err != nil {
		return err
	}
	registrar := NewRegistrarForManifest(compat, toolRegistry, bus)
	for _, processTool := range supervisor.Tools(manifest.ID) {
		spec := processTool.ToToolSpec(manifest.ID)
		name := processTool.Name
		if err := registrar.Tools.Register(ctx, spec, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			return supervisor.InvokeTool(ctx, manifest.ID, name, input)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s ProcessToolSpec) ToToolSpec(pluginID string) tool.Spec {
	scope := s.Scope
	if scope == "" {
		scope = tool.ScopeBoth
	}
	permission := s.Permission
	if permission == "" {
		permission = tool.PermReadOnly
	}
	return tool.Spec{
		Name:        s.Name,
		Description: s.Description,
		Parameters:  append(json.RawMessage(nil), s.Parameters...),
		Scope:       scope,
		Permission:  permission,
	}
}
