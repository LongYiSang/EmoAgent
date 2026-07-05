package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/longyisang/emoagent/internal/chat"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/web"
)

const httpServerShutdownTimeout = 5 * time.Second

type Server struct {
	httpServer *http.Server
	platforms  *PlatformService
	shutdownMu sync.Mutex
	shutdown   bool
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
	kernel.Services.PromptCenter.StartBackground(ctx)
	chatHandler := chat.NewHandler(engine, facade, kernel.Infra.Logger, kernel.Services.Chat.HandlerOptions()...)

	api := web.NewAPIHandler(facade, kernel.Infra.Logger)

	mux := http.NewServeMux()
	if kernel.Services.Platforms != nil {
		if err := kernel.Services.Platforms.Configure(ctx, cfg.Platforms); err != nil {
			return nil, err
		}
		kernel.Services.Platforms.InstallHTTPRoutes(mux)
	}
	registerRoutes(mux, api, chatHandler, web.NewStaticHandler(web.StaticFS))
	if kernel.Services.Platforms != nil {
		if err := kernel.Services.Platforms.Start(ctx); err != nil {
			_ = kernel.Services.Platforms.Stop(context.Background())
			return nil, err
		}
	}

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return &Server{
		httpServer: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
		platforms: kernel.Services.Platforms,
		logger:    kernel.Infra.Logger,
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
		return s.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err == nil {
			return nil
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), httpServerShutdownTimeout)
		defer cancel()
		return errors.Join(err, s.Shutdown(shutdownCtx))
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.shutdownMu.Lock()
	if s.shutdown {
		s.shutdownMu.Unlock()
		return nil
	}
	s.shutdown = true
	s.shutdownMu.Unlock()
	var shutdownErr error
	if s.platforms != nil {
		shutdownErr = errors.Join(shutdownErr, s.platforms.Stop(ctx))
	}
	if s.httpServer != nil {
		shutdownErr = errors.Join(shutdownErr, s.httpServer.Shutdown(ctx))
	}
	return shutdownErr
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
	mux.HandleFunc("POST /api/media", api.HandleUploadMedia)
	mux.HandleFunc("GET /api/config/effective", api.HandleGetConfigEffective)
	mux.HandleFunc("POST /api/config/validate", api.HandleValidateConfig)
	mux.HandleFunc("GET /api/config/issues", api.HandleListConfigIssues)
	mux.HandleFunc("GET /api/platforms/status", api.HandleGetPlatformStatus)
	mux.HandleFunc("PUT /api/platforms/config", api.HandleUpdatePlatformsConfig)
	mux.HandleFunc("GET /api/python-toolchain", api.HandleGetPythonToolchain)
	mux.HandleFunc("PUT /api/python-toolchain", api.HandleUpdatePythonToolchain)
	mux.HandleFunc("POST /api/python-toolchain/probe", api.HandleProbePythonToolchain)
	mux.HandleFunc("GET /api/python-toolchain/environments", api.HandleListPythonEnvironments)
	mux.HandleFunc("POST /api/python-toolchain/environments/{kind}/{id}/sync", api.HandleSyncPythonEnvironment)
	mux.HandleFunc("POST /api/python-toolchain/environments/{kind}/{id}/repair", api.HandleRepairPythonEnvironment)
	mux.HandleFunc("GET /api/memory/config", api.HandleGetMemoryConfig)
	mux.HandleFunc("PUT /api/memory/config", api.HandleUpdateMemoryConfig)
	mux.HandleFunc("GET /api/websearch/config", api.HandleGetWebSearchConfig)
	mux.HandleFunc("PUT /api/websearch/config", api.HandleUpdateWebSearchConfig)
	mux.HandleFunc("GET /api/memory/features", api.HandleGetMemoryFeatures)
	mux.HandleFunc("PUT /api/memory/features", api.HandleUpdateMemoryFeatures)
	mux.HandleFunc("GET /api/llm-usage/events", api.HandleListLLMUsageEvents)
	mux.HandleFunc("GET /api/llm-usage/summary", api.HandleSummarizeLLMUsage)
	mux.HandleFunc("GET /api/token-estimator/calibrations", api.HandleListTokenEstimatorCalibrations)
	mux.HandleFunc("POST /api/token-estimator/calibrations/refresh", api.HandleRefreshTokenEstimatorCalibrations)
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
	mux.HandleFunc("GET /api/agent-configs/{id}/user-address-migration/preview", api.HandlePreviewUserAddressMigration)
	mux.HandleFunc("POST /api/agent-configs/{id}/user-address-migration/execute", api.HandleExecuteUserAddressMigration)
	mux.HandleFunc("GET /api/prompts/components", api.HandleListPromptComponents)
	mux.HandleFunc("GET /api/prompts/components/{component_id}", api.HandleGetPromptComponent)
	mux.HandleFunc("PUT /api/prompts/overrides", api.HandleUpsertPromptOverride)
	mux.HandleFunc("DELETE /api/prompts/overrides", api.HandleDeletePromptOverride)
	mux.HandleFunc("POST /api/prompts/preview", api.HandlePreviewPrompt)
	mux.HandleFunc("GET /api/prompts/snapshots", api.HandleListPromptSnapshots)
	mux.HandleFunc("GET /api/prompts/snapshots/{id}", api.HandleGetPromptSnapshot)
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
	mux.HandleFunc("GET /api/sessions/{id}/media/{media_id}", api.HandleGetSessionMedia)
	mux.HandleFunc("GET /api/sessions/{id}/approvals", api.HandleListSessionApprovals)
	mux.HandleFunc("DELETE /api/sessions/{id}", api.HandleDeleteSession)
	mux.HandleFunc("POST /api/memory/extractions", api.HandleQueueMemoryExtraction)
	mux.HandleFunc("GET /api/memory/extractions", api.HandleListMemoryExtractions)
	mux.HandleFunc("POST /api/memory/natural-runs", api.HandleRunNaturalMemory)
	mux.HandleFunc("GET /api/memory/natural-runs/latest", api.HandleLatestNaturalMemoryRun)
	mux.HandleFunc("GET /api/memory/segments", api.HandleListMemorySegments)
	mux.HandleFunc("GET /api/commands", api.HandleListCommands)
	mux.HandleFunc("PUT /api/commands/{id}", api.HandleUpdateCommandConfig)
	mux.HandleFunc("PUT /api/commands/{id}/config", api.HandleUpdateCommandConfig)
	mux.HandleFunc("GET /api/commands/invocations", api.HandleListCommandInvocations)
	mux.HandleFunc("GET /api/commands/conflicts", api.HandleListCommandConflicts)
	mux.HandleFunc("GET /api/agent-affect/config", api.HandleGetAgentAffectConfig)
	mux.HandleFunc("PUT /api/agent-affect/config", api.HandleUpdateAgentAffectConfig)
	mux.HandleFunc("GET /api/agent-affect/profile", api.HandleGetAgentAffectProfile)
	mux.HandleFunc("PUT /api/agent-affect/profile", api.HandleUpdateAgentAffectProfile)
	mux.HandleFunc("GET /api/agent-affect/history", api.HandleListAgentAffectHistory)
	mux.HandleFunc("GET /api/agent-affect/plugin-writes", api.HandleListAgentAffectPluginWrites)
	mux.HandleFunc("GET /api/agent-affect/current", api.HandleGetAgentAffectCurrent)
	mux.HandleFunc("POST /api/agent-affect/evaluate", api.HandleEvaluateAgentAffect)
	mux.HandleFunc("POST /api/agent-affect/submit", api.HandleSubmitAgentAffect)
	mux.HandleFunc("POST /api/agent-affect/delta", api.HandleApplyAgentAffectDelta)
	mux.HandleFunc("POST /api/agent-affect/reset", api.HandleResetAgentAffect)
	mux.HandleFunc("POST /api/agent-affect/prompt-preview", api.HandlePreviewAgentAffectPrompt)
	mux.HandleFunc("GET /api/agent-affect/queue", api.HandleGetAgentAffectQueue)
	mux.HandleFunc("POST /api/agent-affect/process-once", api.HandleProcessAgentAffectBatchOnce)
	mux.HandleFunc("POST /api/agent-affect/clear-failed", api.HandleClearAgentAffectFailedJobs)
	mux.HandleFunc("POST /api/agent-affect/supersede-pending", api.HandleSupersedeAgentAffectPendingJobs)
	mux.HandleFunc("GET /api/plugins", api.HandleListPlugins)
	mux.HandleFunc("GET /api/plugins/diagnostics", api.HandlePluginDiagnostics)
	mux.HandleFunc("GET /api/plugins/{id}", api.HandleGetPlugin)
	mux.HandleFunc("GET /api/plugins/{id}/settings", api.HandleGetPluginSettings)
	mux.HandleFunc("PUT /api/plugins/{id}/settings", api.HandleUpdatePluginSettings)
	mux.HandleFunc("POST /api/plugins/install/local", api.HandleInstallLocalPlugin)
	mux.HandleFunc("POST /api/plugins/install/local-zip", api.HandleInstallLocalPlugin)
	mux.HandleFunc("POST /api/plugins/install/github-release", api.HandleInstallGitHubPlugin)
	mux.HandleFunc("POST /api/plugins/{id}/enable", api.HandleEnablePlugin)
	mux.HandleFunc("POST /api/plugins/{id}/disable", api.HandleDisablePlugin)
	mux.HandleFunc("POST /api/plugins/{id}/restart", api.HandleRestartPlugin)
	mux.HandleFunc("GET /api/plugins/{id}/status", api.HandlePluginStatus)
	mux.HandleFunc("DELETE /api/plugins/{id}", api.HandleDeletePlugin)
	mux.HandleFunc("GET /api/plugins/{id}/logs", api.HandlePluginLogs)
	mux.HandleFunc("GET /api/plugins/{id}/access-events", api.HandlePluginAccessEvents)
	mux.HandleFunc("GET /api/plugins/{id}/provider-usage", api.HandlePluginProviderUsage)
	mux.HandleFunc("GET /api/resource-grants", api.HandleListResourceGrants)
	mux.HandleFunc("POST /api/resource-grants/{id}/revoke", api.HandleRevokeResourceGrant)
	mux.HandleFunc("GET /api/resource-changesets", api.HandleListResourceChangeSets)
	mux.HandleFunc("GET /api/resource-changesets/{id}", api.HandleGetResourceChangeSet)
	mux.HandleFunc("POST /api/resource-changesets/{id}/cancel", api.HandleCancelResourceChangeSet)
	mux.Handle("/ws", chatHandler)
	mux.Handle("/", staticHandler)
}
