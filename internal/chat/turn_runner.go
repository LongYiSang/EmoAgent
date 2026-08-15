package chat

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/turn"
)

type TurnStores struct {
	Journal     turn.TurnJournal
	Idempotency turn.IdempotencyStore
}

func NewTurnStores(cfg config.TurnPipelineConfig, db *sql.DB, logger *slog.Logger, timezone string) TurnStores {
	journal, ids := buildTurnRuntimeStores(cfg, db, logger, timezone)
	return TurnStores{Journal: journal, Idempotency: ids}
}

type TurnRunner struct {
	runtime *chatTurnRuntime
}

type TurnPluginHost = turnPluginHost

func NewTurnRunnerWithStores(engine conversationEngine, cfg config.TurnPipelineConfig, stores TurnStores, logger *slog.Logger, pluginHost TurnPluginHost) *TurnRunner {
	return &TurnRunner{
		runtime: newChatTurnRuntimeWithStore(engine, cfg, stores.Journal, stores.Idempotency, logger, pluginHost),
	}
}

func (r *TurnRunner) Execute(ctx context.Context, env turn.InboundEnvelope, persona *config.Persona, sink turn.OutboundSink) (turn.TurnResult, error) {
	if r == nil || r.runtime == nil {
		return turn.TurnResult{}, errors.New("turn runner is not configured")
	}
	return r.runtime.Execute(ctx, env, persona, sink)
}

// SetProactiveConfig updates the proactive settings the pipeline consults
// (currently whether Emotion may decline a proactive turn).
func (r *TurnRunner) SetProactiveConfig(cfg config.ProactiveConfig) {
	if r == nil || r.runtime == nil {
		return
	}
	r.runtime.SetProactiveConfig(cfg)
}

// SetAmbientActivityProvider wires the block that tells Emotion what the user
// has been doing while they were not talking.
func (r *TurnRunner) SetAmbientActivityProvider(provider AmbientActivityProvider) {
	if r == nil || r.runtime == nil {
		return
	}
	r.runtime.SetAmbientActivityProvider(provider)
}

func (r *TurnRunner) Shadow(ctx context.Context, env turn.InboundEnvelope) (turn.TurnResult, error) {
	if r == nil || r.runtime == nil {
		return turn.TurnResult{}, errors.New("turn runner is not configured")
	}
	return r.runtime.Shadow(ctx, env)
}
