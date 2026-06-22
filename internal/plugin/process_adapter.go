package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/tool/resultv2"
)

type ProcessSupervisor interface {
	AddPlugin(ManifestV2)
	EnsureReady(context.Context, string) (*ProcessRuntime, error)
	InvokeHook(context.Context, string, HookName, HookContext) (HookResult, error)
	InvokeTool(context.Context, string, string, json.RawMessage) (json.RawMessage, error)
	Tools(string) []ProcessToolSpec
}

type ProcessPluginPolicy struct {
	AllowActiveHooks bool
}

func RegisterProcessPlugin(ctx context.Context, manifest ManifestV2, pluginRegistry *PluginRegistry, toolRegistry *tool.Registry, bus *HookBus, supervisor ProcessSupervisor) error {
	return RegisterProcessPluginWithPolicy(ctx, manifest, pluginRegistry, toolRegistry, bus, supervisor, ProcessPluginPolicy{})
}

func RegisterProcessPluginWithPolicy(ctx context.Context, manifest ManifestV2, pluginRegistry *PluginRegistry, toolRegistry *tool.Registry, bus *HookBus, supervisor ProcessSupervisor, policy ProcessPluginPolicy) error {
	if supervisor == nil {
		return fmt.Errorf("process supervisor is not configured")
	}
	compat := manifest.CompatManifest()
	if err := compat.Validate(ManifestValidationOptions{}); err != nil {
		return err
	}
	if err := validateProcessPluginPolicy(manifest, policy); err != nil {
		return err
	}
	if pluginRegistry != nil {
		if err := pluginRegistry.Register(compat, ManifestValidationOptions{}); err != nil {
			return err
		}
	}
	supervisor.AddPlugin(manifest)
	registrar := NewRegistrarForManifest(compat, toolRegistry, bus)
	for _, spec := range manifest.Hooks {
		spec := spec
		if err := registrar.Hooks.Register(spec, func(ctx context.Context, hc HookContext) (HookResult, error) {
			return supervisor.InvokeHook(ctx, manifest.ID, spec.Name, hc)
		}); err != nil {
			return err
		}
	}
	if _, err := supervisor.EnsureReady(ctx, manifest.ID); err != nil {
		return err
	}
	for _, processTool := range supervisor.Tools(manifest.ID) {
		spec := processTool.ToToolSpec(manifest.ID, manifest.Version, manifest.Runtime.Kind)
		name := processTool.Name
		if err := registrar.Tools.Register(ctx, spec, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			return supervisor.InvokeTool(ctx, manifest.ID, name, input)
		}); err != nil {
			return err
		}
	}
	return nil
}

func validateProcessPluginPolicy(manifest ManifestV2, policy ProcessPluginPolicy) error {
	if policy.AllowActiveHooks {
		return nil
	}
	for _, hook := range manifest.Hooks {
		if activeHookMode(hook.Mode) {
			return &PluginError{
				Kind: ErrorKindPluginPolicyViolation,
				Err:  fmt.Errorf("active hooks are disabled by host policy: %s uses mode %s", hook.Name, hook.Mode),
			}
		}
	}
	return nil
}

func activeHookMode(mode HookMode) bool {
	return mode == HookModeTransform || mode == HookModeSideEffect
}

func (s ProcessToolSpec) ToToolSpec(pluginID, version string, runtimeKind RuntimeKind) tool.Spec {
	sourceRuntime, executor := processToolSourceRuntime(runtimeKind)
	return tool.Spec{
		Name:        s.Name,
		Description: s.Description,
		Parameters:  append(json.RawMessage(nil), s.Parameters...),
		Scope:       tool.ScopeWork,
		Permission:  tool.PermApprovedDestructive,
		Source: tool.ToolSourceMetadata{
			Kind:            tool.ToolSourcePlugin,
			ProducerID:      pluginID,
			ProducerVersion: version,
			RuntimeKind:     sourceRuntime,
			DefaultLabels: resultv2.ContentLabels{
				Executor:             executor,
				Origin:               resultv2.OriginPluginGenerated,
				Integrity:            resultv2.IntegrityUnverified,
				InstructionAuthority: resultv2.InstructionDataOnly,
			},
		},
	}
}

func processToolSourceRuntime(kind RuntimeKind) (string, string) {
	switch kind {
	case RuntimeManagedPythonProcess:
		return resultv2.RuntimeManagedPython, resultv2.ExecutorManagedPythonPlugin
	case RuntimeProcess, RuntimePythonProcess:
		return resultv2.RuntimeProcessDev, resultv2.ExecutorLegacyProcessPlugin
	default:
		return resultv2.RuntimeProcessDev, resultv2.ExecutorLegacyProcessPlugin
	}
}
