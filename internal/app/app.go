package app

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/longyisang/emoagent/internal/apperrors"
	"github.com/longyisang/emoagent/internal/chat"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/logger"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/web"
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

	for _, p := range personas {
		if err := a.DB.UpsertPersona(p.Name, p.Description, p.SystemPrompt, p.Tone, p.Quirks, p.Greeting); err != nil {
			a.Logger.Warn("sync persona to db failed", "name", p.Name, "error", err)
		}
	}

	watchCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	go config.WatchPersonas(watchCtx, cfg.Personas.Dir, personaWatchInterval, func(updated map[string]*config.Persona) {
		a.mu.Lock()
		a.Personas = clonePersonaMap(updated)
		a.mu.Unlock()

		for _, p := range updated {
			if err := a.DB.UpsertPersona(p.Name, p.Description, p.SystemPrompt, p.Tone, p.Quirks, p.Greeting); err != nil {
				a.Logger.Warn("sync updated persona failed", "name", p.Name, "error", err)
			}
		}
		a.Logger.Info("personas reloaded", "count", len(updated))
	})

	a.Logger.Info("EmoAgent initialized")
	return nil
}

// Run starts the HTTP server and blocks until the context is cancelled.
func (a *App) Run(ctx context.Context) error {
	a.mu.RLock()
	activeProfile := cloneLLMProfile(a.ActiveLLMProfile)
	currentClient := a.LLM
	a.mu.RUnlock()

	model := a.Config.LLM.Model
	maxTokens := a.Config.LLM.MaxTokens
	temperature := a.Config.LLM.Temperature
	if activeProfile != nil {
		model = activeProfile.Model
		maxTokens = activeProfile.MaxTokens
		temperature = activeProfile.Temperature
	}

	a.engine = chat.NewEngine(chat.EngineConfig{
		LLM:          currentClient,
		DB:           a.DB,
		Logger:       a.Logger,
		Model:        model,
		MaxTokens:    maxTokens,
		Temperature:  temperature,
		HistoryLimit: 20,
	})
	chatHandler := chat.NewHandler(a.engine, a, a.Logger)

	staticSub, err := fs.Sub(web.StaticFS, "static")
	if err != nil {
		return fmt.Errorf("load embedded web assets: %w", err)
	}

	api := web.NewAPIHandler(a, a.Logger)

	mux := http.NewServeMux()
	registerRoutes(mux, api, chatHandler, http.FileServer(http.FS(staticSub)))

	addr := fmt.Sprintf("%s:%d", a.Config.Server.Host, a.Config.Server.Port)
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
	mux.HandleFunc("POST /api/personas/{name}/activate", api.HandleActivatePersona)
	mux.HandleFunc("DELETE /api/personas/{name}", api.HandleDeletePersona)
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
		case "llm.temperature":
			if f, parseErr := strconv.ParseFloat(v, 64); parseErr == nil {
				a.Config.LLM.Temperature = f
			}
		case "llm.max_tokens":
			if n, parseErr := strconv.Atoi(v); parseErr == nil {
				a.Config.LLM.MaxTokens = n
			}
		case "personas.default":
			a.Config.Personas.Default = v
		case "server.port":
			if n, parseErr := strconv.Atoi(v); parseErr == nil {
				a.Config.Server.Port = n
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
		Name:         "default",
		Provider:     a.Config.LLM.Provider,
		BaseURL:      a.Config.LLM.BaseURL,
		APIKeyEnv:    a.Config.LLM.APIKeyEnv,
		Model:        a.Config.LLM.Model,
		SummaryModel: a.Config.LLM.SummaryModel,
		MaxTokens:    a.Config.LLM.MaxTokens,
		Temperature:  a.Config.LLM.Temperature,
	}
	if err := validateLLMProfile(seed); err != nil {
		return err
	}
	if err := a.DB.UpsertLLMProfile(seed.Name, seed.Provider, seed.BaseURL, seed.Model, seed.SummaryModel, seed.MaxTokens, seed.Temperature, seed.APIKeyEnv); err != nil {
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
	if err := a.DB.UpsertPersona(next.Name, next.Description, next.SystemPrompt, next.Tone, next.Quirks, next.Greeting); err != nil {
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
	current, exists := a.Personas[key]
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
	if err := a.DB.UpsertPersona(next.Name, next.Description, next.SystemPrompt, next.Tone, next.Quirks, next.Greeting); err != nil {
		return fmt.Errorf("upsert persona: %w", err)
	}
	if current.Name != "" && current.Name != next.Name {
		if err := a.DB.DeletePersona(context.Background(), current.Name); err != nil {
			return fmt.Errorf("delete old persona from db: %w", err)
		}
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
	current, exists := a.Personas[key]
	if !exists {
		return ErrPersonaNotFound
	}
	if err := config.DeletePersonaFile(a.Config.Personas.Dir, key); err != nil {
		return fmt.Errorf("delete persona file: %w", err)
	}
	dbKey := current.Name
	if dbKey == "" {
		dbKey = key
	}
	if err := a.DB.DeletePersona(context.Background(), dbKey); err != nil {
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

	existing, err := a.DB.GetLLMProfile(context.Background(), profile.Name)
	if err != nil {
		return err
	}
	if existing != nil {
		return ErrLLMProfileExists
	}

	return a.DB.UpsertLLMProfile(profile.Name, profile.Provider, profile.BaseURL, profile.Model, profile.SummaryModel, profile.MaxTokens, profile.Temperature, profile.APIKeyEnv)
}

// UpdateLLMProfile updates an existing LLM profile.
func (a *App) UpdateLLMProfile(id string, profile config.LLMProfile) error {
	profile.Name = id
	if err := validateLLMProfile(profile); err != nil {
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

	if err := a.DB.UpsertLLMProfile(profile.Name, profile.Provider, profile.BaseURL, profile.Model, profile.SummaryModel, profile.MaxTokens, profile.Temperature, profile.APIKeyEnv); err != nil {
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
			engine.UpdateConfig(newClient, profile.Model, profile.MaxTokens, profile.Temperature)
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
		engine.UpdateConfig(client, profile.Model, profile.MaxTokens, profile.Temperature)
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
		Provider:     profile.Provider,
		BaseURL:      profile.BaseURL,
		APIKeyEnv:    profile.APIKeyEnv,
		Model:        profile.Model,
		SummaryModel: profile.SummaryModel,
		MaxTokens:    profile.MaxTokens,
		Temperature:  profile.Temperature,
	}
	return llm.NewClient(cfg, a.Logger)
}

func (a *App) syncConfigLLMFromProfileLocked(profile config.LLMProfile) {
	a.Config.LLM.Provider = profile.Provider
	a.Config.LLM.BaseURL = profile.BaseURL
	a.Config.LLM.APIKeyEnv = profile.APIKeyEnv
	a.Config.LLM.Model = profile.Model
	a.Config.LLM.SummaryModel = profile.SummaryModel
	a.Config.LLM.MaxTokens = profile.MaxTokens
	a.Config.LLM.Temperature = profile.Temperature
}

func validateLLMProfile(profile config.LLMProfile) error {
	if profile.Name == "" {
		return fmt.Errorf("name is required")
	}
	switch profile.Provider {
	case "openai", "anthropic":
	default:
		return fmt.Errorf("unsupported provider: %s", profile.Provider)
	}
	if profile.BaseURL == "" {
		return fmt.Errorf("base_url is required")
	}
	if profile.Model == "" {
		return fmt.Errorf("model is required")
	}
	if profile.MaxTokens <= 0 {
		return fmt.Errorf("max_tokens must be greater than 0")
	}
	if profile.Temperature < 0 || profile.Temperature > 2 {
		return fmt.Errorf("temperature must be between 0 and 2")
	}
	return nil
}

func llmProfileFromRecord(record storage.LLMProfileRecord) config.LLMProfile {
	return config.LLMProfile{
		Name:         record.Name,
		Provider:     record.Provider,
		BaseURL:      record.BaseURL,
		APIKeyEnv:    record.APIKeyEnv,
		Model:        record.Model,
		SummaryModel: record.SummaryModel,
		MaxTokens:    record.MaxTokens,
		Temperature:  record.Temperature,
	}
}

func cloneLLMProfile(profile *config.LLMProfile) *config.LLMProfile {
	if profile == nil {
		return nil
	}
	cp := *profile
	return &cp
}

func clonePersona(p *config.Persona) *config.Persona {
	if p == nil {
		return nil
	}
	cp := *p
	if p.Quirks != nil {
		cp.Quirks = append([]string(nil), p.Quirks...)
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
