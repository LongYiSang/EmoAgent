package app

import (
	"context"
	"errors"
	"fmt"
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
}

func NewPlatformService(infra *Infra, conversation *ConversationService, commands *CommandService, chat *ChatService, personas *PersonaService) *PlatformService {
	var receipts platform.ReceiptStore
	if infra != nil && infra.DB != nil {
		receipts = NewStorageReceiptStore(infra.DB)
	}
	return &PlatformService{
		gateway: NewPlatformGateway(infra, conversation, commands, chat, personas, receipts),
		manager: platform.NewManager(),
	}
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
	s.onebotReverse = nil
	if !cfg.Enabled {
		return nil
	}
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
			s.manager.Register(id, adapter)
			s.adapters = append(s.adapters, adapter)
			if adapter.Config().Transport.Mode == onebotv11.TransportModeWSReverse {
				if s.onebotReverse == nil {
					s.onebotReverse = onebotv11.NewReverseServer()
				}
				s.onebotReverse.RegisterAdapter(id, adapter)
			}
		default:
			return fmt.Errorf("unsupported platform adapter kind %q for %s", adapterCfg.Kind, id)
		}
	}
	return nil
}

func (s *PlatformService) InstallHTTPRoutes(mux *http.ServeMux) {
	if s == nil || mux == nil || s.onebotReverse == nil {
		return
	}
	mux.HandleFunc("GET /api/platforms/onebot/v11/{adapter_id}/ws", s.onebotReverse.ServeHTTP)
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
