package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/work"
)

type ActiveAgentRuntime struct {
	ID             string
	PersonaKey     string
	EmotionMain    ModelRuntime
	EmotionSummary ModelRuntime
	WorkMain       ModelRuntime
	WorkSummary    ModelRuntime
	Context        config.ContextConfig
}

type ModelRuntime struct {
	Provider config.LLMProvider
	Model    string
	Params   llm.RequestParams
	Client   llm.Client
}

type AgentRuntimeService struct {
	infra    *Infra
	personas *PersonaService
	chat     *ChatService
	mu       sync.RWMutex
	active   *ActiveAgentRuntime
}

func (s *AgentRuntimeService) BootstrapAgentConfigs() error {
	providers, err := s.infra.DB.ListLLMProviders()
	if err != nil {
		return err
	}
	agents, err := s.infra.DB.ListAgentConfigs()
	if err != nil {
		return err
	}
	if len(providers) > 0 || len(agents) > 0 {
		if _, found, err := s.infra.DB.GetActiveAgentConfig(); err != nil {
			return err
		} else if !found && len(agents) > 0 {
			return s.infra.DB.SetActiveAgentConfig(agents[0].ID)
		}
		return nil
	}

	if len(s.infra.Config.LLMProviders) == 0 || len(s.infra.Config.AgentConfigs) == 0 {
		return fmt.Errorf("active agent config is not configured: config.yaml must define llm_providers and agent_configs")
	}
	for _, provider := range s.infra.Config.LLMProviders {
		if err := provider.Validate(); err != nil {
			return err
		}
		if err := s.infra.DB.UpsertLLMProvider(provider); err != nil {
			return err
		}
	}
	for _, agent := range s.infra.Config.AgentConfigs {
		if err := agent.Validate(); err != nil {
			return err
		}
		if err := s.infra.DB.UpsertAgentConfig(agent); err != nil {
			return err
		}
	}
	activeID := strings.TrimSpace(s.infra.Config.Agent.ActiveConfig)
	if activeID == "" {
		activeID = s.infra.Config.AgentConfigs[0].ID
	}
	return s.infra.DB.SetActiveAgentConfig(activeID)
}

func (s *AgentRuntimeService) LoadActive() error {
	activeID, found, err := s.infra.DB.GetActiveAgentConfig()
	if err != nil {
		return err
	}
	if !found || strings.TrimSpace(activeID) == "" {
		s.setActive(nil)
		return nil
	}
	runtime, err := s.Build(activeID, false)
	if err != nil {
		s.infra.Logger.Warn("active agent config is not currently usable", "agent_config", activeID, "error", err)
		s.setActive(nil)
		return nil
	}
	s.setActive(runtime)
	return nil
}

func (s *AgentRuntimeService) ListAgentConfigs() ([]config.AgentConfig, error) {
	return s.infra.DB.ListAgentConfigs()
}

func (s *AgentRuntimeService) GetAgentConfig(id string) (*config.AgentConfig, error) {
	agent, err := s.infra.DB.GetAgentConfig(context.Background(), id)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, ErrAgentConfigNotFound
	}
	return agent, nil
}

func (s *AgentRuntimeService) GetActiveAgentConfig() (*config.AgentConfig, bool, error) {
	activeID, found, err := s.infra.DB.GetActiveAgentConfig()
	if err != nil || !found {
		return nil, false, err
	}
	agent, err := s.infra.DB.GetAgentConfig(context.Background(), activeID)
	if err != nil {
		return nil, false, err
	}
	return agent, agent != nil, nil
}

func (s *AgentRuntimeService) CreateAgentConfig(agent config.AgentConfig) error {
	if err := agent.Validate(); err != nil {
		return err
	}
	if _, err := agent.ResolveContextConfig(s.GlobalContextConfig()); err != nil {
		return err
	}
	existing, err := s.infra.DB.GetAgentConfig(context.Background(), agent.ID)
	if err != nil {
		return err
	}
	if existing != nil {
		return ErrAgentConfigExists
	}
	return s.infra.DB.UpsertAgentConfig(agent)
}

func (s *AgentRuntimeService) UpdateAgentConfig(id string, agent config.AgentConfig) error {
	agent.ID = id
	if err := agent.Validate(); err != nil {
		return err
	}
	if _, err := agent.ResolveContextConfig(s.GlobalContextConfig()); err != nil {
		return err
	}
	existing, err := s.infra.DB.GetAgentConfig(context.Background(), id)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrAgentConfigNotFound
	}
	if err := s.infra.DB.UpsertAgentConfig(agent); err != nil {
		return err
	}
	active, ok, err := s.infra.DB.GetActiveAgentConfig()
	if err != nil {
		return err
	}
	if ok && active == id {
		return s.Activate(id)
	}
	return nil
}

func (s *AgentRuntimeService) DeleteAgentConfig(id string) error {
	err := s.infra.DB.DeleteAgentConfig(id)
	if errors.Is(err, storage.ErrCannotDeleteActiveAgentConfig) {
		return ErrCannotDeleteActiveAgentConfig
	}
	if errors.Is(err, storage.ErrCannotDeleteLastAgentConfig) {
		return ErrCannotDeleteLastAgentConfig
	}
	return err
}

func (s *AgentRuntimeService) Activate(id string) error {
	runtime, err := s.Build(id, true)
	if err != nil {
		return err
	}
	if err := s.infra.DB.SetActiveAgentConfig(id); err != nil {
		return err
	}
	s.setActive(runtime)
	if s.chat != nil {
		s.chat.UpdateAgentRuntime(runtime)
	}
	return nil
}

func (s *AgentRuntimeService) Build(id string, requireClient bool) (*ActiveAgentRuntime, error) {
	agent, err := s.infra.DB.GetAgentConfig(context.Background(), id)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, ErrAgentConfigNotFound
	}
	if !s.personas.Exists(agent.PersonaKey) {
		return nil, fmt.Errorf("active agent config persona not found")
	}
	contextCfg, err := agent.ResolveContextConfig(s.GlobalContextConfig())
	if err != nil {
		return nil, err
	}

	emotionMain, err := s.modelRuntime(agent.Emotion.Main, requireClient)
	if err != nil {
		return nil, fmt.Errorf("emotion.main: %w", err)
	}
	emotionSummary, err := s.modelRuntime(agent.Emotion.Summary, requireClient)
	if err != nil {
		return nil, fmt.Errorf("emotion.summary: %w", err)
	}
	workMain, err := s.modelRuntime(agent.Work.Main, requireClient)
	if err != nil {
		return nil, fmt.Errorf("work.main: %w", err)
	}
	workSummary, err := s.modelRuntime(agent.Work.Summary, requireClient)
	if err != nil {
		return nil, fmt.Errorf("work.summary: %w", err)
	}

	return &ActiveAgentRuntime{
		ID:             agent.ID,
		PersonaKey:     agent.PersonaKey,
		EmotionMain:    emotionMain,
		EmotionSummary: emotionSummary,
		WorkMain:       workMain,
		WorkSummary:    workSummary,
		Context:        contextCfg,
	}, nil
}

func (s *AgentRuntimeService) NewWorkRuntime(dispatcher *tool.Dispatcher, registry *tool.Registry) (*work.Runtime, error) {
	active := s.Active()
	if active == nil || active.WorkMain.Client == nil {
		return nil, fmt.Errorf("active agent config is not configured")
	}
	decider := work.NewLLMRuntimeDecider(active.WorkMain.Client, active.WorkMain.Model)
	return work.NewRuntime(work.RuntimeConfig{
		LLM:                      active.WorkMain.Client,
		SummaryClient:            active.WorkSummary.Client,
		SummaryModel:             active.WorkSummary.Model,
		SummaryParams:            cloneRequestParams(active.WorkSummary.Params),
		Provider:                 toolProviderName(active.WorkMain.Provider.Protocol),
		Model:                    active.WorkMain.Model,
		Params:                   cloneRequestParams(active.WorkMain.Params),
		MaxTokens:                active.WorkMain.Params.MaxTokens,
		Temperature:              derefFloat64(active.WorkMain.Params.Temperature, 0),
		MaxToolRounds:            s.infra.Config.Work.MaxToolRounds,
		MaxInputTokens:           s.infra.Config.Work.MaxInputTokens,
		CompressSoftRatio:        s.infra.Config.Work.CompressSoftRatio,
		CompressKeepRounds:       s.infra.Config.Work.CompressKeepRounds,
		ToolSnipSoftTokens:       s.infra.Config.Work.ToolSnipSoftTokens,
		ToolSnipHardTokens:       s.infra.Config.Work.ToolSnipHardTokens,
		Registry:                 registry,
		Dispatcher:               dispatcher,
		Logger:                   s.infra.Logger,
		Decider:                  decider,
		MaxEscalations:           s.infra.Config.Work.MaxEscalationsPerTask,
		PendingSnapshotMaxTokens: s.infra.Config.Work.PendingSnapshotMaxTokens,
		EnvironmentFacts:         s.infra.Environment,
	}), nil
}

func (s *AgentRuntimeService) Active() *ActiveAgentRuntime {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneActiveAgentRuntime(s.active)
}

func (s *AgentRuntimeService) ActivePersonaKey() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.active != nil {
		return s.active.PersonaKey
	}
	return ""
}

func (s *AgentRuntimeService) GlobalContextConfig() config.ContextConfig {
	if s != nil && s.infra.Config != nil {
		if err := s.infra.Config.Context.Validate(); err == nil {
			return s.infra.Config.Context
		}
	}
	return config.DefaultConfig().Context
}

func (s *AgentRuntimeService) modelRuntime(binding config.ModelBinding, requireClient bool) (ModelRuntime, error) {
	record, err := s.infra.DB.GetLLMProvider(context.Background(), binding.ProviderID)
	if err != nil {
		return ModelRuntime{}, err
	}
	if record == nil {
		return ModelRuntime{}, fmt.Errorf("provider %q not found", binding.ProviderID)
	}
	provider := record.LLMProvider
	if !provider.Enabled {
		return ModelRuntime{}, fmt.Errorf("provider %q is disabled", binding.ProviderID)
	}
	if strings.TrimSpace(binding.Model) == "" {
		return ModelRuntime{}, fmt.Errorf("model is required")
	}
	client, err := s.buildClientForProvider(provider)
	if err != nil {
		if requireClient {
			return ModelRuntime{}, err
		}
		return ModelRuntime{Provider: provider, Model: binding.Model, Params: cloneRequestParams(binding.Params)}, nil
	}
	return ModelRuntime{
		Provider: provider,
		Model:    binding.Model,
		Params:   cloneRequestParams(binding.Params),
		Client:   client,
	}, nil
}

func (s *AgentRuntimeService) buildClientForProvider(provider config.LLMProvider) (llm.Client, error) {
	return llm.NewClient(llm.ProviderConfig{
		ID:        provider.ID,
		PresetID:  provider.PresetID,
		Protocol:  provider.Protocol,
		BaseURL:   provider.BaseURL,
		APIKeyEnv: provider.APIKeyEnv,
	}, s.infra.Logger)
}

func (s *AgentRuntimeService) setActive(runtime *ActiveAgentRuntime) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = cloneActiveAgentRuntime(runtime)
	if runtime == nil {
		s.infra.LLM = nil
		return
	}
	s.infra.LLM = runtime.EmotionMain.Client
}
