package app

import (
	"fmt"
	"os"
	"runtime"

	"github.com/longyisang/emoagent/internal/runtimeenv"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/tool/builtin"
)

type ToolService struct {
	infra    *Infra
	registry *tool.Registry
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
		s.registry = tool.NewRegistry()
		builtin.RegisterAllWithFacts(s.registry, cfg, projectRoot, s.infra.Environment, s.infra.Logger)
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
	}
	return nil
}
