package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
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
	Memory       *MemoryService
	Tools        *ToolService
	Plugins      *PluginService
	Work         *WorkService
	Chat         *ChatService
	Sessions     *SessionService
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
	services.Memory = &MemoryService{infra: infra, config: services.Config, sidecar: services.Sidecar}
	services.Tools = &ToolService{infra: infra}
	services.Plugins = &PluginService{infra: infra, tools: services.Tools, agentAffect: services.AgentAffect}
	services.AgentAffect.plugins = services.Plugins
	services.Work = &WorkService{
		infra:        infra,
		tools:        services.Tools,
		plugins:      services.Plugins,
		agentRuntime: services.AgentRuntime,
	}
	services.Chat = &ChatService{
		infra:        infra,
		agentRuntime: services.AgentRuntime,
		tools:        services.Tools,
		plugins:      services.Plugins,
		work:         services.Work,
		memory:       services.Memory,
		agentAffect:  services.AgentAffect,
	}
	services.Sessions = &SessionService{infra: infra, work: services.Work}
	services.AgentRuntime.chat = services.Chat
	return services
}

func (k *Kernel) Close(ctx context.Context) error {
	if k == nil {
		return nil
	}
	if k.Background.Cancel != nil {
		k.Background.Cancel()
		k.Background.Cancel = nil
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
