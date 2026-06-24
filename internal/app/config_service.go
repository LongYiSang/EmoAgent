package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/configcenter"
	"github.com/longyisang/emoagent/internal/plugin"
	"github.com/longyisang/emoagent/internal/pytoolchain"
	sidecarruntime "github.com/longyisang/emoagent/internal/sidecar"
	"github.com/longyisang/emoagent/internal/storage"
)

type ConfigService struct {
	infra *Infra
	tools *ToolService
}

func (s *ConfigService) service() *configcenter.Service {
	return configcenter.NewService(s.infra.Config, s.infra.DB)
}

func (s *ConfigService) ApplyRuntimeOverrides() error {
	overrides, err := s.infra.DB.GetAllRuntimeConfig()
	if err != nil {
		return err
	}

	for k, v := range overrides {
		switch k {
		case "chat.realtime_streaming":
			enabled, parseErr := strconv.ParseBool(v)
			if parseErr == nil {
				s.infra.Config.Chat.RealtimeStreaming = enabled
			} else {
				s.infra.Logger.Warn("invalid runtime override", "key", "chat.realtime_streaming", "value", v, "error", parseErr)
			}
		case "server.port":
			if n, parseErr := strconv.Atoi(v); parseErr == nil {
				s.infra.Config.Server.Port = n
			} else {
				s.infra.Logger.Warn("invalid runtime override", "key", "server.port", "value", v, "error", parseErr)
			}
		}
	}

	if len(overrides) > 0 {
		s.infra.Logger.Info("runtime config overrides applied", "count", len(overrides))
	}
	settings, err := s.infra.DB.ListRuntimeSettings()
	if err != nil {
		return err
	}
	if len(settings) > 0 {
		runtimeCfg, issues := configcenter.ApplyRuntimeSettings(s.infra.Config, settings)
		s.infra.Config = &runtimeCfg
		for _, issue := range issues {
			s.infra.Logger.Warn("runtime setting rejected", "path", issue.Path, "message", issue.Message)
		}
		s.infra.Logger.Info("runtime settings applied", "count", len(settings))
	}
	return nil
}

func (s *ConfigService) GetChatSettings() config.ChatConfig {
	if s.infra.Config == nil {
		return config.ChatConfig{}
	}
	return s.infra.Config.Chat
}

func (s *ConfigService) UpdateChatSettings(settings config.ChatConfig, chat *ChatService) error {
	if s.infra.DB == nil {
		return fmt.Errorf("database is not initialized")
	}
	if err := s.infra.DB.SetRuntimeConfig("chat.realtime_streaming", strconv.FormatBool(settings.RealtimeStreaming)); err != nil {
		return err
	}

	if s.infra.Config == nil {
		s.infra.Config = config.DefaultConfig()
	}
	s.infra.Config.Chat.RealtimeStreaming = settings.RealtimeStreaming
	if chat != nil {
		chat.UpdateRealtimeStreaming(settings.RealtimeStreaming)
	}
	return nil
}

func (s *ConfigService) GetEffective(ctx context.Context) (configcenter.EffectiveConfig, error) {
	effective, err := s.service().BuildEffective(ctx)
	if err == nil {
		s.attachWebSearchRuntime(&effective)
	}
	return effective, err
}

func (s *ConfigService) Validate(ctx context.Context, req configcenter.ValidateRequest) (configcenter.ValidateResponse, error) {
	return s.service().Validate(ctx, req)
}

func (s *ConfigService) ListIssues(ctx context.Context) ([]configcenter.ConfigIssue, error) {
	return s.service().Issues(ctx)
}

func (s *ConfigService) GetMemoryConfig(ctx context.Context) (configcenter.MemoryConfigResponse, error) {
	return s.service().MemoryConfig(ctx)
}

func (s *ConfigService) GetAgentAffectConfig(ctx context.Context) (configcenter.AgentAffectConfigResponse, error) {
	return s.service().AgentAffectConfig(ctx)
}

func (s *ConfigService) GetWebSearchConfig(ctx context.Context) (configcenter.WebSearchConfigResponse, error) {
	resp, err := s.service().WebSearchConfig(ctx)
	if err == nil {
		resp.Runtime = s.webSearchRuntimeStatus(resp.WebSearch)
	}
	return resp, err
}

func (s *ConfigService) GetPythonToolchainConfig(ctx context.Context) (config.PythonToolchainConfig, error) {
	effective, err := s.service().BuildEffective(ctx)
	if err != nil {
		return config.PythonToolchainConfig{}, err
	}
	return effective.PythonToolchain, nil
}

func (s *ConfigService) ProbePythonToolchain(ctx context.Context, cfg config.PythonToolchainConfig) (pytoolchain.ProbeResult, error) {
	manager := pytoolchain.NewManager(cfg)
	return manager.Probe(ctx)
}

func (s *ConfigService) UpdatePythonToolchainConfig(ctx context.Context, cfg config.PythonToolchainConfig) (configcenter.EffectiveConfig, error) {
	effective, err := s.service().UpdatePythonToolchainConfig(ctx, cfg)
	if err == nil && s.infra.Config != nil {
		s.infra.Config.PythonToolchain = effective.PythonToolchain
	}
	return effective, err
}

func (s *ConfigService) ListPythonEnvironments(ctx context.Context) ([]pytoolchain.EnvironmentSummary, error) {
	cfg, err := s.GetPythonToolchainConfig(ctx)
	if err != nil {
		return nil, err
	}
	owners, err := s.pythonEnvironmentOwners(ctx, cfg)
	if err != nil {
		return nil, err
	}
	var probe pytoolchain.ProbeResult
	var probeErr error
	if cfg.Enabled {
		probe, probeErr = pytoolchain.NewManager(cfg).Probe(ctx)
	} else if len(owners) > 0 {
		probeErr = fmt.Errorf("python toolchain is not configured")
	}
	out := make([]pytoolchain.EnvironmentSummary, 0, len(owners))
	for _, item := range owners {
		summary := pytoolchain.EnvironmentSummary{
			Owner:       item.owner,
			Enabled:     item.enabled,
			RuntimeKind: item.runtimeKind,
		}
		if probeErr != nil {
			summary.Status = pytoolchain.EnvironmentStatus{State: pytoolchain.EnvBroken, Reason: probeErr.Error()}
		} else {
			manager := pytoolchain.NewEnvironmentManager(cfg, pytoolchain.EnvironmentManagerOptions{Toolchain: probe})
			status, err := manager.Status(ctx, item.owner)
			if err != nil {
				status = pytoolchain.EnvironmentStatus{State: pytoolchain.EnvBroken, Reason: err.Error()}
			}
			summary.Status = status
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Owner.Kind != out[j].Owner.Kind {
			return out[i].Owner.Kind < out[j].Owner.Kind
		}
		if out[i].Owner.ID != out[j].Owner.ID {
			return out[i].Owner.ID < out[j].Owner.ID
		}
		return out[i].Owner.Version < out[j].Owner.Version
	})
	return out, nil
}

func (s *ConfigService) SyncPythonEnvironment(ctx context.Context, kind, id, version string) (pytoolchain.EnvironmentSummary, error) {
	return s.ensurePythonEnvironment(ctx, kind, id, version)
}

func (s *ConfigService) RepairPythonEnvironment(ctx context.Context, kind, id, version string) (pytoolchain.EnvironmentSummary, error) {
	return s.ensurePythonEnvironment(ctx, kind, id, version)
}

func (s *ConfigService) UpdateAgentAffectConfig(ctx context.Context, cfg config.AgentAffectConfig) (configcenter.EffectiveConfig, error) {
	effective, err := s.service().UpdateAgentAffectConfig(ctx, cfg)
	if err == nil && s.infra.Config != nil {
		s.infra.Config.AgentAffect = effective.AgentAffect
	}
	return effective, err
}

func (s *ConfigService) UpdateWebSearchConfig(ctx context.Context, cfg config.WebSearchConfig) (configcenter.EffectiveConfig, error) {
	effective, err := s.service().UpdateWebSearchConfig(ctx, cfg)
	if err == nil && s.infra.Config != nil {
		s.infra.Config.WebSearch = effective.WebSearch
	}
	if err == nil {
		if s.tools != nil {
			effective.WebSearchRuntime = s.tools.ReconfigureWebSearch(effective.WebSearch)
		} else {
			s.attachWebSearchRuntime(&effective)
		}
	}
	return effective, err
}

func (s *ConfigService) attachWebSearchRuntime(effective *configcenter.EffectiveConfig) {
	if effective == nil {
		return
	}
	effective.WebSearchRuntime = s.webSearchRuntimeStatus(effective.WebSearch)
}

func (s *ConfigService) webSearchRuntimeStatus(cfg config.WebSearchConfig) configcenter.WebSearchRuntimeStatus {
	if s.tools != nil {
		return s.tools.WebSearchRuntimeStatus(cfg)
	}
	return configcenter.BuildWebSearchRuntimeStatus(cfg, false, "", nil)
}

func (s *ConfigService) UpdateMemoryConfig(ctx context.Context, memory config.MemoryConfig) (configcenter.EffectiveConfig, error) {
	effective, err := s.service().UpdateMemoryConfig(ctx, memory)
	if err == nil && s.infra.Config != nil {
		s.infra.Config.Memory = effective.Memory
	}
	return effective, err
}

func (s *ConfigService) GetMemoryFeatures(ctx context.Context) (configcenter.MemoryConfigResponse, error) {
	return s.GetMemoryConfig(ctx)
}

func (s *ConfigService) UpdateMemoryFeatures(ctx context.Context, memory config.MemoryConfig) (configcenter.EffectiveConfig, error) {
	effective, err := s.service().UpdateMemoryFeatures(ctx, memory)
	if err == nil && s.infra.Config != nil {
		s.infra.Config.Memory = effective.Memory
	}
	return effective, err
}

func (s *ConfigService) BuildSidecarSpec(ctx context.Context) (sidecarruntime.Spec, []configcenter.ConfigIssue, error) {
	spec, issues, err := s.service().BuildSidecarSpec(ctx)
	if err != nil {
		return spec, issues, err
	}
	preparedSpec, environmentIssues := s.attachSidecarEnvironmentCommand(ctx, spec)
	issues = append(issues, environmentIssues...)
	return preparedSpec, issues, nil
}

func (s *ConfigService) BuildMemoryCoreOpenConfig(ctx context.Context, status *sidecarruntime.Status) (configcenter.MemoryCoreOpenConfig, error) {
	return s.service().BuildMemoryCoreOpenConfig(ctx, status)
}

func (s *ConfigService) EnsureSidecarEnvironment(ctx context.Context) (pytoolchain.EnvironmentStatus, error) {
	spec, _, err := s.service().BuildSidecarSpec(ctx)
	if err != nil {
		return pytoolchain.EnvironmentStatus{}, err
	}
	if !spec.Enabled || !spec.Managed {
		return pytoolchain.EnvironmentStatus{}, fmt.Errorf("managed sidecar is not enabled")
	}
	cfg, err := s.GetPythonToolchainConfig(ctx)
	if err != nil {
		return pytoolchain.EnvironmentStatus{}, err
	}
	probe, err := pytoolchain.NewManager(cfg).Probe(ctx)
	if err != nil {
		return pytoolchain.EnvironmentStatus{}, err
	}
	owner, err := s.sidecarEnvironmentOwner(spec, cfg)
	if err != nil {
		return pytoolchain.EnvironmentStatus{}, err
	}
	manager := pytoolchain.NewEnvironmentManager(cfg, pytoolchain.EnvironmentManagerOptions{Toolchain: probe})
	return manager.Ensure(ctx, owner)
}

type pythonEnvironmentOwnerSummary struct {
	owner       pytoolchain.EnvironmentOwner
	enabled     bool
	runtimeKind string
}

func (s *ConfigService) ensurePythonEnvironment(ctx context.Context, kind, id, version string) (pytoolchain.EnvironmentSummary, error) {
	cfg, err := s.GetPythonToolchainConfig(ctx)
	if err != nil {
		return pytoolchain.EnvironmentSummary{}, err
	}
	if !cfg.Enabled {
		return pytoolchain.EnvironmentSummary{}, fmt.Errorf("python toolchain is not configured")
	}
	item, err := s.pythonEnvironmentOwner(ctx, cfg, kind, id, version)
	if err != nil {
		return pytoolchain.EnvironmentSummary{}, err
	}
	probe, err := pytoolchain.NewManager(cfg).Probe(ctx)
	if err != nil {
		return pytoolchain.EnvironmentSummary{}, fmt.Errorf("python toolchain probe: %w", err)
	}
	manager := pytoolchain.NewEnvironmentManager(cfg, pytoolchain.EnvironmentManagerOptions{Toolchain: probe})
	status, err := manager.Ensure(ctx, item.owner)
	if err != nil {
		return pytoolchain.EnvironmentSummary{}, err
	}
	return pytoolchain.EnvironmentSummary{
		Owner:       item.owner,
		Status:      status,
		Enabled:     item.enabled,
		RuntimeKind: item.runtimeKind,
	}, nil
}

func (s *ConfigService) pythonEnvironmentOwners(ctx context.Context, cfg config.PythonToolchainConfig) ([]pythonEnvironmentOwnerSummary, error) {
	var out []pythonEnvironmentOwnerSummary
	spec, _, err := s.service().BuildSidecarSpec(ctx)
	if err != nil {
		return nil, err
	}
	if spec.Managed {
		owner, err := s.sidecarEnvironmentOwner(spec, cfg)
		if err != nil {
			return nil, err
		}
		out = append(out, pythonEnvironmentOwnerSummary{owner: owner, enabled: spec.Enabled, runtimeKind: "memory_sidecar"})
	}
	if s.infra == nil || s.infra.DB == nil {
		return out, nil
	}
	installations, err := s.infra.DB.ListPluginInstallations(ctx)
	if err != nil {
		return nil, err
	}
	for _, installation := range installations {
		item, ok, err := s.managedPluginEnvironmentOwner(ctx, cfg, installation)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, item)
		}
	}
	return out, nil
}

func (s *ConfigService) pythonEnvironmentOwner(ctx context.Context, cfg config.PythonToolchainConfig, kind, id, version string) (pythonEnvironmentOwnerSummary, error) {
	switch pytoolchain.EnvironmentOwnerKind(strings.TrimSpace(kind)) {
	case pytoolchain.OwnerMemorySidecar:
		spec, _, err := s.service().BuildSidecarSpec(ctx)
		if err != nil {
			return pythonEnvironmentOwnerSummary{}, err
		}
		if !spec.Managed {
			return pythonEnvironmentOwnerSummary{}, fmt.Errorf("managed sidecar is not enabled")
		}
		owner, err := s.sidecarEnvironmentOwner(spec, cfg)
		if err != nil {
			return pythonEnvironmentOwnerSummary{}, err
		}
		if id != "" && id != owner.ID {
			return pythonEnvironmentOwnerSummary{}, fmt.Errorf("unknown memory sidecar environment %q", id)
		}
		return pythonEnvironmentOwnerSummary{owner: owner, enabled: spec.Enabled, runtimeKind: "memory_sidecar"}, nil
	case pytoolchain.OwnerPlugin:
		if s.infra == nil || s.infra.DB == nil {
			return pythonEnvironmentOwnerSummary{}, fmt.Errorf("plugin database is not configured")
		}
		installation, err := s.infra.DB.GetPluginInstallationVersion(ctx, id, version)
		if strings.TrimSpace(version) == "" {
			installation, err = s.infra.DB.GetPluginInstallation(ctx, id)
		}
		if err != nil {
			return pythonEnvironmentOwnerSummary{}, err
		}
		if installation == nil {
			return pythonEnvironmentOwnerSummary{}, fmt.Errorf("plugin %q is not installed", id)
		}
		item, ok, err := s.managedPluginEnvironmentOwner(ctx, cfg, *installation)
		if err != nil {
			return pythonEnvironmentOwnerSummary{}, err
		}
		if !ok {
			return pythonEnvironmentOwnerSummary{}, fmt.Errorf("plugin %q is not a managed Python plugin", id)
		}
		return item, nil
	default:
		return pythonEnvironmentOwnerSummary{}, fmt.Errorf("unknown python environment kind %q", kind)
	}
}

func (s *ConfigService) managedPluginEnvironmentOwner(ctx context.Context, cfg config.PythonToolchainConfig, installation storage.PluginInstallation) (pythonEnvironmentOwnerSummary, bool, error) {
	manifest, err := decodeInstalledManifest(installation)
	if err != nil {
		return pythonEnvironmentOwnerSummary{}, false, err
	}
	if manifest.Runtime.Kind != plugin.RuntimeManagedPythonProcess {
		return pythonEnvironmentOwnerSummary{}, false, nil
	}
	root := resolvePluginStoreRoot(s.infra.ProjectRoot, s.infra.Config.Plugins.Store.RootDir)
	store, err := plugin.NewPluginStore(root)
	if err != nil {
		return pythonEnvironmentOwnerSummary{}, false, err
	}
	packageDir, err := store.PackageDir(manifest.ID, manifest.Version)
	if err != nil {
		return pythonEnvironmentOwnerSummary{}, false, err
	}
	envRoot := strings.TrimSpace(cfg.EnvironmentRoot)
	if envRoot == "" {
		envRoot = "data/python-envs"
	}
	envRoot, err = s.resolveAppPath(envRoot)
	if err != nil {
		return pythonEnvironmentOwnerSummary{}, false, err
	}
	enabled := false
	if state, err := s.infra.DB.GetPluginEnabledState(ctx, installation.PluginID); err != nil {
		return pythonEnvironmentOwnerSummary{}, false, err
	} else if state != nil {
		enabled = state.Enabled && (state.Version == "" || state.Version == installation.Version)
	}
	return pythonEnvironmentOwnerSummary{
		owner: pytoolchain.EnvironmentOwner{
			Kind:       pytoolchain.OwnerPlugin,
			ID:         manifest.ID,
			Version:    manifest.Version,
			ProjectDir: packageDir,
			EnvDir:     filepath.Join(envRoot, "plugins", manifest.ID, manifest.Version),
		},
		enabled:     enabled,
		runtimeKind: string(manifest.Runtime.Kind),
	}, true, nil
}

func (s *ConfigService) attachSidecarEnvironmentCommand(ctx context.Context, spec sidecarruntime.Spec) (sidecarruntime.Spec, []configcenter.ConfigIssue) {
	if !spec.Enabled || !spec.Managed {
		return spec, nil
	}
	cfg, err := s.GetPythonToolchainConfig(ctx)
	if err != nil {
		return spec, []configcenter.ConfigIssue{s.sidecarEnvironmentIssue(spec, fmt.Sprintf("load python toolchain config: %v", err))}
	}
	probe, err := pytoolchain.NewManager(cfg).Probe(ctx)
	if err != nil {
		return spec, []configcenter.ConfigIssue{s.sidecarEnvironmentIssue(spec, fmt.Sprintf("python toolchain is not ready: %v", err))}
	}
	owner, err := s.sidecarEnvironmentOwner(spec, cfg)
	if err != nil {
		return spec, []configcenter.ConfigIssue{s.sidecarEnvironmentIssue(spec, err.Error())}
	}
	manager := pytoolchain.NewEnvironmentManager(cfg, pytoolchain.EnvironmentManagerOptions{Toolchain: probe})
	status, err := manager.Status(ctx, owner)
	if err != nil {
		return spec, []configcenter.ConfigIssue{s.sidecarEnvironmentIssue(spec, fmt.Sprintf("inspect sidecar environment: %v", err))}
	}
	if status.State != pytoolchain.EnvReady || status.Marker == nil || strings.TrimSpace(status.Marker.EnvironmentPython) == "" {
		reason := status.Reason
		if reason == "" {
			reason = fmt.Sprintf("sidecar environment is %s", status.State)
		}
		return spec, []configcenter.ConfigIssue{s.sidecarEnvironmentIssue(spec, reason)}
	}
	spec.Command = []string{status.Marker.EnvironmentPython, "-I", "-P", "-u", "-m", "memorycore_sidecar.server"}
	return spec, nil
}

func (s *ConfigService) sidecarEnvironmentOwner(spec sidecarruntime.Spec, cfg config.PythonToolchainConfig) (pytoolchain.EnvironmentOwner, error) {
	projectDir, err := s.resolveAppPath(spec.WorkingDir)
	if err != nil {
		return pytoolchain.EnvironmentOwner{}, fmt.Errorf("resolve sidecar working_dir: %w", err)
	}
	envRoot := cfg.EnvironmentRoot
	if strings.TrimSpace(envRoot) == "" {
		envRoot = "data/python-envs"
	}
	envRoot, err = s.resolveAppPath(envRoot)
	if err != nil {
		return pytoolchain.EnvironmentOwner{}, fmt.Errorf("resolve python environment_root: %w", err)
	}
	return pytoolchain.EnvironmentOwner{
		Kind:       pytoolchain.OwnerMemorySidecar,
		ID:         "memorycore_sidecar",
		Version:    "0.1.0",
		ProjectDir: projectDir,
		EnvDir:     filepath.Join(envRoot, "memory-sidecar"),
	}, nil
}

func (s *ConfigService) resolveAppPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) {
		return path, nil
	}
	base := ""
	if s.infra != nil {
		base = strings.TrimSpace(s.infra.ProjectRoot)
	}
	if base == "" {
		return filepath.Abs(path)
	}
	return filepath.Abs(filepath.Join(base, path))
}

func (s *ConfigService) sidecarEnvironmentIssue(spec sidecarruntime.Spec, message string) configcenter.ConfigIssue {
	severity := "error"
	if spec.FailOpen {
		severity = "warning"
	}
	return configcenter.ConfigIssue{
		Path:     "memory.sidecar.environment",
		Severity: severity,
		Message:  message,
	}
}
