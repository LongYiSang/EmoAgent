package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

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

func (s *AgentAffectService) QueueStatus(ctx context.Context, q agentaffect.JobQueueQuery) (agentaffect.QueueStatusResponse, error) {
	return s.buildCoreRuntime(s.config()).QueueStatus(ctx, q)
}

func (s *AgentAffectService) ProcessBatchOnce(ctx context.Context) (agentaffect.ProcessBatchOnceResponse, error) {
	processed, err := s.buildCoreRuntime(s.config()).ProcessNextBatch(ctx, "agent_affect_admin_once")
	if err != nil {
		return agentaffect.ProcessBatchOnceResponse{}, err
	}
	return agentaffect.ProcessBatchOnceResponse{Processed: processed}, nil
}

func (s *AgentAffectService) ClearFailedJobs(ctx context.Context, q agentaffect.JobQueueQuery) (agentaffect.ClearFailedJobsResponse, error) {
	return s.buildCoreRuntime(s.config()).ClearFailedJobs(ctx, q)
}

func (s *AgentAffectService) SupersedePendingJobs(ctx context.Context, q agentaffect.JobQueueQuery) (agentaffect.SupersedePendingJobsResponse, error) {
	return s.buildCoreRuntime(s.config()).SupersedePendingQueue(ctx, q, "admin_supersede_pending")
}

func (s *AgentAffectService) SupersedeAllPending(ctx context.Context, reason string) (int, error) {
	store := s.persistentStore()
	if store == nil {
		return 0, nil
	}
	rt := agentaffect.NewRuntime(agentaffect.RuntimeOptions{
		Config:    s.config(),
		Store:     store,
		Evaluator: agentaffect.DisabledEvaluator{},
		Logger:    s.logger(),
	})
	return rt.SupersedeAllPending(ctx, reason)
}

func (s *AgentAffectService) StartBackground(ctx context.Context) {
	if s == nil {
		return
	}
	cfg := s.config()
	workers := cfg.Async.WorkerConcurrency
	if workers <= 0 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		go s.runBackgroundWorker(ctx, i+1)
	}
}

func (s *AgentAffectService) runBackgroundWorker(ctx context.Context, index int) {
	workerID := fmt.Sprintf("agent_affect_worker_%d", index)
	for {
		cfg := s.config()
		interval := time.Duration(cfg.Async.PollIntervalMS) * time.Millisecond
		if interval <= 0 {
			interval = 800 * time.Millisecond
		}
		if cfg.Enabled && cfg.StorageEnabled && cfg.Async.Enabled && cfg.Async.QueueEnabled && cfg.Async.WorkerEnabled {
			processed, err := s.buildCoreRuntime(cfg).ProcessNextBatch(ctx, workerID)
			if err != nil && s.logger() != nil {
				s.logger().Warn("agent affect background worker failed", "worker_id", workerID, "error", err)
			}
			if processed {
				continue
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
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
	return hookedAgentAffectRuntime{inner: s.buildCoreRuntime(cfg), plugins: s.plugins}
}

func (s *AgentAffectService) buildCoreRuntime(cfg config.AgentAffectConfig) *agentaffect.Runtime {
	return agentaffect.NewRuntime(agentaffect.RuntimeOptions{
		Config:    cfg,
		Store:     s.store(),
		Evaluator: s.evaluator(cfg),
		Logger:    s.logger(),
	})
}

func (s *AgentAffectService) store() agentaffect.Store {
	cfg := s.config()
	if cfg.StorageEnabled && s != nil && s.infra != nil && s.infra.DB != nil {
		return agentaffect.NewSQLiteStore(s.infra.DB.SqlDB())
	}
	return nil
}

func (s *AgentAffectService) persistentStore() agentaffect.Store {
	if s != nil && s.infra != nil && s.infra.DB != nil {
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

func (r hookedAgentAffectRuntime) UpdateMode() string {
	return r.inner.UpdateMode()
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

func (r hookedAgentAffectRuntime) EnqueueTurnEvaluationJob(ctx context.Context, req agentaffect.EnqueueTurnEvaluationJobRequest) (agentaffect.AffectJobRecord, error) {
	return r.inner.EnqueueTurnEvaluationJob(ctx, req)
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
