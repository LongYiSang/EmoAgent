package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/longyisang/emoagent/internal/chat"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/web"
)

const httpServerShutdownTimeout = 5 * time.Second

type Server struct {
	httpServer *http.Server
	logger     interface {
		Info(string, ...any)
	}
}

func BuildServer(ctx context.Context, kernel *Kernel, facade *App) (*Server, error) {
	cfg := config.DefaultConfig()
	if kernel.Infra.Config != nil {
		cfg = kernel.Infra.Config
	}
	if err := kernel.Services.Tools.EnsureRegistry(); err != nil {
		return nil, err
	}
	registry := kernel.Services.Tools.Registry()
	dispatcher := tool.NewDispatcher(registry, tool.MinimalSchemaValidator{}, kernel.Infra.Logger)
	if err := kernel.Services.Plugins.Configure(ctx, dispatcher, nil); err != nil {
		return nil, err
	}
	if err := kernel.Services.Work.Configure(ctx, dispatcher); err != nil {
		return nil, err
	}

	engine := kernel.Services.Chat.BuildEngine(dispatcher)
	kernel.Services.Chat.StartBackground(ctx)
	chatHandler := chat.NewHandler(engine, facade, kernel.Infra.Logger, kernel.Services.Chat.HandlerOptions()...)

	api := web.NewAPIHandler(facade, kernel.Infra.Logger)

	mux := http.NewServeMux()
	registerRoutes(mux, api, chatHandler, web.NewStaticHandler(web.StaticFS))

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return &Server{
		httpServer: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
		logger: kernel.Infra.Logger,
	}, nil
}

func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("server started", "url", fmt.Sprintf("http://%s", s.httpServer.Addr))
		if listenErr := s.httpServer.ListenAndServe(); listenErr != nil && listenErr != http.ErrServerClosed {
			errCh <- listenErr
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), httpServerShutdownTimeout)
		defer cancel()
		return s.httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err == nil {
			return nil
		}
		return err
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil || s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

func registerRoutes(mux *http.ServeMux, api *web.APIHandler, chatHandler http.Handler, staticHandler http.Handler) {
	mux.HandleFunc("GET /api/llm-providers", api.HandleListLLMProviders)
	mux.HandleFunc("GET /api/llm-provider-presets", api.HandleListLLMProviderPresets)
	mux.HandleFunc("POST /api/llm-providers", api.HandleCreateLLMProvider)
	mux.HandleFunc("GET /api/llm-providers/{id}", api.HandleGetLLMProvider)
	mux.HandleFunc("PUT /api/llm-providers/{id}", api.HandleUpdateLLMProvider)
	mux.HandleFunc("DELETE /api/llm-providers/{id}", api.HandleDeleteLLMProvider)
	mux.HandleFunc("POST /api/llm-providers/{id}/refresh-models", api.HandleRefreshLLMProviderModels)
	mux.HandleFunc("GET /api/llm-providers/{id}/models", api.HandleGetLLMProviderModels)
	mux.HandleFunc("GET /api/llm-providers/{id}/env-status", api.HandleGetLLMProviderEnvStatus)
	mux.HandleFunc("GET /api/providers/{id}/env-status", api.HandleGetLLMProviderEnvStatus)
	mux.HandleFunc("POST /api/providers/{id}/test", api.HandleTestProvider)
	mux.HandleFunc("GET /api/config/effective", api.HandleGetConfigEffective)
	mux.HandleFunc("POST /api/config/validate", api.HandleValidateConfig)
	mux.HandleFunc("GET /api/config/issues", api.HandleListConfigIssues)
	mux.HandleFunc("GET /api/memory/config", api.HandleGetMemoryConfig)
	mux.HandleFunc("PUT /api/memory/config", api.HandleUpdateMemoryConfig)
	mux.HandleFunc("GET /api/memory/features", api.HandleGetMemoryFeatures)
	mux.HandleFunc("PUT /api/memory/features", api.HandleUpdateMemoryFeatures)
	mux.HandleFunc("GET /api/sidecar/status", api.HandleGetSidecarStatus)
	mux.HandleFunc("POST /api/sidecar/start", api.HandleStartSidecar)
	mux.HandleFunc("POST /api/sidecar/stop", api.HandleStopSidecar)
	mux.HandleFunc("POST /api/sidecar/restart", api.HandleRestartSidecar)
	mux.HandleFunc("GET /api/sidecar/generated-config", api.HandleGetSidecarGeneratedConfig)
	mux.HandleFunc("GET /api/sidecar/logs", api.HandleGetSidecarLogs)
	mux.HandleFunc("GET /api/agent-configs", api.HandleListAgentConfigs)
	mux.HandleFunc("POST /api/agent-configs", api.HandleCreateAgentConfig)
	mux.HandleFunc("GET /api/agent-configs/active", api.HandleGetActiveAgentConfig)
	mux.HandleFunc("GET /api/agent-configs/{id}", api.HandleGetAgentConfig)
	mux.HandleFunc("PUT /api/agent-configs/{id}", api.HandleUpdateAgentConfig)
	mux.HandleFunc("DELETE /api/agent-configs/{id}", api.HandleDeleteAgentConfig)
	mux.HandleFunc("POST /api/agent-configs/{id}/activate", api.HandleActivateAgentConfig)
	mux.HandleFunc("GET /api/settings/chat", api.HandleGetChatSettings)
	mux.HandleFunc("PUT /api/settings/chat", api.HandleUpdateChatSettings)
	mux.HandleFunc("GET /api/personas", api.HandleListPersonas)
	mux.HandleFunc("POST /api/personas", api.HandleCreatePersona)
	mux.HandleFunc("GET /api/personas/{name}", api.HandleGetPersona)
	mux.HandleFunc("PUT /api/personas/{name}", api.HandleUpdatePersona)
	mux.HandleFunc("GET /api/personas/{name}/progress-phrases", api.HandleGetProgressPhrases)
	mux.HandleFunc("PUT /api/personas/{name}/progress-phrases", api.HandleUpdateProgressPhrases)
	mux.HandleFunc("GET /api/progress-phrases/defaults", api.HandleGetProgressPhrasesDefaults)
	mux.HandleFunc("DELETE /api/personas/{name}", api.HandleDeletePersona)
	mux.HandleFunc("GET /api/sessions", api.HandleListSessions)
	mux.HandleFunc("GET /api/sessions/latest", api.HandleGetLatestSession)
	mux.HandleFunc("GET /api/sessions/{id}", api.HandleGetSession)
	mux.HandleFunc("GET /api/sessions/{id}/approvals", api.HandleListSessionApprovals)
	mux.HandleFunc("DELETE /api/sessions/{id}", api.HandleDeleteSession)
	mux.HandleFunc("POST /api/memory/extractions", api.HandleQueueMemoryExtraction)
	mux.HandleFunc("GET /api/memory/extractions", api.HandleListMemoryExtractions)
	mux.HandleFunc("POST /api/memory/natural-runs", api.HandleRunNaturalMemory)
	mux.HandleFunc("GET /api/memory/natural-runs/latest", api.HandleLatestNaturalMemoryRun)
	mux.HandleFunc("GET /api/memory/segments", api.HandleListMemorySegments)
	mux.HandleFunc("GET /api/agent-affect/current", api.HandleGetAgentAffectCurrent)
	mux.HandleFunc("POST /api/agent-affect/evaluate", api.HandleEvaluateAgentAffect)
	mux.HandleFunc("POST /api/agent-affect/submit", api.HandleSubmitAgentAffect)
	mux.HandleFunc("POST /api/agent-affect/delta", api.HandleApplyAgentAffectDelta)
	mux.Handle("/ws", chatHandler)
	mux.Handle("/", staticHandler)
}
