package builtin

import (
	"database/sql"
	"log/slog"
	"path/filepath"
	"runtime"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/resource"
	"github.com/longyisang/emoagent/internal/runtimeenv"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/tool/builtin/webfetch"
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
	RegisterAllWithFactsAndDB(registry, cfg, projectRoot, env, logger, nil)
}

// RegisterAllWithFactsAndDB registers built-in tools with optional DB-backed host resource state.
func RegisterAllWithFactsAndDB(registry *tool.Registry, cfg *config.Config, projectRoot string, env runtimeenv.Facts, logger *slog.Logger, db *sql.DB) {
	hostBroker := configuredHostBroker(cfg, logger)
	changeManager := configuredChangeSetManager(cfg, projectRoot, hostBroker, db, logger)
	registry.Register(GetCurrentTimeSpec, GetCurrentTimeHandler)

	readFileSpec, readFileHandler := NewReadFileToolWithBroker(projectRoot, hostBroker)
	registry.Register(readFileSpec, readFileHandler)

	listDirSpec, listDirHandler := NewListDirToolWithBroker(projectRoot, hostBroker)
	registry.Register(listDirSpec, listDirHandler)

	writeFileSpec, writeFileHandler := NewWriteFileTool(projectRoot)
	registry.Register(writeFileSpec, writeFileHandler)

	editFileSpec, editFileHandler := NewEditFileTool(projectRoot)
	registry.Register(editFileSpec, editFileHandler)

	registerWebSearch(registry, cfg, logger)
	registerWebFetch(registry, cfg, logger)
	registerBash(registry, cfg, env, logger)
	registerHostResourceTools(registry, cfg, projectRoot, hostBroker, changeManager, logger)
}

func configuredHostBroker(cfg *config.Config, logger *slog.Logger) *resource.Broker {
	if cfg == nil || !cfg.HostResources.Enabled {
		return nil
	}
	broker, err := resource.NewBroker(cfg.HostResources)
	if err != nil {
		if logger != nil {
			logger.Warn("host resource broker unavailable; external host resources fail closed", "error", err)
		}
		return resource.NewDenyBroker()
	}
	return broker
}

func configuredChangeSetManager(cfg *config.Config, projectRoot string, broker *resource.Broker, db *sql.DB, logger *slog.Logger) *resource.ChangeSetManager {
	if cfg == nil || !cfg.HostResources.Enabled || broker == nil {
		return nil
	}
	var store resource.ChangeSetStore
	if db != nil {
		store = resource.NewSQLiteChangeSetStore(db)
	}
	manager, err := resource.NewChangeSetManager(broker, store, resource.ChangeSetManagerOptions{
		StagingDir:    hostResourceDataDir(projectRoot, cfg.HostResources.StagingDir),
		QuarantineDir: hostResourceDataDir(projectRoot, cfg.HostResources.QuarantineDir),
		MaxBytes:      cfg.HostResources.MaxReadBytes,
	})
	if err != nil {
		if logger != nil {
			logger.Warn("host resource changeset manager unavailable; external writes fail closed", "error", err)
		}
		return nil
	}
	return manager
}

func hostResourceDataDir(projectRoot, dir string) string {
	if filepath.IsAbs(dir) || projectRoot == "" {
		return dir
	}
	return filepath.Join(projectRoot, dir)
}

func registerHostResourceTools(registry *tool.Registry, cfg *config.Config, projectRoot string, broker *resource.Broker, changeManager *resource.ChangeSetManager, logger *slog.Logger) {
	if cfg == nil || !cfg.HostResources.Enabled {
		return
	}
	for _, pair := range []struct {
		spec    tool.Spec
		handler tool.Handler
	}{
		mustHostTool(NewHostReadTool(broker)),
		mustHostTool(NewHostListTool(broker)),
		mustHostTool(NewHostStatTool(broker)),
		mustHostTool(NewHostSearchTool(broker)),
		mustHostTool(NewHostCopyToWorkspaceTool(broker, projectRoot)),
		mustHostTool(NewHostStageResourceTool(changeManager)),
		mustHostTool(NewHostPrepareChangeTool(changeManager)),
		mustHostTool(NewHostPreviewChangeTool(changeManager)),
		mustHostTool(NewHostApplyChangeTool(changeManager)),
		mustHostTool(NewHostCancelChangeTool(changeManager)),
		mustHostTool(NewHostRestoreQuarantineTool(changeManager)),
	} {
		registry.Register(pair.spec, pair.handler)
	}
	if logger != nil {
		logger.Info("host resource tools registered")
	}
}

func mustHostTool(spec tool.Spec, handler tool.Handler) struct {
	spec    tool.Spec
	handler tool.Handler
} {
	return struct {
		spec    tool.Spec
		handler tool.Handler
	}{spec: spec, handler: handler}
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
	provider, err := webfetch.NewProvider(cfg.WebFetch, logger)
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
