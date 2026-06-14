package configcenter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	memconfig "github.com/longyisang/emoagent-memorycore/config"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/memoryhost"
	sidecarruntime "github.com/longyisang/emoagent/internal/sidecar"
	"github.com/longyisang/emoagent/internal/storage"
)

var ErrInvalidRuntimeConfig = errors.New("invalid runtime config")

type Service struct {
	Seed      *config.Config
	DB        *storage.DB
	EnvLookup func(string) (string, bool)
}

type EffectiveConfig struct {
	AgentAffect            config.AgentAffectConfig `json:"agent_affect"`
	Memory                 config.MemoryConfig      `json:"memory"`
	Providers              []ProviderEffective      `json:"providers"`
	RuntimeSettings        []storage.RuntimeSetting `json:"runtime_settings"`
	MemoryCore             *MemoryCoreEffective     `json:"memory_core,omitempty"`
	SidecarGeneratedConfig string                   `json:"sidecar_generated_config,omitempty"`
	Issues                 []ConfigIssue            `json:"issues"`
}

type MemoryCoreOpenConfig struct {
	ConfigPath       string
	Overrides        memconfig.ConfigOverrides
	ProviderRegistry memconfig.ProviderRegistry
	Runtime          memconfig.RuntimeValidationOptions
	NaturalMemory    memoryhost.NaturalMemoryCoreOverrides
	Memory           config.MemoryConfig
	MemoryCore       *MemoryCoreEffective
	Issues           []ConfigIssue
}

type MemoryConfigResponse struct {
	Memory     config.MemoryConfig  `json:"memory"`
	MemoryCore *MemoryCoreEffective `json:"memory_core,omitempty"`
	Issues     []ConfigIssue        `json:"issues"`
}

type AgentAffectConfigResponse struct {
	AgentAffect config.AgentAffectConfig `json:"agent_affect"`
	Issues      []ConfigIssue            `json:"issues"`
}

type ValidationError struct {
	Issues []ConfigIssue `json:"issues"`
}

func (e *ValidationError) Error() string {
	return ErrInvalidRuntimeConfig.Error()
}

type ProviderEffective struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	PresetID       string            `json:"preset_id"`
	Protocol       string            `json:"protocol"`
	BaseURL        string            `json:"base_url"`
	ModelDiscovery string            `json:"model_discovery"`
	Enabled        bool              `json:"enabled"`
	Capabilities   []string          `json:"capabilities"`
	Env            ProviderEnvStatus `json:"env"`
}

type ProviderEnvStatus struct {
	APIKeyEnv string `json:"api_key_env"`
	Present   bool   `json:"present"`
}

type MemoryCoreEffective struct {
	Enabled           bool                                 `json:"enabled"`
	ConfigPath        string                               `json:"config_path"`
	Core              MemoryCoreCoreEffective              `json:"core"`
	Retrieval         MemoryCoreRetrievalEffective         `json:"retrieval"`
	Pipelines         memconfig.PipelinesConfig            `json:"pipelines"`
	Sidecar           MemoryCoreSidecarEffective           `json:"sidecar"`
	SidecarResilience MemoryCoreSidecarResilienceEffective `json:"sidecar_resilience"`
	Mirror            MemoryCoreMirrorEffective            `json:"mirror"`
	SemanticOps       memconfig.SemanticOpsConfig          `json:"semantic_ops"`
	NaturalMemory     memconfig.NaturalMemoryConfig        `json:"natural_memory"`
	Retention         memconfig.RetentionConfig            `json:"retention"`
	ForgettingPrivacy memconfig.ForgettingPrivacyConfig    `json:"forgetting_privacy"`
	AgentAffect       memconfig.AgentAffectConfig          `json:"agent_affect"`
}

type MemoryCoreCoreEffective struct {
	DBPath      string `json:"db_path"`
	PersonaID   string `json:"persona_id"`
	AutoMigrate bool   `json:"auto_migrate"`
	EnableFTS   bool   `json:"enable_fts"`
	Timezone    string `json:"timezone"`
}

type MemoryCoreRetrievalEffective struct {
	UseFTS                bool   `json:"use_fts"`
	UseMirror             bool   `json:"use_mirror"`
	AllowHistorical       bool   `json:"allow_historical"`
	AllowDeepArchive      bool   `json:"allow_deep_archive"`
	SensitivityPermission string `json:"sensitivity_permission"`
	FinalMemoryCount      int    `json:"final_memory_count"`
	ContextBudgetTokens   int    `json:"context_budget_tokens"`
}

type MemoryCoreSidecarEffective struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	Adapter string `json:"adapter"`
}

type MemoryCoreSidecarResilienceEffective struct {
	TotalTimeoutMS      int                                        `json:"total_timeout_ms"`
	MirrorTimeoutMS     int                                        `json:"mirror_timeout_ms"`
	ActivationTimeoutMS int                                        `json:"activation_timeout_ms"`
	RerankTimeoutMS     int                                        `json:"rerank_timeout_ms"`
	CircuitBreaker      MemoryCoreSidecarBreakerEffective          `json:"circuit_breaker"`
	ActivationBudget    MemoryCoreSidecarActivationBudgetEffective `json:"activation_budget"`
}

type MemoryCoreSidecarBreakerEffective struct {
	Enabled          bool `json:"enabled"`
	Window           int  `json:"window"`
	FailureThreshold int  `json:"failure_threshold"`
	OpenMS           int  `json:"open_ms"`
}

type MemoryCoreSidecarActivationBudgetEffective struct {
	MaxEdgesScannedPerRequest int `json:"max_edges_scanned_per_request"`
	MaxNeighborsPerNode       int `json:"max_neighbors_per_node"`
	MaxWallMS                 int `json:"max_wall_ms"`
}

type MemoryCoreMirrorEffective struct {
	Enabled        bool `json:"enabled"`
	SyncLimit      int  `json:"sync_limit"`
	RebuildOnStart bool `json:"rebuild_on_start"`
}

type ValidateRequest struct{}

type ValidateResponse struct {
	Issues []ConfigIssue `json:"issues"`
}

func NewService(seed *config.Config, db *storage.DB) *Service {
	return &Service{Seed: seed, DB: db, EnvLookup: os.LookupEnv}
}

func (s *Service) BuildEffective(ctx context.Context) (EffectiveConfig, error) {
	runtimeCfg, providers, runtimeSettings, runtimeIssues, err := s.runtimeConfig(ctx)
	if err != nil {
		return EffectiveConfig{}, err
	}

	effective := EffectiveConfig{
		AgentAffect:     runtimeCfg.AgentAffect,
		Memory:          runtimeCfg.Memory,
		Providers:       s.providerEffective(providers),
		RuntimeSettings: runtimeSettings,
	}
	issues := append([]ConfigIssue{}, runtimeIssues...)
	issues = append(issues, BuildIssues(&runtimeCfg, effective.Providers, nil)...)

	memoryCore, memoryCoreIssues := s.memoryCoreEffective(&runtimeCfg, providers, nil)
	effective.MemoryCore = memoryCore
	effective.SidecarGeneratedConfig, issues = s.sidecarGeneratedConfig(&runtimeCfg, providers, issues)
	issues = append(issues, memoryCoreIssues...)
	if memoryCore != nil {
		issues = append(issues, BuildIssues(&runtimeCfg, effective.Providers, memoryCore)...)
	}
	effective.Issues = dedupeIssues(issues)
	return effective, nil
}

func (s *Service) sidecarGeneratedConfig(runtimeCfg *config.Config, providers []config.LLMProvider, issues []ConfigIssue) (string, []ConfigIssue) {
	if runtimeCfg == nil || !runtimeCfg.Memory.Sidecar.Enabled {
		return "", issues
	}
	spec, specIssues := s.sidecarSpec(runtimeCfg.Memory, providers)
	issues = append(issues, specIssues...)
	body, err := sidecarruntime.RenderConfig(spec)
	if err != nil {
		issues = append(issues, ConfigIssue{
			Path:     "memory.sidecar",
			Severity: "error",
			Message:  fmt.Sprintf("render sidecar generated config: %v", err),
		})
		return "", issues
	}
	return string(body), issues
}

func (s *Service) AgentAffectConfig(ctx context.Context) (AgentAffectConfigResponse, error) {
	effective, err := s.BuildEffective(ctx)
	if err != nil {
		return AgentAffectConfigResponse{}, err
	}
	return AgentAffectConfigResponse{
		AgentAffect: effective.AgentAffect,
		Issues:      effective.Issues,
	}, nil
}

func (s *Service) UpdateAgentAffectConfig(ctx context.Context, cfg config.AgentAffectConfig) (EffectiveConfig, error) {
	if s.DB == nil {
		return EffectiveConfig{}, fmt.Errorf("runtime settings database is not configured")
	}
	payload, err := json.Marshal(cfg)
	if err != nil {
		return EffectiveConfig{}, err
	}
	if err := s.validateAgentAffectConfigUpdate(ctx, storage.RuntimeSetting{
		Namespace: "agent_affect",
		Key:       "config",
		ValueJSON: string(payload),
		Source:    "ui",
	}); err != nil {
		return EffectiveConfig{}, err
	}
	if err := s.DB.UpsertRuntimeSetting("agent_affect", "config", string(payload), "ui"); err != nil {
		return EffectiveConfig{}, err
	}
	return s.BuildEffective(ctx)
}

func (s *Service) validateAgentAffectConfigUpdate(ctx context.Context, next storage.RuntimeSetting) error {
	seed := s.Seed
	if seed == nil {
		seed = config.DefaultConfig()
	}
	current, err := s.runtimeSettings()
	if err != nil {
		return err
	}
	settings := replaceRuntimeSetting(current, next)
	runtimeCfg, runtimeIssues := ApplyRuntimeSettings(seed, settings)
	providers, err := s.providers(ctx, &runtimeCfg)
	if err != nil {
		return err
	}
	allIssues := append([]ConfigIssue{}, runtimeIssues...)
	allIssues = append(allIssues, BuildIssues(&runtimeCfg, s.providerEffective(providers), nil)...)
	issues := filterIssuesByPathPrefix(allIssues, "agent_affect")
	issues = dedupeIssues(issues)
	if hasBlockingIssues(issues) {
		return &ValidationError{Issues: issues}
	}
	return nil
}

func (s *Service) Validate(ctx context.Context, _ ValidateRequest) (ValidateResponse, error) {
	effective, err := s.BuildEffective(ctx)
	if err != nil {
		return ValidateResponse{}, err
	}
	return ValidateResponse{Issues: effective.Issues}, nil
}

func (s *Service) Issues(ctx context.Context) ([]ConfigIssue, error) {
	effective, err := s.BuildEffective(ctx)
	if err != nil {
		return nil, err
	}
	return effective.Issues, nil
}

func (s *Service) MemoryConfig(ctx context.Context) (MemoryConfigResponse, error) {
	effective, err := s.BuildEffective(ctx)
	if err != nil {
		return MemoryConfigResponse{}, err
	}
	return MemoryConfigResponse{
		Memory:     effective.Memory,
		MemoryCore: effective.MemoryCore,
		Issues:     effective.Issues,
	}, nil
}

func (s *Service) UpdateMemoryConfig(ctx context.Context, memory config.MemoryConfig) (EffectiveConfig, error) {
	if s.DB == nil {
		return EffectiveConfig{}, fmt.Errorf("runtime settings database is not configured")
	}
	payload, err := json.Marshal(memory)
	if err != nil {
		return EffectiveConfig{}, err
	}
	if err := s.validateRuntimeSettingUpdate(ctx, storage.RuntimeSetting{
		Namespace: "memory",
		Key:       "config",
		ValueJSON: string(payload),
		Source:    "ui",
	}); err != nil {
		return EffectiveConfig{}, err
	}
	if err := s.DB.UpsertRuntimeSetting("memory", "config", string(payload), "ui"); err != nil {
		return EffectiveConfig{}, err
	}
	return s.BuildEffective(ctx)
}

func (s *Service) UpdateMemoryFeatures(ctx context.Context, memory config.MemoryConfig) (EffectiveConfig, error) {
	return s.UpdateMemoryConfig(ctx, memory)
}

func (s *Service) BuildSidecarSpec(ctx context.Context) (sidecarruntime.Spec, []ConfigIssue, error) {
	runtimeCfg, providers, _, runtimeIssues, err := s.runtimeConfig(ctx)
	if err != nil {
		return sidecarruntime.Spec{}, nil, err
	}
	spec, issues := s.sidecarSpec(runtimeCfg.Memory, providers)
	return spec, append(runtimeIssues, issues...), nil
}

func (s *Service) BuildMemoryCoreOpenConfig(ctx context.Context, status *sidecarruntime.Status) (MemoryCoreOpenConfig, error) {
	runtimeCfg, providers, _, runtimeIssues, err := s.runtimeConfig(ctx)
	if err != nil {
		return MemoryCoreOpenConfig{}, err
	}
	overrides := memoryCoreOverridesFromConfig(runtimeCfg.Memory, runtimeCfg.Time.Timezone)
	if status != nil {
		mergeSidecarStatusOverrides(&overrides, *status)
	}
	memoryCore, memoryCoreIssues := s.memoryCoreEffective(&runtimeCfg, providers, status)
	runtimeIssues = append(runtimeIssues, memoryCoreIssues...)
	return MemoryCoreOpenConfig{
		ConfigPath:       runtimeCfg.Memory.ConfigPath,
		Overrides:        overrides,
		ProviderRegistry: s.providerRegistry(providers, runtimeCfg.Memory),
		Runtime: memconfig.RuntimeValidationOptions{
			CheckEnv: true,
			Env: func(name string) string {
				value, _ := s.lookupEnv(name)
				return value
			},
		},
		NaturalMemory: naturalMemoryCoreOverridesFromConfig(runtimeCfg.Memory.NaturalMemory),
		Memory:        runtimeCfg.Memory,
		MemoryCore:    memoryCore,
		Issues:        runtimeIssues,
	}, nil
}

func (s *Service) runtimeConfig(ctx context.Context) (config.Config, []config.LLMProvider, []storage.RuntimeSetting, []ConfigIssue, error) {
	seed := s.Seed
	if seed == nil {
		seed = config.DefaultConfig()
	}
	runtimeSettings, err := s.runtimeSettings()
	if err != nil {
		return config.Config{}, nil, nil, nil, err
	}
	runtimeCfg, runtimeIssues := ApplyRuntimeSettings(seed, runtimeSettings)
	providers, err := s.providers(ctx, &runtimeCfg)
	if err != nil {
		return config.Config{}, nil, nil, nil, err
	}
	return runtimeCfg, providers, runtimeSettings, runtimeIssues, nil
}

func (s *Service) validateRuntimeSettingUpdate(ctx context.Context, next storage.RuntimeSetting) error {
	seed := s.Seed
	if seed == nil {
		seed = config.DefaultConfig()
	}
	current, err := s.runtimeSettings()
	if err != nil {
		return err
	}
	settings := replaceRuntimeSetting(current, next)
	runtimeCfg, runtimeIssues := ApplyRuntimeSettings(seed, settings)
	providers, err := s.providers(ctx, &runtimeCfg)
	if err != nil {
		return err
	}
	providerEffective := s.providerEffective(providers)
	issues := append([]ConfigIssue{}, runtimeIssues...)
	issues = append(issues, BuildIssues(&runtimeCfg, providerEffective, nil)...)
	_, sidecarIssues := s.sidecarSpec(runtimeCfg.Memory, providers)
	issues = append(issues, sidecarIssues...)
	if memoryCore, memoryIssues := s.memoryCoreEffective(&runtimeCfg, providers, nil); memoryCore != nil {
		issues = append(issues, memoryIssues...)
		issues = append(issues, BuildIssues(&runtimeCfg, providerEffective, memoryCore)...)
	} else {
		issues = append(issues, memoryIssues...)
	}
	issues = dedupeIssues(issues)
	if hasBlockingIssues(issues) {
		return &ValidationError{Issues: issues}
	}
	return nil
}

func replaceRuntimeSetting(settings []storage.RuntimeSetting, next storage.RuntimeSetting) []storage.RuntimeSetting {
	out := make([]storage.RuntimeSetting, 0, len(settings)+1)
	replaced := false
	for _, setting := range settings {
		if setting.Namespace == next.Namespace && setting.Key == next.Key {
			out = append(out, next)
			replaced = true
			continue
		}
		out = append(out, setting)
	}
	if !replaced {
		out = append(out, next)
	}
	return out
}

func filterIssuesByPathPrefix(issues []ConfigIssue, prefix string) []ConfigIssue {
	out := make([]ConfigIssue, 0, len(issues))
	for _, issue := range issues {
		if issue.Path == prefix || strings.HasPrefix(issue.Path, prefix+".") {
			out = append(out, issue)
		}
	}
	return out
}

func (s *Service) providers(_ context.Context, seed *config.Config) ([]config.LLMProvider, error) {
	if s.DB == nil {
		return append([]config.LLMProvider(nil), seed.LLMProviders...), nil
	}
	records, err := s.DB.ListLLMProviders()
	if err != nil {
		return nil, fmt.Errorf("list llm providers: %w", err)
	}
	if len(records) == 0 {
		return append([]config.LLMProvider(nil), seed.LLMProviders...), nil
	}
	providers := make([]config.LLMProvider, 0, len(records))
	for _, record := range records {
		providers = append(providers, record.LLMProvider)
	}
	return providers, nil
}

func (s *Service) runtimeSettings() ([]storage.RuntimeSetting, error) {
	if s.DB == nil {
		return nil, nil
	}
	settings, err := s.DB.ListRuntimeSettings()
	if err != nil {
		return nil, fmt.Errorf("list runtime settings: %w", err)
	}
	return settings, nil
}

func (s *Service) providerEffective(providers []config.LLMProvider) []ProviderEffective {
	out := make([]ProviderEffective, 0, len(providers))
	for _, provider := range providers {
		apiKeyEnv := strings.TrimSpace(provider.APIKeyEnv)
		_, present := s.lookupEnv(apiKeyEnv)
		out = append(out, ProviderEffective{
			ID:             provider.ID,
			Name:           provider.Name,
			PresetID:       provider.PresetID,
			Protocol:       provider.Protocol,
			BaseURL:        provider.BaseURL,
			ModelDiscovery: provider.ModelDiscovery,
			Enabled:        provider.Enabled,
			Capabilities:   config.NormalizeProviderCapabilities(provider.Capabilities),
			Env: ProviderEnvStatus{
				APIKeyEnv: apiKeyEnv,
				Present:   apiKeyEnv != "" && present,
			},
		})
	}
	return out
}

func (s *Service) lookupEnv(name string) (string, bool) {
	if strings.TrimSpace(name) == "" {
		return "", false
	}
	if s.EnvLookup != nil {
		return s.EnvLookup(name)
	}
	return os.LookupEnv(name)
}

func (s *Service) memoryCoreEffective(seed *config.Config, providers []config.LLMProvider, status *sidecarruntime.Status) (*MemoryCoreEffective, []ConfigIssue) {
	path := strings.TrimSpace(seed.Memory.ConfigPath)
	if path == "" {
		return nil, nil
	}
	overrides := memoryCoreOverridesFromConfig(seed.Memory, seed.Time.Timezone)
	if status != nil {
		mergeSidecarStatusOverrides(&overrides, *status)
	}
	naturalMemoryOverrides := naturalMemoryCoreOverridesFromConfig(seed.Memory.NaturalMemory)
	cfg, err := memconfig.LoadEffective(memconfig.LoadEffectiveOptions{
		ConfigPath:       path,
		ProviderRegistry: s.providerRegistry(providers, seed.Memory),
		Overrides:        overrides,
	})
	if err != nil {
		if !seed.Memory.Enabled && os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []ConfigIssue{{
			Path:     "memory.config_path",
			Severity: "error",
			Message:  fmt.Sprintf("load memorycore config %q: %v", path, err),
		}}
	}
	memoryhost.ApplyNaturalMemoryCoreOverrides(&cfg, naturalMemoryOverrides)
	issues := []ConfigIssue{}
	if err := memoryhost.ValidateLLMProviderBindings(cfg); err != nil {
		issues = append(issues, ConfigIssue{
			Path:     "memory.provider_bindings",
			Severity: "error",
			Message:  err.Error(),
		})
	}
	return &MemoryCoreEffective{
		Enabled:    cfg.Enabled,
		ConfigPath: path,
		Core: MemoryCoreCoreEffective{
			DBPath:      cfg.Core.DBPath,
			PersonaID:   cfg.Core.PersonaID,
			AutoMigrate: cfg.Core.AutoMigrate,
			EnableFTS:   cfg.Core.EnableFTS,
			Timezone:    cfg.Core.Timezone,
		},
		Retrieval: MemoryCoreRetrievalEffective{
			UseFTS:                cfg.Retrieval.UseFTS,
			UseMirror:             cfg.Retrieval.UseMirror,
			AllowHistorical:       cfg.Retrieval.AllowHistorical,
			AllowDeepArchive:      cfg.Retrieval.AllowDeepArchive,
			SensitivityPermission: cfg.Retrieval.SensitivityPermission,
			FinalMemoryCount:      cfg.Retrieval.FinalMemoryCount,
			ContextBudgetTokens:   cfg.Retrieval.ContextBudgetTokens,
		},
		Pipelines: cfg.Pipelines,
		Sidecar: MemoryCoreSidecarEffective{
			Enabled: cfg.Sidecar.Enabled,
			URL:     cfg.Sidecar.URL,
			Adapter: cfg.Sidecar.Adapter,
		},
		SidecarResilience: MemoryCoreSidecarResilienceEffective{
			TotalTimeoutMS:      cfg.Sidecar.TotalTimeoutMS,
			MirrorTimeoutMS:     cfg.Sidecar.MirrorTimeoutMS,
			ActivationTimeoutMS: cfg.Sidecar.ActivationTimeoutMS,
			RerankTimeoutMS:     cfg.Sidecar.RerankTimeoutMS,
			CircuitBreaker: MemoryCoreSidecarBreakerEffective{
				Enabled:          cfg.Sidecar.CircuitBreaker.Enabled,
				Window:           cfg.Sidecar.CircuitBreaker.Window,
				FailureThreshold: cfg.Sidecar.CircuitBreaker.FailureThreshold,
				OpenMS:           cfg.Sidecar.CircuitBreaker.OpenMS,
			},
			ActivationBudget: MemoryCoreSidecarActivationBudgetEffective{
				MaxEdgesScannedPerRequest: cfg.Sidecar.ActivationBudget.MaxEdgesScannedPerRequest,
				MaxNeighborsPerNode:       cfg.Sidecar.ActivationBudget.MaxNeighborsPerNode,
				MaxWallMS:                 cfg.Sidecar.ActivationBudget.MaxWallMS,
			},
		},
		Mirror: MemoryCoreMirrorEffective{
			Enabled:        cfg.Mirror.Enabled,
			SyncLimit:      cfg.Mirror.SyncLimit,
			RebuildOnStart: cfg.Mirror.RebuildOnStart,
		},
		SemanticOps:       cfg.SemanticOps,
		NaturalMemory:     cfg.NaturalMemory,
		Retention:         cfg.Retention,
		ForgettingPrivacy: cfg.ForgettingPrivacy,
		AgentAffect:       cfg.AgentAffect,
	}, issues
}

func naturalMemoryCoreOverridesFromConfig(cfg config.MemoryNaturalMemoryConfig) memoryhost.NaturalMemoryCoreOverrides {
	return memoryhost.NaturalMemoryCoreOverrides{
		Configured:              true,
		Enabled:                 cfg.Enabled,
		LocalTime:               strings.TrimSpace(cfg.LocalTime),
		Timezone:                strings.TrimSpace(cfg.Timezone),
		RunMissedOnStart:        cfg.RunMissedOnStart,
		ManualEnabled:           cfg.Manual.Enabled,
		AllowDryRun:             cfg.Manual.AllowDryRun,
		AllowForce:              cfg.Manual.AllowForce,
		MarkSleepCycleByDefault: cfg.Manual.MarkSleepCycleByDefault || cfg.MarkSleepCycleByDefault,
	}
}

func (s *Service) providerRegistry(providers []config.LLMProvider, memory config.MemoryConfig) memconfig.ProviderRegistry {
	registry := memoryhost.BuildProviderRegistry(providers)
	defaultProvider, ok := defaultMemoryProvider(providers, memory)
	if ok {
		appendProviderAlias(&registry, "llm_main", defaultProvider)
	}
	if len(registry.LLM) == 0 {
		registry.LLM = nil
	}
	return registry
}

func defaultMemoryProvider(providers []config.LLMProvider, memory config.MemoryConfig) (config.LLMProvider, bool) {
	for _, id := range []string{
		memory.ProviderBindings.Extraction.ProviderID,
		memory.ProviderBindings.QueryAnalysis.ProviderID,
		memory.ProviderBindings.Prefilter.ProviderID,
	} {
		if provider, ok := providerByID(providers, id); ok {
			return provider, true
		}
	}
	for _, provider := range providers {
		if provider.Enabled && normalizeProtocol(provider.Protocol) == "openai_compatible" {
			return provider, true
		}
	}
	for _, provider := range providers {
		if normalizeProtocol(provider.Protocol) == "openai_compatible" {
			return provider, true
		}
	}
	return config.LLMProvider{}, false
}

func appendProviderAlias(registry *memconfig.ProviderRegistry, alias string, provider config.LLMProvider) {
	appendProviderMapping(registry, memconfig.ProviderMapping{
		ID:        alias,
		Provider:  "openai-compatible",
		Protocol:  "openai_compatible",
		BaseURL:   strings.TrimSpace(provider.BaseURL),
		APIKeyEnv: strings.TrimSpace(provider.APIKeyEnv),
		Enabled:   provider.Enabled,
	})
}

func appendProviderMapping(registry *memconfig.ProviderRegistry, mapping memconfig.ProviderMapping) {
	mapping.ID = strings.TrimSpace(mapping.ID)
	if mapping.ID == "" {
		return
	}
	for _, existing := range registry.LLM {
		if existing.ID == mapping.ID {
			return
		}
	}
	if mapping.TimeoutMS == 0 {
		mapping.TimeoutMS = 30000
	}
	registry.LLM = append(registry.LLM, mapping)
}

func memoryCoreOverridesFromConfig(memory config.MemoryConfig, timezone string) memconfig.ConfigOverrides {
	enabled := memory.Enabled
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	finalMemoryCount := memory.Retrieval.FinalMemoryCount
	contextBudgetTokens := memory.Retrieval.ContextBudgetTokens
	useFTS := memory.Retrieval.UseFTS
	useMirror := memory.Retrieval.UseMirror
	sidecarEnabled := memory.Sidecar.Enabled
	sidecarURL := strings.TrimSpace(memory.Sidecar.URL)
	sidecarAdapter := strings.TrimSpace(memory.Sidecar.Adapter)
	if sidecarURL == "" {
		sidecarURL = "http://127.0.0.1:8765"
	}
	if sidecarAdapter == "" {
		sidecarAdapter = "trivium"
	}
	mirrorEnabled := memory.Retrieval.UseMirror && memory.Sidecar.Enabled
	mirrorSyncLimit := memory.Extraction.MirrorSync.Limit
	overrides := memconfig.ConfigOverrides{
		Enabled: &enabled,
		Core: &memconfig.CoreOverrides{
			Timezone: &timezone,
		},
		Retrieval: &memconfig.RetrievalOverrides{
			FinalMemoryCount:    &finalMemoryCount,
			ContextBudgetTokens: &contextBudgetTokens,
			UseFTS:              &useFTS,
			UseMirror:           &useMirror,
		},
		Sidecar: &memconfig.SidecarOverrides{
			Enabled: &sidecarEnabled,
			URL:     &sidecarURL,
			Adapter: &sidecarAdapter,
		},
		Mirror: &memconfig.MirrorOverrides{
			Enabled:   &mirrorEnabled,
			SyncLimit: &mirrorSyncLimit,
		},
	}
	if prefilter := llmPipelineOverride(memory.ProviderBindings.Prefilter); prefilter != nil {
		ensurePipelineOverrides(&overrides).Prefilter = prefilter
	}
	if extraction := llmPipelineOverride(memory.ProviderBindings.Extraction); extraction != nil {
		ensurePipelineOverrides(&overrides).Extraction = extraction
	}
	if repair := llmPipelineOverride(memory.ProviderBindings.ExtractionRepair); repair != nil {
		ensurePipelineOverrides(&overrides).ExtractionRepair = repair
	}
	if query := llmPipelineOverride(memory.ProviderBindings.QueryAnalysis); query != nil {
		ensurePipelineOverrides(&overrides).QueryAnalysis = query
	}
	if curation := curationLLMOverride(memory.ProviderBindings.Curation); curation != nil {
		overrides.SemanticOps = &memconfig.SemanticOpsOverrides{
			Curation: &memconfig.CurationOverrides{LLM: curation},
		}
	}
	if memory.Retention != nil {
		overrides.Retention = &memconfig.RetentionOverrides{Config: memory.Retention}
	}
	if memory.ForgettingPrivacy != nil {
		overrides.ForgettingPrivacy = memory.ForgettingPrivacy
	}
	if memory.AgentAffect != nil {
		overrides.AgentAffect = memory.AgentAffect
	}
	return overrides
}

func ensurePipelineOverrides(overrides *memconfig.ConfigOverrides) *memconfig.PipelineOverrides {
	if overrides.Pipelines == nil {
		overrides.Pipelines = &memconfig.PipelineOverrides{}
	}
	return overrides.Pipelines
}

func llmPipelineOverride(binding config.MemoryProviderBindingConfig) *memconfig.LLMPipelineOverrides {
	providerID := strings.TrimSpace(binding.ProviderID)
	model := strings.TrimSpace(binding.Model)
	thinkingType := strings.TrimSpace(binding.Thinking.Type)
	maxTokens := binding.MaxTokens
	if providerID == "" && model == "" && thinkingType == "" && maxTokens == 0 {
		return nil
	}
	override := memconfig.LLMPipelineOverrides{}
	if providerID != "" {
		override.ProviderID = &providerID
	}
	if model != "" {
		override.Model = &model
	}
	if maxTokens != 0 {
		override.MaxOutputTokens = &maxTokens
	}
	if thinkingType != "" {
		override.Thinking = &memconfig.ThinkingConfig{Type: thinkingType}
	}
	return &override
}

func curationLLMOverride(binding config.MemoryProviderBindingConfig) *memconfig.CurationLLMOverrides {
	providerID := strings.TrimSpace(binding.ProviderID)
	model := strings.TrimSpace(binding.Model)
	thinkingType := strings.TrimSpace(binding.Thinking.Type)
	maxTokens := binding.MaxTokens
	if providerID == "" && model == "" && thinkingType == "" && maxTokens == 0 {
		return nil
	}
	override := memconfig.CurationLLMOverrides{}
	if providerID != "" {
		override.ProviderID = &providerID
	}
	if model != "" {
		override.Model = &model
	}
	if maxTokens != 0 {
		override.MaxTokens = &maxTokens
	}
	if thinkingType != "" {
		override.Thinking = &memconfig.ThinkingConfig{Type: thinkingType}
	}
	return &override
}

func mergeSidecarStatusOverrides(overrides *memconfig.ConfigOverrides, status sidecarruntime.Status) {
	if status.State == sidecarruntime.StateHealthy {
		enabled := true
		url := strings.TrimSpace(status.URL)
		adapter := strings.TrimSpace(status.Adapter)
		if adapter == "" {
			adapter = "trivium"
		}
		overrides.Sidecar = &memconfig.SidecarOverrides{
			Enabled: &enabled,
			URL:     &url,
			Adapter: &adapter,
		}
		return
	}
	disabled := false
	if overrides.Retrieval == nil {
		overrides.Retrieval = &memconfig.RetrievalOverrides{}
	}
	overrides.Retrieval.UseMirror = &disabled
	if overrides.Sidecar == nil {
		overrides.Sidecar = &memconfig.SidecarOverrides{}
	}
	overrides.Sidecar.Enabled = &disabled
	if overrides.Mirror == nil {
		overrides.Mirror = &memconfig.MirrorOverrides{}
	}
	overrides.Mirror.Enabled = &disabled
}

func (s *Service) sidecarSpec(memory config.MemoryConfig, providers []config.LLMProvider) (sidecarruntime.Spec, []ConfigIssue) {
	spec := sidecarruntime.DefaultSpec()
	cfg := memory.Sidecar
	spec.Enabled = cfg.Enabled
	spec.Managed = cfg.Managed
	spec.Adapter = cfg.Adapter
	spec.Host = cfg.Host
	spec.Port = cfg.Port
	spec.URL = cfg.URL
	spec.WorkingDir = cfg.WorkingDir
	spec.ConfigPath = cfg.ConfigPath
	spec.FailOpen = cfg.FailOpen
	spec.LogPath = cfg.LogPath
	spec.TriviumDir = cfg.TriviumDir
	spec.EmbeddingCacheDBPath = cfg.EmbeddingCachePath
	spec.StartupTimeout = durationMilliseconds(cfg.StartupTimeoutMS)
	spec.ShutdownTimeout = durationMilliseconds(cfg.ShutdownTimeoutMS)

	var issues []ConfigIssue
	if binding, bindingIssues := sidecarProviderBinding("memory.provider_bindings.embedding", memory.ProviderBindings.Embedding, providers, "embedding"); binding.Provider != "" {
		spec.Embedding = binding
		issues = append(issues, bindingIssues...)
	} else {
		issues = append(issues, bindingIssues...)
	}
	if binding, bindingIssues := sidecarProviderBinding("memory.provider_bindings.rerank", memory.ProviderBindings.Rerank, providers, "rerank"); binding.Provider != "" {
		spec.Rerank = binding
		issues = append(issues, bindingIssues...)
	} else {
		issues = append(issues, bindingIssues...)
	}
	if binding, bindingIssues := sidecarProviderBinding("memory.provider_bindings.query_analysis", memory.ProviderBindings.QueryAnalysis, providers, "query_analysis"); binding.Provider != "" {
		spec.QueryAnalysis = binding
		issues = append(issues, bindingIssues...)
	} else {
		issues = append(issues, bindingIssues...)
	}
	if spec.Enabled && spec.Managed && strings.TrimSpace(spec.Adapter) != "fake" && strings.TrimSpace(spec.Embedding.Provider) == "none" {
		issues = append(issues, ConfigIssue{
			Path:     "memory.provider_bindings.embedding.provider_id",
			Severity: "error",
			Message:  "managed trivium sidecar requires an embedding provider binding from Provider Center",
		})
	}
	return spec, issues
}

func sidecarProviderBinding(path string, binding config.MemoryProviderBindingConfig, providers []config.LLMProvider, capability string) (sidecarruntime.ProviderBinding, []ConfigIssue) {
	providerID := strings.TrimSpace(binding.ProviderID)
	if providerID == "" || (!binding.Enabled && strings.TrimSpace(binding.Model) == "") {
		if binding.Enabled {
			return sidecarruntime.ProviderBinding{}, []ConfigIssue{{
				Path:     path + ".provider_id",
				Severity: "error",
				Message:  "provider_id is required when binding is enabled",
			}}
		}
		return sidecarruntime.ProviderBinding{}, nil
	}
	provider, ok := providerByID(providers, providerID)
	if !ok {
		return sidecarruntime.ProviderBinding{}, []ConfigIssue{{
			Path:     path + ".provider_id",
			Severity: "error",
			Message:  fmt.Sprintf("provider %q does not exist", providerID),
		}}
	}
	if !provider.Enabled {
		return sidecarruntime.ProviderBinding{}, []ConfigIssue{{
			Path:     path + ".provider_id",
			Severity: "error",
			Message:  fmt.Sprintf("provider %q is disabled", providerID),
		}}
	}
	if !providerSupportsCapability(provider, capability) {
		return sidecarruntime.ProviderBinding{}, []ConfigIssue{{
			Path:     path + ".provider_id",
			Severity: "error",
			Message:  fmt.Sprintf("provider %q does not advertise %s capability", providerID, capability),
		}}
	}
	sidecarProvider, err := sidecarProviderName(capability, provider)
	if err != nil {
		return sidecarruntime.ProviderBinding{}, []ConfigIssue{{
			Path:     path + ".provider_id",
			Severity: "error",
			Message:  err.Error(),
		}}
	}
	return sidecarruntime.ProviderBinding{
		Provider:    sidecarProvider,
		BaseURL:     strings.TrimSpace(provider.BaseURL),
		EndpointURL: sidecarEndpointURL(provider, capability),
		APIKeyEnv:   strings.TrimSpace(provider.APIKeyEnv),
		Model:       strings.TrimSpace(binding.Model),
		MaxTokens:   binding.MaxTokens,
		Dimensions:  binding.Dimensions,
		TopK:        binding.TopK,
	}, nil
}

func providerByID(providers []config.LLMProvider, id string) (config.LLMProvider, bool) {
	id = strings.TrimSpace(id)
	for _, provider := range providers {
		if provider.ID == id {
			return provider, true
		}
	}
	return config.LLMProvider{}, false
}

func providerSupportsCapability(provider config.LLMProvider, capability string) bool {
	for _, item := range config.NormalizeProviderCapabilities(provider.Capabilities) {
		if item == capability {
			return true
		}
		if capability == "query_analysis" && item == "chat" {
			return true
		}
	}
	return false
}

func sidecarProviderName(capability string, provider config.LLMProvider) (string, error) {
	protocol := normalizeProtocol(provider.Protocol)
	switch capability {
	case "embedding", "query_analysis":
		if protocol != "openai_compatible" {
			return "", fmt.Errorf("%s provider requires openai_compatible protocol, got %q", capability, protocol)
		}
		return "openai-compatible", nil
	case "rerank":
		if protocol == "dashscope_vl" || protocol == "dashscope-vl" {
			return "dashscope-vl", nil
		}
		if protocol == "openai_compatible" && isSiliconFlowRerankProvider(provider) {
			return "siliconflow-rerank", nil
		}
		return "", fmt.Errorf("rerank provider requires dashscope-vl protocol or siliconflow preset, got %q", protocol)
	default:
		return "", fmt.Errorf("unsupported sidecar capability %q", capability)
	}
}

func isSiliconFlowRerankProvider(provider config.LLMProvider) bool {
	preset, ok := llm.ProviderPresetByID(provider.PresetID)
	return ok && preset.ID == "siliconflow" && strings.TrimSpace(preset.RerankPath) != ""
}

func sidecarEndpointURL(provider config.LLMProvider, capability string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	if capability != "rerank" {
		return baseURL
	}
	preset, ok := llm.ProviderPresetByID(provider.PresetID)
	if !ok || strings.TrimSpace(preset.RerankPath) == "" {
		return baseURL
	}
	return baseURL + "/" + strings.Trim(strings.TrimSpace(preset.RerankPath), "/")
}

func normalizeProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "openai-compatible":
		return "openai_compatible"
	default:
		return strings.ToLower(strings.TrimSpace(protocol))
	}
}

func durationMilliseconds(ms int) time.Duration {
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func hasBlockingIssues(issues []ConfigIssue) bool {
	for _, issue := range issues {
		if issue.Severity == "error" || issue.AutoFix != nil {
			return true
		}
	}
	return false
}
