package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/platform"
	"github.com/longyisang/emoagent/internal/platform/onebotv11"
)

type PlatformService struct {
	gateway       *PlatformGateway
	manager       *platform.Manager
	adapters      []platform.Adapter
	started       []platform.Adapter
	onebotReverse *onebotv11.ReverseServer
	enabled       bool
	logger        *slog.Logger
	media         *MediaService
}

func NewPlatformService(infra *Infra, conversation *ConversationService, commands *CommandService, chat *ChatService, agentRuntime *AgentRuntimeService, personas *PersonaService, media *MediaService) *PlatformService {
	var receipts platform.ReceiptStore
	if infra != nil && infra.DB != nil {
		receipts = NewStorageReceiptStore(infra.DB)
	}
	service := &PlatformService{
		gateway:       NewPlatformGateway(infra, conversation, commands, chat, agentRuntime, personas, receipts),
		manager:       platform.NewManager(),
		onebotReverse: onebotv11.NewReverseServer(),
		media:         media,
	}
	if infra != nil {
		service.logger = infra.Logger
	}
	return service
}

func (s *PlatformService) Gateway() *PlatformGateway {
	if s == nil {
		return nil
	}
	return s.gateway
}

func (s *PlatformService) Manager() *platform.Manager {
	if s == nil {
		return nil
	}
	return s.manager
}

func (s *PlatformService) Configure(ctx context.Context, cfg config.PlatformsConfig) error {
	if s == nil {
		return nil
	}
	if err := s.Stop(ctx); err != nil {
		return err
	}
	s.manager = platform.NewManager()
	s.adapters = nil
	if s.onebotReverse == nil {
		s.onebotReverse = onebotv11.NewReverseServer()
	} else {
		s.onebotReverse.ClearAdapters()
	}
	s.enabled = false
	if !cfg.Enabled {
		return nil
	}
	s.enabled = true
	for id, adapterCfg := range cfg.Adapters {
		if !adapterCfg.Enabled {
			continue
		}
		switch strings.TrimSpace(adapterCfg.Kind) {
		case onebotv11.KindOneBotV11:
			adapter, err := onebotv11.NewAdapter(id, adapterCfg)
			if err != nil {
				return fmt.Errorf("configure platform adapter %s: %w", id, err)
			}
			adapter.SetLogger(s.logger)
			adapter.SetInboundMediaStore(s.media)
			s.manager.Register(id, adapter)
			s.adapters = append(s.adapters, adapter)
			if s.logger != nil {
				adapterStatus := adapter.Status()
				s.logger.Info("onebot adapter configured", "id", id, "implementation", adapterStatus.Implementation, "mode", adapterStatus.Transport.Mode)
			}
			if adapter.Config().Transport.Mode == onebotv11.TransportModeWSReverse {
				s.onebotReverse.RegisterAdapter(id, adapter)
			}
		default:
			return fmt.Errorf("unsupported platform adapter kind %q for %s", adapterCfg.Kind, id)
		}
	}
	return nil
}

func (s *PlatformService) Status() platform.PlatformStatus {
	if s == nil {
		return platform.PlatformStatus{}
	}
	status := platform.PlatformStatus{Enabled: s.enabled}
	if !s.enabled || s.manager == nil {
		return status
	}
	for _, registered := range s.manager.List() {
		reporter, ok := registered.Adapter.(interface {
			Status() platform.AdapterStatus
		})
		if !ok {
			status.Adapters = append(status.Adapters, platform.AdapterStatus{ID: registered.ID, Enabled: true})
			continue
		}
		adapterStatus := reporter.Status()
		if adapterStatus.ID == "" {
			adapterStatus.ID = registered.ID
		}
		status.Adapters = append(status.Adapters, adapterStatus)
	}
	return status
}

func (s *PlatformService) InstallHTTPRoutes(mux *http.ServeMux) {
	if s == nil || mux == nil {
		return
	}
	if s.onebotReverse == nil {
		s.onebotReverse = onebotv11.NewReverseServer()
	}
	mux.HandleFunc("GET /api/platforms/onebot/v11/{adapter_id}/ws", s.onebotReverse.ServeHTTP)
	if s.logger != nil {
		for _, registered := range s.manager.List() {
			adapter, ok := registered.Adapter.(*onebotv11.Adapter)
			if !ok || adapter.Config().Transport.Mode != onebotv11.TransportModeWSReverse {
				continue
			}
			s.logger.Info("onebot reverse route installed", "id", registered.ID, "path", adapter.Config().Transport.ReversePath)
		}
	}
}

func (s *PlatformService) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}
	for _, adapter := range s.adapters {
		if err := adapter.Start(ctx, s.gateway); err != nil {
			stopErr := s.Stop(ctx)
			return errors.Join(err, stopErr)
		}
		s.started = append(s.started, adapter)
	}
	return nil
}

func (s *PlatformService) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	var stopErr error
	for i := len(s.started) - 1; i >= 0; i-- {
		if err := s.started[i].Stop(ctx); err != nil {
			stopErr = errors.Join(stopErr, err)
		}
	}
	s.started = nil
	return stopErr
}
