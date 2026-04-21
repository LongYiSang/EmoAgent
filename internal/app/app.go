package app

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/longyisang/emoagent/internal/apperrors"
	"github.com/longyisang/emoagent/internal/chat"
	"github.com/longyisang/emoagent/internal/config"
	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/logger"
	"github.com/longyisang/emoagent/internal/runtimeenv"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/tool/builtin"
	"github.com/longyisang/emoagent/internal/web"
	"github.com/longyisang/emoagent/internal/work"
)

const personaWatchInterval = 5 * time.Second

var (
	ErrLLMProfileExists             = apperrors.ErrLLMProfileExists
	ErrLLMProfileNotFound           = apperrors.ErrLLMProfileNotFound
	ErrCannotDeleteActiveLLMProfile = apperrors.ErrCannotDeleteActiveLLMProfile
	ErrCannotDeleteLastLLMProfile   = apperrors.ErrCannotDeleteLastLLMProfile
	ErrPersonaExists                = apperrors.ErrPersonaExists
	ErrPersonaNotFound              = apperrors.ErrPersonaNotFound
	ErrCannotDeleteDefault          = apperrors.ErrCannotDeleteDefault
	ErrSessionNotFound              = apperrors.ErrSessionNotFound
)

// App is the top-level application container.
type App struct {
	Config           *config.Config
	DB               *storage.DB
	LLM              llm.Client
	Logger           *slog.Logger
	Personas         map[string]*config.Persona
	ActiveLLMProfile *config.LLMProfile
	engine           *chat.Engine
	toolRegistry     *tool.Registry
	environment      runtimeenv.Facts
	mu               sync.RWMutex
	cancel           context.CancelFunc
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

	if err := a.bootstrapLLMProfiles(); err != nil {
		return fmt.Errorf("bootstrap llm profiles: %w", err)
	}
	if err := a.loadActiveLLMProfile(); err != nil {
		return fmt.Errorf("load active llm profile: %w", err)
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
	activeProfile := cloneLLMProfile(a.ActiveLLMProfile)
	currentClient := a.LLM
	a.mu.RUnlock()

	model := cfg.LLM.Model
	summaryModel := cfg.LLM.SummaryModel
	summaryTemperature := effectiveSummaryTemperature(cfg.LLM.SummaryTemperature, activeProfile)
	maxTokens := cfg.LLM.MaxTokens
	temperature := cfg.LLM.Temperature
	provider := cfg.LLM.Provider
	contextCfg := a.globalContextConfig()
	if activeProfile != nil {
		model = activeProfile.Model
		summaryModel = activeProfile.SummaryModel
		maxTokens = activeProfile.MaxTokens
		temperature = activeProfile.Temperature
		provider = activeProfile.Provider
		contextCfg = a.effectiveContextForProfile(*activeProfile)
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
	if _, ok := a.toolRegistry.GetSpec("delegate_to_work"); !ok {
		workLLM, workProfile, err := a.buildWorkClient()
		if err != nil {
			a.Logger.Warn("work runtime disabled", "error", err)
		} else {
			pendingRegistry = work.NewPendingRegistry(a.DB.SqlDB(), a.Logger, work.PendingRegistryConfig{
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

			decider := work.NewLLMRuntimeDecider(workLLM, workProfile.Model)
			workSummaryModel := resolveWorkSummaryModel(workProfile)
			workRuntime := work.NewRuntime(work.RuntimeConfig{
				LLM:                      workLLM,
				SummaryClient:            workLLM,
				SummaryModel:             workSummaryModel,
				Provider:                 workProfile.Provider,
				Model:                    workProfile.Model,
				MaxTokens:                workProfile.MaxTokens,
				Temperature:              workProfile.Temperature,
				MaxToolRounds:            cfg.Work.MaxToolRounds,
				MaxInputTokens:           cfg.Work.MaxInputTokens,
				CompressSoftRatio:        cfg.Work.CompressSoftRatio,
				CompressKeepRounds:       cfg.Work.CompressKeepRounds,
				ToolSnipSoftTokens:       cfg.Work.ToolSnipSoftTokens,
				ToolSnipHardTokens:       cfg.Work.ToolSnipHardTokens,
				Registry:                 a.toolRegistry,
				Dispatcher:               dispatcher,
				Logger:                   a.Logger,
				Decider:                  decider,
				MaxEscalations:           cfg.Work.MaxEscalationsPerTask,
				PendingSnapshotMaxTokens: cfg.Work.PendingSnapshotMaxTokens,
				EnvironmentFacts:         a.environment,
			})
			if _, ok := a.toolRegistry.GetSpec("finish_task"); !ok {
				a.toolRegistry.Register(work.NewFinishTaskTool(), work.FinishTaskPlaceholderHandler)
			}
			if _, ok := a.toolRegistry.GetSpec("request_decision"); !ok {
				a.toolRegistry.Register(work.NewRequestDecisionTool(), work.RequestDecisionPlaceholderHandler)
			}
			if _, ok := a.toolRegistry.GetSpec("resume_work"); !ok {
				resumeSpec, resumeHandler := work.NewResumeTool(workRuntime, pendingRegistry, cfg.Work.JournalDir, a.Logger)
				a.toolRegistry.Register(resumeSpec, resumeHandler)
			}
			if _, ok := a.toolRegistry.GetSpec("list_pending_decisions"); !ok {
				spec, handler := work.NewListDecisionsTool(pendingRegistry)
				a.toolRegistry.Register(spec, handler)
			}
			delegateSpec, delegateHandler := work.NewDelegateTool(workRuntime, pendingRegistry, cfg.Work.JournalDir, a.Logger)
			a.toolRegistry.Register(delegateSpec, delegateHandler)
		}
	}

	a.engine = chat.NewEngine(chat.EngineConfig{
		LLM:                currentClient,
		DB:                 a.DB,
		Logger:             a.Logger,
		Model:              model,
		SummaryModel:       summaryModel,
		SummaryTemperature: summaryTemperature,
		MaxTokens:          maxTokens,
		Temperature:        temperature,
		ContextConfig:      contextCfg,
		Provider:           provider,
		Registry:           a.toolRegistry,
		Dispatcher:         dispatcher,
		Pending:            pendingRegistry,
		Environment:        a.environment,
	})
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
	mux.HandleFunc("GET /api/llm-profiles", api.HandleListLLMProfiles)
	mux.HandleFunc("POST /api/llm-profiles", api.HandleCreateLLMProfile)
	mux.HandleFunc("GET /api/llm-profiles/{id}", api.HandleGetLLMProfile)
	mux.HandleFunc("PUT /api/llm-profiles/{id}", api.HandleUpdateLLMProfile)
	mux.HandleFunc("POST /api/llm-profiles/{id}/activate", api.HandleActivateLLMProfile)
	mux.HandleFunc("DELETE /api/llm-profiles/{id}", api.HandleDeleteLLMProfile)
	mux.HandleFunc("GET /api/personas", api.HandleListPersonas)
	mux.HandleFunc("POST /api/personas", api.HandleCreatePersona)
	mux.HandleFunc("GET /api/personas/{name}", api.HandleGetPersona)
	mux.HandleFunc("PUT /api/personas/{name}", api.HandleUpdatePersona)
	mux.HandleFunc("GET /api/personas/{name}/progress-phrases", api.HandleGetProgressPhrases)
	mux.HandleFunc("PUT /api/personas/{name}/progress-phrases", api.HandleUpdateProgressPhrases)
	mux.HandleFunc("GET /api/progress-phrases/defaults", api.HandleGetProgressPhrasesDefaults)
	mux.HandleFunc("POST /api/personas/{name}/activate", api.HandleActivatePersona)
	mux.HandleFunc("DELETE /api/personas/{name}", api.HandleDeletePersona)
	mux.HandleFunc("GET /api/sessions", api.HandleListSessions)
	mux.HandleFunc("GET /api/sessions/latest", api.HandleGetLatestSession)
	mux.HandleFunc("GET /api/sessions/{id}", api.HandleGetSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", api.HandleDeleteSession)
	mux.Handle("/ws", chatHandler)
	mux.Handle("/", staticHandler)
}

// Shutdown cleanly releases resources.
func (a *App) Shutdown() error {
	if a.cancel != nil {
		a.cancel()
	}
	if a.DB != nil {
		if err := a.DB.Close(); err != nil {
			return fmt.Errorf("close database: %w", err)
		}
	}
	if a.Logger != nil {
		a.Logger.Info("EmoAgent stopped")
	}
	return nil
}

func (a *App) applyRuntimeOverrides() error {
	overrides, err := a.DB.GetAllRuntimeConfig()
	if err != nil {
		return err
	}

	for k, v := range overrides {
		switch k {
		case "llm.provider":
			a.Config.LLM.Provider = v
		case "llm.base_url":
			a.Config.LLM.BaseURL = v
		case "llm.api_key_env":
			a.Config.LLM.APIKeyEnv = v
		case "llm.model":
			a.Config.LLM.Model = v
		case "llm.summary_model":
			a.Config.LLM.SummaryModel = v
		case "llm.summary_temperature":
			if strings.TrimSpace(v) == "" {
				a.Config.LLM.SummaryTemperature = nil
			} else if f, parseErr := strconv.ParseFloat(v, 64); parseErr == nil {
				a.Config.LLM.SummaryTemperature = &f
			} else {
				a.Logger.Warn("invalid runtime override", "key", "llm.summary_temperature", "value", v, "error", parseErr)
			}
		case "llm.temperature":
			if f, parseErr := strconv.ParseFloat(v, 64); parseErr == nil {
				a.Config.LLM.Temperature = f
			} else {
				a.Logger.Warn("invalid runtime override", "key", "llm.temperature", "value", v, "error", parseErr)
			}
		case "llm.max_tokens":
			if n, parseErr := strconv.Atoi(v); parseErr == nil {
				a.Config.LLM.MaxTokens = n
			} else {
				a.Logger.Warn("invalid runtime override", "key", "llm.max_tokens", "value", v, "error", parseErr)
			}
		case "personas.default":
			a.Config.Personas.Default = v
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

func (a *App) bootstrapLLMProfiles() error {
	profiles, err := a.DB.ListLLMProfiles()
	if err != nil {
		return err
	}
	if len(profiles) > 0 {
		return nil
	}

	seed := config.LLMProfile{
		Name:               "default",
		Provider:           a.Config.LLM.Provider,
		BaseURL:            a.Config.LLM.BaseURL,
		APIKeyEnv:          a.Config.LLM.APIKeyEnv,
		Model:              a.Config.LLM.Model,
		SummaryModel:       a.Config.LLM.SummaryModel,
		SummaryTemperature: cloneFloat64Ptr(a.Config.LLM.SummaryTemperature),
		MaxTokens:          a.Config.LLM.MaxTokens,
		Temperature:        a.Config.LLM.Temperature,
	}
	if err := validateLLMProfile(seed); err != nil {
		return err
	}
	if _, err := seed.ResolveContextConfig(a.globalContextConfig()); err != nil {
		return err
	}
	if err := a.DB.UpsertLLMProfile(seed); err != nil {
		return err
	}
	return nil
}

func (a *App) loadActiveLLMProfile() error {
	profiles, err := a.ListLLMProfiles()
	if err != nil {
		return err
	}
	if len(profiles) == 0 {
		return nil
	}

	activeID, found, err := a.DB.GetRuntimeConfig("llm.active_profile")
	if err != nil {
		return err
	}

	var active *config.LLMProfile
	if found {
		for i := range profiles {
			if profiles[i].Name == activeID {
				active = &profiles[i]
				break
			}
		}
	}
	if active == nil {
		active = cloneLLMProfile(&profiles[0])
		if active != nil {
			if err := a.DB.SetRuntimeConfig("llm.active_profile", active.Name); err != nil {
				return err
			}
		}
	}
	if active == nil {
		return nil
	}

	a.mu.Lock()
	a.ActiveLLMProfile = cloneLLMProfile(active)
	a.syncConfigLLMFromProfileLocked(*active)
	a.mu.Unlock()

	client, err := a.buildClientForProfile(*active)
	if err != nil {
		a.Logger.Warn("active llm profile is not currently usable", "profile_name", active.Name, "error", err)
		a.mu.Lock()
		a.LLM = nil
		a.mu.Unlock()
		return nil
	}

	a.mu.Lock()
	a.LLM = client
	a.mu.Unlock()
	a.Logger.Info("active llm profile loaded", "profile_name", active.Name, "provider", active.Provider, "model", active.Model)
	return nil
}

func (a *App) resolveWorkProfile() (*config.LLMProfile, error) {
	if a == nil || a.Config == nil {
		return nil, fmt.Errorf("app config is not initialized")
	}
	if a.DB == nil {
		return nil, fmt.Errorf("database is not initialized")
	}

	name := a.Config.Work.Profile
	if name == "" {
		name = "default"
	}

	record, err := a.DB.GetLLMProfile(context.Background(), name)
	if err != nil {
		return nil, err
	}
	if record != nil {
		profile := llmProfileFromRecord(*record)
		return &profile, nil
	}

	for _, profile := range a.Config.LLMProfiles {
		if profile.Name != name {
			continue
		}
		if err := validateLLMProfile(profile); err != nil {
			return nil, err
		}
		if err := a.DB.UpsertLLMProfile(profile); err != nil {
			return nil, err
		}
		seeded := profile
		return &seeded, nil
	}

	return nil, fmt.Errorf("work profile %q not found in db or config.llm_profiles", name)
}

func (a *App) buildWorkClient() (llm.Client, config.LLMProfile, error) {
	profile, err := a.resolveWorkProfile()
	if err != nil {
		return nil, config.LLMProfile{}, err
	}
	client, err := a.buildClientForProfile(*profile)
	if err != nil {
		return nil, config.LLMProfile{}, err
	}
	return client, *profile, nil
}

func resolveWorkSummaryModel(profile config.LLMProfile) string {
	if strings.TrimSpace(profile.SummaryModel) != "" {
		return profile.SummaryModel
	}
	return profile.Model
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
	return a.Config.Personas.Default
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

	if key == a.Config.Personas.Default {
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

// ActivatePersona switches the default persona used by new chat sessions.
func (a *App) ActivatePersona(key string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, exists := a.Personas[key]; !exists {
		return ErrPersonaNotFound
	}
	if err := a.DB.SetRuntimeConfig("personas.default", key); err != nil {
		return err
	}
	a.Config.Personas.Default = key
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

// ListLLMProfiles returns all stored LLM profiles sorted by name.
func (a *App) ListLLMProfiles() ([]config.LLMProfile, error) {
	records, err := a.DB.ListLLMProfiles()
	if err != nil {
		return nil, err
	}

	profiles := make([]config.LLMProfile, 0, len(records))
	for _, record := range records {
		profiles = append(profiles, llmProfileFromRecord(record))
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles, nil
}

// GetLLMProfile fetches one LLM profile by id.
func (a *App) GetLLMProfile(id string) (*config.LLMProfile, error) {
	record, err := a.DB.GetLLMProfile(context.Background(), id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, ErrLLMProfileNotFound
	}
	profile := llmProfileFromRecord(*record)
	return &profile, nil
}

// GetActiveLLMProfile returns a copy of the currently active LLM profile.
func (a *App) GetActiveLLMProfile() (*config.LLMProfile, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.ActiveLLMProfile == nil {
		return nil, false
	}
	return cloneLLMProfile(a.ActiveLLMProfile), true
}

// CreateLLMProfile creates a new LLM profile.
func (a *App) CreateLLMProfile(profile config.LLMProfile) error {
	if err := validateLLMProfile(profile); err != nil {
		return err
	}
	if _, err := profile.ResolveContextConfig(a.globalContextConfig()); err != nil {
		return err
	}

	existing, err := a.DB.GetLLMProfile(context.Background(), profile.Name)
	if err != nil {
		return err
	}
	if existing != nil {
		return ErrLLMProfileExists
	}

	return a.DB.UpsertLLMProfile(profile)
}

// UpdateLLMProfile updates an existing LLM profile.
func (a *App) UpdateLLMProfile(id string, profile config.LLMProfile) error {
	profile.Name = id
	if err := validateLLMProfile(profile); err != nil {
		return err
	}
	if _, err := profile.ResolveContextConfig(a.globalContextConfig()); err != nil {
		return err
	}

	currentRecord, err := a.DB.GetLLMProfile(context.Background(), id)
	if err != nil {
		return err
	}
	if currentRecord == nil {
		return ErrLLMProfileNotFound
	}
	current := llmProfileFromRecord(*currentRecord)

	active, activeOK := a.GetActiveLLMProfile()
	isActive := activeOK && active.Name == id
	a.mu.RLock()
	hasClient := a.LLM != nil
	a.mu.RUnlock()

	var newClient llm.Client
	needRebuild := isActive && (!hasClient || current.Provider != profile.Provider || current.BaseURL != profile.BaseURL || current.APIKeyEnv != profile.APIKeyEnv)
	if needRebuild {
		newClient, err = a.buildClientForProfile(profile)
		if err != nil {
			return err
		}
	}

	if err := a.DB.UpsertLLMProfile(profile); err != nil {
		return err
	}

	if isActive {
		a.mu.Lock()
		a.ActiveLLMProfile = cloneLLMProfile(&profile)
		if needRebuild {
			a.LLM = newClient
		}
		a.syncConfigLLMFromProfileLocked(profile)
		engine := a.engine
		a.mu.Unlock()

		if engine != nil {
			engine.UpdateConfig(newClient, profile.Provider, profile.Model, profile.SummaryModel, effectiveSummaryTemperature(a.Config.LLM.SummaryTemperature, &profile), profile.MaxTokens, profile.Temperature, a.effectiveContextForProfile(profile))
		}
	}

	return nil
}

// ActivateLLMProfile switches the active profile and hot-swaps the chat engine.
func (a *App) ActivateLLMProfile(id string) error {
	profile, err := a.GetLLMProfile(id)
	if err != nil {
		return err
	}

	client, err := a.buildClientForProfile(*profile)
	if err != nil {
		return err
	}
	if err := a.DB.SetRuntimeConfig("llm.active_profile", id); err != nil {
		return err
	}

	a.mu.Lock()
	a.ActiveLLMProfile = cloneLLMProfile(profile)
	a.LLM = client
	a.syncConfigLLMFromProfileLocked(*profile)
	engine := a.engine
	a.mu.Unlock()

	if engine != nil {
		engine.UpdateConfig(client, profile.Provider, profile.Model, profile.SummaryModel, effectiveSummaryTemperature(a.Config.LLM.SummaryTemperature, profile), profile.MaxTokens, profile.Temperature, a.effectiveContextForProfile(*profile))
	}
	return nil
}

// DeleteLLMProfile removes a profile that is not active.
func (a *App) DeleteLLMProfile(id string) error {
	active, activeOK := a.GetActiveLLMProfile()
	if activeOK && active.Name == id {
		return ErrCannotDeleteActiveLLMProfile
	}

	profiles, err := a.ListLLMProfiles()
	if err != nil {
		return err
	}
	if len(profiles) <= 1 {
		return ErrCannotDeleteLastLLMProfile
	}

	record, err := a.DB.GetLLMProfile(context.Background(), id)
	if err != nil {
		return err
	}
	if record == nil {
		return ErrLLMProfileNotFound
	}

	return a.DB.DeleteLLMProfile(id)
}

func (a *App) buildClientForProfile(profile config.LLMProfile) (llm.Client, error) {
	cfg := config.LLMConfig{
		Provider:           profile.Provider,
		BaseURL:            profile.BaseURL,
		APIKeyEnv:          profile.APIKeyEnv,
		Model:              profile.Model,
		SummaryModel:       profile.SummaryModel,
		SummaryTemperature: cloneFloat64Ptr(profile.SummaryTemperature),
		MaxTokens:          profile.MaxTokens,
		Temperature:        profile.Temperature,
	}
	return llm.NewClient(cfg, a.Logger)
}

func (a *App) syncConfigLLMFromProfileLocked(profile config.LLMProfile) {
	if a.Config == nil {
		return
	}
	a.Config.LLM.Provider = profile.Provider
	a.Config.LLM.BaseURL = profile.BaseURL
	a.Config.LLM.APIKeyEnv = profile.APIKeyEnv
	a.Config.LLM.Model = profile.Model
	a.Config.LLM.SummaryModel = profile.SummaryModel
	a.Config.LLM.MaxTokens = profile.MaxTokens
	a.Config.LLM.Temperature = profile.Temperature
}

func (a *App) effectiveContextForProfile(profile config.LLMProfile) config.ContextConfig {
	base := a.globalContextConfig()
	effective, err := profile.ResolveContextConfig(base)
	if err != nil {
		if a.Logger != nil {
			a.Logger.Warn("resolve context config for profile failed", "profile", profile.Name, "error", err)
		}
		return base
	}
	return effective
}

func (a *App) globalContextConfig() config.ContextConfig {
	if a != nil && a.Config != nil {
		if err := a.Config.Context.Validate(); err == nil {
			return a.Config.Context
		}
	}
	return config.DefaultConfig().Context
}

func validateLLMProfile(profile config.LLMProfile) error {
	return profile.Validate()
}

func llmProfileFromRecord(record storage.LLMProfileRecord) config.LLMProfile {
	return config.LLMProfile{
		Name:                record.Name,
		Provider:            record.Provider,
		BaseURL:             record.BaseURL,
		APIKeyEnv:           record.APIKeyEnv,
		Model:               record.Model,
		SummaryModel:        record.SummaryModel,
		SummaryTemperature:  nullableFloatPtr(record.SummaryTemperature),
		MaxTokens:           record.MaxTokens,
		Temperature:         record.Temperature,
		InputBudgetTokens:   nullableIntPtr(record.InputBudgetTokens),
		SoftCompactRatio:    nullableFloatPtr(record.SoftCompactRatio),
		HardCompactRatio:    nullableFloatPtr(record.HardCompactRatio),
		ReserveOutputTokens: nullableIntPtr(record.ReserveOutputTokens),
	}
}

func cloneLLMProfile(profile *config.LLMProfile) *config.LLMProfile {
	if profile == nil {
		return nil
	}
	cp := *profile
	if profile.SummaryTemperature != nil {
		value := *profile.SummaryTemperature
		cp.SummaryTemperature = &value
	}
	if profile.InputBudgetTokens != nil {
		value := *profile.InputBudgetTokens
		cp.InputBudgetTokens = &value
	}
	if profile.SoftCompactRatio != nil {
		value := *profile.SoftCompactRatio
		cp.SoftCompactRatio = &value
	}
	if profile.HardCompactRatio != nil {
		value := *profile.HardCompactRatio
		cp.HardCompactRatio = &value
	}
	if profile.ReserveOutputTokens != nil {
		value := *profile.ReserveOutputTokens
		cp.ReserveOutputTokens = &value
	}
	return &cp
}

func nullableIntPtr(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	v := int(value.Int64)
	return &v
}

func nullableFloatPtr(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	v := value.Float64
	return &v
}

func effectiveSummaryTemperature(global *float64, profile *config.LLMProfile) *float64 {
	if profile != nil && profile.SummaryTemperature != nil {
		return cloneFloat64Ptr(profile.SummaryTemperature)
	}
	if global != nil {
		return cloneFloat64Ptr(global)
	}
	value := contextutil.DefaultSummaryTemperature()
	return &value
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
