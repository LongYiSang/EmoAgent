package app

import (
	"io"
	"log/slog"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/memoryhost"
	"github.com/longyisang/emoagent/internal/plugin"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tool"
)

func newTestApp(cfg *config.Config, db *storage.DB, logger *slog.Logger) *App {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	kernel := NewKernel(&Infra{
		Config: cfg,
		DB:     db,
		Logger: logger,
	})
	return &App{kernel: kernel}
}

func newTestAppWithMemory(cfg *config.Config, host *memoryhost.Host, logger *slog.Logger) *App {
	a := newTestApp(cfg, nil, logger)
	a.kernel.Services.Memory.host = host
	return a
}

func setTestPersonas(a *App, personas map[string]*config.Persona) {
	a.kernel.Services.Personas.SetAll(personas)
}

func setTestActiveRuntime(a *App, runtime *ActiveAgentRuntime) {
	a.kernel.Services.AgentRuntime.setActive(runtime)
}

func testActiveRuntime(a *App) *ActiveAgentRuntime {
	return a.kernel.Services.AgentRuntime.Active()
}

func testConfig(a *App) *config.Config {
	return a.kernel.Infra.Config
}

func testMemoryHost(a *App) *memoryhost.Host {
	return a.kernel.Services.Memory.Host()
}

func testPluginHost(a *App) *plugin.PluginHost {
	return a.kernel.Services.Plugins.Host()
}

func setTestToolRegistry(a *App, registry *tool.Registry) {
	a.kernel.Services.Tools.registry = registry
}
