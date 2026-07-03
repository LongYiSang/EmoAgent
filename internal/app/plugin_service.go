package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/configcenter"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/plugin"
	"github.com/longyisang/emoagent/internal/processguard"
	"github.com/longyisang/emoagent/internal/pytoolchain"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tokenmeter"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/turn"
)

type PluginService struct {
	mu           sync.Mutex
	infra        *Infra
	tools        *ToolService
	agentAffect  *AgentAffectService
	agentRuntime *AgentRuntimeService
	commands     *CommandService
	host         *plugin.PluginHost
	runner       *plugin.BuiltinRunner
	dispatcher   *tool.Dispatcher

	store            *plugin.PluginStore
	installer        *plugin.PluginInstaller
	manager          *plugin.Manager
	supervisor       *plugin.RuntimeSupervisor
	facadeBroker     *plugin.FacadeBroker
	providerGateway  *plugin.ProviderGateway
	registered       map[string]string
	pendingTrustAcks map[string]plugin.PluginTrustAcknowledgement
}

const pluginSettingsKVKey = plugin.PluginSettingsKey

func (s *PluginService) Host() *plugin.PluginHost {
	return s.host
}

func (s *PluginService) Configure(ctx context.Context, dispatcher *tool.Dispatcher, journal turn.TurnJournal) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dispatcher = dispatcher
	if s == nil || s.infra.Config == nil || !s.infra.Config.Plugins.Enabled {
		s.host = nil
		s.runner = nil
		return nil
	}
	host := plugin.NewPluginHost(s.infra.Config.Plugins, journal, s.infra.Logger)
	runner := plugin.NewBuiltinRunner(host, s.tools.Registry())
	if s.agentAffect != nil {
		runner.SetAgentAffectRuntime(s.agentAffect.PluginAPI())
	}
	if err := runner.Load(ctx, plugin.DefaultBuiltinPlugins(), s.infra.Config.Plugins.BuiltinEnabled); err != nil {
		return fmt.Errorf("load builtin plugins: %w", err)
	}
	if dispatcher != nil {
		dispatcher.SetHook(plugin.NewToolHook(host))
	}
	s.host = host
	s.runner = runner
	if err := s.ensureRuntimeLocked(); err != nil {
		return err
	}
	return s.loadEnabledProcessPluginsLocked(ctx)
}

func (s *PluginService) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.supervisor != nil {
		if err := s.supervisor.StopAll(ctx); err != nil {
			return fmt.Errorf("shutdown plugin runtimes: %w", err)
		}
	}
	if s.runner != nil {
		if err := s.runner.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown plugins: %w", err)
		}
		s.runner = nil
	}
	s.host = nil
	return nil
}

func (s *PluginService) InvokeCommand(ctx context.Context, pluginID string, req plugin.CommandInvokeRequest) (plugin.CommandInvokeResult, error) {
	if s == nil || s.supervisor == nil {
		return plugin.CommandInvokeResult{}, fmt.Errorf("plugin runtime is not configured")
	}
	return s.supervisor.InvokeCommand(ctx, pluginID, req)
}

func (s *PluginService) AuthorizeProviderGenerate(ctx context.Context, pluginID string) error {
	if s == nil || s.facadeBroker == nil {
		return fmt.Errorf("plugin facade is not configured")
	}
	return s.facadeBroker.AuthorizeCapability(ctx, pluginID, plugin.CapabilityProviderGenerate)
}

func (s *PluginService) ensureRuntimeLocked() error {
	if s == nil || s.infra == nil || s.infra.Config == nil {
		return fmt.Errorf("plugin service is not configured")
	}
	if s.store != nil && s.installer != nil && s.supervisor != nil && s.facadeBroker != nil {
		return nil
	}
	root := resolvePluginStoreRoot(s.infra.ProjectRoot, s.infra.Config.Plugins.Store.RootDir)
	store, err := plugin.NewPluginStore(root)
	if err != nil {
		return err
	}
	providerGateway := plugin.NewProviderGateway(s.infra.DB, s.infra.Config.Plugins.ProviderGateway, s.providerClient)
	providerGateway.SetFallbackResolver(s.providerGatewayFallback)
	facadeBroker := plugin.NewFacadeBroker(s.infra.DB, providerGateway)
	facadeBroker.SetStore(store)
	facadeBroker.SetHostPolicy(s.pluginFacadeHostPolicy())
	supervisor := plugin.NewRuntimeSupervisor(store, s.infra.Config.Plugins.Runtime, nil)
	supervisor.SetManagedPythonEnvironmentResolver(s.prepareManagedPluginEnvironment)
	supervisor.SetHostHandlerForPlugin(s.hostRPCHandlerForPlugin)
	supervisor.SetEnabledChecker(s.isPluginEnabled)
	supervisor.SetBlockedEnvNames(s.pluginBlockedEnvNames())
	supervisor.SetBlockedEnvNamesProvider(s.pluginRuntimeBlockedEnvNames)
	supervisor.SetAdditionalEnvVars(s.pluginProcessEnv())
	s.store = store
	s.installer = plugin.NewPluginInstaller(store, s.infra.Config.Plugins.Installer)
	s.providerGateway = providerGateway
	s.facadeBroker = facadeBroker
	s.supervisor = supervisor
	s.manager = plugin.NewManager(store, supervisor, facadeBroker, providerGateway)
	if s.registered == nil {
		s.registered = map[string]string{}
	}
	return nil
}

func (s *PluginService) pluginFacadeHostPolicy() plugin.FacadeHostPolicy {
	if s == nil || s.infra == nil || s.infra.Config == nil {
		return plugin.FacadeHostPolicy{}
	}
	configured := s.infra.Config.Plugins.Policy.AllowedCapabilities
	allowed := make([]plugin.Capability, 0, len(configured))
	for _, capability := range configured {
		capability = strings.TrimSpace(capability)
		if capability != "" {
			allowed = append(allowed, plugin.Capability(capability))
		}
	}
	return plugin.FacadeHostPolicy{AllowedCapabilities: allowed}
}

func (s *PluginService) processPluginPolicy() plugin.ProcessPluginPolicy {
	if s == nil || s.infra == nil || s.infra.Config == nil {
		return plugin.ProcessPluginPolicy{}
	}
	return plugin.ProcessPluginPolicy{AllowActiveHooks: s.infra.Config.Plugins.Policy.AllowActiveHooks}
}

func (s *PluginService) pluginProcessEnv() []string {
	if s == nil || s.infra == nil || strings.TrimSpace(s.infra.ProjectRoot) == "" {
		return nil
	}
	sdkPath := filepath.Join(s.infra.ProjectRoot, "sdk", "python")
	if _, err := os.Stat(sdkPath); err != nil {
		return nil
	}
	return []string{"PYTHONPATH=" + sdkPath}
}

func (s *PluginService) prepareManagedPluginEnvironment(ctx context.Context, manifest plugin.ManifestV2) (plugin.ManagedPythonEnvironment, error) {
	if s == nil || s.infra == nil || s.store == nil {
		return plugin.ManagedPythonEnvironment{}, fmt.Errorf("plugin runtime is not configured")
	}
	packageDir, err := s.store.PackageDir(manifest.ID, manifest.Version)
	if err != nil {
		return plugin.ManagedPythonEnvironment{}, err
	}
	if err := plugin.ValidateManagedPythonProject(packageDir); err != nil {
		return plugin.ManagedPythonEnvironment{}, err
	}
	effective, err := configcenter.NewService(s.infra.Config, s.infra.DB).BuildEffective(ctx)
	if err != nil {
		return plugin.ManagedPythonEnvironment{}, err
	}
	toolchainCfg := effective.PythonToolchain
	if !toolchainCfg.Enabled {
		return plugin.ManagedPythonEnvironment{}, fmt.Errorf("python toolchain is not configured")
	}
	probe, err := pytoolchain.NewManager(toolchainCfg).Probe(ctx)
	if err != nil {
		return plugin.ManagedPythonEnvironment{}, fmt.Errorf("python toolchain probe: %w", err)
	}
	envRoot := strings.TrimSpace(toolchainCfg.EnvironmentRoot)
	if envRoot == "" {
		envRoot = "data/python-envs"
	}
	envRoot = resolvePluginStoreRoot(s.infra.ProjectRoot, envRoot)
	owner := pytoolchain.EnvironmentOwner{
		Kind:       pytoolchain.OwnerPlugin,
		ID:         manifest.ID,
		Version:    manifest.Version,
		ProjectDir: packageDir,
		EnvDir:     filepath.Join(envRoot, "plugins", manifest.ID, manifest.Version),
	}
	manager := pytoolchain.NewEnvironmentManager(toolchainCfg, pytoolchain.EnvironmentManagerOptions{Toolchain: probe})
	status, err := manager.Ensure(ctx, owner)
	if err != nil {
		return plugin.ManagedPythonEnvironment{}, err
	}
	if status.State != pytoolchain.EnvReady || status.Marker == nil || strings.TrimSpace(status.Marker.EnvironmentPython) == "" {
		return plugin.ManagedPythonEnvironment{}, fmt.Errorf("plugin environment is %s: %s", status.State, status.Reason)
	}
	return plugin.ManagedPythonEnvironment{
		PythonExecutable: status.Marker.EnvironmentPython,
		EnvironmentDir:   owner.EnvDir,
	}, nil
}

func (s *PluginService) pluginBlockedEnvNames() []string {
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	if s != nil && s.infra != nil && s.infra.Config != nil {
		for _, provider := range s.infra.Config.LLMProviders {
			add(provider.APIKeyEnv)
		}
		add(s.infra.Config.Memory.Extraction.Provider.APIKeyEnv)
		add(s.infra.Config.WebSearch.APIKeyEnv)
		add(s.infra.Config.WebSearch.Pipeline.Rerank.APIKeyEnv)
		add(s.infra.Config.WebFetch.APIKeyEnv)
	}
	if s != nil && s.infra != nil && s.infra.DB != nil {
		if providers, err := s.infra.DB.ListLLMProviders(); err == nil {
			for _, provider := range providers {
				add(provider.APIKeyEnv)
			}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (s *PluginService) pluginRuntimeBlockedEnvNames() []string {
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	if s != nil && s.infra != nil && s.infra.DB != nil {
		if providers, err := s.infra.DB.ListLLMProviders(); err == nil {
			for _, provider := range providers {
				add(provider.APIKeyEnv)
			}
		}
		if setting, ok, err := s.infra.DB.GetRuntimeSetting("websearch", "config"); err == nil && ok {
			var cfg config.WebSearchConfig
			if json.Unmarshal([]byte(setting.ValueJSON), &cfg) == nil {
				add(cfg.APIKeyEnv)
				add(cfg.Pipeline.Rerank.APIKeyEnv)
			}
		}
		if setting, ok, err := s.infra.DB.GetRuntimeSetting("memory", "config"); err == nil && ok {
			var cfg config.MemoryConfig
			if json.Unmarshal([]byte(setting.ValueJSON), &cfg) == nil {
				add(cfg.Extraction.Provider.APIKeyEnv)
			}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func resolvePluginStoreRoot(projectRoot, root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "data/plugins"
	}
	if filepath.IsAbs(root) || strings.TrimSpace(projectRoot) == "" {
		return root
	}
	return filepath.Join(projectRoot, root)
}

func (s *PluginService) providerClient(ctx context.Context, providerID string) (llm.Client, error) {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return nil, fmt.Errorf("provider storage is not configured")
	}
	record, err := s.infra.DB.GetLLMProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, fmt.Errorf("provider %q not found", providerID)
	}
	provider := record.LLMProvider
	if !provider.Enabled {
		return nil, fmt.Errorf("provider %q is disabled", providerID)
	}
	cfg := llm.ProviderConfig{
		ID:        provider.ID,
		PresetID:  provider.PresetID,
		Protocol:  provider.Protocol,
		BaseURL:   provider.BaseURL,
		APIKeyEnv: provider.APIKeyEnv,
	}
	resolved, err := llm.ResolveProviderConfig(cfg)
	if err != nil {
		return nil, err
	}
	client, err := llm.NewClient(resolved, s.infra.Logger)
	if err != nil {
		return nil, err
	}
	return tokenmeter.NewMeteredClient(tokenmeter.MeteredClientConfig{
		Inner:        client,
		Recorder:     s.infra.DB,
		ProviderID:   provider.ID,
		ProviderName: provider.Name,
		Protocol:     resolved.Protocol,
		Endpoint:     resolved.BaseURL,
		Logger:       s.infra.Logger,
	}), nil
}

func (s *PluginService) providerGatewayFallback(ctx context.Context) (string, string, bool, error) {
	if s == nil || s.agentRuntime == nil || s.infra == nil || s.infra.Config == nil {
		return "", "", false, nil
	}
	if s.infra.Config.Plugins.ProviderGateway.DefaultProviderID != "" || s.infra.Config.Plugins.ProviderGateway.DefaultModel != "" {
		return "", "", false, nil
	}
	active := s.agentRuntime.Active()
	if active == nil || strings.TrimSpace(active.WorkSummary.Provider.ID) == "" || strings.TrimSpace(active.WorkSummary.Model) == "" {
		return "", "", false, nil
	}
	_ = ctx
	return active.WorkSummary.Provider.ID, active.WorkSummary.Model, true, nil
}

func (s *PluginService) hostRPCHandlerForPlugin(boundPluginID string) plugin.JSONRPCHandler {
	return func(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
		return s.hostRPCHandler(ctx, boundPluginID, method, params)
	}
}

func (s *PluginService) hostRPCHandler(ctx context.Context, boundPluginID string, method string, params json.RawMessage) (json.RawMessage, error) {
	if s == nil || s.facadeBroker == nil {
		return nil, fmt.Errorf("plugin facade is not configured")
	}
	if strings.TrimSpace(boundPluginID) == "" {
		return nil, fmt.Errorf("plugin identity is not bound")
	}
	switch method {
	case "facade.call":
		var req struct {
			PluginID string          `json:"plugin_id"`
			Method   string          `json:"method"`
			Params   json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(defaultRawObject(params), &req); err != nil {
			return nil, fmt.Errorf("decode facade.call: %w", err)
		}
		if req.PluginID != "" && req.PluginID != boundPluginID {
			return nil, fmt.Errorf("plugin_id mismatch")
		}
		if strings.TrimSpace(req.Method) == "" {
			return nil, fmt.Errorf("facade method is required")
		}
		return s.facadeBroker.Call(ctx, boundPluginID, req.Method, req.Params)
	default:
		var req struct {
			PluginID string `json:"plugin_id"`
		}
		if err := json.Unmarshal(defaultRawObject(params), &req); err != nil {
			return nil, fmt.Errorf("decode host request: %w", err)
		}
		if req.PluginID != "" && req.PluginID != boundPluginID {
			return nil, fmt.Errorf("plugin_id mismatch")
		}
		return s.facadeBroker.Call(ctx, boundPluginID, method, params)
	}
}

func defaultRawObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}

func (s *PluginService) isPluginEnabled(ctx context.Context, pluginID string) bool {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return false
	}
	state, err := s.infra.DB.GetPluginEnabledState(ctx, pluginID)
	return err == nil && state != nil && state.Enabled
}

func (s *PluginService) loadEnabledProcessPluginsLocked(ctx context.Context) error {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return nil
	}
	states, err := s.infra.DB.ListPluginEnabledStates(ctx)
	if err != nil {
		return err
	}
	for _, state := range states {
		if !state.Enabled {
			continue
		}
		installation, err := s.installedVersion(ctx, state.PluginID, state.Version)
		if err != nil {
			return err
		}
		if installation == nil {
			continue
		}
		manifest, err := decodeInstalledManifest(*installation)
		if err != nil {
			return err
		}
		if !isProcessManifest(manifest) {
			continue
		}
		if err := s.ensureProcessPluginTrustCurrent(ctx, *installation, manifest); err != nil {
			s.infra.Logger.Warn("process plugin trust review required before start", "plugin_id", manifest.ID, "error", err)
			if s.infra.Config.Plugins.Runtime.FailClosedIfUnavailable {
				return fmt.Errorf("process plugin %s trust review required: %w", manifest.ID, err)
			}
			continue
		}
		if err := s.registerProcessPluginLocked(ctx, manifest); err != nil {
			s.infra.Logger.Warn("process plugin failed to start", "plugin_id", manifest.ID, "error", err)
			_ = s.recordRuntimeStatus(ctx, manifest, s.supervisor.Status(manifest.ID))
			if s.infra.Config.Plugins.Runtime.FailClosedIfUnavailable {
				return fmt.Errorf("process plugin %s failed to start: %w", manifest.ID, err)
			}
		}
	}
	return nil
}

func (s *PluginService) installedVersion(ctx context.Context, pluginID, version string) (*storage.PluginInstallation, error) {
	if strings.TrimSpace(version) != "" {
		return s.infra.DB.GetPluginInstallationVersion(ctx, pluginID, version)
	}
	return s.infra.DB.GetPluginInstallation(ctx, pluginID)
}

func decodeInstalledManifest(installation storage.PluginInstallation) (plugin.ManifestV2, error) {
	var manifest plugin.ManifestV2
	if err := json.Unmarshal([]byte(installation.ManifestJSON), &manifest); err != nil {
		return plugin.ManifestV2{}, fmt.Errorf("decode installed manifest: %w", err)
	}
	return manifest, nil
}

func isProcessManifest(manifest plugin.ManifestV2) bool {
	return manifest.Runtime.Kind == plugin.RuntimeManagedPythonProcess ||
		manifest.Runtime.Kind == plugin.RuntimePythonProcess ||
		manifest.Runtime.Kind == plugin.RuntimeProcess
}

func (s *PluginService) registerProcessPluginLocked(ctx context.Context, manifest plugin.ManifestV2) error {
	if s.host == nil {
		return fmt.Errorf("plugin host is not configured")
	}
	if s.supervisor == nil || s.facadeBroker == nil || s.providerGateway == nil {
		if err := s.ensureRuntimeLocked(); err != nil {
			return err
		}
	}
	if s.tools == nil || s.tools.Registry() == nil {
		return fmt.Errorf("tool registry is not configured")
	}
	if registeredVersion := s.registered[manifest.ID]; registeredVersion != "" && registeredVersion != manifest.Version {
		if err := s.unregisterProcessPluginLocked(ctx, manifest.ID); err != nil {
			return err
		}
	}
	s.facadeBroker.AddPlugin(manifest)
	if s.registered[manifest.ID] == manifest.Version {
		s.supervisor.AddPlugin(manifest)
		_, err := s.supervisor.EnsureReady(ctx, manifest.ID)
		_ = s.recordRuntimeStatus(ctx, manifest, s.supervisor.Status(manifest.ID))
		return err
	}
	if err := plugin.RegisterProcessPluginWithPolicy(ctx, manifest, s.host.Registry(), s.tools.Registry(), s.host.HookBus(), s.supervisor, s.processPluginPolicy()); err != nil {
		_ = s.unregisterProcessPluginLocked(ctx, manifest.ID)
		_ = s.recordRuntimeStatus(ctx, manifest, s.supervisor.Status(manifest.ID))
		return err
	}
	s.registered[manifest.ID] = manifest.Version
	if s.commands != nil {
		s.commands.RegisterPluginCommands(ctx, manifest)
	}
	return s.recordRuntimeStatus(ctx, manifest, s.supervisor.Status(manifest.ID))
}

func (s *PluginService) unregisterProcessPluginLocked(ctx context.Context, pluginID string) error {
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return nil
	}
	var stopErr error
	if s.supervisor != nil {
		stopErr = s.supervisor.Stop(ctx, pluginID)
		s.supervisor.RemovePlugin(pluginID)
	}
	if s.host != nil {
		if registry := s.host.Registry(); registry != nil {
			registry.Unregister(pluginID)
		}
		if bus := s.host.HookBus(); bus != nil {
			bus.UnregisterPlugin(pluginID)
		}
	}
	if s.tools != nil && s.tools.Registry() != nil {
		s.tools.Registry().UnregisterPlugin(pluginID)
	}
	if s.facadeBroker != nil {
		s.facadeBroker.RemovePlugin(pluginID)
	}
	if s.providerGateway != nil {
		s.providerGateway.RemovePlugin(pluginID)
	}
	if s.commands != nil {
		s.commands.UnregisterPluginCommands(pluginID)
	}
	delete(s.registered, pluginID)
	return stopErr
}

func (s *PluginService) recordRuntimeStatus(ctx context.Context, manifest plugin.ManifestV2, status plugin.RuntimeStatus) error {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return nil
	}
	var pid *int
	if status.Status == "running" && status.PID > 0 {
		value := status.PID
		pid = &value
	}
	return s.infra.DB.UpsertPluginRuntimeRecord(ctx, storage.PluginRuntimeRecord{
		PluginID:     manifest.ID,
		Version:      manifest.Version,
		RuntimeKind:  string(manifest.Runtime.Kind),
		Status:       status.Status,
		PID:          pid,
		LastError:    status.LastError,
		RestartCount: status.RestartCount,
	})
}

func (s *PluginService) requireAdminLocked() error {
	if s == nil || s.infra == nil || s.infra.Config == nil {
		return fmt.Errorf("plugin service is not configured")
	}
	if s.infra.DB == nil {
		return fmt.Errorf("plugin storage is not configured")
	}
	if !s.infra.Config.Plugins.Admin.Enabled {
		return plugin.ErrPluginAdminDisabled
	}
	return s.ensureRuntimeLocked()
}

func (s *PluginService) InstallLocal(ctx context.Context, req plugin.AdminPluginInstallRequest) (plugin.AdminPluginSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireAdminLocked(); err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	path := strings.TrimSpace(req.Path)
	if path == "" {
		return plugin.AdminPluginSummary{}, fmt.Errorf("path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	var result plugin.InstallResult
	if info.IsDir() {
		result, err = s.installer.InstallFromDirectory(ctx, path)
	} else {
		result, err = s.installer.InstallFromZip(ctx, path)
	}
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	installedBy := strings.TrimSpace(req.InstalledBy)
	if installedBy == "" {
		installedBy = "admin"
	}
	if err := s.infra.DB.UpsertPluginInstallation(ctx, storage.PluginInstallation{
		ID:              result.PluginID + "@" + result.Version,
		PluginID:        result.PluginID,
		Version:         result.Version,
		Name:            result.Name,
		ManifestJSON:    result.ManifestJSON,
		SourceType:      result.SourceType,
		SourceRef:       result.SourceRef,
		PackageDigest:   result.PackageDigest,
		ManifestDigest:  result.ManifestDigest,
		SignatureStatus: result.SignatureStatus,
		PublisherID:     result.PublisherID,
		InstalledBy:     installedBy,
		StorePath:       result.StorePath,
	}); err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	return s.summaryForInstallation(ctx, storage.PluginInstallation{
		PluginID:        result.PluginID,
		Version:         result.Version,
		Name:            result.Name,
		ManifestJSON:    result.ManifestJSON,
		SourceType:      result.SourceType,
		SourceRef:       result.SourceRef,
		PackageDigest:   result.PackageDigest,
		ManifestDigest:  result.ManifestDigest,
		SignatureStatus: result.SignatureStatus,
		PublisherID:     result.PublisherID,
		InstalledBy:     installedBy,
		StorePath:       result.StorePath,
	}), nil
}

func (s *PluginService) InstallGitHubRelease(ctx context.Context, req plugin.AdminGitHubInstallRequest) (plugin.AdminPluginSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireAdminLocked(); err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	result, err := s.installer.InstallFromGitHubRelease(ctx, req.Owner, req.Repo, req.Tag, req.Asset)
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	installedBy := strings.TrimSpace(req.InstalledBy)
	if installedBy == "" {
		installedBy = "admin"
	}
	record := storage.PluginInstallation{
		ID:              result.PluginID + "@" + result.Version,
		PluginID:        result.PluginID,
		Version:         result.Version,
		Name:            result.Name,
		ManifestJSON:    result.ManifestJSON,
		SourceType:      result.SourceType,
		SourceRef:       result.SourceRef,
		PackageDigest:   result.PackageDigest,
		ManifestDigest:  result.ManifestDigest,
		SignatureStatus: result.SignatureStatus,
		PublisherID:     result.PublisherID,
		InstalledBy:     installedBy,
		StorePath:       result.StorePath,
	}
	if err := s.infra.DB.UpsertPluginInstallation(ctx, record); err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	return s.summaryForInstallation(ctx, record), nil
}

func (s *PluginService) ListPlugins(ctx context.Context) ([]plugin.AdminPluginSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireAdminLocked(); err != nil {
		return nil, err
	}
	installations, err := s.infra.DB.ListPluginInstallations(ctx)
	if err != nil {
		return nil, err
	}
	summaries := make([]plugin.AdminPluginSummary, 0, len(installations))
	for _, installation := range installations {
		summaries = append(summaries, s.summaryForInstallation(ctx, installation))
	}
	return summaries, nil
}

func (s *PluginService) GetPlugin(ctx context.Context, pluginID string) (plugin.AdminPluginSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireAdminLocked(); err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	installation, err := s.activeOrLatestInstallation(ctx, pluginID)
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	if installation == nil {
		return plugin.AdminPluginSummary{}, plugin.ErrPluginNotFound
	}
	return s.summaryForInstallation(ctx, *installation), nil
}

func (s *PluginService) GetPluginSettings(ctx context.Context, pluginID string) (plugin.AdminPluginSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireAdminLocked(); err != nil {
		return plugin.AdminPluginSettings{}, err
	}
	installation, err := s.activeOrLatestInstallation(ctx, pluginID)
	if err != nil {
		return plugin.AdminPluginSettings{}, err
	}
	if installation == nil {
		return plugin.AdminPluginSettings{}, plugin.ErrPluginNotFound
	}
	value, found, err := s.infra.DB.PluginKVGet(ctx, pluginID, pluginSettingsKVKey)
	if err != nil {
		return plugin.AdminPluginSettings{}, err
	}
	raw := json.RawMessage("{}")
	if found {
		raw = json.RawMessage(value)
	}
	if !json.Valid(raw) {
		raw = json.RawMessage("{}")
		found = false
	}
	return plugin.AdminPluginSettings{
		PluginID: pluginID,
		Key:      pluginSettingsKVKey,
		Found:    found,
		Value:    raw,
	}, nil
}

func (s *PluginService) UpdatePluginSettings(ctx context.Context, pluginID string, req plugin.AdminPluginSettingsUpdateRequest) (plugin.AdminPluginSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireAdminLocked(); err != nil {
		return plugin.AdminPluginSettings{}, err
	}
	installation, err := s.activeOrLatestInstallation(ctx, pluginID)
	if err != nil {
		return plugin.AdminPluginSettings{}, err
	}
	if installation == nil {
		return plugin.AdminPluginSettings{}, plugin.ErrPluginNotFound
	}
	value := req.Value
	if len(value) == 0 {
		value = json.RawMessage("{}")
	}
	if !json.Valid(value) {
		return plugin.AdminPluginSettings{}, fmt.Errorf("settings value must be valid JSON")
	}
	manifest, err := decodeInstalledManifest(*installation)
	if err != nil {
		return plugin.AdminPluginSettings{}, err
	}
	if manifest.Settings != nil && manifest.Settings.HasSchema() {
		if manifest.Settings.EffectiveKey() != pluginSettingsKVKey {
			return plugin.AdminPluginSettings{}, fmt.Errorf("plugin settings key %q is unsupported", manifest.Settings.EffectiveKey())
		}
		value, err = manifest.Settings.Schema.CleanValue(value)
		if err != nil {
			return plugin.AdminPluginSettings{}, err
		}
	}
	if err := s.infra.DB.PluginKVSet(ctx, pluginID, pluginSettingsKVKey, string(value)); err != nil {
		return plugin.AdminPluginSettings{}, err
	}
	return plugin.AdminPluginSettings{
		PluginID: pluginID,
		Key:      pluginSettingsKVKey,
		Found:    true,
		Value:    append(json.RawMessage(nil), value...),
	}, nil
}

func (s *PluginService) activeOrLatestInstallation(ctx context.Context, pluginID string) (*storage.PluginInstallation, error) {
	if state, err := s.infra.DB.GetPluginEnabledState(ctx, pluginID); err != nil {
		return nil, err
	} else if state != nil && state.Enabled && strings.TrimSpace(state.Version) != "" {
		if installation, err := s.infra.DB.GetPluginInstallationVersion(ctx, pluginID, state.Version); err != nil {
			return nil, err
		} else if installation != nil {
			return installation, nil
		}
	}
	return s.infra.DB.GetPluginInstallation(ctx, pluginID)
}

func (s *PluginService) GetPluginVersion(ctx context.Context, pluginID, version string) (plugin.AdminPluginSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireAdminLocked(); err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	installation, err := s.installedVersion(ctx, pluginID, version)
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	if installation == nil {
		return plugin.AdminPluginSummary{}, plugin.ErrPluginNotFound
	}
	return s.summaryForInstallation(ctx, *installation), nil
}

func (s *PluginService) GetPluginVersionForGrant(ctx context.Context, pluginID, version, userGrantJSON string) (plugin.AdminPluginSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireAdminLocked(); err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	installation, err := s.installedVersion(ctx, pluginID, version)
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	if installation == nil {
		return plugin.AdminPluginSummary{}, plugin.ErrPluginNotFound
	}
	manifest, err := decodeInstalledManifest(*installation)
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	grant, err := plugin.ValidateUserGrantForManifest(userGrantJSON, manifest)
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	return s.summaryForInstallationWithGrant(ctx, *installation, &grant), nil
}

func (s *PluginService) EnablePlugin(ctx context.Context, pluginID string, req plugin.AdminPluginEnableRequest) (plugin.AdminPluginSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireAdminLocked(); err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	installation, err := s.installedVersion(ctx, pluginID, req.Version)
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	if installation == nil {
		return plugin.AdminPluginSummary{}, plugin.ErrPluginNotFound
	}
	manifest, err := decodeInstalledManifest(*installation)
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	if err := s.ensurePluginInstallAllowed(*installation); err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	grant := strings.TrimSpace(req.UserGrantJSON)
	if grant == "" {
		grant = "{}"
	}
	grantValue, err := plugin.ValidateUserGrantForManifest(grant, manifest)
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	review, err := s.trustReviewForInstallation(ctx, *installation, manifest, &grantValue)
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	acceptedAck, err := s.validatePluginTrustAcknowledgement(review, req.TrustAcknowledgement)
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	previousState, err := s.infra.DB.GetPluginEnabledState(ctx, manifest.ID)
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	trustAcceptance, err := s.pluginTrustAcceptanceRecord(*installation, manifest, grantValue, review, acceptedAck)
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	if err := s.infra.DB.SetPluginEnabledWithTrust(ctx, manifest.ID, manifest.Version, true, grant, trustAcceptance); err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	if s.infra.Config.Plugins.Enabled && isProcessManifest(manifest) {
		if err := s.registerProcessPluginLocked(ctx, manifest); err != nil {
			s.restorePluginEnabledState(ctx, manifest.ID, previousState, manifest.Version)
			return s.summaryForInstallation(ctx, *installation), err
		}
	}
	if err := s.recordPluginTrustAcceptanceHistory(ctx, *installation, manifest); err != nil {
		_ = s.unregisterProcessPluginLocked(ctx, manifest.ID)
		s.restorePluginEnabledState(ctx, manifest.ID, previousState, manifest.Version)
		return s.summaryForInstallation(ctx, *installation), err
	}
	return s.summaryForInstallation(ctx, *installation), nil
}

func (s *PluginService) DisablePlugin(ctx context.Context, pluginID string) (plugin.AdminPluginSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireAdminLocked(); err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	installation, err := s.activeOrLatestInstallation(ctx, pluginID)
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	if installation == nil {
		return plugin.AdminPluginSummary{}, plugin.ErrPluginNotFound
	}
	if err := s.infra.DB.SetPluginEnabled(ctx, pluginID, installation.Version, false, "{}"); err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	manifest, _ := decodeInstalledManifest(*installation)
	_ = s.unregisterProcessPluginLocked(ctx, pluginID)
	_ = s.recordRuntimeStatus(ctx, manifest, plugin.RuntimeStatus{PluginID: pluginID, Status: "stopped"})
	return s.summaryForInstallation(ctx, *installation), nil
}

func (s *PluginService) RestartPlugin(ctx context.Context, pluginID string) (plugin.AdminPluginSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireAdminLocked(); err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	installation, err := s.activeOrLatestInstallation(ctx, pluginID)
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	if installation == nil {
		return plugin.AdminPluginSummary{}, plugin.ErrPluginNotFound
	}
	manifest, err := decodeInstalledManifest(*installation)
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	if s.infra.Config.Plugins.Enabled && isProcessManifest(manifest) {
		if err := s.ensureProcessPluginTrustCurrent(ctx, *installation, manifest); err != nil {
			return s.summaryForInstallation(ctx, *installation), err
		}
	}
	if err := s.unregisterProcessPluginLocked(ctx, pluginID); err != nil {
		return s.summaryForInstallation(ctx, *installation), err
	}
	if s.infra.Config.Plugins.Enabled && isProcessManifest(manifest) {
		if err := s.registerProcessPluginLocked(ctx, manifest); err != nil {
			return s.summaryForInstallation(ctx, *installation), err
		}
	}
	return s.summaryForInstallation(ctx, *installation), nil
}

func (s *PluginService) DeletePlugin(ctx context.Context, pluginID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireAdminLocked(); err != nil {
		return err
	}
	installations, err := s.infra.DB.ListPluginInstallations(ctx)
	if err != nil {
		return err
	}
	packageDirs := make([]string, 0)
	for _, installation := range installations {
		if installation.PluginID != pluginID {
			continue
		}
		packageDir, err := s.store.PackageDir(installation.PluginID, installation.Version)
		if err != nil {
			return err
		}
		packageDirs = append(packageDirs, packageDir)
	}
	_ = s.unregisterProcessPluginLocked(ctx, pluginID)
	if err := s.infra.DB.SetPluginEnabled(ctx, pluginID, "0.0.0", false, "{}"); err != nil {
		state, stateErr := s.infra.DB.GetPluginEnabledState(ctx, pluginID)
		if stateErr == nil && state == nil {
			err = nil
		}
		if err != nil {
			return err
		}
	}
	for _, packageDir := range packageDirs {
		if err := os.RemoveAll(packageDir); err != nil {
			return fmt.Errorf("delete plugin package %s: %w", packageDir, err)
		}
	}
	return s.infra.DB.DeletePluginInstallation(ctx, pluginID)
}

func (s *PluginService) ensurePluginInstallAllowed(installation storage.PluginInstallation) error {
	if s == nil || s.infra == nil || s.infra.Config == nil {
		return nil
	}
	cfg := s.infra.Config.Plugins.Installer
	if pluginDigestBlocked(installation.PackageDigest, cfg.BlockedPackageDigests) {
		return fmt.Errorf("blocked package digest: %s", installation.PackageDigest)
	}
	if pluginDigestBlocked(installation.ManifestDigest, cfg.BlockedManifestDigests) {
		return fmt.Errorf("blocked manifest digest: %s", installation.ManifestDigest)
	}
	if pluginPublisherBlocked(installation.PublisherID, cfg.BlockedPublishers) {
		return fmt.Errorf("blocked publisher: %s", installation.PublisherID)
	}
	return nil
}

func (s *PluginService) pluginInstallBlocked(installation storage.PluginInstallation) bool {
	return s.ensurePluginInstallAllowed(installation) != nil
}

func (s *PluginService) ensureProcessPluginTrustCurrent(ctx context.Context, installation storage.PluginInstallation, manifest plugin.ManifestV2) error {
	review, err := s.trustReviewForInstallation(ctx, installation, manifest, nil)
	if err != nil {
		return err
	}
	if !review.Required {
		return nil
	}
	return fmt.Errorf("plugin trust review required before process activation: %s", strings.Join(review.Reasons, ","))
}

func (s *PluginService) trustReviewForInstallation(ctx context.Context, installation storage.PluginInstallation, manifest plugin.ManifestV2, proposedGrant *plugin.UserGrant) (plugin.PluginTrustReview, error) {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return plugin.PluginTrustReview{}, nil
	}
	state, err := s.infra.DB.GetPluginEnabledState(ctx, installation.PluginID)
	if err != nil {
		return plugin.PluginTrustReview{}, err
	}
	if state == nil || !state.Enabled || strings.TrimSpace(state.Version) == "" {
		return plugin.PluginTrustReview{}, nil
	}
	target := pluginTrustSubject(installation, manifest)
	review := plugin.PluginTrustReview{}
	if state.Version != installation.Version {
		current, err := s.infra.DB.GetPluginInstallationVersion(ctx, installation.PluginID, state.Version)
		if err != nil {
			return plugin.PluginTrustReview{}, err
		}
		if current != nil {
			currentManifest, err := decodeInstalledManifest(*current)
			if err != nil {
				return plugin.PluginTrustReview{}, err
			}
			review = plugin.BuildPluginTrustReview(pluginTrustSubject(*current, currentManifest), target)
		}
	}
	grantReasons, err := pluginUserGrantExpansionReasons(*state, manifest, proposedGrant)
	if err != nil {
		return plugin.PluginTrustReview{}, err
	}
	for _, reason := range grantReasons {
		review = pluginTrustReviewWithAdditionalReason(target, review, reason)
	}
	activeHookReasons, err := s.pluginActiveHookAllowedReasonsSinceLastAcceptance(ctx, installation.PluginID, state, manifest)
	if err != nil {
		return plugin.PluginTrustReview{}, err
	}
	for _, reason := range activeHookReasons {
		review = pluginTrustReviewWithAdditionalReason(target, review, reason)
	}
	dependencyLockDigest, err := s.pluginDependencyLockDigest(manifest)
	if err != nil {
		return plugin.PluginTrustReview{}, err
	}
	dependencyLockChanged, err := s.pluginDependencyLockChangedSinceLastAcceptance(ctx, installation.PluginID, state, dependencyLockDigest)
	if err != nil {
		return plugin.PluginTrustReview{}, err
	}
	if dependencyLockChanged {
		review = pluginTrustReviewWithAdditionalReason(target, review, "dependency_lock_changed")
	}
	hostPolicyChanged, err := s.pluginHostPolicyChangedSinceLastAcceptance(ctx, installation.PluginID, state)
	if err != nil {
		return plugin.PluginTrustReview{}, err
	}
	if hostPolicyChanged && len(activeHookReasons) == 0 {
		review = pluginTrustReviewWithAdditionalReason(target, review, "host_policy_changed")
		return pluginTrustReviewWithTargetBinding(review, proposedGrant, dependencyLockDigest)
	}
	return pluginTrustReviewWithTargetBinding(review, proposedGrant, dependencyLockDigest)
}

func pluginUserGrantExpansionReasons(state storage.PluginEnabledState, manifest plugin.ManifestV2, proposedGrant *plugin.UserGrant) ([]string, error) {
	if proposedGrant == nil || !state.Enabled || strings.TrimSpace(state.Version) != manifest.Version {
		return nil, nil
	}
	currentGrant, err := plugin.ValidateUserGrantForManifest(state.UserGrantJSON, manifest)
	if err != nil {
		return nil, err
	}
	currentCapabilities := capabilitySet(currentGrant.Capabilities)
	var reasons []string
	for _, capability := range proposedGrant.Capabilities {
		if _, ok := currentCapabilities[capability]; ok {
			continue
		}
		reasons = append(reasons, "user_grant_capability_added:"+string(capability))
	}
	sort.Strings(reasons)
	return reasons, nil
}

func pluginTrustReviewWithAdditionalReason(subject plugin.PluginTrustSubject, review plugin.PluginTrustReview, reason string) plugin.PluginTrustReview {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return review
	}
	reasons := append([]string(nil), review.Reasons...)
	for _, existing := range reasons {
		if existing == reason {
			return review
		}
	}
	reasons = append(reasons, reason)
	ack := plugin.BuildPluginTrustAcknowledgement(subject, reasons)
	return plugin.PluginTrustReview{Required: true, Reasons: reasons, Acknowledgement: &ack}
}

func pluginTrustReviewWithTargetBinding(review plugin.PluginTrustReview, proposedGrant *plugin.UserGrant, dependencyLockDigest string) (plugin.PluginTrustReview, error) {
	if !review.Required || review.Acknowledgement == nil {
		return review, nil
	}
	ack := *review.Acknowledgement
	if proposedGrant != nil {
		hash, err := plugin.HashUserGrant(*proposedGrant)
		if err != nil {
			return plugin.PluginTrustReview{}, err
		}
		ack.TargetUserGrantHash = hash
	}
	ack.DependencyLockDigest = dependencyLockDigest
	review.Acknowledgement = &ack
	return review, nil
}

func (s *PluginService) pluginDependencyLockDigest(manifest plugin.ManifestV2) (string, error) {
	if s == nil || s.store == nil {
		return "", nil
	}
	return plugin.DependencyLockDigest(s.store, manifest)
}

func (s *PluginService) pluginDependencyLockChangedSinceLastAcceptance(ctx context.Context, pluginID string, state *storage.PluginEnabledState, currentDigest string) (bool, error) {
	if state == nil || !state.Enabled || strings.TrimSpace(state.TrustAcknowledgementHash) == "" {
		return false, nil
	}
	record, err := s.pluginTrustAcceptanceHistoryForState(ctx, pluginID, state)
	if err != nil {
		return false, err
	}
	if record == nil {
		return strings.TrimSpace(currentDigest) != "", nil
	}
	return strings.TrimSpace(record.DependencyLockDigest) != strings.TrimSpace(currentDigest), nil
}

func (s *PluginService) pluginHostPolicyChangedSinceLastAcceptance(ctx context.Context, pluginID string, state *storage.PluginEnabledState) (bool, error) {
	if state == nil || !state.Enabled || strings.TrimSpace(state.TrustAcknowledgementHash) == "" {
		return false, nil
	}
	currentFingerprint, err := s.pluginHostPolicyFingerprintForState(state)
	if err != nil {
		return false, err
	}
	record, err := s.pluginTrustAcceptanceHistoryForState(ctx, pluginID, state)
	if err != nil {
		return false, err
	}
	if record == nil {
		return true, nil
	}
	previousFingerprint := strings.TrimSpace(record.HostPolicyFingerprint)
	if previousFingerprint == "" {
		return true, nil
	}
	return previousFingerprint != currentFingerprint, nil
}

func (s *PluginService) pluginActiveHookAllowedReasonsSinceLastAcceptance(ctx context.Context, pluginID string, state *storage.PluginEnabledState, manifest plugin.ManifestV2) ([]string, error) {
	if state == nil || !state.Enabled || strings.TrimSpace(state.TrustAcknowledgementHash) == "" || !s.processPluginPolicy().AllowActiveHooks {
		return nil, nil
	}
	reasons := pluginActiveHookAllowedReasons(manifest)
	if len(reasons) == 0 {
		return nil, nil
	}
	record, err := s.pluginTrustAcceptanceHistoryForState(ctx, pluginID, state)
	if err != nil {
		return nil, err
	}
	if record == nil || strings.TrimSpace(record.HostPolicyFingerprint) == "" {
		return nil, nil
	}
	disabledFingerprint, err := s.pluginHostPolicyFingerprintForStateWithActiveHooks(state, false)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(record.HostPolicyFingerprint) != disabledFingerprint {
		return nil, nil
	}
	return reasons, nil
}

func (s *PluginService) pluginTrustAcceptanceHistoryForState(ctx context.Context, pluginID string, state *storage.PluginEnabledState) (*storage.PluginTrustAcceptanceHistoryRecord, error) {
	if state == nil {
		return nil, nil
	}
	history, err := s.infra.DB.ListPluginTrustAcceptanceHistory(ctx, pluginID, 25)
	if err != nil {
		return nil, err
	}
	for _, record := range history {
		if record.Version != state.Version || record.AcknowledgementHash != state.TrustAcknowledgementHash {
			continue
		}
		matched := record
		return &matched, nil
	}
	return nil, nil
}

func pluginActiveHookAllowedReasons(manifest plugin.ManifestV2) []string {
	seen := map[string]struct{}{}
	var reasons []string
	for _, hook := range manifest.Hooks {
		if !hookModeIsActive(hook.Mode) {
			continue
		}
		reason := "active_hook_allowed:" + string(hook.Name) + ":" + string(hook.Mode)
		if _, ok := seen[reason]; ok {
			continue
		}
		seen[reason] = struct{}{}
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	return reasons
}

func (s *PluginService) issuePluginTrustReviewAcknowledgement(review plugin.PluginTrustReview) plugin.PluginTrustReview {
	if !review.Required || review.Acknowledgement == nil {
		return review
	}
	nonce, err := newPluginTrustAcknowledgementNonce()
	if err != nil {
		if s != nil && s.infra != nil && s.infra.Logger != nil {
			s.infra.Logger.Warn("plugin trust acknowledgement nonce unavailable", "error", err)
		}
		return review
	}
	ack := *review.Acknowledgement
	ack.AckNonce = nonce
	ack.AckIssuedAt = time.Now().UTC().Format(time.RFC3339Nano)
	ack.UserAction = plugin.TrustAcknowledgementActionEnablePlugin
	if s.pendingTrustAcks == nil {
		s.pendingTrustAcks = map[string]plugin.PluginTrustAcknowledgement{}
	}
	s.pendingTrustAcks[nonce] = ack
	review.Acknowledgement = &ack
	return review
}

func newPluginTrustAcknowledgementNonce() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func (s *PluginService) validatePluginTrustAcknowledgement(review plugin.PluginTrustReview, ack *plugin.PluginTrustAcknowledgement) (*plugin.PluginTrustAcknowledgement, error) {
	if !review.Required {
		if ack != nil {
			return nil, fmt.Errorf("plugin trust review acknowledgement is not required")
		}
		return nil, nil
	}
	if ack == nil {
		return nil, plugin.ValidatePluginTrustAcknowledgement(review, ack)
	}
	nonce := strings.TrimSpace(ack.AckNonce)
	if nonce == "" {
		return nil, fmt.Errorf("plugin trust acknowledgement nonce is required")
	}
	if strings.TrimSpace(ack.UserAction) != plugin.TrustAcknowledgementActionEnablePlugin {
		return nil, fmt.Errorf("plugin trust acknowledgement user action does not match enable_plugin")
	}
	issued, ok := s.pendingTrustAcks[nonce]
	if !ok {
		return nil, fmt.Errorf("plugin trust acknowledgement was not issued or has expired")
	}
	if review.Acknowledgement == nil {
		return nil, fmt.Errorf("plugin trust review acknowledgement does not match target")
	}
	expected := *review.Acknowledgement
	expected.AckNonce = issued.AckNonce
	expected.AckIssuedAt = issued.AckIssuedAt
	expected.UserAction = issued.UserAction
	expectedReview := review
	expectedReview.Acknowledgement = &expected
	if err := plugin.ValidatePluginTrustAcknowledgement(expectedReview, &issued); err != nil {
		return nil, fmt.Errorf("plugin trust acknowledgement was not issued for current review")
	}
	if err := plugin.ValidatePluginTrustAcknowledgement(expectedReview, ack); err != nil {
		return nil, err
	}
	delete(s.pendingTrustAcks, nonce)
	accepted := *ack
	return &accepted, nil
}

func pluginTrustSubject(installation storage.PluginInstallation, manifest plugin.ManifestV2) plugin.PluginTrustSubject {
	return plugin.PluginTrustSubject{
		PluginID:                installation.PluginID,
		Version:                 installation.Version,
		PackageDigest:           installation.PackageDigest,
		ManifestDigest:          installation.ManifestDigest,
		SignatureStatus:         installation.SignatureStatus,
		PublisherID:             installation.PublisherID,
		RuntimeKind:             manifest.Runtime.Kind,
		Capabilities:            append([]plugin.Capability(nil), manifest.Access.Capabilities...),
		Hooks:                   append([]plugin.HookSpec(nil), manifest.Hooks...),
		DefaultToolExposure:     plugin.ExposureWork,
		DefaultInvocationPolicy: plugin.InvocationAsk,
	}
}

func (s *PluginService) pluginTrustAcceptanceRecord(installation storage.PluginInstallation, manifest plugin.ManifestV2, grant plugin.UserGrant, review plugin.PluginTrustReview, acceptedAck *plugin.PluginTrustAcknowledgement) (storage.PluginTrustAcceptanceRecord, error) {
	subject := pluginTrustSubject(installation, manifest)
	ack := pluginTrustAcknowledgementForSubject(subject, review.Reasons)
	grantHash, err := plugin.HashUserGrant(grant)
	if err != nil {
		return storage.PluginTrustAcceptanceRecord{}, err
	}
	ack.TargetUserGrantHash = grantHash
	dependencyLockDigest, err := s.pluginDependencyLockDigest(manifest)
	if err != nil {
		return storage.PluginTrustAcceptanceRecord{}, err
	}
	ack.DependencyLockDigest = dependencyLockDigest
	if review.Required {
		if acceptedAck == nil {
			return storage.PluginTrustAcceptanceRecord{}, fmt.Errorf("plugin trust acknowledgement is required")
		}
		ack = *acceptedAck
	}
	hash, err := plugin.HashPluginTrustAcknowledgement(ack)
	if err != nil {
		return storage.PluginTrustAcceptanceRecord{}, err
	}
	reviewReasons := append([]string(nil), review.Reasons...)
	if reviewReasons == nil {
		reviewReasons = []string{}
	}
	reasons, err := json.Marshal(reviewReasons)
	if err != nil {
		return storage.PluginTrustAcceptanceRecord{}, err
	}
	return storage.PluginTrustAcceptanceRecord{
		TrustLevel:              string(plugin.TrustLevelForInstall(installation.SignatureStatus, installation.SourceType, true, false)),
		AcknowledgementHash:     hash,
		ReviewReasonsJSON:       string(reasons),
		DefaultToolExposure:     string(subject.DefaultToolExposure),
		DefaultInvocationPolicy: string(subject.DefaultInvocationPolicy),
	}, nil
}

func pluginTrustAcknowledgementForSubject(subject plugin.PluginTrustSubject, reasons []string) plugin.PluginTrustAcknowledgement {
	return plugin.BuildPluginTrustAcknowledgement(subject, reasons)
}

func (s *PluginService) restorePluginEnabledState(ctx context.Context, pluginID string, previous *storage.PluginEnabledState, fallbackVersion string) {
	if previous == nil {
		_ = s.infra.DB.SetPluginEnabled(ctx, pluginID, fallbackVersion, false, "{}")
		return
	}
	trust := storage.PluginTrustAcceptanceRecord{
		TrustLevel:              previous.TrustLevel,
		AcceptedAt:              previous.TrustAcceptedAt,
		AcknowledgementHash:     previous.TrustAcknowledgementHash,
		ReviewReasonsJSON:       previous.TrustReviewReasonsJSON,
		DefaultToolExposure:     previous.DefaultToolExposure,
		DefaultInvocationPolicy: previous.DefaultInvocationPolicy,
	}
	_ = s.infra.DB.SetPluginEnabledWithTrust(ctx, previous.PluginID, previous.Version, previous.Enabled, previous.UserGrantJSON, trust)
}

func (s *PluginService) recordPluginTrustAcceptanceHistory(ctx context.Context, installation storage.PluginInstallation, manifest plugin.ManifestV2) error {
	state, err := s.infra.DB.GetPluginEnabledState(ctx, installation.PluginID)
	if err != nil {
		return err
	}
	if state == nil || !state.Enabled || state.Version != installation.Version {
		return fmt.Errorf("plugin trust acceptance state is not active for %s@%s", installation.PluginID, installation.Version)
	}
	if strings.TrimSpace(state.TrustAcknowledgementHash) == "" {
		return fmt.Errorf("plugin trust acceptance hash is required")
	}
	grant, err := plugin.ValidateUserGrantForManifest(state.UserGrantJSON, manifest)
	if err != nil {
		return err
	}
	grantHash, err := plugin.HashUserGrant(grant)
	if err != nil {
		return err
	}
	policyFingerprint, err := s.pluginHostPolicyFingerprintForState(state)
	if err != nil {
		return err
	}
	dependencyLockDigest, err := s.pluginDependencyLockDigest(manifest)
	if err != nil {
		return err
	}
	return s.infra.DB.RecordPluginTrustAcceptance(ctx, storage.PluginTrustAcceptanceHistoryRecord{
		PluginID:                installation.PluginID,
		Version:                 installation.Version,
		TrustLevel:              state.TrustLevel,
		AcceptedAt:              state.TrustAcceptedAt,
		AcknowledgementHash:     state.TrustAcknowledgementHash,
		ReviewReasonsJSON:       state.TrustReviewReasonsJSON,
		DefaultToolExposure:     state.DefaultToolExposure,
		DefaultInvocationPolicy: state.DefaultInvocationPolicy,
		UserGrantHash:           grantHash,
		HostPolicyFingerprint:   policyFingerprint,
		DependencyLockDigest:    dependencyLockDigest,
		PackageDigest:           installation.PackageDigest,
		ManifestDigest:          installation.ManifestDigest,
		SignatureStatus:         installation.SignatureStatus,
		PublisherID:             installation.PublisherID,
		SourceType:              installation.SourceType,
	})
}

func (s *PluginService) pluginHostPolicyFingerprintForState(state *storage.PluginEnabledState) (string, error) {
	return s.pluginHostPolicyFingerprintForStateWithActiveHooks(state, s.processPluginPolicy().AllowActiveHooks)
}

func (s *PluginService) pluginHostPolicyFingerprintForStateWithActiveHooks(state *storage.PluginEnabledState, allowActiveHooks bool) (string, error) {
	defaultExposure := plugin.ToolExposure(state.DefaultToolExposure)
	if strings.TrimSpace(string(defaultExposure)) == "" {
		defaultExposure = plugin.ExposureWork
	}
	defaultInvocation := plugin.InvocationPolicy(state.DefaultInvocationPolicy)
	if strings.TrimSpace(string(defaultInvocation)) == "" {
		defaultInvocation = plugin.InvocationAsk
	}
	return plugin.FingerprintPluginHostPolicy(plugin.PluginHostPolicyFingerprintSubject{
		FacadePolicy:            s.pluginFacadeHostPolicy(),
		AllowActiveHooks:        allowActiveHooks,
		DefaultToolExposure:     defaultExposure,
		DefaultInvocationPolicy: defaultInvocation,
	})
}

func pluginDigestBlocked(digest string, blocked []string) bool {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return false
	}
	for _, candidate := range blocked {
		if strings.TrimSpace(candidate) == digest {
			return true
		}
	}
	return false
}

func pluginPublisherBlocked(publisherID string, blocked []string) bool {
	publisherID = strings.TrimSpace(publisherID)
	if publisherID == "" {
		return false
	}
	for _, candidate := range blocked {
		if strings.TrimSpace(candidate) == publisherID {
			return true
		}
	}
	return false
}

func (s *PluginService) PluginLogs(ctx context.Context, pluginID string) (plugin.AdminPluginLogs, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireAdminLocked(); err != nil {
		return plugin.AdminPluginLogs{}, err
	}
	installation, err := s.infra.DB.GetPluginInstallation(ctx, pluginID)
	if err != nil {
		return plugin.AdminPluginLogs{}, err
	}
	if installation == nil {
		return plugin.AdminPluginLogs{}, plugin.ErrPluginNotFound
	}
	status := plugin.RuntimeStatus{PluginID: pluginID}
	if s.supervisor != nil {
		status = s.supervisor.Status(pluginID)
	}
	return plugin.AdminPluginLogs{PluginID: pluginID, StderrTail: status.StderrTail}, nil
}

func (s *PluginService) PluginDiagnostics(ctx context.Context) (plugin.AdminPluginDiagnostics, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireAdminLocked(); err != nil {
		return plugin.AdminPluginDiagnostics{}, err
	}
	if err := s.ensureRuntimeLocked(); err != nil {
		return plugin.AdminPluginDiagnostics{}, err
	}
	installations, err := s.infra.DB.ListPluginInstallations(ctx)
	if err != nil {
		return plugin.AdminPluginDiagnostics{}, err
	}
	summaries := make([]plugin.AdminPluginSummary, 0, len(installations))
	for _, installation := range installations {
		summaries = append(summaries, s.summaryForInstallation(ctx, installation))
	}
	checks := []plugin.AdminPluginDiagnosticCheck{
		s.pythonToolchainDiagnosticCheck(ctx, summaries),
		s.pythonEnvironmentDiagnosticCheck(ctx, summaries),
		s.processGuardDiagnosticCheck(),
		uvProjectDiagnosticCheck(summaries),
		pluginLogsDiagnosticCheck(summaries),
		pluginRepairDiagnosticCheck(summaries),
	}
	return plugin.AdminPluginDiagnostics{Status: diagnosticOverallStatus(checks), Checks: checks}, nil
}

func (s *PluginService) pythonToolchainDiagnosticCheck(ctx context.Context, summaries []plugin.AdminPluginSummary) plugin.AdminPluginDiagnosticCheck {
	managedCount := managedPythonSummaryCount(summaries)
	check := plugin.AdminPluginDiagnosticCheck{
		ID:     "python_toolchain",
		Label:  "Python Toolchain",
		Status: "ok",
		Details: map[string]any{
			"managed_python_plugins": managedCount,
		},
	}
	if s == nil || s.infra == nil {
		check.Status = "error"
		check.Message = "plugin service is not configured"
		return check
	}
	effective, err := configcenter.NewService(s.infra.Config, s.infra.DB).BuildEffective(ctx)
	if err != nil {
		check.Status = "error"
		check.Message = "load python toolchain config: " + err.Error()
		return check
	}
	cfg := effective.PythonToolchain
	check.Details["enabled"] = cfg.Enabled
	check.Details["python_executable"] = cfg.PythonExecutable
	check.Details["uv_executable"] = cfg.UVExecutable
	if !cfg.Enabled {
		if managedCount == 0 {
			check.Message = "no managed Python plugin requires a configured toolchain"
			return check
		}
		check.Status = "warning"
		check.Message = "python toolchain is not configured"
		return check
	}
	probe, err := pytoolchain.NewManager(cfg).Probe(ctx)
	if err != nil {
		check.Status = "error"
		check.Message = "python toolchain probe failed: " + err.Error()
		return check
	}
	check.Details["python_version"] = probe.Python.Version
	check.Details["python_architecture"] = probe.Python.Architecture
	check.Details["uv_version"] = probe.UV.Version
	check.Message = fmt.Sprintf("CPython %s and uv %s are available", probe.Python.Version, probe.UV.Version)
	return check
}

func (s *PluginService) pythonEnvironmentDiagnosticCheck(ctx context.Context, summaries []plugin.AdminPluginSummary) plugin.AdminPluginDiagnosticCheck {
	check := plugin.AdminPluginDiagnosticCheck{
		ID:     "python_environments",
		Label:  "uv environments",
		Status: "ok",
		Details: map[string]any{
			"managed_python_plugins": managedPythonSummaryCount(summaries),
		},
	}
	service := &ConfigService{infra: s.infra}
	environments, err := service.ListPythonEnvironments(ctx)
	if err != nil {
		check.Status = "warning"
		check.Message = "python environment status unavailable: " + err.Error()
		return check
	}
	states := map[string]int{}
	for _, environment := range environments {
		if environment.Owner.Kind != pytoolchain.OwnerPlugin {
			continue
		}
		states[string(environment.Status.State)]++
	}
	check.Details["states"] = states
	environmentCount := 0
	for _, count := range states {
		environmentCount += count
	}
	check.Details["environment_count"] = environmentCount
	if states[string(pytoolchain.EnvBroken)] > 0 {
		check.Status = "warning"
	}
	check.Message = fmt.Sprintf("%d managed plugin uv environment(s)", environmentCount)
	return check
}

func (s *PluginService) processGuardDiagnosticCheck() plugin.AdminPluginDiagnosticCheck {
	limits := processguard.Limits{}
	if s != nil && s.infra != nil && s.infra.Config != nil {
		runtimeCfg := s.infra.Config.Plugins.Runtime
		limits.MaxProcesses = runtimeCfg.MaxProcesses
		limits.MemoryBytes = int64(runtimeCfg.MemoryMB) << 20
		limits.CPUQuota = runtimeCfg.CPUs
	}
	guard := processguard.NewWithLimits(limits)
	snapshot := guard.Snapshot()
	_ = guard.Close()
	status := "ok"
	message := snapshot.Kind + " lifecycle control is available for managed processes"
	if snapshot.Kind == processguard.KindNone {
		status = "warning"
		message = "process guard is not available on this platform"
	}
	if snapshot.Error != "" {
		status = "error"
		message = "process guard unavailable: " + snapshot.Error
	}
	return plugin.AdminPluginDiagnosticCheck{
		ID:      "process_guard",
		Label:   "Job Object / ProcessGuard",
		Status:  status,
		Message: message,
		Details: map[string]any{
			"kind":          snapshot.Kind,
			"goos":          runtime.GOOS,
			"max_processes": snapshot.MaxProcesses,
			"memory_bytes":  snapshot.MemoryBytes,
			"cpu_quota":     snapshot.CPUQuota,
		},
	}
}

func uvProjectDiagnosticCheck(summaries []plugin.AdminPluginSummary) plugin.AdminPluginDiagnosticCheck {
	var uvProjects int
	var legacyLocks int
	for _, summary := range summaries {
		if summary.DependencySummary.Format == "uv" {
			uvProjects++
		}
		if summary.DependencySummary.LegacyDependency {
			legacyLocks++
		}
	}
	status := "ok"
	if legacyLocks > 0 {
		status = "warning"
	}
	return plugin.AdminPluginDiagnosticCheck{
		ID:      "uv_project",
		Label:   "uv project",
		Status:  status,
		Message: fmt.Sprintf("%d managed uv project(s) across %d installed plugin version(s)", uvProjects, len(summaries)),
		Details: map[string]any{"uv_projects": uvProjects, "legacy_locks": legacyLocks, "installed_versions": len(summaries)},
	}
}

func managedPythonSummaryCount(summaries []plugin.AdminPluginSummary) int {
	count := 0
	for _, summary := range summaries {
		if summary.RuntimeKind == plugin.RuntimeManagedPythonProcess {
			count++
		}
	}
	return count
}

func pluginLogsDiagnosticCheck(summaries []plugin.AdminPluginSummary) plugin.AdminPluginDiagnosticCheck {
	return plugin.AdminPluginDiagnosticCheck{
		ID:      "plugin_logs",
		Label:   "Plugin logs",
		Status:  "ok",
		Message: fmt.Sprintf("bounded stderr logs are available for %d installed plugin version(s)", len(summaries)),
		Details: map[string]any{"installed_versions": len(summaries)},
	}
}

func pluginRepairDiagnosticCheck(summaries []plugin.AdminPluginSummary) plugin.AdminPluginDiagnosticCheck {
	ids := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		ids = append(ids, summary.PluginID)
	}
	sort.Strings(ids)
	return plugin.AdminPluginDiagnosticCheck{
		ID:      "repair",
		Label:   "Repair",
		Status:  "ok",
		Message: fmt.Sprintf("manual reinstall/delete repair path available for %d installed plugin version(s): %s", len(summaries), strings.Join(ids, ", ")),
		Details: map[string]any{"plugin_ids": ids},
	}
}

func diagnosticOverallStatus(checks []plugin.AdminPluginDiagnosticCheck) string {
	status := "ok"
	for _, check := range checks {
		switch check.Status {
		case "error":
			return "error"
		case "warning", "warn":
			status = "warning"
		}
	}
	return status
}

func (s *PluginService) ListPluginAccessEvents(ctx context.Context, pluginID string, limit int) ([]storage.PluginAccessEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireAdminLocked(); err != nil {
		return nil, err
	}
	return s.infra.DB.ListPluginAccessEvents(ctx, pluginID, limit)
}

func (s *PluginService) ListPluginProviderUsage(ctx context.Context, pluginID string, limit int) ([]storage.PluginProviderUsage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireAdminLocked(); err != nil {
		return nil, err
	}
	return s.infra.DB.ListPluginProviderUsage(ctx, pluginID, limit)
}

func (s *PluginService) summaryForInstallation(ctx context.Context, installation storage.PluginInstallation) plugin.AdminPluginSummary {
	return s.summaryForInstallationWithGrant(ctx, installation, nil)
}

func (s *PluginService) summaryForInstallationWithGrant(ctx context.Context, installation storage.PluginInstallation, proposedGrant *plugin.UserGrant) plugin.AdminPluginSummary {
	manifest, err := decodeInstalledManifest(installation)
	if err != nil {
		manifest = plugin.ManifestV2{
			ID:      installation.PluginID,
			Name:    installation.Name,
			Version: installation.Version,
		}
	}
	state, _ := s.infra.DB.GetPluginEnabledState(ctx, installation.PluginID)
	enabled := state != nil && state.Enabled && (state.Version == "" || state.Version == installation.Version)
	status := plugin.RuntimeStatus{PluginID: installation.PluginID, Status: "stopped"}
	if s.supervisor != nil {
		status = s.supervisor.Status(installation.PluginID)
	}
	if status.Version != "" && status.Version != installation.Version {
		status = plugin.RuntimeStatus{
			PluginID:     installation.PluginID,
			Version:      installation.Version,
			RuntimeKind:  manifest.Runtime.Kind,
			Status:       "stopped",
			RestartCount: status.RestartCount,
		}
	}
	if !enabled && status.Status == "stopped" {
		status.LastError = ""
	}
	blocked := s.pluginInstallBlocked(installation)
	trustReview, _ := s.trustReviewForInstallation(ctx, installation, manifest, proposedGrant)
	trustReview = s.issuePluginTrustReviewAcknowledgement(trustReview)
	trustAcceptance := pluginTrustAcceptanceFromState(state, enabled)
	hostAPIPolicy := s.pluginHostAPIPolicySummary(installation, manifest, state)
	toolPolicy := s.pluginToolPolicySummary(installation, status, enabled)
	hookPolicy := s.pluginHookPolicySummary(manifest)
	dependencySummary := s.pluginDependencyLockSummary(manifest)
	var settingsSchema *plugin.PluginSettingsSchema
	if manifest.Settings != nil && manifest.Settings.HasSchema() {
		schema := manifest.Settings.Schema
		settingsSchema = &schema
	}
	return plugin.AdminPluginSummary{
		PluginID:           installation.PluginID,
		Version:            installation.Version,
		Name:               installation.Name,
		RuntimeKind:        manifest.Runtime.Kind,
		AccessTier:         manifest.Access.Tier,
		Capabilities:       append([]plugin.Capability(nil), manifest.Access.Capabilities...),
		Hooks:              append([]plugin.HookSpec(nil), manifest.Hooks...),
		Enabled:            enabled,
		RuntimeStatus:      status,
		PackageDigest:      installation.PackageDigest,
		ManifestDigest:     installation.ManifestDigest,
		SignatureStatus:    installation.SignatureStatus,
		PublisherID:        installation.PublisherID,
		TrustLevel:         plugin.TrustLevelForInstall(installation.SignatureStatus, installation.SourceType, enabled, blocked),
		TrustAcceptance:    trustAcceptance,
		TrustReview:        trustReview,
		DependencySummary:  dependencySummary,
		HostAPIPolicy:      hostAPIPolicy,
		ToolPolicy:         toolPolicy,
		HookPolicy:         hookPolicy,
		SettingsSchema:     settingsSchema,
		SourceType:         installation.SourceType,
		SourceRef:          installation.SourceRef,
		InstalledAt:        installation.InstalledAt,
		StorePath:          installation.StorePath,
		StatePath:          s.storePathFor(installation.PluginID, "state"),
		CachePath:          s.storePathFor(installation.PluginID, "cache"),
		RunPath:            s.storePathFor(installation.PluginID, "run"),
		WorkspacePath:      s.storePathFor(installation.PluginID, "workspace"),
		ProviderUsageToday: s.providerUsageSummary(ctx, installation.PluginID),
	}
}

func (s *PluginService) pluginDependencyLockSummary(manifest plugin.ManifestV2) plugin.DependencyLockSummary {
	if s == nil || s.store == nil {
		return plugin.DependencyLockSummary{}
	}
	summary, err := plugin.DependencyLockSummaryForPackage(s.store, manifest)
	if err != nil {
		return plugin.DependencyLockSummary{}
	}
	return summary
}

func pluginTrustAcceptanceFromState(state *storage.PluginEnabledState, enabled bool) plugin.PluginTrustAcceptance {
	if state == nil || !enabled || strings.TrimSpace(state.TrustAcceptedAt) == "" {
		return plugin.PluginTrustAcceptance{}
	}
	var reasons []string
	if strings.TrimSpace(state.TrustReviewReasonsJSON) != "" {
		_ = json.Unmarshal([]byte(state.TrustReviewReasonsJSON), &reasons)
	}
	return plugin.PluginTrustAcceptance{
		TrustLevel:              plugin.PluginTrustLevel(state.TrustLevel),
		AcceptedAt:              state.TrustAcceptedAt,
		AcknowledgementHash:     state.TrustAcknowledgementHash,
		Reasons:                 reasons,
		DefaultToolExposure:     plugin.ToolExposure(state.DefaultToolExposure),
		DefaultInvocationPolicy: plugin.InvocationPolicy(state.DefaultInvocationPolicy),
	}
}

func (s *PluginService) pluginHostAPIPolicySummary(installation storage.PluginInstallation, manifest plugin.ManifestV2, state *storage.PluginEnabledState) plugin.PluginHostAPIPolicySummary {
	summary := plugin.PluginHostAPIPolicySummary{
		ManifestCapabilities: append([]plugin.Capability(nil), manifest.Access.Capabilities...),
		HostPolicyMode:       "allow_all_compat",
	}
	hostAllowed := s.pluginFacadeHostPolicy().AllowedCapabilities
	if len(hostAllowed) > 0 {
		summary.HostPolicyMode = "allowlist"
		summary.HostAllowedCapabilities = append([]plugin.Capability(nil), hostAllowed...)
	}
	if state == nil || !state.Enabled || (strings.TrimSpace(state.Version) != "" && state.Version != installation.Version) {
		return summary
	}
	grant, err := plugin.ValidateUserGrantForManifest(state.UserGrantJSON, manifest)
	if err != nil {
		return summary
	}
	summary.UserGrantedCapabilities = append([]plugin.Capability(nil), grant.Capabilities...)
	if len(grant.Capabilities) == 0 {
		return summary
	}
	manifestSet := capabilitySet(manifest.Access.Capabilities)
	grantSet := capabilitySet(grant.Capabilities)
	hostSet := capabilitySet(hostAllowed)
	for _, capability := range manifest.Access.Capabilities {
		if _, ok := manifestSet[capability]; !ok {
			continue
		}
		if _, ok := grantSet[capability]; !ok {
			continue
		}
		if len(hostSet) > 0 {
			if _, ok := hostSet[capability]; !ok {
				continue
			}
		}
		summary.EffectiveCapabilities = append(summary.EffectiveCapabilities, capability)
	}
	return summary
}

func (s *PluginService) pluginToolPolicySummary(installation storage.PluginInstallation, status plugin.RuntimeStatus, enabled bool) plugin.PluginToolPolicySummary {
	summary := plugin.PluginToolPolicySummary{
		DefaultExposure:   plugin.ExposureWork,
		DefaultInvocation: plugin.InvocationAsk,
	}
	if s == nil || s.supervisor == nil || !enabled {
		return summary
	}
	if strings.TrimSpace(status.Version) != "" && status.Version != installation.Version {
		return summary
	}
	for _, processTool := range s.supervisor.Tools(installation.PluginID) {
		invocation := pluginSummaryInvocationPolicy(processTool.InvocationPolicy)
		if invocation == plugin.InvocationDeny {
			continue
		}
		summary.RegisteredTools = append(summary.RegisteredTools, plugin.PluginToolPolicyEntry{
			Name:                   processTool.Name,
			HostExposure:           plugin.ExposureWork,
			HostInvocation:         invocation,
			SelfReportedScope:      string(processTool.Scope),
			SelfReportedPermission: string(processTool.Permission),
		})
	}
	sort.Slice(summary.RegisteredTools, func(i, j int) bool {
		return summary.RegisteredTools[i].Name < summary.RegisteredTools[j].Name
	})
	return summary
}

func pluginSummaryInvocationPolicy(policy plugin.InvocationPolicy) plugin.InvocationPolicy {
	switch policy {
	case plugin.InvocationAuto:
		return plugin.InvocationAuto
	case plugin.InvocationDeny:
		return plugin.InvocationDeny
	default:
		return plugin.InvocationAsk
	}
}

func (s *PluginService) pluginHookPolicySummary(manifest plugin.ManifestV2) plugin.PluginHookPolicySummary {
	summary := plugin.PluginHookPolicySummary{}
	if s != nil && s.infra != nil && s.infra.Config != nil {
		summary.AllowActiveHooks = s.infra.Config.Plugins.Policy.AllowActiveHooks
	}
	for _, hook := range manifest.Hooks {
		if hookModeIsActive(hook.Mode) {
			summary.ActiveHooks = append(summary.ActiveHooks, hook.Name)
			continue
		}
		if hook.Mode == plugin.HookModeObserve {
			summary.ObserveHooks = append(summary.ObserveHooks, hook.Name)
		}
	}
	sort.Slice(summary.ObserveHooks, func(i, j int) bool {
		return summary.ObserveHooks[i] < summary.ObserveHooks[j]
	})
	sort.Slice(summary.ActiveHooks, func(i, j int) bool {
		return summary.ActiveHooks[i] < summary.ActiveHooks[j]
	})
	return summary
}

func hookModeIsActive(mode plugin.HookMode) bool {
	return mode == plugin.HookModeTransform || mode == plugin.HookModeSideEffect
}

func capabilitySet(capabilities []plugin.Capability) map[plugin.Capability]struct{} {
	values := make(map[plugin.Capability]struct{}, len(capabilities))
	for _, capability := range capabilities {
		values[capability] = struct{}{}
	}
	return values
}

func (s *PluginService) storePathFor(pluginID, kind string) string {
	if s == nil || s.store == nil {
		return ""
	}
	var (
		path string
		err  error
	)
	switch kind {
	case "state":
		path, err = s.store.StateDir(pluginID)
	case "cache":
		path, err = s.store.CacheDir(pluginID)
	case "run":
		path, err = s.store.RunDir(pluginID)
	case "workspace":
		path, err = s.store.WorkspaceDir(pluginID)
	}
	if err != nil {
		return ""
	}
	return path
}

func (s *PluginService) providerUsageSummary(ctx context.Context, pluginID string) plugin.PluginProviderUsageSummary {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return plugin.PluginProviderUsageSummary{}
	}
	usages, err := s.infra.DB.ListPluginProviderUsage(ctx, pluginID, 100)
	if err != nil {
		return plugin.PluginProviderUsageSummary{}
	}
	var summary plugin.PluginProviderUsageSummary
	for _, usage := range usages {
		summary.Count++
		if usage.Status != "success" {
			summary.ErrorCount++
		}
		summary.InputTokens += usage.InputTokens
		summary.OutputTokens += usage.OutputTokens
		summary.EstimatedTokens += usage.EstimatedTokens
	}
	return summary
}
