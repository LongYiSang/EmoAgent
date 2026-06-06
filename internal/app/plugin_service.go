package app

import (
	"context"
	"fmt"

	"github.com/longyisang/emoagent/internal/plugin"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/turn"
)

type PluginService struct {
	infra  *Infra
	tools  *ToolService
	host   *plugin.PluginHost
	runner *plugin.BuiltinRunner
}

func (s *PluginService) Host() *plugin.PluginHost {
	return s.host
}

func (s *PluginService) Configure(ctx context.Context, dispatcher *tool.Dispatcher, journal turn.TurnJournal) error {
	if s == nil || s.infra.Config == nil || !s.infra.Config.Plugins.Enabled {
		if s != nil {
			s.host = nil
			s.runner = nil
		}
		return nil
	}
	host := plugin.NewPluginHost(s.infra.Config.Plugins, journal, s.infra.Logger)
	runner := plugin.NewBuiltinRunner(host, s.tools.Registry())
	if err := runner.Load(ctx, plugin.DefaultBuiltinPlugins(), s.infra.Config.Plugins.BuiltinEnabled); err != nil {
		return fmt.Errorf("load builtin plugins: %w", err)
	}
	if dispatcher != nil {
		dispatcher.SetHook(plugin.NewToolHook(host))
	}
	s.host = host
	s.runner = runner
	return nil
}

func (s *PluginService) Close(ctx context.Context) error {
	if s.runner != nil {
		if err := s.runner.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown plugins: %w", err)
		}
		s.runner = nil
	}
	s.host = nil
	return nil
}
