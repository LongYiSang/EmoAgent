package app

import (
	"context"
	"fmt"
	"sync"

	"github.com/longyisang/emoagent/internal/apperrors"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/configcenter"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/memoryhost"
	"github.com/longyisang/emoagent/internal/protocol"
	sidecarruntime "github.com/longyisang/emoagent/internal/sidecar"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/web"
)

var (
	ErrLLMProviderExists             = apperrors.ErrLLMProviderExists
	ErrLLMProviderNotFound           = apperrors.ErrLLMProviderNotFound
	ErrLLMProviderInUse              = apperrors.ErrLLMProviderInUse
	ErrAgentConfigExists             = apperrors.ErrAgentConfigExists
	ErrAgentConfigNotFound           = apperrors.ErrAgentConfigNotFound
	ErrCannotDeleteActiveAgentConfig = apperrors.ErrCannotDeleteActiveAgentConfig
	ErrCannotDeleteLastAgentConfig   = apperrors.ErrCannotDeleteLastAgentConfig
	ErrPersonaExists                 = apperrors.ErrPersonaExists
	ErrPersonaNotFound               = apperrors.ErrPersonaNotFound
	ErrCannotDeleteDefault           = apperrors.ErrCannotDeleteDefault
	ErrSessionNotFound               = apperrors.ErrSessionNotFound
)

// App is the public lifecycle facade for EmoAgent.
type App struct {
	mu     sync.RWMutex
	kernel *Kernel
	cancel context.CancelFunc
}

// New creates an uninitialized App.
func New() *App {
	return &App{}
}

// Init builds the application kernel and starts background watchers.
func (a *App) Init(ctx context.Context, configPath string) error {
	kernel, cancel, err := (Bootstrapper{ConfigPath: configPath}).Build(ctx)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.kernel = kernel
	a.cancel = cancel
	a.mu.Unlock()
	kernel.Infra.Logger.Info("EmoAgent initialized")
	return nil
}

// Run starts the HTTP server and blocks until the context is cancelled.
func (a *App) Run(ctx context.Context) error {
	kernel, err := a.kernelSnapshot()
	if err != nil {
		return err
	}
	server, err := BuildServer(ctx, kernel, a)
	if err != nil {
		return err
	}
	kernel.HTTPServer = server
	return server.Run(ctx)
}

// Shutdown cleanly releases resources.
func (a *App) Shutdown() error {
	a.mu.Lock()
	kernel := a.kernel
	cancel := a.cancel
	a.kernel = nil
	a.cancel = nil
	a.mu.Unlock()
	if kernel == nil {
		if cancel != nil {
			cancel()
		}
		return nil
	}
	return kernel.Close(context.Background())
}

func (a *App) kernelSnapshot() (*Kernel, error) {
	kernel := a.kernelOrNil()
	if kernel == nil || kernel.Infra == nil || kernel.Services == nil {
		return nil, fmt.Errorf("app is not initialized")
	}
	return kernel, nil
}

func (a *App) kernelOrNil() *Kernel {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.kernel
}

func (a *App) services() (*Services, error) {
	kernel, err := a.kernelSnapshot()
	if err != nil {
		return nil, err
	}
	return kernel.Services, nil
}

func (a *App) ListLLMProviders() ([]config.LLMProvider, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.LLMProviders.List()
}

func (a *App) GetLLMProvider(id string) (*config.LLMProvider, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.LLMProviders.Get(id)
}

func (a *App) CreateLLMProvider(provider config.LLMProvider) error {
	services, err := a.services()
	if err != nil {
		return err
	}
	return services.LLMProviders.Create(provider)
}

func (a *App) UpdateLLMProvider(id string, provider config.LLMProvider) error {
	services, err := a.services()
	if err != nil {
		return err
	}
	return services.LLMProviders.Update(id, provider)
}

func (a *App) DeleteLLMProvider(id string) error {
	services, err := a.services()
	if err != nil {
		return err
	}
	return services.LLMProviders.Delete(id)
}

func (a *App) RefreshLLMProviderModels(id string) ([]llm.ModelInfo, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.LLMProviders.RefreshModels(id)
}

func (a *App) GetLLMProviderModels(id string) ([]llm.ModelInfo, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.LLMProviders.Models(id)
}

func (a *App) GetAgentAffectConfig(ctx context.Context) (configcenter.AgentAffectConfigResponse, error) {
	services, err := a.services()
	if err != nil {
		return configcenter.AgentAffectConfigResponse{}, err
	}
	return services.Config.GetAgentAffectConfig(ctx)
}

func (a *App) UpdateAgentAffectConfig(ctx context.Context, cfg config.AgentAffectConfig) (configcenter.EffectiveConfig, error) {
	services, err := a.services()
	if err != nil {
		return configcenter.EffectiveConfig{}, err
	}
	effective, err := services.Config.UpdateAgentAffectConfig(ctx, cfg)
	if err != nil {
		return configcenter.EffectiveConfig{}, err
	}
	services.Chat.UpdateAgentAffect()
	return effective, nil
}

func (a *App) GetAgentAffectCurrent(ctx context.Context, req web.AgentAffectCurrentRequest) (web.AgentAffectCurrentResponse, error) {
	services, err := a.services()
	if err != nil {
		return web.AgentAffectCurrentResponse{}, err
	}
	return services.AgentAffect.GetCurrentMood(ctx, req)
}

func (a *App) GetAgentAffectProfile(ctx context.Context, personaID string) (web.AgentAffectProfileResponse, error) {
	services, err := a.services()
	if err != nil {
		return web.AgentAffectProfileResponse{}, err
	}
	return services.AgentAffect.GetProfile(ctx, personaID)
}

func (a *App) UpdateAgentAffectProfile(ctx context.Context, profile web.AgentAffectProfileResponse) (web.AgentAffectProfileResponse, error) {
	services, err := a.services()
	if err != nil {
		return web.AgentAffectProfileResponse{}, err
	}
	return services.AgentAffect.UpdateProfile(ctx, profile)
}

func (a *App) ListAgentAffectHistory(ctx context.Context, req web.AgentAffectHistoryRequest) (web.AgentAffectHistoryResponse, error) {
	services, err := a.services()
	if err != nil {
		return web.AgentAffectHistoryResponse{}, err
	}
	return services.AgentAffect.ListHistory(ctx, req)
}

func (a *App) ListAgentAffectPluginWrites(ctx context.Context, req web.AgentAffectPluginWritesRequest) (web.AgentAffectPluginWritesResponse, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.AgentAffect.ListPluginWrites(ctx, req)
}

func (a *App) EvaluateAgentAffect(ctx context.Context, req web.AgentAffectEvaluateRequest) (web.AgentAffectEvaluateResponse, error) {
	services, err := a.services()
	if err != nil {
		return web.AgentAffectEvaluateResponse{}, err
	}
	return services.AgentAffect.EvaluateMoodImpact(ctx, req)
}

func (a *App) SubmitAgentAffect(ctx context.Context, req web.AgentAffectSubmitRequest) (web.AgentAffectSubmitResponse, error) {
	services, err := a.services()
	if err != nil {
		return web.AgentAffectSubmitResponse{}, err
	}
	return services.AgentAffect.SubmitMoodImpact(ctx, req)
}

func (a *App) ApplyAgentAffectDelta(ctx context.Context, req web.AgentAffectDeltaRequest) (web.AgentAffectDeltaResponse, error) {
	services, err := a.services()
	if err != nil {
		return web.AgentAffectDeltaResponse{}, err
	}
	return services.AgentAffect.ApplyMoodDelta(ctx, req)
}

func (a *App) ResetAgentAffect(ctx context.Context, req web.AgentAffectResetRequest) (web.AgentAffectResetResponse, error) {
	services, err := a.services()
	if err != nil {
		return web.AgentAffectResetResponse{}, err
	}
	return services.AgentAffect.ResetMood(ctx, req)
}

func (a *App) PreviewAgentAffectPrompt(ctx context.Context, req web.AgentAffectPromptPreviewRequest) (web.AgentAffectPromptPreviewResponse, error) {
	services, err := a.services()
	if err != nil {
		return web.AgentAffectPromptPreviewResponse{}, err
	}
	return services.AgentAffect.PreviewPrompt(ctx, req)
}

func (a *App) GetLLMProviderEnvStatus(id string) (configcenter.ProviderEnvStatus, error) {
	services, err := a.services()
	if err != nil {
		return configcenter.ProviderEnvStatus{}, err
	}
	return services.LLMProviders.EnvStatus(id)
}

func (a *App) ListAgentConfigs() ([]config.AgentConfig, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.AgentRuntime.ListAgentConfigs()
}

func (a *App) GetAgentConfig(id string) (*config.AgentConfig, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.AgentRuntime.GetAgentConfig(id)
}

func (a *App) GetActiveAgentConfig() (*config.AgentConfig, bool, error) {
	services, err := a.services()
	if err != nil {
		return nil, false, err
	}
	return services.AgentRuntime.GetActiveAgentConfig()
}

func (a *App) CreateAgentConfig(agent config.AgentConfig) error {
	services, err := a.services()
	if err != nil {
		return err
	}
	return services.AgentRuntime.CreateAgentConfig(agent)
}

func (a *App) UpdateAgentConfig(id string, agent config.AgentConfig) error {
	services, err := a.services()
	if err != nil {
		return err
	}
	return services.AgentRuntime.UpdateAgentConfig(id, agent)
}

func (a *App) DeleteAgentConfig(id string) error {
	services, err := a.services()
	if err != nil {
		return err
	}
	return services.AgentRuntime.DeleteAgentConfig(id)
}

func (a *App) ActivateAgentConfig(id string) error {
	services, err := a.services()
	if err != nil {
		return err
	}
	return services.AgentRuntime.Activate(id)
}

func (a *App) GetPersona(name string) (*config.Persona, bool) {
	kernel := a.kernelOrNil()
	if kernel == nil || kernel.Services == nil || kernel.Services.Personas == nil {
		return nil, false
	}
	return kernel.Services.Personas.Get(name)
}

func (a *App) ListPersonas() map[string]*config.Persona {
	kernel := a.kernelOrNil()
	if kernel == nil || kernel.Services == nil || kernel.Services.Personas == nil {
		return map[string]*config.Persona{}
	}
	return kernel.Services.Personas.List()
}

func (a *App) GetDefaultPersonaName() string {
	kernel := a.kernelOrNil()
	if kernel == nil || kernel.Services == nil || kernel.Services.AgentRuntime == nil {
		return ""
	}
	return kernel.Services.AgentRuntime.ActivePersonaKey()
}

func (a *App) CreatePersona(key string, p *config.Persona) error {
	services, err := a.services()
	if err != nil {
		return err
	}
	return services.Personas.Create(key, p)
}

func (a *App) UpdatePersona(key string, p *config.Persona) error {
	services, err := a.services()
	if err != nil {
		return err
	}
	return services.Personas.Update(key, p)
}

func (a *App) DeletePersona(key string) error {
	services, err := a.services()
	if err != nil {
		return err
	}
	return services.Personas.Delete(key, services.AgentRuntime.ActivePersonaKey())
}

func (a *App) GetProgressPhrases(key string) (map[string][]string, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.Personas.GetProgressPhrases(key)
}

func (a *App) UpdateProgressPhrases(key string, phrases map[string][]string) error {
	services, err := a.services()
	if err != nil {
		return err
	}
	return services.Personas.UpdateProgressPhrases(key, phrases)
}

func (a *App) GetChatSettings() config.ChatConfig {
	kernel := a.kernelOrNil()
	if kernel == nil || kernel.Services == nil || kernel.Services.Config == nil {
		return config.ChatConfig{}
	}
	return kernel.Services.Config.GetChatSettings()
}

func (a *App) UpdateChatSettings(settings config.ChatConfig) error {
	services, err := a.services()
	if err != nil {
		return err
	}
	return services.Config.UpdateChatSettings(settings, services.Chat)
}

func (a *App) ListSessions(ctx context.Context, persona string, limit int) ([]storage.SessionSummary, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.Sessions.List(ctx, persona, limit)
}

func (a *App) GetLatestSession(ctx context.Context, persona string) (*storage.SessionSummary, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.Sessions.Latest(ctx, persona)
}

func (a *App) GetSessionDetail(ctx context.Context, id string) (*storage.SessionRecord, []storage.MessageRecord, error) {
	services, err := a.services()
	if err != nil {
		return nil, nil, err
	}
	return services.Sessions.Detail(ctx, id)
}

func (a *App) DeleteSession(ctx context.Context, id string) error {
	services, err := a.services()
	if err != nil {
		return err
	}
	return services.Sessions.Delete(ctx, id)
}

func (a *App) ListSessionApprovals(ctx context.Context, sessionID string) ([]protocol.ApprovalRequest, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.Sessions.ListApprovals(ctx, sessionID)
}

func (a *App) QueueMemoryExtraction(ctx context.Context, req web.MemoryExtractionRequest) (web.MemoryExtractionQueueResponse, error) {
	services, err := a.services()
	if err != nil {
		return web.MemoryExtractionQueueResponse{}, err
	}
	return services.Memory.QueueExtraction(ctx, req)
}

func (a *App) ListMemoryExtractions(ctx context.Context, req web.MemoryExtractionListRequest) ([]storage.MemoryExtractionJob, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.Memory.ListExtractions(ctx, req)
}

func (a *App) RunNaturalMemory(ctx context.Context, req web.NaturalMemoryRunRequest) (memoryhost.NaturalMemoryRunResponse, error) {
	services, err := a.services()
	if err != nil {
		return memoryhost.NaturalMemoryRunResponse{}, err
	}
	return services.Memory.RunNatural(ctx, req)
}

func (a *App) LatestNaturalMemoryRun(ctx context.Context) (*memoryhost.NaturalMemoryRunResponse, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.Memory.LatestNatural(ctx)
}

func (a *App) ListMemorySegments(ctx context.Context, sessionID string) ([]storage.MemorySegment, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.Memory.ListSegments(ctx, sessionID)
}

func (a *App) GetEffectiveConfig(ctx context.Context) (configcenter.EffectiveConfig, error) {
	services, err := a.services()
	if err != nil {
		return configcenter.EffectiveConfig{}, err
	}
	return services.Config.GetEffective(ctx)
}

func (a *App) ValidateConfig(ctx context.Context, req configcenter.ValidateRequest) (configcenter.ValidateResponse, error) {
	services, err := a.services()
	if err != nil {
		return configcenter.ValidateResponse{}, err
	}
	return services.Config.Validate(ctx, req)
}

func (a *App) ListConfigIssues(ctx context.Context) ([]configcenter.ConfigIssue, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.Config.ListIssues(ctx)
}

func (a *App) GetMemoryConfig(ctx context.Context) (configcenter.MemoryConfigResponse, error) {
	services, err := a.services()
	if err != nil {
		return configcenter.MemoryConfigResponse{}, err
	}
	return services.Config.GetMemoryConfig(ctx)
}

func (a *App) UpdateMemoryConfig(ctx context.Context, memory config.MemoryConfig) (configcenter.EffectiveConfig, error) {
	services, err := a.services()
	if err != nil {
		return configcenter.EffectiveConfig{}, err
	}
	return services.Config.UpdateMemoryConfig(ctx, memory)
}

func (a *App) GetMemoryFeatures(ctx context.Context) (configcenter.MemoryConfigResponse, error) {
	services, err := a.services()
	if err != nil {
		return configcenter.MemoryConfigResponse{}, err
	}
	return services.Config.GetMemoryFeatures(ctx)
}

func (a *App) UpdateMemoryFeatures(ctx context.Context, memory config.MemoryConfig) (configcenter.EffectiveConfig, error) {
	services, err := a.services()
	if err != nil {
		return configcenter.EffectiveConfig{}, err
	}
	return services.Config.UpdateMemoryFeatures(ctx, memory)
}

func (a *App) GetSidecarStatus(ctx context.Context) (sidecarruntime.Status, error) {
	services, err := a.services()
	if err != nil {
		return sidecarruntime.Status{}, err
	}
	return services.Sidecar.Status(ctx)
}

func (a *App) StartSidecar(ctx context.Context) (sidecarruntime.Status, error) {
	services, err := a.services()
	if err != nil {
		return sidecarruntime.Status{}, err
	}
	return services.Sidecar.Start(ctx)
}

func (a *App) StopSidecar(ctx context.Context) (sidecarruntime.Status, error) {
	services, err := a.services()
	if err != nil {
		return sidecarruntime.Status{}, err
	}
	return services.Sidecar.Stop(ctx)
}

func (a *App) RestartSidecar(ctx context.Context) (sidecarruntime.Status, error) {
	services, err := a.services()
	if err != nil {
		return sidecarruntime.Status{}, err
	}
	return services.Sidecar.Restart(ctx)
}

func (a *App) GetSidecarGeneratedConfig(ctx context.Context) (string, error) {
	services, err := a.services()
	if err != nil {
		return "", err
	}
	return services.Sidecar.GeneratedConfig(ctx)
}

func (a *App) GetSidecarLogs(ctx context.Context, maxBytes int) (string, error) {
	services, err := a.services()
	if err != nil {
		return "", err
	}
	return services.Sidecar.Logs(ctx, maxBytes)
}
