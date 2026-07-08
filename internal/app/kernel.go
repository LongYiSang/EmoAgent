package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/conversation"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/logcenter"
	"github.com/longyisang/emoagent/internal/runtimeenv"
	"github.com/longyisang/emoagent/internal/storage"
)

type Kernel struct {
	Infra      *Infra
	Services   *Services
	Background Background
	HTTPServer *Server
}

type Infra struct {
	Config      *config.Config
	DB          *storage.DB
	Logger      *slog.Logger
	LogCenter   *logcenter.Service
	LLM         llm.Client
	Environment runtimeenv.Facts
	ProjectRoot string
}

type Background struct {
	Cancel context.CancelFunc
}

type Services struct {
	Config       *ConfigService
	Personas     *PersonaService
	LLMProviders *LLMProviderService
	AgentRuntime *AgentRuntimeService
	AgentAffect  *AgentAffectService
	Sidecar      *SidecarService
	LogCenter    *logcenter.Service
	Memory       *MemoryService
	Media        *MediaService
	Tools        *ToolService
	Resource     *ResourceService
	Plugins      *PluginService
	Work         *WorkService
	Conversation *ConversationService
	Commands     *CommandService
	Chat         *ChatService
	Platforms    *PlatformService
	Sessions     *SessionService
	PromptCenter *PromptCenterService
}

func NewKernel(infra *Infra) *Kernel {
	services := newServices(infra)
	return &Kernel{
		Infra:    infra,
		Services: services,
	}
}

func newServices(infra *Infra) *Services {
	services := &Services{}
	services.Config = &ConfigService{infra: infra}
	services.Personas = &PersonaService{infra: infra}
	services.LLMProviders = &LLMProviderService{infra: infra}
	services.AgentRuntime = &AgentRuntimeService{infra: infra, personas: services.Personas}
	services.AgentAffect = &AgentAffectService{infra: infra, agentRuntime: services.AgentRuntime}
	services.Sidecar = &SidecarService{infra: infra, config: services.Config}
	services.LogCenter = infra.LogCenter
	if services.LogCenter == nil {
		services.LogCenter = logcenter.NewService()
		infra.LogCenter = services.LogCenter
	}
	services.Memory = &MemoryService{infra: infra, config: services.Config, sidecar: services.Sidecar}
	services.Media = &MediaService{infra: infra}
	services.Tools = &ToolService{infra: infra}
	services.Resource = &ResourceService{infra: infra}
	services.Config.tools = services.Tools
	services.Plugins = &PluginService{infra: infra, tools: services.Tools, agentAffect: services.AgentAffect, agentRuntime: services.AgentRuntime}
	services.LogCenter.SetProviders(sidecarLogSource{service: services.Sidecar}, pluginLogSource{service: services.Plugins})
	services.AgentAffect.plugins = services.Plugins
	services.Work = &WorkService{
		infra:        infra,
		tools:        services.Tools,
		plugins:      services.Plugins,
		agentRuntime: services.AgentRuntime,
	}
	services.Conversation = &ConversationService{
		bindings: conversation.NewBindingService(infra.DB, nil),
		timeline: conversation.NewTimelineEventStore(infra.DB),
		runs:     conversation.NewRunRegistry(),
	}
	services.Commands = NewCommandService()
	services.Commands.configure(infra, services.Conversation, services.Memory, services.AgentRuntime)
	services.Commands.LoadCommandConfigs(context.Background())
	services.Commands.pluginRuntime = services.Plugins
	services.Plugins.commands = services.Commands
	services.Chat = &ChatService{
		infra:        infra,
		agentRuntime: services.AgentRuntime,
		tools:        services.Tools,
		plugins:      services.Plugins,
		work:         services.Work,
		memory:       services.Memory,
		media:        services.Media,
		llmProviders: services.LLMProviders,
		agentAffect:  services.AgentAffect,
		conversation: services.Conversation,
		commands:     services.Commands,
	}
	services.Commands.chat = services.Chat
	services.Platforms = NewPlatformService(infra, services.Conversation, services.Commands, services.Chat, services.AgentRuntime, services.Personas, services.Media)
	services.Sessions = &SessionService{infra: infra, work: services.Work}
	services.PromptCenter = &PromptCenterService{infra: infra, agentRuntime: services.AgentRuntime, personas: services.Personas, memory: services.Memory, agentAffect: services.AgentAffect}
	services.AgentRuntime.chat = services.Chat
	return services
}

type ConversationService struct {
	bindings *conversation.BindingService
	timeline *conversation.TimelineEventStore
	runs     *conversation.RunRegistry
}

func (s *ConversationService) Bindings() *conversation.BindingService {
	if s == nil {
		return nil
	}
	return s.bindings
}

func (s *ConversationService) Timeline() *conversation.TimelineEventStore {
	if s == nil {
		return nil
	}
	return s.timeline
}

func (s *ConversationService) RunRegistry() *conversation.RunRegistry {
	if s == nil {
		return nil
	}
	return s.runs
}

func (k *Kernel) Close(ctx context.Context) error {
	if k == nil {
		return nil
	}
	if k.Background.Cancel != nil {
		k.Background.Cancel()
		k.Background.Cancel = nil
	}
	if k.Services != nil && k.Services.LogCenter != nil {
		k.Services.LogCenter.Close()
	}

	var closeErr error
	if k.HTTPServer != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, httpServerShutdownTimeout)
		if err := k.HTTPServer.Shutdown(shutdownCtx); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("shutdown http server: %w", err))
		}
		cancel()
		k.HTTPServer = nil
	}
	if k.Services != nil && k.Services.Plugins != nil {
		if err := k.Services.Plugins.Close(ctx); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}
	if k.Services != nil && k.Services.Memory != nil {
		if err := k.Services.Memory.Close(ctx); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}
	if k.Services != nil && k.Services.Sidecar != nil {
		if err := k.Services.Sidecar.Close(ctx); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}
	if k.Infra != nil && k.Infra.DB != nil {
		if err := k.Infra.DB.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close database: %w", err))
		} else {
			k.Infra.DB = nil
		}
	}
	if k.Infra != nil && k.Infra.Logger != nil {
		k.Infra.Logger.Info("EmoAgent stopped")
	}
	return closeErr
}
