package builtin

import (
	"log/slog"
	"runtime"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/runtimeenv"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/tool/builtin/web_fetch_tavily"
	"github.com/longyisang/emoagent/internal/tool/builtin/websearch"
)

// RegisterAll registers all built-in tools with the given registry.
// Called once during App initialization.
func RegisterAll(registry *tool.Registry, cfg *config.Config, projectRoot string, logger *slog.Logger) {
	env := runtimeenv.BuildEnvironmentFacts(runtime.GOOS, projectRoot, cfg.Bash)
	RegisterAllWithFacts(registry, cfg, projectRoot, env, logger)
}

// RegisterAllWithFacts registers all built-in tools with explicit environment facts.
func RegisterAllWithFacts(registry *tool.Registry, cfg *config.Config, projectRoot string, env runtimeenv.Facts, logger *slog.Logger) {
	registry.Register(GetCurrentTimeSpec, GetCurrentTimeHandler)

	readFileSpec, readFileHandler := NewReadFileTool(projectRoot)
	registry.Register(readFileSpec, readFileHandler)

	listDirSpec, listDirHandler := NewListDirTool(projectRoot)
	registry.Register(listDirSpec, listDirHandler)

	writeFileSpec, writeFileHandler := NewWriteFileTool(projectRoot)
	registry.Register(writeFileSpec, writeFileHandler)

	editFileSpec, editFileHandler := NewEditFileTool(projectRoot)
	registry.Register(editFileSpec, editFileHandler)

	registerWebSearch(registry, cfg, logger)
	registerWebFetch(registry, cfg, logger)
	registerBash(registry, cfg, env, logger)
}

// registerWebSearch conditionally registers the web_search tool.
// Failures are logged and skipped — they do NOT abort registration of other tools.
func registerWebSearch(registry *tool.Registry, cfg *config.Config, logger *slog.Logger) {
	if !cfg.WebSearch.Enabled {
		return
	}
	provider, err := websearch.NewProvider(cfg.WebSearch, logger)
	if err != nil {
		logger.Warn("web_search disabled", "error", err)
		return
	}
	registry.Register(WebSearchSpec, NewWebSearchHandler(provider, cfg.WebSearch.MaxResults, logger))
	logger.Info("web_search registered", "provider", provider.Name())
}

// registerWebFetch conditionally registers the web_fetch tool.
func registerWebFetch(registry *tool.Registry, cfg *config.Config, logger *slog.Logger) {
	if !cfg.WebFetch.Enabled {
		return
	}
	provider, err := web_fetch_tavily.NewProvider(cfg.WebFetch, logger)
	if err != nil {
		logger.Warn("web_fetch disabled", "error", err)
		return
	}
	spec, handler := NewWebFetchToolWithProvider(provider, cfg.WebFetch, logger)
	registry.Register(spec, handler)
	logger.Info("web_fetch registered", "provider", provider.Name())
}

// registerBash conditionally registers the bash tool.
// Disabled by default — must be explicitly enabled in config for security.
func registerBash(registry *tool.Registry, cfg *config.Config, env runtimeenv.Facts, logger *slog.Logger) {
	if !cfg.Bash.Enabled {
		return
	}
	spec, handler := NewBashToolWithFacts(cfg.Bash, env, logger)
	registry.Register(spec, handler)
	logger.Info("bash registered")
}
