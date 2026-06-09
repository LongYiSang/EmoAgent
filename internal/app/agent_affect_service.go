package app

import (
	"context"
	"log/slog"

	"github.com/longyisang/emoagent/internal/agentaffect"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/plugin"
	"github.com/longyisang/emoagent/internal/turn"
)

type AgentAffectService struct {
	infra        *Infra
	agentRuntime *AgentRuntimeService
	plugins      *PluginService
}

func (s *AgentAffectService) Runtime() agentaffect.Service {
	cfg := s.config()
	if !cfg.Enabled {
		return nil
	}
	return s.buildRuntime(cfg)
}

func (s *AgentAffectService) runtimeForDebug() agentaffect.Service {
	return s.buildRuntime(s.config())
}

func (s *AgentAffectService) GetCurrentMood(ctx context.Context, req agentaffect.GetCurrentMoodRequest) (agentaffect.GetCurrentMoodResponse, error) {
	return s.runtimeForDebug().GetCurrentMood(ctx, req)
}

func (s *AgentAffectService) GetProfile(ctx context.Context, personaID string) (agentaffect.AffectProfile, error) {
	return s.runtimeForDebug().GetProfile(ctx, personaID)
}

func (s *AgentAffectService) UpdateProfile(ctx context.Context, profile agentaffect.AffectProfile) (agentaffect.AffectProfile, error) {
	return s.runtimeForDebug().UpdateProfile(ctx, profile)
}

func (s *AgentAffectService) ListHistory(ctx context.Context, q agentaffect.HistoryQuery) (agentaffect.HistoryResponse, error) {
	return s.runtimeForDebug().ListHistory(ctx, q)
}

func (s *AgentAffectService) ListPluginWrites(ctx context.Context, q agentaffect.PluginWritesQuery) ([]agentaffect.PluginWriteRecord, error) {
	return s.runtimeForDebug().ListPluginWrites(ctx, q)
}

func (s *AgentAffectService) EvaluateMoodImpact(ctx context.Context, req agentaffect.EvaluateMoodImpactRequest) (agentaffect.EvaluateMoodImpactResponse, error) {
	return s.runtimeForDebug().EvaluateMoodImpact(ctx, req)
}

func (s *AgentAffectService) SubmitMoodImpact(ctx context.Context, req agentaffect.SubmitMoodImpactRequest) (agentaffect.SubmitMoodImpactResponse, error) {
	return s.runtimeForDebug().SubmitMoodImpact(ctx, req)
}

func (s *AgentAffectService) ApplyMoodDelta(ctx context.Context, req agentaffect.ApplyMoodDeltaRequest) (agentaffect.ApplyMoodDeltaResponse, error) {
	return s.runtimeForDebug().ApplyMoodDelta(ctx, req)
}

func (s *AgentAffectService) ResetMood(ctx context.Context, req agentaffect.ResetMoodRequest) (agentaffect.ResetMoodResponse, error) {
	return s.runtimeForDebug().ResetMood(ctx, req)
}

func (s *AgentAffectService) PreviewPrompt(ctx context.Context, req agentaffect.BuildPromptAffectBlockRequest) (agentaffect.PromptPreviewResponse, error) {
	return s.runtimeForDebug().PreviewPrompt(ctx, req)
}

func (s *AgentAffectService) PluginAPI() agentaffect.PluginAPI {
	return agentaffect.NewPluginAPI(s.runtimeForDebug(), s.store())
}

func (s *AgentAffectService) config() config.AgentAffectConfig {
	if s == nil || s.infra == nil || s.infra.Config == nil {
		return config.DefaultConfig().AgentAffect
	}
	return s.infra.Config.AgentAffect
}

func (s *AgentAffectService) buildRuntime(cfg config.AgentAffectConfig) agentaffect.Service {
	runtime := agentaffect.NewRuntime(agentaffect.RuntimeOptions{
		Config:    cfg,
		Store:     s.store(),
		Evaluator: s.evaluator(cfg),
		Logger:    s.logger(),
	})
	return hookedAgentAffectRuntime{inner: runtime, plugins: s.plugins}
}

func (s *AgentAffectService) store() agentaffect.Store {
	cfg := s.config()
	if cfg.StorageEnabled && s != nil && s.infra != nil && s.infra.DB != nil {
		return agentaffect.NewSQLiteStore(s.infra.DB.SqlDB())
	}
	return nil
}

func (s *AgentAffectService) evaluator(cfg config.AgentAffectConfig) agentaffect.Evaluator {
	if cfg.Evaluator.Mode == "disabled" {
		return agentaffect.DisabledEvaluator{}
	}
	return agentaffect.NewLLMEvaluator(s.evaluatorClient(), s.withResolvedModel(cfg))
}

func (s *AgentAffectService) withResolvedModel(cfg config.AgentAffectConfig) config.AgentAffectConfig {
	if cfg.Evaluator.Model != "" {
		return cfg
	}
	if s != nil && s.agentRuntime != nil {
		if active := s.agentRuntime.Active(); active != nil {
			cfg.Evaluator.Model = active.EmotionSummary.Model
		}
	}
	return cfg
}

func (s *AgentAffectService) evaluatorClient() llm.Client {
	if s != nil && s.agentRuntime != nil {
		if active := s.agentRuntime.Active(); active != nil && active.EmotionSummary.Client != nil {
			return active.EmotionSummary.Client
		}
	}
	if s != nil && s.infra != nil {
		return s.infra.LLM
	}
	return nil
}

func (s *AgentAffectService) logger() *slog.Logger {
	if s != nil && s.infra != nil && s.infra.Logger != nil {
		return s.infra.Logger
	}
	return nil
}

type hookedAgentAffectRuntime struct {
	inner   agentaffect.Service
	plugins *PluginService
}

func (r hookedAgentAffectRuntime) GetCurrentMood(ctx context.Context, req agentaffect.GetCurrentMoodRequest) (agentaffect.GetCurrentMoodResponse, error) {
	if err := r.dispatch(ctx, plugin.HookAgentAffectGetState, req.PersonaID, req.SessionID, "", nil); err != nil {
		return agentaffect.GetCurrentMoodResponse{}, err
	}
	resp, err := r.inner.GetCurrentMood(ctx, req)
	if err != nil {
		return resp, err
	}
	return resp, nil
}

func (r hookedAgentAffectRuntime) GetProfile(ctx context.Context, personaID string) (agentaffect.AffectProfile, error) {
	return r.inner.GetProfile(ctx, personaID)
}

func (r hookedAgentAffectRuntime) UpdateProfile(ctx context.Context, profile agentaffect.AffectProfile) (agentaffect.AffectProfile, error) {
	return r.inner.UpdateProfile(ctx, profile)
}

func (r hookedAgentAffectRuntime) ListHistory(ctx context.Context, q agentaffect.HistoryQuery) (agentaffect.HistoryResponse, error) {
	return r.inner.ListHistory(ctx, q)
}

func (r hookedAgentAffectRuntime) ListPluginWrites(ctx context.Context, q agentaffect.PluginWritesQuery) ([]agentaffect.PluginWriteRecord, error) {
	return r.inner.ListPluginWrites(ctx, q)
}

func (r hookedAgentAffectRuntime) EvaluateMoodImpact(ctx context.Context, req agentaffect.EvaluateMoodImpactRequest) (agentaffect.EvaluateMoodImpactResponse, error) {
	if err := r.dispatch(ctx, plugin.HookBeforeAgentAffectEvaluate, req.PersonaID, req.SessionID, req.TurnID, req); err != nil {
		return agentaffect.EvaluateMoodImpactResponse{}, err
	}
	resp, err := r.inner.EvaluateMoodImpact(ctx, req)
	if err != nil {
		return resp, err
	}
	if err := r.dispatch(ctx, plugin.HookAfterAgentAffectEvaluate, req.PersonaID, req.SessionID, req.TurnID, resp); err != nil {
		return agentaffect.EvaluateMoodImpactResponse{}, err
	}
	return resp, nil
}

func (r hookedAgentAffectRuntime) SubmitMoodImpact(ctx context.Context, req agentaffect.SubmitMoodImpactRequest) (agentaffect.SubmitMoodImpactResponse, error) {
	if err := r.dispatch(ctx, plugin.HookBeforeAgentAffectEvaluate, req.PersonaID, req.SessionID, req.TurnID, req); err != nil {
		return agentaffect.SubmitMoodImpactResponse{}, err
	}
	if req.CommitMode == "" || req.CommitMode == agentaffect.CommitModeCommitIfAllowed {
		if err := r.dispatch(ctx, plugin.HookBeforeAgentAffectCommit, req.PersonaID, req.SessionID, req.TurnID, req); err != nil {
			return agentaffect.SubmitMoodImpactResponse{}, err
		}
	}
	resp, err := r.inner.SubmitMoodImpact(ctx, req)
	if err != nil {
		return resp, err
	}
	if err := r.dispatch(ctx, plugin.HookAfterAgentAffectEvaluate, req.PersonaID, req.SessionID, req.TurnID, resp); err != nil {
		return agentaffect.SubmitMoodImpactResponse{}, err
	}
	if resp.EventID != "" {
		if err := r.dispatch(ctx, plugin.HookAfterAgentAffectCommit, req.PersonaID, req.SessionID, req.TurnID, resp); err != nil {
			return agentaffect.SubmitMoodImpactResponse{}, err
		}
	}
	return resp, nil
}

func (r hookedAgentAffectRuntime) ApplyMoodDelta(ctx context.Context, req agentaffect.ApplyMoodDeltaRequest) (agentaffect.ApplyMoodDeltaResponse, error) {
	if err := r.dispatch(ctx, plugin.HookBeforeAgentAffectCommit, req.PersonaID, req.SessionID, req.TurnID, req); err != nil {
		return agentaffect.ApplyMoodDeltaResponse{}, err
	}
	resp, err := r.inner.ApplyMoodDelta(ctx, req)
	if err != nil {
		return resp, err
	}
	if err := r.dispatch(ctx, plugin.HookAfterAgentAffectCommit, req.PersonaID, req.SessionID, req.TurnID, resp); err != nil {
		return agentaffect.ApplyMoodDeltaResponse{}, err
	}
	return resp, nil
}

func (r hookedAgentAffectRuntime) ResetMood(ctx context.Context, req agentaffect.ResetMoodRequest) (agentaffect.ResetMoodResponse, error) {
	return r.inner.ResetMood(ctx, req)
}

func (r hookedAgentAffectRuntime) BuildPromptAffectBlock(ctx context.Context, req agentaffect.BuildPromptAffectBlockRequest) (string, error) {
	return r.inner.BuildPromptAffectBlock(ctx, req)
}

func (r hookedAgentAffectRuntime) PreviewPrompt(ctx context.Context, req agentaffect.BuildPromptAffectBlockRequest) (agentaffect.PromptPreviewResponse, error) {
	return r.inner.PreviewPrompt(ctx, req)
}

func (r hookedAgentAffectRuntime) dispatch(ctx context.Context, hook plugin.HookName, personaID string, sessionID string, turnID string, payload any) error {
	if r.plugins == nil || r.plugins.Host() == nil || !r.plugins.Host().Enabled() {
		return nil
	}
	_, err := r.plugins.Host().HookBus().Dispatch(ctx, hook, plugin.HookContext{
		Envelope: plugin.HookEnvelope{
			Hook:       hook,
			TurnID:     turnID,
			Stage:      turn.StageEmotionPrepare,
			SessionID:  sessionID,
			PersonaKey: personaID,
		},
		Config: map[string]any{"agent_affect": payload},
	})
	return err
}
