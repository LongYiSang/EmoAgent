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

func (r *TurnRunner) Shadow(ctx context.Context, env turn.InboundEnvelope) (turn.TurnResult, error) {
	if r == nil || r.runtime == nil {
		return turn.TurnResult{}, errors.New("turn runner is not configured")
	}
	return r.runtime.Shadow(ctx, env)
}
