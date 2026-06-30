package app

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"sync"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/configcenter"
	"github.com/longyisang/emoagent/internal/runtimeenv"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/tool/builtin"
	"github.com/longyisang/emoagent/internal/tool/builtin/websearch"
)

type ToolService struct {
	infra              *Infra
	registry           *tool.Registry
	webSearchMu        sync.RWMutex
	webSearchLastError string
}

func (s *ToolService) Registry() *tool.Registry {
	return s.registry
}

func (s *ToolService) EnsureRegistry() error {
	cfg := s.infra.Config
	if cfg == nil {
		return fmt.Errorf("config is not initialized")
	}
	if s.registry == nil {
		projectRoot := s.infra.ProjectRoot
		if projectRoot == "" {
			var err error
			projectRoot, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}
			s.infra.ProjectRoot = projectRoot
		}
		s.infra.Environment = runtimeenv.BuildEnvironmentFacts(runtime.GOOS, projectRoot, cfg.Bash)
		s.infra.Environment.Timezone = cfg.Time.Timezone
		s.registry = tool.NewRegistry()
		var sqlDB = (*sql.DB)(nil)
		if s.infra.DB != nil {
			sqlDB = s.infra.DB.SqlDB()
		}
		builtin.RegisterAllWithFactsDBAndUsageRecorder(s.registry, cfg, projectRoot, s.infra.Environment, s.infra.Logger, sqlDB, s.infra.DB)
		s.infra.Logger.Info("tool registry initialized", "tools", len(s.registry.Specs()))
	} else if s.infra.Environment.OS == "" {
		projectRoot := s.infra.ProjectRoot
		if projectRoot == "" {
			var err error
			projectRoot, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}
			s.infra.ProjectRoot = projectRoot
		}
		s.infra.Environment = runtimeenv.BuildEnvironmentFacts(runtime.GOOS, projectRoot, cfg.Bash)
		s.infra.Environment.Timezone = cfg.Time.Timezone
	}
	return nil
}

func (s *ToolService) ReconfigureWebSearch(cfg config.WebSearchConfig) configcenter.WebSearchRuntimeStatus {
	if s == nil {
		return configcenter.BuildWebSearchRuntimeStatus(cfg, false, "tool service is unavailable", os.LookupEnv)
	}
	if s.registry == nil {
		if err := s.EnsureRegistry(); err != nil {
			s.setWebSearchLastError(err.Error())
			return configcenter.BuildWebSearchRuntimeStatus(cfg, false, err.Error(), os.LookupEnv)
		}
	}
	if !cfg.Enabled {
		s.registry.Unregister(builtin.WebSearchSpec.Name)
		s.setWebSearchLastError("")
		if s.infra != nil && s.infra.Logger != nil {
			s.infra.Logger.Info("web_search unregistered")
		}
		return s.WebSearchRuntimeStatus(cfg)
	}

	var logger *slog.Logger
	if s.infra != nil {
		logger = s.infra.Logger
	}
	var usageRecorder websearch.UsageRecorder
	if s.infra != nil {
		usageRecorder = s.infra.DB
	}
	provider, err := websearch.NewProviderWithUsageRecorder(cfg, logger, usageRecorder)
	if err != nil {
		s.registry.Unregister(builtin.WebSearchSpec.Name)
		s.setWebSearchLastError(err.Error())
		if logger != nil {
			logger.Warn("web_search disabled", "error", err)
		}
		return s.WebSearchRuntimeStatus(cfg)
	}
	handler := builtin.NewWebSearchHandler(provider, cfg.MaxResults, logger)
	if err := s.registry.Upsert(builtin.WebSearchSpec, handler); err != nil {
		s.registry.Unregister(builtin.WebSearchSpec.Name)
		s.setWebSearchLastError(err.Error())
		if logger != nil {
			logger.Warn("web_search reconfigure failed", "error", err)
		}
		return s.WebSearchRuntimeStatus(cfg)
	}
	s.setWebSearchLastError("")
	if logger != nil {
		logger.Info("web_search reconfigured", "provider", provider.Name())
	}
	return s.WebSearchRuntimeStatus(cfg)
}

func (s *ToolService) WebSearchRuntimeStatus(cfg config.WebSearchConfig) configcenter.WebSearchRuntimeStatus {
	if s == nil {
		return configcenter.BuildWebSearchRuntimeStatus(cfg, false, "tool service is unavailable", os.LookupEnv)
	}
	registered := false
	if s.registry != nil {
		_, registered = s.registry.GetSpec(builtin.WebSearchSpec.Name)
	}
	return configcenter.BuildWebSearchRuntimeStatus(cfg, registered, s.webSearchError(), os.LookupEnv)
}

func (s *ToolService) setWebSearchLastError(message string) {
	s.webSearchMu.Lock()
	defer s.webSearchMu.Unlock()
	s.webSearchLastError = message
}

func (s *ToolService) webSearchError() string {
	s.webSearchMu.RLock()
	defer s.webSearchMu.RUnlock()
	return s.webSearchLastError
}
