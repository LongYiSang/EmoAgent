package app

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/logger"
	"github.com/longyisang/emoagent/internal/storage"
)

const personaWatchInterval = 5 // seconds

// App is the top-level application container.
type App struct {
	Config   *config.Config
	DB       *storage.DB
	LLM      llm.Client
	Logger   *slog.Logger
	Personas map[string]*config.Persona
	mu       sync.RWMutex
	cancel   context.CancelFunc
}

// New creates an uninitialized App.
func New() *App {
	return &App{}
}

// Init loads config, opens DB, creates LLM client, and starts persona watcher.
func (a *App) Init(ctx context.Context, configPath string) error {
	// 1. Load .env (silent fail if missing).
	_ = godotenv.Load()

	// 2. Load YAML config.
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	a.Config = cfg

	// 3. Initialize logger.
	a.Logger = logger.Init(cfg.Log.Level, cfg.Log.Format)
	a.Logger.Info("config loaded", "path", configPath)

	// 4. Open database.
	db, err := storage.Open(cfg.DB.Path, a.Logger)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	a.DB = db

	// 5. Apply runtime config overrides (3-tier config).
	if err := a.applyRuntimeOverrides(); err != nil {
		a.Logger.Warn("runtime config overrides failed", "error", err)
	}

	// 6. Load persona files.
	personas, err := config.LoadAllPersonas(cfg.Personas.Dir)
	if err != nil {
		a.Logger.Warn("load personas failed", "error", err)
		personas = make(map[string]*config.Persona)
	}
	a.Personas = personas
	a.Logger.Info("personas loaded", "count", len(personas))

	// 7. Sync personas to DB.
	for _, p := range personas {
		if err := a.DB.UpsertPersona(p.Name, p.Description, p.SystemPrompt, p.Tone, p.Quirks, p.Greeting); err != nil {
			a.Logger.Warn("sync persona to db failed", "name", p.Name, "error", err)
		}
	}

	// 8. Create LLM client.
	client, err := llm.NewClient(cfg.LLM, a.Logger)
	if err != nil {
		a.Logger.Warn("LLM client creation failed (will be needed for conversation)", "error", err)
	} else {
		a.LLM = client
		a.Logger.Info("LLM client created", "provider", cfg.LLM.Provider, "model", cfg.LLM.Model)
	}

	// 9. Start persona file watcher.
	watchCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	go config.WatchPersonas(watchCtx, cfg.Personas.Dir, personaWatchInterval*time.Second, func(updated map[string]*config.Persona) {
		a.mu.Lock()
		a.Personas = updated
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

// Run blocks until the context is cancelled. Phase 0 has no HTTP server.
func (a *App) Run(ctx context.Context) error {
	a.Logger.Info("EmoAgent running, press Ctrl+C to stop")
	<-ctx.Done()
	return nil
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
	a.Logger.Info("EmoAgent stopped")
	return nil
}

// applyRuntimeOverrides reads config_runtime table and overrides Config fields.
func (a *App) applyRuntimeOverrides() error {
	overrides, err := a.DB.GetAllRuntimeConfig()
	if err != nil {
		return err
	}

	for k, v := range overrides {
		switch k {
		case "llm.model":
			a.Config.LLM.Model = v
		case "llm.temperature":
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				a.Config.LLM.Temperature = f
			}
		case "llm.max_tokens":
			if n, err := strconv.Atoi(v); err == nil {
				a.Config.LLM.MaxTokens = n
			}
		case "personas.default":
			a.Config.Personas.Default = v
		case "server.port":
			if n, err := strconv.Atoi(v); err == nil {
				a.Config.Server.Port = n
			}
		}
	}

	if len(overrides) > 0 {
		a.Logger.Info("runtime config overrides applied", "count", len(overrides))
	}
	return nil
}

// GetPersona returns a persona by name (thread-safe).
func (a *App) GetPersona(name string) (*config.Persona, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	p, ok := a.Personas[name]
	return p, ok
}
