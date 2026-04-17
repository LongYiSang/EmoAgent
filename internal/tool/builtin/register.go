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

	listDirSpec, listDirHandler := NewListDirTool(projectRoot)
	registry.Register(listDirSpec, listDirHandler)

	writeFileSpec, writeFileHandler := NewWriteFileTool(projectRoot)
	registry.Register(writeFileSpec, writeFileHandler)

	editFileSpec, editFileHandler := NewEditFileTool(projectRoot)
	registry.Register(editFileSpec, editFileHandler)

	registerWebSearch(registry, cfg, logger)
	registerWebFetch(registry, cfg, logger)
	registerBash(registry, cfg, projectRoot, logger)
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
	spec, handler := NewWebFetchTool(cfg.WebFetch, logger)
	registry.Register(spec, handler)
	logger.Info("web_fetch registered")
}

// registerBash conditionally registers the bash tool.
// Disabled by default — must be explicitly enabled in config for security.
func registerBash(registry *tool.Registry, cfg *config.Config, projectRoot string, logger *slog.Logger) {
	if !cfg.Bash.Enabled {
		return
	}
	spec, handler := NewBashTool(cfg.Bash, projectRoot, logger)
	registry.Register(spec, handler)
	logger.Info("bash registered")
}
