package builtin

import (
	"log/slog"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/tool/builtin/websearch"
)

// RegisterAll registers all built-in tools with the given registry.
// Called once during App initialization.
func RegisterAll(registry *tool.Registry, cfg *config.Config, projectRoot string, logger *slog.Logger) {
	registry.Register(GetCurrentTimeSpec, GetCurrentTimeHandler)
	readFileSpec, readFileHandler := NewReadFileTool(projectRoot)
	registry.Register(readFileSpec, readFileHandler)

	registerWebSearch(registry, cfg, logger)

	// Future built-in tools: add registerXxx() calls here.
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
