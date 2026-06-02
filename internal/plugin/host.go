package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/turn"
)

type PluginHost struct {
	enabled  bool
	config   config.PluginsConfig
	registry *PluginRegistry
	bus      *HookBus
	audit    AuditSink
	logger   *slog.Logger
}

func NewPluginHost(cfg config.PluginsConfig, journal turn.TurnJournal, logger *slog.Logger) *PluginHost {
	if cfg.DefaultTimeoutMS == 0 {
		cfg.DefaultTimeoutMS = 80
	}
	if cfg.MaxTimeoutMS == 0 {
		cfg.MaxTimeoutMS = 1000
	}
	audit := NewTurnJournalAudit(journal)
	bus := NewHookBus(HookBusConfig{
		DefaultTimeout: time.Duration(cfg.DefaultTimeoutMS) * time.Millisecond,
		MaxTimeout:     time.Duration(cfg.MaxTimeoutMS) * time.Millisecond,
	}, audit)
	return &PluginHost{
		enabled:  cfg.Enabled,
		config:   cfg,
		registry: NewPluginRegistry(),
		bus:      bus,
		audit:    audit,
		logger:   logger,
	}
}

func (h *PluginHost) Enabled() bool {
	return h != nil && h.enabled
}

func (h *PluginHost) HookBus() *HookBus {
	if h == nil {
		return nil
	}
	return h.bus
}

func (h *PluginHost) Registry() *PluginRegistry {
	if h == nil {
		return nil
	}
	return h.registry
}

func (h *PluginHost) SetTurnJournal(journal turn.TurnJournal) {
	if h == nil || journal == nil {
		return
	}
	audit := NewTurnJournalAudit(journal)
	h.audit = audit
	if h.bus != nil {
		h.bus.audit = audit
	}
}

func (h *PluginHost) WrapStages(stages []turn.Stage) []turn.Stage {
	if !h.Enabled() || h.bus == nil || len(stages) == 0 {
		return stages
	}
	wrapped := make([]turn.Stage, 0, len(stages))
	for _, stage := range stages {
		wrapped = append(wrapped, h.wrapStage(stage))
	}
	return wrapped
}

func (h *PluginHost) wrapStage(stage turn.Stage) turn.Stage {
	return turn.StageFunc{
		NameValue: stage.Name(),
		RunFunc: func(ctx context.Context, tc *turn.TurnContext) (turn.StageResult, error) {
			before, after := hookNamesForStage(stage.Name())
			if before != "" {
				if _, err := h.dispatchStageHook(ctx, before, tc, stage.Name()); err != nil {
					return pluginStageFailure(err)
				}
			}
			result, err := stage.Run(ctx, tc)
			if err != nil || result.Err != nil {
				h.DispatchTurnError(ctx, turn.TurnResult{TurnID: tc.TurnID, State: result.NextState, Status: result.Status, ErrorKind: result.ErrorKind}, firstNonNil(err, result.Err), tc.Inbound)
				return result, err
			}
			if after != "" {
				if _, hookErr := h.dispatchStageHook(ctx, after, tc, stage.Name()); hookErr != nil {
					return pluginStageFailure(hookErr)
				}
			}
			return result, nil
		},
	}
}

func (h *PluginHost) dispatchStageHook(ctx context.Context, hook HookName, tc *turn.TurnContext, stage turn.StageName) (HookResult, error) {
	hc := NewHookContext(tc, hook, stage)
	hc.Memory = MemoryViewFromDiagnostics(tc.Diagnostics)
	return h.bus.Dispatch(ctx, hook, hc)
}

func pluginStageFailure(err error) (turn.StageResult, error) {
	kind := ErrorKind(err)
	return turn.StageResult{
		NextState: turn.StateFailed,
		Terminal:  true,
		Status:    "failed",
		ErrorKind: kind,
	}, err
}

func hookNamesForStage(stage turn.StageName) (HookName, HookName) {
	switch stage {
	case turn.StageNormalize:
		return HookBeforeIngressNormalize, HookAfterIngressNormalize
	case turn.StageMemoryPrepare:
		return HookBeforeMemoryPrepare, HookAfterMemoryPrepare
	case turn.StageEmotionPrepare:
		return HookBeforeMemoryRetrieve, HookAfterMemoryRetrieve
	case turn.StageMemoryCommit:
		return HookBeforeMemoryCommit, HookAfterMemoryCommit
	default:
		return "", ""
	}
}

func (h *PluginHost) WrapOutboundSink(next turn.OutboundSink) turn.OutboundSink {
	if !h.Enabled() || h.bus == nil || next == nil {
		return next
	}
	return &outboundSink{host: h, next: next}
}

type outboundSink struct {
	host *PluginHost
	next turn.OutboundSink
}

func (s *outboundSink) Emit(ctx context.Context, event turn.OutboundEvent) error {
	hc := HookContext{
		Envelope: HookEnvelope{Hook: HookBeforeOutbound, TurnID: event.TurnID, Stage: turn.StageOutboundCommit},
		Turn:     TurnView{TurnID: event.TurnID},
		Outbound: ptr(OutboundViewFromEvent(event)),
	}
	result, err := s.host.bus.Dispatch(ctx, HookBeforeOutbound, hc)
	if err != nil {
		return err
	}
	event = applyOutboundPatches(event, result.Patches)
	if err := s.next.Emit(ctx, event); err != nil {
		return err
	}
	_, _ = s.host.bus.Dispatch(ctx, HookAfterOutbound, HookContext{
		Envelope: HookEnvelope{Hook: HookAfterOutbound, TurnID: event.TurnID, Stage: turn.StageOutboundCommit},
		Turn:     TurnView{TurnID: event.TurnID},
		Outbound: ptr(OutboundViewFromEvent(event)),
	})
	return nil
}

func (s *outboundSink) Close(ctx context.Context) error {
	closer, ok := s.next.(interface{ Close(context.Context) error })
	if !ok {
		return nil
	}
	return closer.Close(ctx)
}

func applyOutboundPatches(event turn.OutboundEvent, patches []Patch) turn.OutboundEvent {
	for _, patch := range patches {
		switch patch.Type {
		case PatchOutboundAddPayload, PatchOutboundEmitSafeDebug:
			payload, ok := patch.Value.(map[string]any)
			if !ok {
				continue
			}
			if event.Payload == nil {
				event.Payload = map[string]any{}
			}
			plugins, _ := event.Payload["plugins"].(map[string]any)
			if plugins == nil {
				plugins = map[string]any{}
				event.Payload["plugins"] = plugins
			}
			pluginID := strings.TrimSpace(patch.PluginID)
			if pluginID == "" {
				pluginID = "unknown"
			}
			plugins[pluginID] = payload
		case PatchOutboundDecorateText:
			// Text transformation is intentionally disabled in v0.1 because the
			// current engine commits DB/Memory content before an outbound sink can
			// prove canonical_assistant_content consistency.
			continue
		}
	}
	return event
}

func (h *PluginHost) DispatchTurnEnd(ctx context.Context, result turn.TurnResult, env turn.InboundEnvelope) {
	if !h.Enabled() || h.bus == nil {
		return
	}
	_, _ = h.bus.Dispatch(ctx, HookAfterTurnEnd, HookContext{
		Envelope: HookEnvelope{Hook: HookAfterTurnEnd, TurnID: result.TurnID, State: result.State, SessionID: env.SessionID, PersonaKey: env.PersonaKey},
		Turn:     TurnView{TurnID: result.TurnID, State: result.State, SessionID: env.SessionID, PersonaKey: env.PersonaKey, RequestID: env.RequestID},
	})
}

func (h *PluginHost) DispatchTurnError(ctx context.Context, result turn.TurnResult, err error, env turn.InboundEnvelope) {
	if !h.Enabled() || h.bus == nil {
		return
	}
	_, _ = h.bus.Dispatch(ctx, HookOnTurnError, HookContext{
		Envelope: HookEnvelope{Hook: HookOnTurnError, TurnID: result.TurnID, State: result.State, SessionID: env.SessionID, PersonaKey: env.PersonaKey},
		Turn:     TurnView{TurnID: result.TurnID, State: result.State, SessionID: env.SessionID, PersonaKey: env.PersonaKey, RequestID: env.RequestID},
		Config:   map[string]any{"error": fmt.Sprint(err)},
	})
}

func firstNonNil(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func ptr[T any](value T) *T {
	return &value
}
