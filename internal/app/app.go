package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/apperrors"
	"github.com/longyisang/emoagent/internal/chat"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/logger"
	"github.com/longyisang/emoagent/internal/memoryhost"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/runtimeenv"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/tool/builtin"
	"github.com/longyisang/emoagent/internal/web"
	"github.com/longyisang/emoagent/internal/work"
)

const personaWatchInterval = 5 * time.Second

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

// App is the top-level application container.
type App struct {
	Config             *config.Config
	DB                 *storage.DB
	Memory             *memoryhost.Host
	ManualMemoryRules  *memoryhost.ManualRules
	LLM                llm.Client
	Logger             *slog.Logger
	Personas           map[string]*config.Persona
	ActiveAgentRuntime *ActiveAgentRuntime
	engine             *chat.Engine
	toolRegistry       *tool.Registry
	approvalService    *work.ApprovalService
	environment        runtimeenv.Facts
	mu                 sync.RWMutex
	cancel             context.CancelFunc
}

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

// New creates an uninitialized App.
func New() *App {
	return &App{}
}

// Init loads config, opens DB, loads runtime state, and starts persona watching.
func (a *App) Init(ctx context.Context, configPath string) error {
	_ = godotenv.Load()

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	a.Config = cfg

	a.Logger = logger.Init(cfg.Log.Level, cfg.Log.Format)
	a.Logger.Info("config loaded", "path", configPath)

	db, err := storage.Open(cfg.DB.Path, a.Logger)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	a.DB = db

	if err := a.applyRuntimeOverrides(); err != nil {
		a.Logger.Warn("runtime config overrides failed", "error", err)
	}

	personas, err := config.LoadAllPersonas(cfg.Personas.Dir)
	if err != nil {
		a.Logger.Warn("load personas failed", "error", err)
		personas = make(map[string]*config.Persona)
	}
	a.mu.Lock()
	a.Personas = personas
	a.mu.Unlock()
	a.Logger.Info("personas loaded", "count", len(personas))

	for key, p := range personas {
		if err := a.DB.UpsertPersona(key, p.Name, p.Description, p.SystemPrompt, p.Tone, p.Quirks, p.Greeting, p.WorkProgressPhrases); err != nil {
			a.Logger.Warn("sync persona to db failed", "key", key, "name", p.Name, "error", err)
		}
	}

	if err := a.bootstrapAgentConfigs(); err != nil {
		return fmt.Errorf("bootstrap agent configs: %w", err)
	}
	if err := a.loadActiveAgentRuntime(); err != nil {
		return fmt.Errorf("load active agent config: %w", err)
	}

	watchCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	go config.WatchPersonas(watchCtx, cfg.Personas.Dir, personaWatchInterval, func(updated map[string]*config.Persona) {
		a.mu.Lock()
		a.Personas = clonePersonaMap(updated)
		a.mu.Unlock()

		for key, p := range updated {
			if err := a.DB.UpsertPersona(key, p.Name, p.Description, p.SystemPrompt, p.Tone, p.Quirks, p.Greeting, p.WorkProgressPhrases); err != nil {
				a.Logger.Warn("sync updated persona failed", "key", key, "name", p.Name, "error", err)
			}
		}
		a.Logger.Info("personas reloaded", "count", len(updated))
	})

	// Initialize tool registry with built-in tools.
	a.toolRegistry = tool.NewRegistry()
	projectRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	a.environment = runtimeenv.BuildEnvironmentFacts(runtime.GOOS, projectRoot, a.Config.Bash)
	builtin.RegisterAllWithFacts(a.toolRegistry, a.Config, projectRoot, a.environment, a.Logger)
	a.Logger.Info("tool registry initialized", "tools", len(a.toolRegistry.Specs()))

	if cfg.Memory.Enabled {
		manualRules, err := memoryhost.LoadManualRules(cfg.Memory.ManualRulesPath)
		if err != nil {
			return fmt.Errorf("load memory manual rules: %w", err)
		}
		memoryHost, err := memoryhost.OpenFromConfig(ctx, cfg.Memory.ConfigPath, a.Logger)
		if err != nil {
			return fmt.Errorf("open memorycore: %w", err)
		}
		memoryHost.ConfigureExtractionPolicy(memoryExtractionHostConfig(cfg.Memory.Extraction))
		a.Memory = memoryHost
		a.ManualMemoryRules = manualRules
	}

	a.Logger.Info("EmoAgent initialized")
	return nil
}

// Run starts the HTTP server and blocks until the context is cancelled.
func (a *App) Run(ctx context.Context) error {
	cfg := config.DefaultConfig()
	if a.Config != nil {
		cfg = a.Config
	}

	a.mu.RLock()
	activeRuntime := cloneActiveAgentRuntime(a.ActiveAgentRuntime)
	a.mu.RUnlock()

	model := ""
	params := llm.RequestParams{}
	summaryModel := ""
	summaryParams := llm.RequestParams{}
	maxTokens := 0
	temperature := 0.0
	provider := ""
	providerName := ""
	currentClient := a.LLM
	summaryClient := a.LLM
	contextCfg := a.globalContextConfig()
	if activeRuntime != nil {
		currentClient = activeRuntime.EmotionMain.Client
		summaryClient = activeRuntime.EmotionSummary.Client
		model = activeRuntime.EmotionMain.Model
		params = cloneRequestParams(activeRuntime.EmotionMain.Params)
		summaryModel = activeRuntime.EmotionSummary.Model
		summaryParams = cloneRequestParams(activeRuntime.EmotionSummary.Params)
		maxTokens = params.MaxTokens
		temperature = derefFloat64(params.Temperature, 0)
		provider = toolProviderName(activeRuntime.EmotionMain.Provider.Protocol)
		providerName = providerDisplayName(activeRuntime.EmotionMain.Provider)
		contextCfg = activeRuntime.Context
	}

	if a.toolRegistry == nil {
		projectRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
		a.environment = runtimeenv.BuildEnvironmentFacts(runtime.GOOS, projectRoot, cfg.Bash)
		a.toolRegistry = tool.NewRegistry()
		builtin.RegisterAllWithFacts(a.toolRegistry, cfg, projectRoot, a.environment, a.Logger)
	} else if a.environment.OS == "" {
		projectRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
		a.environment = runtimeenv.BuildEnvironmentFacts(runtime.GOOS, projectRoot, cfg.Bash)
	}

	dispatcher := tool.NewDispatcher(a.toolRegistry, tool.MinimalSchemaValidator{}, a.Logger)
	var pendingRegistry *work.PendingRegistry
	var approvalService *work.ApprovalService
	if _, ok := a.toolRegistry.GetSpec("delegate_to_work"); !ok {
		if activeRuntime == nil || activeRuntime.WorkMain.Client == nil {
			a.Logger.Warn("work runtime disabled", "error", "active agent config is not configured")
		} else {
			approvalService = work.NewApprovalService(a.DB.SqlDB(), a.Logger)
			pendingRegistry = work.NewPendingRegistry(a.DB.SqlDB(), approvalService, a.Logger, work.PendingRegistryConfig{
				SoftTTL:        cfg.Work.SoftTTL,
				HardTTL:        cfg.Work.HardTTL,
				ArchiveTTL:     cfg.Work.ArchiveTTL,
				ResumeClaimTTL: cfg.Work.ResumeClaimTTL,
			})
			cleanupInterval := cfg.Work.DeciderCleanupInterval
			if cleanupInterval <= 0 {
				cleanupInterval = 5 * time.Minute
			}
			go func() {
				ticker := time.NewTicker(cleanupInterval)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if n := pendingRegistry.ExpireOnce(); n > 0 {
							a.Logger.Info("expired pending work decisions", "count", n)
						}
						if n := pendingRegistry.ArchiveOnce(); n > 0 {
							a.Logger.Info("archived pending work decisions", "count", n)
						}
					}
				}
			}()

			runtimeFactory := func() (*work.Runtime, error) {
				return a.newWorkRuntime(dispatcher)
			}
			if _, ok := a.toolRegistry.GetSpec("finish_task"); !ok {
				a.toolRegistry.Register(work.NewFinishTaskTool(), work.FinishTaskPlaceholderHandler)
			}
			if _, ok := a.toolRegistry.GetSpec("request_decision"); !ok {
				a.toolRegistry.Register(work.NewRequestDecisionTool(), work.RequestDecisionPlaceholderHandler)
			}
			if _, ok := a.toolRegistry.GetSpec("resume_work"); !ok {
				resumeSpec, resumeHandler := work.NewResumeToolWithFactory(runtimeFactory, pendingRegistry, cfg.Work.JournalDir, a.Logger)
				a.toolRegistry.Register(resumeSpec, resumeHandler)
			}
			if _, ok := a.toolRegistry.GetSpec("list_pending_decisions"); !ok {
				spec, handler := work.NewListDecisionsTool(pendingRegistry)
				a.toolRegistry.Register(spec, handler)
			}
			delegateSpec, delegateHandler := work.NewDelegateToolWithFactory(runtimeFactory, pendingRegistry, cfg.Work.JournalDir, a.Logger)
			a.toolRegistry.Register(delegateSpec, delegateHandler)
		}
	}

	a.engine = chat.NewEngine(chat.EngineConfig{
		LLM:                currentClient,
		SummaryLLM:         summaryClient,
		DB:                 a.DB,
		Logger:             a.Logger,
		Model:              model,
		Params:             params,
		SummaryModel:       summaryModel,
		SummaryParams:      summaryParams,
		SummaryTemperature: summaryParams.Temperature,
		SummaryMaxTokens:   summaryParams.MaxTokens,
		MaxTokens:          maxTokens,
		Temperature:        temperature,
		ContextConfig:      contextCfg,
		Provider:           provider,
		ProviderName:       providerName,
		Registry:           a.toolRegistry,
		Dispatcher:         dispatcher,
		Pending:            pendingRegistry,
		Approvals:          approvalService,
		Environment:        a.environment,
		RealtimeStreaming:  cfg.Chat.RealtimeStreaming,
		Memory:             memoryhost.NewBridge(a.Memory, a.DB, a.Logger, a.ManualMemoryRules, memoryRetrievalPolicy(cfg.Memory.Retrieval)),
		MemoryRetrieval:    cfg.Memory.Retrieval,
	})
	a.approvalService = approvalService
	startMemoryExtractionBackground(ctx, a.Memory, a.DB, a.Logger, cfg.Memory.Extraction)
	chatHandler := chat.NewHandler(a.engine, a, a.Logger)

	staticSub, err := fs.Sub(web.StaticFS, "static")
	if err != nil {
		return fmt.Errorf("load embedded web assets: %w", err)
	}

	api := web.NewAPIHandler(a, a.Logger)

	mux := http.NewServeMux()
	registerRoutes(mux, api, chatHandler, http.FileServer(http.FS(staticSub)))

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	errCh := make(chan error, 1)
	go func() {
		a.Logger.Info("server started", "url", fmt.Sprintf("http://%s", addr))
		if listenErr := srv.ListenAndServe(); listenErr != nil && listenErr != http.ErrServerClosed {
			errCh <- listenErr
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err == nil {
			return nil
		}
		return err
	}
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
	mux.HandleFunc("GET /api/memory/segments", api.HandleListMemorySegments)
	mux.Handle("/ws", chatHandler)
	mux.Handle("/", staticHandler)
}

func memoryRetrievalPolicy(cfg config.MemoryRetrievalConfig) memorycore.RetrievalPolicy {
	return memorycore.RetrievalPolicy{
		SensitivityPermission: memorycore.SensitivityNormal,
		FinalMemoryCount:      cfg.FinalMemoryCount,
		ContextBudgetTokens:   cfg.ContextBudgetTokens,
		UseFTS:                cfg.UseFTS,
		UseMirror:             cfg.UseMirror,
	}
}

// Shutdown cleanly releases resources.
func (a *App) Shutdown() error {
	if a.cancel != nil {
		a.cancel()
	}
	var closeErr error
	if a.Memory != nil {
		if err := a.Memory.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close memorycore: %w", err))
		} else {
			a.Memory = nil
		}
	}
	a.ManualMemoryRules = nil
	if a.DB != nil {
		if err := a.DB.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close database: %w", err))
		}
	}
	if a.Logger != nil {
		a.Logger.Info("EmoAgent stopped")
	}
	return closeErr
}

func (a *App) applyRuntimeOverrides() error {
	overrides, err := a.DB.GetAllRuntimeConfig()
	if err != nil {
		return err
	}

	for k, v := range overrides {
		switch k {
		case "chat.realtime_streaming":
			enabled, parseErr := strconv.ParseBool(v)
			if parseErr == nil {
				a.Config.Chat.RealtimeStreaming = enabled
			} else {
				a.Logger.Warn("invalid runtime override", "key", "chat.realtime_streaming", "value", v, "error", parseErr)
			}
		case "server.port":
			if n, parseErr := strconv.Atoi(v); parseErr == nil {
				a.Config.Server.Port = n
			} else {
				a.Logger.Warn("invalid runtime override", "key", "server.port", "value", v, "error", parseErr)
			}
		}
	}

	if len(overrides) > 0 {
		a.Logger.Info("runtime config overrides applied", "count", len(overrides))
	}
	return nil
}

func (a *App) bootstrapAgentConfigs() error {
	providers, err := a.DB.ListLLMProviders()
	if err != nil {
		return err
	}
	agents, err := a.DB.ListAgentConfigs()
	if err != nil {
		return err
	}
	if len(providers) > 0 || len(agents) > 0 {
		if _, found, err := a.DB.GetActiveAgentConfig(); err != nil {
			return err
		} else if !found && len(agents) > 0 {
			return a.DB.SetActiveAgentConfig(agents[0].ID)
		}
		return nil
	}

	if len(a.Config.LLMProviders) == 0 || len(a.Config.AgentConfigs) == 0 {
		return fmt.Errorf("active agent config is not configured: config.yaml must define llm_providers and agent_configs")
	}
	for _, provider := range a.Config.LLMProviders {
		if err := provider.Validate(); err != nil {
			return err
		}
		if err := a.DB.UpsertLLMProvider(provider); err != nil {
			return err
		}
	}
	for _, agent := range a.Config.AgentConfigs {
		if err := agent.Validate(); err != nil {
			return err
		}
		if err := a.DB.UpsertAgentConfig(agent); err != nil {
			return err
		}
	}
	activeID := strings.TrimSpace(a.Config.Agent.ActiveConfig)
	if activeID == "" {
		activeID = a.Config.AgentConfigs[0].ID
	}
	return a.DB.SetActiveAgentConfig(activeID)
}

func (a *App) loadActiveAgentRuntime() error {
	activeID, found, err := a.DB.GetActiveAgentConfig()
	if err != nil {
		return err
	}
	if !found || strings.TrimSpace(activeID) == "" {
		a.mu.Lock()
		a.ActiveAgentRuntime = nil
		a.LLM = nil
		a.mu.Unlock()
		return nil
	}
	runtime, err := a.buildAgentRuntime(activeID, false)
	if err != nil {
		a.Logger.Warn("active agent config is not currently usable", "agent_config", activeID, "error", err)
		a.mu.Lock()
		a.ActiveAgentRuntime = nil
		a.LLM = nil
		a.mu.Unlock()
		return nil
	}
	a.mu.Lock()
	a.ActiveAgentRuntime = cloneActiveAgentRuntime(runtime)
	a.LLM = runtime.EmotionMain.Client
	a.mu.Unlock()
	return nil
}

func (a *App) buildAgentRuntime(id string, requireClient bool) (*ActiveAgentRuntime, error) {
	agent, err := a.DB.GetAgentConfig(context.Background(), id)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, ErrAgentConfigNotFound
	}
	if _, exists := a.Personas[agent.PersonaKey]; !exists {
		return nil, fmt.Errorf("active agent config persona not found")
	}
	contextCfg, err := agent.ResolveContextConfig(a.globalContextConfig())
	if err != nil {
		return nil, err
	}

	emotionMain, err := a.modelRuntime(agent.Emotion.Main, requireClient)
	if err != nil {
		return nil, fmt.Errorf("emotion.main: %w", err)
	}
	emotionSummary, err := a.modelRuntime(agent.Emotion.Summary, requireClient)
	if err != nil {
		return nil, fmt.Errorf("emotion.summary: %w", err)
	}
	workMain, err := a.modelRuntime(agent.Work.Main, requireClient)
	if err != nil {
		return nil, fmt.Errorf("work.main: %w", err)
	}
	workSummary, err := a.modelRuntime(agent.Work.Summary, requireClient)
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

func (a *App) modelRuntime(binding config.ModelBinding, requireClient bool) (ModelRuntime, error) {
	record, err := a.DB.GetLLMProvider(context.Background(), binding.ProviderID)
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
	client, err := a.buildClientForProvider(provider)
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

func (a *App) buildClientForProvider(provider config.LLMProvider) (llm.Client, error) {
	return llm.NewClient(llm.ProviderConfig{
		ID:        provider.ID,
		PresetID:  provider.PresetID,
		Protocol:  provider.Protocol,
		BaseURL:   provider.BaseURL,
		APIKeyEnv: provider.APIKeyEnv,
	}, a.Logger)
}

func (a *App) newWorkRuntime(dispatcher *tool.Dispatcher) (*work.Runtime, error) {
	a.mu.RLock()
	active := cloneActiveAgentRuntime(a.ActiveAgentRuntime)
	a.mu.RUnlock()
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
		MaxToolRounds:            a.Config.Work.MaxToolRounds,
		MaxInputTokens:           a.Config.Work.MaxInputTokens,
		CompressSoftRatio:        a.Config.Work.CompressSoftRatio,
		CompressKeepRounds:       a.Config.Work.CompressKeepRounds,
		ToolSnipSoftTokens:       a.Config.Work.ToolSnipSoftTokens,
		ToolSnipHardTokens:       a.Config.Work.ToolSnipHardTokens,
		Registry:                 a.toolRegistry,
		Dispatcher:               dispatcher,
		Logger:                   a.Logger,
		Decider:                  decider,
		MaxEscalations:           a.Config.Work.MaxEscalationsPerTask,
		PendingSnapshotMaxTokens: a.Config.Work.PendingSnapshotMaxTokens,
		EnvironmentFacts:         a.environment,
	}), nil
}

// GetPersona returns a persona by key.
func (a *App) GetPersona(name string) (*config.Persona, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	p, ok := a.Personas[name]
	if !ok {
		return nil, false
	}
	return clonePersona(p), true
}

// ListPersonas returns a copy of all personas keyed by file name.
func (a *App) ListPersonas() map[string]*config.Persona {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return clonePersonaMap(a.Personas)
}

// GetDefaultPersonaName returns the configured default persona key.
func (a *App) GetDefaultPersonaName() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.ActiveAgentRuntime != nil {
		return a.ActiveAgentRuntime.PersonaKey
	}
	return ""
}

func (a *App) GetChatSettings() config.ChatConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.Config == nil {
		return config.ChatConfig{}
	}
	return a.Config.Chat
}

func (a *App) UpdateChatSettings(settings config.ChatConfig) error {
	if a.DB == nil {
		return fmt.Errorf("database is not initialized")
	}
	if err := a.DB.SetRuntimeConfig("chat.realtime_streaming", strconv.FormatBool(settings.RealtimeStreaming)); err != nil {
		return err
	}

	a.mu.Lock()
	if a.Config == nil {
		a.Config = config.DefaultConfig()
	}
	a.Config.Chat = settings
	engine := a.engine
	a.mu.Unlock()

	if engine != nil {
		engine.UpdateRealtimeStreaming(settings.RealtimeStreaming)
	}
	return nil
}

// CreatePersona creates a new persona.
func (a *App) CreatePersona(key string, p *config.Persona) error {
	if p == nil {
		return fmt.Errorf("persona is required")
	}
	if key == "" {
		return fmt.Errorf("persona key is required")
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.Personas[key]; exists {
		return ErrPersonaExists
	}

	next := clonePersona(p)
	if next.Name == "" {
		next.Name = key
	}
	if err := config.SavePersona(a.Config.Personas.Dir, key, next); err != nil {
		return fmt.Errorf("save persona file: %w", err)
	}
	if err := a.DB.UpsertPersona(key, next.Name, next.Description, next.SystemPrompt, next.Tone, next.Quirks, next.Greeting, next.WorkProgressPhrases); err != nil {
		return fmt.Errorf("upsert persona: %w", err)
	}
	a.Personas[key] = next
	return nil
}

// UpdatePersona updates an existing persona by key.
func (a *App) UpdatePersona(key string, p *config.Persona) error {
	if p == nil {
		return fmt.Errorf("persona is required")
	}
	if key == "" {
		return fmt.Errorf("persona key is required")
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	_, exists := a.Personas[key]
	if !exists {
		return ErrPersonaNotFound
	}

	next := clonePersona(p)
	if next.Name == "" {
		next.Name = key
	}
	if err := config.SavePersona(a.Config.Personas.Dir, key, next); err != nil {
		return fmt.Errorf("save persona file: %w", err)
	}
	if err := a.DB.UpsertPersona(key, next.Name, next.Description, next.SystemPrompt, next.Tone, next.Quirks, next.Greeting, next.WorkProgressPhrases); err != nil {
		return fmt.Errorf("upsert persona: %w", err)
	}
	a.Personas[key] = next
	return nil
}

// GetProgressPhrases returns a copy of work progress phrases for one persona.
func (a *App) GetProgressPhrases(key string) (map[string][]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	persona, exists := a.Personas[key]
	if !exists || persona == nil {
		return nil, ErrPersonaNotFound
	}
	return cloneProgressPhrases(persona.WorkProgressPhrases), nil
}

// UpdateProgressPhrases updates one persona's work progress phrase map.
func (a *App) UpdateProgressPhrases(key string, phrases map[string][]string) error {
	if key == "" {
		return fmt.Errorf("persona key is required")
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	current, exists := a.Personas[key]
	if !exists || current == nil {
		return ErrPersonaNotFound
	}

	next := clonePersona(current)
	next.WorkProgressPhrases = cloneProgressPhrases(phrases)
	if err := config.SavePersona(a.Config.Personas.Dir, key, next); err != nil {
		return fmt.Errorf("save persona file: %w", err)
	}
	if err := a.DB.UpsertPersona(key, next.Name, next.Description, next.SystemPrompt, next.Tone, next.Quirks, next.Greeting, next.WorkProgressPhrases); err != nil {
		return fmt.Errorf("upsert persona: %w", err)
	}
	a.Personas[key] = next
	return nil
}

// DeletePersona removes a persona by key.
func (a *App) DeletePersona(key string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.ActiveAgentRuntime != nil && key == a.ActiveAgentRuntime.PersonaKey {
		return ErrCannotDeleteDefault
	}
	_, exists := a.Personas[key]
	if !exists {
		return ErrPersonaNotFound
	}
	if err := config.DeletePersonaFile(a.Config.Personas.Dir, key); err != nil {
		return fmt.Errorf("delete persona file: %w", err)
	}
	if err := a.DB.DeletePersona(context.Background(), key); err != nil {
		return fmt.Errorf("delete persona from db: %w", err)
	}
	delete(a.Personas, key)
	return nil
}

// ListSessions returns recent non-empty sessions for the given persona key.
func (a *App) ListSessions(ctx context.Context, persona string, limit int) ([]storage.SessionSummary, error) {
	return a.DB.ListSessions(ctx, persona, limit)
}

// GetLatestSession returns the latest non-empty session for the given persona key.
func (a *App) GetLatestSession(ctx context.Context, persona string) (*storage.SessionSummary, error) {
	return a.DB.GetLatestSession(ctx, persona)
}

// GetSessionDetail returns the session and all of its messages.
func (a *App) GetSessionDetail(ctx context.Context, id string) (*storage.SessionRecord, []storage.MessageRecord, error) {
	session, err := a.DB.GetSession(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if session == nil {
		return nil, nil, ErrSessionNotFound
	}
	messages, err := a.DB.GetAllMessages(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	return session, messages, nil
}

// DeleteSession removes a session and its messages.
func (a *App) DeleteSession(ctx context.Context, id string) error {
	session, err := a.DB.GetSession(ctx, id)
	if err != nil {
		return err
	}
	if session == nil {
		return ErrSessionNotFound
	}
	return a.DB.DeleteSession(ctx, id)
}

func (a *App) ListSessionApprovals(ctx context.Context, sessionID string) ([]protocol.ApprovalRequest, error) {
	if a.approvalService == nil {
		return []protocol.ApprovalRequest{}, nil
	}
	return a.approvalService.ListSessionApprovals(sessionID, nil), nil
}

func (a *App) QueueMemoryExtraction(ctx context.Context, req web.MemoryExtractionRequest) (web.MemoryExtractionQueueResponse, error) {
	if a.DB == nil {
		return web.MemoryExtractionQueueResponse{}, fmt.Errorf("database is not configured")
	}
	scope := strings.TrimSpace(req.Scope)
	switch scope {
	case "", "session", "segment", "eligible", "all":
	default:
		return web.MemoryExtractionQueueResponse{}, fmt.Errorf("scope must be session, segment, eligible, or all")
	}
	if scope == "session" && strings.TrimSpace(req.SessionID) == "" {
		return web.MemoryExtractionQueueResponse{}, fmt.Errorf("session_id is required for session scope")
	}
	if scope == "segment" && strings.TrimSpace(req.SegmentID) == "" {
		return web.MemoryExtractionQueueResponse{}, fmt.Errorf("segment_id is required for segment scope")
	}
	if a.Config != nil {
		extraction := a.Config.Memory.Extraction
		if !extraction.Enabled {
			return web.MemoryExtractionQueueResponse{}, fmt.Errorf("memory extraction is disabled")
		}
		if !extraction.Async.Enabled {
			return web.MemoryExtractionQueueResponse{}, fmt.Errorf("memory extraction async queue is disabled")
		}
		if !extraction.Async.WorkerEnabled {
			return web.MemoryExtractionQueueResponse{}, fmt.Errorf("memory extraction worker is disabled")
		}
		manual := extraction.Manual
		if !manual.Enabled {
			return web.MemoryExtractionQueueResponse{}, fmt.Errorf("memory extraction manual trigger is disabled")
		}
		if req.Force && !manual.AllowForce {
			return web.MemoryExtractionQueueResponse{}, fmt.Errorf("memory extraction force is disabled")
		}
		if strings.TrimSpace(req.SegmentID) != "" && !manual.AllowSegmentSelection {
			return web.MemoryExtractionQueueResponse{}, fmt.Errorf("memory extraction segment selection is disabled")
		}
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" && a.Config != nil {
		mode = a.Config.Memory.Extraction.Manual.Mode
	}
	if mode == "" {
		mode = "apply"
	}
	switch normalizeAppMemoryExtractionMode(mode) {
	case "validate", "dry-run", "apply":
	default:
		return web.MemoryExtractionQueueResponse{}, fmt.Errorf("mode must be validate, dry_run, or apply")
	}
	mode = string(memoryExtractionMode(mode))

	var segments []storage.MemorySegment
	if strings.TrimSpace(req.SegmentID) != "" {
		segment, err := a.DB.GetMemorySegment(ctx, req.SegmentID)
		if err != nil {
			return web.MemoryExtractionQueueResponse{}, err
		}
		if segment == nil {
			return web.MemoryExtractionQueueResponse{}, fmt.Errorf("segment_id not found")
		}
		segments = []storage.MemorySegment{*segment}
	} else if strings.TrimSpace(req.SessionID) != "" {
		session, err := a.DB.GetSession(ctx, req.SessionID)
		if err != nil {
			return web.MemoryExtractionQueueResponse{}, err
		}
		if session == nil {
			return web.MemoryExtractionQueueResponse{}, ErrSessionNotFound
		}
		list, err := a.DB.ListMemorySegments(ctx, storage.ListMemorySegmentsFilter{ChatSessionID: req.SessionID, Limit: 100})
		if err != nil {
			return web.MemoryExtractionQueueResponse{}, err
		}
		segments = list
	} else {
		idleAfter := 15 * time.Minute
		limit := 20
		minEpisodes := 1
		includeActive := true
		includeFinalized := true
		if a.Config != nil {
			idleAfter = time.Duration(a.Config.Memory.Extraction.Idle.IdleAfterSeconds) * time.Second
			limit = a.Config.Memory.Extraction.Idle.MaxSegmentsPerSweep
			minEpisodes = a.Config.Memory.Extraction.Idle.MinEpisodeCount
			includeActive = a.Config.Memory.Extraction.Idle.IncludeActiveSegments
			includeFinalized = a.Config.Memory.Extraction.Idle.IncludeFinalizedSegments
		}
		list, err := a.DB.ScanEligibleMemorySegments(ctx, storage.ScanEligibleMemorySegmentsParams{
			Now:                      time.Now().UTC(),
			IdleAfter:                idleAfter,
			IncludeActiveSegments:    includeActive,
			IncludeFinalizedSegments: includeFinalized,
			MinEpisodeCount:          minEpisodes,
			Limit:                    limit,
		})
		if err != nil {
			return web.MemoryExtractionQueueResponse{}, err
		}
		segments = list
	}

	resp := web.MemoryExtractionQueueResponse{Status: "queued"}
	for _, segment := range segments {
		if !req.Force && !manualExtractionEligible(segment.ExtractionStatus) {
			resp.SkippedCount++
			continue
		}
		personaID := strings.TrimSpace(req.PersonaID)
		if personaID == "" {
			personaID = a.memorySegmentPersona(ctx, segment.ChatSessionID)
		}
		trigger := storage.MemoryExtractionTriggerManualScan
		if strings.TrimSpace(req.SegmentID) != "" {
			trigger = storage.MemoryExtractionTriggerManualSegmentScan
		}
		maxAttempts := 3
		if a.Config != nil && a.Config.Memory.Extraction.Async.MaxAttempts > 0 {
			maxAttempts = a.Config.Memory.Extraction.Async.MaxAttempts
		}
		job, enqueued, err := a.DB.EnqueueMemoryExtractionJob(ctx, storage.EnqueueMemoryExtractionJobParams{
			PersonaID:       personaID,
			ChatSessionID:   segment.ChatSessionID,
			SegmentID:       segment.ID,
			MemorySessionID: segment.MemorySessionID,
			Trigger:         trigger,
			Scope:           storage.MemoryExtractionScopeSegment,
			Mode:            mode,
			RequestedBy:     "user",
			Priority:        20,
			Force:           req.Force,
			UntilAt:         segment.LastActivityAt,
			EpisodeLimit:    extractionLimit(a.Config),
			MaxAttempts:     maxAttempts,
			RunAfter:        time.Now().UTC(),
		})
		if err != nil {
			return resp, err
		}
		if enqueued {
			resp.EnqueuedCount++
		} else {
			resp.SkippedCount++
		}
		if job != nil {
			resp.Jobs = append(resp.Jobs, *job)
		}
	}
	return resp, nil
}

func (a *App) ListMemoryExtractions(ctx context.Context, req web.MemoryExtractionListRequest) ([]storage.MemoryExtractionJob, error) {
	if a.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	return a.DB.ListMemoryExtractionJobs(ctx, storage.ListMemoryExtractionJobsFilter{
		ChatSessionID: req.SessionID,
		SegmentID:     req.SegmentID,
		Status:        req.Status,
		Limit:         req.Limit,
	})
}

func (a *App) ListMemorySegments(ctx context.Context, sessionID string) ([]storage.MemorySegment, error) {
	if a.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	return a.DB.ListMemorySegments(ctx, storage.ListMemorySegmentsFilter{ChatSessionID: sessionID, Limit: 100})
}

func (a *App) memorySegmentPersona(ctx context.Context, chatSessionID string) string {
	if a.DB == nil {
		return "default"
	}
	link, err := a.DB.GetMemoryChatLink(ctx, chatSessionID)
	if err != nil || link == nil || strings.TrimSpace(link.PersonaID) == "" {
		return "default"
	}
	return link.PersonaID
}

func manualExtractionEligible(status string) bool {
	switch strings.TrimSpace(status) {
	case "", storage.MemorySegmentExtractionStatusNever, storage.MemorySegmentExtractionStatusStale, storage.MemorySegmentExtractionStatusFailed, storage.MemorySegmentExtractionStatusSkipped:
		return true
	default:
		return false
	}
}

func extractionLimit(cfg *config.Config) int {
	if cfg != nil && cfg.Memory.Extraction.Limit > 0 {
		return cfg.Memory.Extraction.Limit
	}
	return 50
}

func normalizeAppMemoryExtractionMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "dry_run":
		return "dry-run"
	default:
		return strings.TrimSpace(mode)
	}
}

func (a *App) ListLLMProviders() ([]config.LLMProvider, error) {
	records, err := a.DB.ListLLMProviders()
	if err != nil {
		return nil, err
	}
	providers := make([]config.LLMProvider, 0, len(records))
	for _, record := range records {
		providers = append(providers, record.LLMProvider)
	}
	return providers, nil
}

func (a *App) GetLLMProvider(id string) (*config.LLMProvider, error) {
	record, err := a.DB.GetLLMProvider(context.Background(), id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrLLMProviderNotFound
	}
	provider := record.LLMProvider
	return &provider, nil
}

func (a *App) CreateLLMProvider(provider config.LLMProvider) error {
	var err error
	provider, err = provider.WithPresetDefaults()
	if err != nil {
		return err
	}
	if err := provider.Validate(); err != nil {
		return err
	}
	existing, err := a.DB.GetLLMProvider(context.Background(), provider.ID)
	if err != nil {
		return err
	}
	if existing != nil {
		return ErrLLMProviderExists
	}
	return a.DB.UpsertLLMProvider(provider)
}

func (a *App) UpdateLLMProvider(id string, provider config.LLMProvider) error {
	provider.ID = id
	var err error
	provider, err = provider.WithPresetDefaults()
	if err != nil {
		return err
	}
	if err := provider.Validate(); err != nil {
		return err
	}
	existing, err := a.DB.GetLLMProvider(context.Background(), id)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrLLMProviderNotFound
	}
	return a.DB.UpsertLLMProvider(provider)
}

func (a *App) DeleteLLMProvider(id string) error {
	err := a.DB.DeleteLLMProvider(id)
	if errors.Is(err, storage.ErrProviderInUse) {
		return ErrLLMProviderInUse
	}
	return err
}

func (a *App) RefreshLLMProviderModels(id string) ([]llm.ModelInfo, error) {
	provider, err := a.GetLLMProvider(id)
	if err != nil {
		return nil, err
	}
	if provider.ModelDiscovery == "manual" || provider.ModelDiscovery == "" {
		return []llm.ModelInfo{}, nil
	}
	models, err := llm.DiscoverModels(context.Background(), llm.ProviderConfig{
		ID:        provider.ID,
		PresetID:  provider.PresetID,
		Protocol:  provider.Protocol,
		BaseURL:   provider.BaseURL,
		APIKeyEnv: provider.APIKeyEnv,
	})
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(models)
	if err != nil {
		return nil, err
	}
	if err := a.DB.UpdateProviderModelsCache(id, string(payload), time.Now().UTC().Format(time.RFC3339)); err != nil {
		return nil, err
	}
	return models, nil
}

func (a *App) GetLLMProviderModels(id string) ([]llm.ModelInfo, error) {
	record, err := a.DB.GetLLMProvider(context.Background(), id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrLLMProviderNotFound
	}
	var models []llm.ModelInfo
	if strings.TrimSpace(record.ModelsCacheJSON) == "" {
		return []llm.ModelInfo{}, nil
	}
	if err := json.Unmarshal([]byte(record.ModelsCacheJSON), &models); err != nil {
		return nil, err
	}
	return models, nil
}

func (a *App) ListAgentConfigs() ([]config.AgentConfig, error) {
	return a.DB.ListAgentConfigs()
}

func (a *App) GetAgentConfig(id string) (*config.AgentConfig, error) {
	agent, err := a.DB.GetAgentConfig(context.Background(), id)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, ErrAgentConfigNotFound
	}
	return agent, nil
}

func (a *App) GetActiveAgentConfig() (*config.AgentConfig, bool, error) {
	activeID, found, err := a.DB.GetActiveAgentConfig()
	if err != nil || !found {
		return nil, false, err
	}
	agent, err := a.DB.GetAgentConfig(context.Background(), activeID)
	if err != nil {
		return nil, false, err
	}
	return agent, agent != nil, nil
}

func (a *App) CreateAgentConfig(agent config.AgentConfig) error {
	if err := agent.Validate(); err != nil {
		return err
	}
	if _, err := agent.ResolveContextConfig(a.globalContextConfig()); err != nil {
		return err
	}
	existing, err := a.DB.GetAgentConfig(context.Background(), agent.ID)
	if err != nil {
		return err
	}
	if existing != nil {
		return ErrAgentConfigExists
	}
	return a.DB.UpsertAgentConfig(agent)
}

func (a *App) UpdateAgentConfig(id string, agent config.AgentConfig) error {
	agent.ID = id
	if err := agent.Validate(); err != nil {
		return err
	}
	if _, err := agent.ResolveContextConfig(a.globalContextConfig()); err != nil {
		return err
	}
	existing, err := a.DB.GetAgentConfig(context.Background(), id)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrAgentConfigNotFound
	}
	if err := a.DB.UpsertAgentConfig(agent); err != nil {
		return err
	}
	active, ok, err := a.DB.GetActiveAgentConfig()
	if err != nil {
		return err
	}
	if ok && active == id {
		return a.ActivateAgentConfig(id)
	}
	return nil
}

func (a *App) DeleteAgentConfig(id string) error {
	err := a.DB.DeleteAgentConfig(id)
	if errors.Is(err, storage.ErrCannotDeleteActiveAgentConfig) {
		return ErrCannotDeleteActiveAgentConfig
	}
	if errors.Is(err, storage.ErrCannotDeleteLastAgentConfig) {
		return ErrCannotDeleteLastAgentConfig
	}
	return err
}

func (a *App) ActivateAgentConfig(id string) error {
	runtime, err := a.buildAgentRuntime(id, true)
	if err != nil {
		return err
	}
	if err := a.DB.SetActiveAgentConfig(id); err != nil {
		return err
	}
	a.mu.Lock()
	a.ActiveAgentRuntime = cloneActiveAgentRuntime(runtime)
	a.LLM = runtime.EmotionMain.Client
	engine := a.engine
	a.mu.Unlock()
	if engine != nil {
		engine.UpdateAgentRuntime(
			runtime.EmotionMain.Client,
			runtime.EmotionSummary.Client,
			toolProviderName(runtime.EmotionMain.Provider.Protocol),
			providerDisplayName(runtime.EmotionMain.Provider),
			runtime.EmotionMain.Model,
			runtime.EmotionMain.Params,
			runtime.EmotionSummary.Model,
			runtime.EmotionSummary.Params,
			runtime.Context,
		)
	}
	return nil
}

func (a *App) globalContextConfig() config.ContextConfig {
	if a != nil && a.Config != nil {
		if err := a.Config.Context.Validate(); err == nil {
			return a.Config.Context
		}
	}
	return config.DefaultConfig().Context
}

func cloneActiveAgentRuntime(runtime *ActiveAgentRuntime) *ActiveAgentRuntime {
	if runtime == nil {
		return nil
	}
	cp := *runtime
	cp.EmotionMain = cloneModelRuntime(runtime.EmotionMain)
	cp.EmotionSummary = cloneModelRuntime(runtime.EmotionSummary)
	cp.WorkMain = cloneModelRuntime(runtime.WorkMain)
	cp.WorkSummary = cloneModelRuntime(runtime.WorkSummary)
	return &cp
}

func cloneModelRuntime(runtime ModelRuntime) ModelRuntime {
	runtime.Params = cloneRequestParams(runtime.Params)
	return runtime
}

func cloneRequestParams(params llm.RequestParams) llm.RequestParams {
	cp := params
	cp.Temperature = cloneFloat64Ptr(params.Temperature)
	cp.TopP = cloneFloat64Ptr(params.TopP)
	cp.PresencePenalty = cloneFloat64Ptr(params.PresencePenalty)
	cp.FrequencyPenalty = cloneFloat64Ptr(params.FrequencyPenalty)
	cp.Stream = cloneBoolPtr(params.Stream)
	if params.Thinking != nil {
		thinking := *params.Thinking
		if params.Thinking.BudgetTokens != nil {
			budget := *params.Thinking.BudgetTokens
			thinking.BudgetTokens = &budget
		}
		cp.Thinking = &thinking
	}
	if params.Extra != nil {
		cp.Extra = make(map[string]any, len(params.Extra))
		for key, value := range params.Extra {
			cp.Extra[key] = value
		}
	}
	return cp
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func derefFloat64(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}

func toolProviderName(protocol string) string {
	if protocol == "anthropic" {
		return "anthropic"
	}
	return "openai"
}

func providerDisplayName(provider config.LLMProvider) string {
	if strings.TrimSpace(provider.Name) != "" {
		return provider.Name
	}
	return provider.ID
}

func cloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func clonePersona(p *config.Persona) *config.Persona {
	if p == nil {
		return nil
	}
	cp := *p
	if p.Quirks != nil {
		cp.Quirks = append([]string(nil), p.Quirks...)
	}
	if p.WorkProgressPhrases != nil {
		cp.WorkProgressPhrases = cloneProgressPhrases(p.WorkProgressPhrases)
	}
	return &cp
}

func clonePersonaMap(src map[string]*config.Persona) map[string]*config.Persona {
	dst := make(map[string]*config.Persona, len(src))
	for key, persona := range src {
		dst[key] = clonePersona(persona)
	}
	return dst
}

func cloneProgressPhrases(src map[string][]string) map[string][]string {
	if src == nil {
		return nil
	}
	dst := make(map[string][]string, len(src))
	for key, phrases := range src {
		dst[key] = append([]string(nil), phrases...)
	}
	return dst
}
