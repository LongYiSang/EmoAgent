package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/plugin"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/turn"
)

type PluginService struct {
	mu           sync.Mutex
	infra        *Infra
	tools        *ToolService
	agentAffect  *AgentAffectService
	agentRuntime *AgentRuntimeService
	host         *plugin.PluginHost
	runner       *plugin.BuiltinRunner
	dispatcher   *tool.Dispatcher

	store           *plugin.PluginStore
	installer       *plugin.PluginInstaller
	manager         *plugin.Manager
	supervisor      *plugin.RuntimeSupervisor
	facadeBroker    *plugin.FacadeBroker
	providerGateway *plugin.ProviderGateway
	registered      map[string]bool
}

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
	supervisor := plugin.NewRuntimeSupervisor(store, s.infra.Config.Plugins.Runtime, nil)
	supervisor.SetHostHandlerForPlugin(s.hostRPCHandlerForPlugin)
	supervisor.SetEnabledChecker(s.isPluginEnabled)
	supervisor.SetBlockedEnvNames(s.pluginBlockedEnvNames())
	supervisor.SetAdditionalEnvVars(s.pluginProcessEnv())
	s.store = store
	s.installer = plugin.NewPluginInstaller(store, s.infra.Config.Plugins.Installer)
	s.providerGateway = providerGateway
	s.facadeBroker = facadeBroker
	s.supervisor = supervisor
	s.manager = plugin.NewManager(store, supervisor, facadeBroker, providerGateway)
	if s.registered == nil {
		s.registered = map[string]bool{}
	}
	return nil
}

func (s *PluginService) pluginProcessEnv() []string {
	if s == nil || s.infra == nil || strings.TrimSpace(s.infra.ProjectRoot) == "" {
		return nil
	}
	sdkPath := filepath.Join(s.infra.ProjectRoot, "sdk", "python")
	if _, err := os.Stat(sdkPath); err != nil {
		return nil
	}
	pythonPath := sdkPath
	if existing := os.Getenv("PYTHONPATH"); existing != "" {
		pythonPath += string(os.PathListSeparator) + existing
	}
	return []string{"PYTHONPATH=" + pythonPath}
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
	return llm.NewClient(llm.ProviderConfig{
		ID:        provider.ID,
		PresetID:  provider.PresetID,
		Protocol:  provider.Protocol,
		BaseURL:   provider.BaseURL,
		APIKeyEnv: provider.APIKeyEnv,
	}, s.infra.Logger)
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
		if err := s.registerProcessPluginLocked(ctx, manifest); err != nil {
			s.infra.Logger.Warn("process plugin failed to start", "plugin_id", manifest.ID, "error", err)
			_ = s.recordRuntimeStatus(ctx, manifest, s.supervisor.Status(manifest.ID))
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
	return manifest.Runtime.Kind == plugin.RuntimePythonProcess || manifest.Runtime.Kind == plugin.RuntimeProcess
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
	s.facadeBroker.AddPlugin(manifest)
	if s.registered[manifest.ID] {
		_, err := s.supervisor.EnsureReady(ctx, manifest.ID)
		_ = s.recordRuntimeStatus(ctx, manifest, s.supervisor.Status(manifest.ID))
		return err
	}
	if err := plugin.RegisterProcessPlugin(ctx, manifest, s.host.Registry(), s.tools.Registry(), s.host.HookBus(), s.supervisor); err != nil {
		_ = s.recordRuntimeStatus(ctx, manifest, s.supervisor.Status(manifest.ID))
		return err
	}
	s.registered[manifest.ID] = true
	return s.recordRuntimeStatus(ctx, manifest, s.supervisor.Status(manifest.ID))
}

func (s *PluginService) recordRuntimeStatus(ctx context.Context, manifest plugin.ManifestV2, status plugin.RuntimeStatus) error {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return nil
	}
	return s.infra.DB.UpsertPluginRuntimeRecord(ctx, storage.PluginRuntimeRecord{
		PluginID:     manifest.ID,
		Version:      manifest.Version,
		RuntimeKind:  string(manifest.Runtime.Kind),
		Status:       status.Status,
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
		SignatureStatus: result.SignatureStatus,
		PublisherID:     result.PublisherID,
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
	installation, err := s.infra.DB.GetPluginInstallation(ctx, pluginID)
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	if installation == nil {
		return plugin.AdminPluginSummary{}, plugin.ErrPluginNotFound
	}
	return s.summaryForInstallation(ctx, *installation), nil
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
	grant := strings.TrimSpace(req.UserGrantJSON)
	if grant == "" {
		grant = "{}"
	}
	if err := s.infra.DB.SetPluginEnabled(ctx, manifest.ID, manifest.Version, true, grant); err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	if s.infra.Config.Plugins.Enabled && isProcessManifest(manifest) {
		if err := s.registerProcessPluginLocked(ctx, manifest); err != nil {
			return s.summaryForInstallation(ctx, *installation), err
		}
	}
	return s.summaryForInstallation(ctx, *installation), nil
}

func (s *PluginService) DisablePlugin(ctx context.Context, pluginID string) (plugin.AdminPluginSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireAdminLocked(); err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	installation, err := s.infra.DB.GetPluginInstallation(ctx, pluginID)
	if err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	if installation == nil {
		return plugin.AdminPluginSummary{}, plugin.ErrPluginNotFound
	}
	if err := s.infra.DB.SetPluginEnabled(ctx, pluginID, installation.Version, false, "{}"); err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	if s.supervisor != nil {
		_ = s.supervisor.Stop(ctx, pluginID)
	}
	manifest, _ := decodeInstalledManifest(*installation)
	_ = s.recordRuntimeStatus(ctx, manifest, plugin.RuntimeStatus{PluginID: pluginID, Status: "stopped"})
	return s.summaryForInstallation(ctx, *installation), nil
}

func (s *PluginService) RestartPlugin(ctx context.Context, pluginID string) (plugin.AdminPluginSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireAdminLocked(); err != nil {
		return plugin.AdminPluginSummary{}, err
	}
	installation, err := s.infra.DB.GetPluginInstallation(ctx, pluginID)
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
	if s.supervisor != nil {
		_ = s.supervisor.Stop(ctx, pluginID)
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
	if s.supervisor != nil {
		_ = s.supervisor.Stop(ctx, pluginID)
	}
	if err := s.infra.DB.SetPluginEnabled(ctx, pluginID, "0.0.0", false, "{}"); err != nil {
		state, stateErr := s.infra.DB.GetPluginEnabledState(ctx, pluginID)
		if stateErr == nil && state == nil {
			err = nil
		}
		if err != nil {
			return err
		}
	}
	return s.infra.DB.DeletePluginInstallation(ctx, pluginID)
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
	if !enabled && status.Status == "stopped" {
		status.LastError = ""
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
