package onebotv11

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

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
	logger    *slog.Logger
	media     InboundMediaStore
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
	a.sink.SetLogger(a.logger)
	return a.transport.Start(ctx, a)
}

// OutboundSink exposes the adapter's long-lived sink so the host can deliver
// messages that no inbound event asked for (proactive messages). The sink is
// addressed purely by event.Origin, so it needs no inbound context — see
// requestForEvent. Returns false before Start has run.
func (a *Adapter) OutboundSink() (platform.OutboundSink, bool) {
	if a == nil || a.sink == nil {
		return nil, false
	}
	return a.sink, true
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
		if a.logger != nil {
			a.logger.Debug("onebot inbound ignored", "id", a.id, "reason", ignoredReason(event, a.cfg))
		}
		return platform.HandleResult{Ignored: true}, nil
	}
	if a.logger != nil {
		a.logger.Info("onebot inbound accepted", "id", a.id, "message_type", event.MessageType, "external_message_id", inbound.ExternalMessageID)
	}
	if a.cfg.Message.InboundMedia.Enabled {
		parts, text, err := resolveInboundMedia(ctx, event.Message, a.cfg.Message, a.media)
		if err != nil {
			if notifyErr := a.notifyInboundMediaFailure(ctx, inbound, err); notifyErr != nil {
				return platform.HandleResult{}, notifyErr
			}
			return platform.HandleResult{Ignored: true}, nil
		}
		if len(parts) > 0 {
			inbound.Parts = parts
			inbound.Text = text
		}
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

func (a *Adapter) SetLogger(logger *slog.Logger) {
	if a == nil {
		return
	}
	a.logger = logger
	if a.sink != nil {
		a.sink.SetLogger(logger)
	}
}

func (a *Adapter) SetInboundMediaStore(store InboundMediaStore) {
	if a == nil {
		return
	}
	a.media = store
}

func (a *Adapter) notifyInboundMediaFailure(ctx context.Context, inbound platform.InboundMessage, err error) error {
	if a == nil || a.sink == nil || a.cfg.Message.InboundMedia.OnFailure != InboundMediaFailureNotify {
		return nil
	}
	origin, originErr := platform.OriginFromInbound(inbound, inbound.OriginScope)
	if originErr != nil {
		return originErr
	}
	return a.sink.Emit(ctx, platform.OutboundEvent{
		Type:                     "error",
		Origin:                   origin,
		Content:                  "图片接收失败：" + strings.TrimSpace(err.Error()),
		Status:                   "failed",
		ErrorKind:                "media_input_failed",
		ReplyToExternalMessageID: inbound.ExternalMessageID,
	})
}

func (a *Adapter) Status() platform.AdapterStatus {
	if a == nil {
		return platform.AdapterStatus{}
	}
	transport := platform.TransportStatus{
		Mode: a.cfg.Transport.Mode,
		URL:  a.cfg.Transport.URL,
	}
	if a.transport != nil {
		status := a.transport.Status()
		transport = platform.TransportStatus{
			Mode:      firstNonEmpty(status.Mode, a.cfg.Transport.Mode),
			State:     status.State,
			URL:       firstNonEmpty(status.URL, a.cfg.Transport.URL),
			SelfID:    status.SelfID,
			Connected: status.Connected,
		}
	}
	return platform.AdapterStatus{
		ID:             a.cfg.AdapterID,
		Kind:           KindOneBotV11,
		Enabled:        true,
		Implementation: a.cfg.Implementation,
		SourceType:     a.cfg.SourceType,
		PlatformID:     a.cfg.PlatformID,
		InstanceID:     a.cfg.InstanceID,
		Transport:      transport,
		Routing: platform.RoutingStatus{
			PrivateEnabled:     a.cfg.Routing.PrivateEnabled,
			GroupEnabled:       a.cfg.Routing.GroupEnabled,
			IgnoreSelfMessages: a.cfg.Routing.IgnoreSelfMessages,
		},
		Auth: platform.AuthStatus{
			AccessTokenConfigured: strings.TrimSpace(a.cfg.Transport.AccessToken) != "" || strings.TrimSpace(a.cfg.Transport.AccessTokenEnv) != "",
		},
	}
}

func ignoredReason(event Event, cfg Config) string {
	if event.PostType != "message" {
		return "post_type_" + event.PostType
	}
	if cfg.Routing.IgnoreSelfMessages && event.SelfID != 0 && event.UserID == event.SelfID {
		return "self_message"
	}
	switch event.MessageType {
	case "private":
		if !cfg.Routing.PrivateEnabled {
			return "private_disabled"
		}
	case "group":
		return "group_disabled"
	case "":
		return "missing_message_type"
	default:
		return "unsupported_message_type"
	}
	return "empty_message"
}
