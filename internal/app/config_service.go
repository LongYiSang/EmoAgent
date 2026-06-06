package app

import (
	"context"
	"fmt"
	"strconv"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/configcenter"
	sidecarruntime "github.com/longyisang/emoagent/internal/sidecar"
)

type ConfigService struct {
	infra *Infra
}

func (s *ConfigService) service() *configcenter.Service {
	return configcenter.NewService(s.infra.Config, s.infra.DB)
}

func (s *ConfigService) ApplyRuntimeOverrides() error {
	overrides, err := s.infra.DB.GetAllRuntimeConfig()
	if err != nil {
		return err
	}

	for k, v := range overrides {
		switch k {
		case "chat.realtime_streaming":
			enabled, parseErr := strconv.ParseBool(v)
			if parseErr == nil {
				s.infra.Config.Chat.RealtimeStreaming = enabled
			} else {
				s.infra.Logger.Warn("invalid runtime override", "key", "chat.realtime_streaming", "value", v, "error", parseErr)
			}
		case "server.port":
			if n, parseErr := strconv.Atoi(v); parseErr == nil {
				s.infra.Config.Server.Port = n
			} else {
				s.infra.Logger.Warn("invalid runtime override", "key", "server.port", "value", v, "error", parseErr)
			}
		}
	}

	if len(overrides) > 0 {
		s.infra.Logger.Info("runtime config overrides applied", "count", len(overrides))
	}
	settings, err := s.infra.DB.ListRuntimeSettings()
	if err != nil {
		return err
	}
	if len(settings) > 0 {
		runtimeCfg, issues := configcenter.ApplyRuntimeSettings(s.infra.Config, settings)
		s.infra.Config = &runtimeCfg
		for _, issue := range issues {
			s.infra.Logger.Warn("runtime setting rejected", "path", issue.Path, "message", issue.Message)
		}
		s.infra.Logger.Info("runtime settings applied", "count", len(settings))
	}
	return nil
}

func (s *ConfigService) GetChatSettings() config.ChatConfig {
	if s.infra.Config == nil {
		return config.ChatConfig{}
	}
	return s.infra.Config.Chat
}

func (s *ConfigService) UpdateChatSettings(settings config.ChatConfig, chat *ChatService) error {
	if s.infra.DB == nil {
		return fmt.Errorf("database is not initialized")
	}
	if err := s.infra.DB.SetRuntimeConfig("chat.realtime_streaming", strconv.FormatBool(settings.RealtimeStreaming)); err != nil {
		return err
	}

	if s.infra.Config == nil {
		s.infra.Config = config.DefaultConfig()
	}
	s.infra.Config.Chat.RealtimeStreaming = settings.RealtimeStreaming
	if chat != nil {
		chat.UpdateRealtimeStreaming(settings.RealtimeStreaming)
	}
	return nil
}

func (s *ConfigService) GetEffective(ctx context.Context) (configcenter.EffectiveConfig, error) {
	return s.service().BuildEffective(ctx)
}

func (s *ConfigService) Validate(ctx context.Context, req configcenter.ValidateRequest) (configcenter.ValidateResponse, error) {
	return s.service().Validate(ctx, req)
}

func (s *ConfigService) ListIssues(ctx context.Context) ([]configcenter.ConfigIssue, error) {
	return s.service().Issues(ctx)
}

func (s *ConfigService) GetMemoryConfig(ctx context.Context) (configcenter.MemoryConfigResponse, error) {
	return s.service().MemoryConfig(ctx)
}

func (s *ConfigService) UpdateMemoryConfig(ctx context.Context, memory config.MemoryConfig) (configcenter.EffectiveConfig, error) {
	effective, err := s.service().UpdateMemoryConfig(ctx, memory)
	if err == nil && s.infra.Config != nil {
		s.infra.Config.Memory = effective.Memory
	}
	return effective, err
}

func (s *ConfigService) GetMemoryFeatures(ctx context.Context) (configcenter.MemoryConfigResponse, error) {
	return s.GetMemoryConfig(ctx)
}

func (s *ConfigService) UpdateMemoryFeatures(ctx context.Context, memory config.MemoryConfig) (configcenter.EffectiveConfig, error) {
	effective, err := s.service().UpdateMemoryFeatures(ctx, memory)
	if err == nil && s.infra.Config != nil {
		s.infra.Config.Memory = effective.Memory
	}
	return effective, err
}

func (s *ConfigService) BuildSidecarSpec(ctx context.Context) (sidecarruntime.Spec, []configcenter.ConfigIssue, error) {
	return s.service().BuildSidecarSpec(ctx)
}

func (s *ConfigService) BuildMemoryCoreOpenConfig(ctx context.Context, status *sidecarruntime.Status) (configcenter.MemoryCoreOpenConfig, error) {
	return s.service().BuildMemoryCoreOpenConfig(ctx, status)
}
