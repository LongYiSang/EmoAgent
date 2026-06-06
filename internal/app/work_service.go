package app

import (
	"context"
	"time"

	"github.com/longyisang/emoagent/internal/plugin"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/work"
)

type WorkService struct {
	infra        *Infra
	tools        *ToolService
	plugins      *PluginService
	agentRuntime *AgentRuntimeService
	approvals    *work.ApprovalService
	pending      *work.PendingRegistry
}

func (s *WorkService) Approvals() *work.ApprovalService {
	return s.approvals
}

func (s *WorkService) Pending() *work.PendingRegistry {
	return s.pending
}

func (s *WorkService) Configure(ctx context.Context, dispatcher *tool.Dispatcher) error {
	registry := s.tools.Registry()
	activeRuntime := s.agentRuntime.Active()
	if _, ok := registry.GetSpec("delegate_to_work"); ok {
		return nil
	}
	if activeRuntime == nil || activeRuntime.WorkMain.Client == nil {
		s.infra.Logger.Warn("work runtime disabled", "error", "active agent config is not configured")
		return nil
	}

	cfg := s.infra.Config
	s.approvals = work.NewApprovalService(s.infra.DB.SqlDB(), s.infra.Logger)
	s.pending = work.NewPendingRegistry(s.infra.DB.SqlDB(), s.approvals, s.infra.Logger, work.PendingRegistryConfig{
		SoftTTL:        cfg.Work.SoftTTL,
		HardTTL:        cfg.Work.HardTTL,
		ArchiveTTL:     cfg.Work.ArchiveTTL,
		ResumeClaimTTL: cfg.Work.ResumeClaimTTL,
	})
	cleanupInterval := cfg.Work.DeciderCleanupInterval
	if cleanupInterval <= 0 {
		cleanupInterval = 5 * time.Minute
	}
	go s.runCleanupLoop(ctx, cleanupInterval)

	runtimeFactory := func() (*work.Runtime, error) {
		return s.agentRuntime.NewWorkRuntime(dispatcher, registry)
	}
	if _, ok := registry.GetSpec("finish_task"); !ok {
		registry.Register(work.NewFinishTaskTool(), work.FinishTaskPlaceholderHandler)
	}
	if _, ok := registry.GetSpec("request_decision"); !ok {
		registry.Register(work.NewRequestDecisionTool(), work.RequestDecisionPlaceholderHandler)
	}
	if _, ok := registry.GetSpec("resume_work"); !ok {
		resumeSpec, resumeHandler := work.NewResumeToolWithFactory(runtimeFactory, s.pending, cfg.Work.JournalDir, s.infra.Logger)
		registry.Register(resumeSpec, resumeHandler)
	}
	if _, ok := registry.GetSpec("list_pending_decisions"); !ok {
		spec, handler := work.NewListDecisionsTool(s.pending)
		registry.Register(spec, handler)
	}
	delegateSpec, delegateHandler := work.NewDelegateToolWithFactoryAndAnnotator(runtimeFactory, s.pending, cfg.Work.JournalDir, s.infra.Logger, plugin.NewWorkAnnotator(s.plugins.Host()))
	registry.Register(delegateSpec, delegateHandler)
	return nil
}

func (s *WorkService) ListSessionApprovals(sessionID string) []protocol.ApprovalRequest {
	if s.approvals == nil {
		return []protocol.ApprovalRequest{}
	}
	return s.approvals.ListSessionApprovals(sessionID, nil)
}

func (s *WorkService) runCleanupLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n := s.pending.ExpireOnce(); n > 0 {
				s.infra.Logger.Info("expired pending work decisions", "count", n)
			}
			if n := s.pending.ArchiveOnce(); n > 0 {
				s.infra.Logger.Info("archived pending work decisions", "count", n)
			}
		}
	}
}
