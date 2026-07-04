package onebotv11

import (
	"context"
	"fmt"

	appconfig "github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/platform"
)

type Adapter struct {
	id        string
	cfg       Config
	profile   Profile
	transport Transport
	handler   platform.InboundHandler
	sink      *Sink
}

func NewAdapter(adapterID string, adapterConfig appconfig.PlatformAdapterConfig) (*Adapter, error) {
	cfg, err := ParseConfig(adapterID, adapterConfig)
	if err != nil {
		return nil, err
	}
	return NewAdapterWithTransport(adapterID, cfg, nil)
}

func NewAdapterWithTransport(adapterID string, cfg Config, transport Transport) (*Adapter, error) {
	if cfg.AdapterID == "" {
		cfg.AdapterID = adapterID
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	profile, err := SelectProfile(cfg.Implementation)
	if err != nil {
		return nil, err
	}
	if transport == nil {
		switch cfg.Transport.Mode {
		case TransportModeWSClient:
			transport = NewWSClientTransport(cfg.Transport)
		case TransportModeWSReverse:
			transport = NewReverseTransport(cfg.Transport)
		default:
			return nil, fmt.Errorf("unsupported onebot transport mode %q", cfg.Transport.Mode)
		}
	}
	return &Adapter{
		id:        cfg.AdapterID,
		cfg:       cfg,
		profile:   profile,
		transport: transport,
	}, nil
}

func (a *Adapter) Start(ctx context.Context, handler platform.InboundHandler) error {
	if a == nil {
		return nil
	}
	a.handler = handler
	a.sink = NewSink(a.transport, a.profile, a.cfg.Outbound)
	return a.transport.Start(ctx, a)
}

func (a *Adapter) Stop(ctx context.Context) error {
	if a == nil || a.transport == nil {
		return nil
	}
	return a.transport.Stop(ctx)
}

func (a *Adapter) HandleOneBotEvent(ctx context.Context, event Event) error {
	_, err := a.HandleEvent(ctx, event)
	return err
}

func (a *Adapter) HandleEvent(ctx context.Context, event Event) (platform.HandleResult, error) {
	if a == nil || a.handler == nil {
		return platform.HandleResult{}, fmt.Errorf("onebot adapter is not started")
	}
	inbound, accepted, err := MapEvent(event, a.cfg, a.profile)
	if err != nil {
		return platform.HandleResult{}, err
	}
	if !accepted {
		return platform.HandleResult{Ignored: true}, nil
	}
	return a.handler.HandleInbound(ctx, inbound, a.sink)
}

func (a *Adapter) ReverseTransport() (*ReverseTransport, bool) {
	if a == nil {
		return nil, false
	}
	transport, ok := a.transport.(*ReverseTransport)
	return transport, ok
}

func (a *Adapter) Config() Config {
	if a == nil {
		return Config{}
	}
	return a.cfg
}
