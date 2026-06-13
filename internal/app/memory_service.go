package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/configcenter"
	"github.com/longyisang/emoagent/internal/memoryhost"
	"github.com/longyisang/emoagent/internal/memoryruntime"
	sidecarruntime "github.com/longyisang/emoagent/internal/sidecar"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/web"
)

type MemoryService struct {
	infra       *Infra
	config      *ConfigService
	sidecar     *SidecarService
	host        *memoryhost.Host
	natural     *memoryhost.NaturalMemoryRunner
	manualRules *memoryhost.ManualRules
}

func (s *MemoryService) Host() *memoryhost.Host {
	return s.host
}

func (s *MemoryService) ManualRules() *memoryhost.ManualRules {
	return s.manualRules
}

func (s *MemoryService) NaturalRunner() *memoryhost.NaturalMemoryRunner {
	return s.natural
}

func (s *MemoryService) Open(ctx context.Context) error {
	cfg := s.infra.Config
	if !cfg.Memory.Enabled {
		return nil
	}
	manualRules, err := memoryhost.LoadManualRules(cfg.Memory.ManualRulesPath)
	if err != nil {
		return fmt.Errorf("load memory manual rules: %w", err)
	}
	var sidecarStatus *sidecarruntime.Status
	sidecarSpec, sidecarIssues, err := s.config.BuildSidecarSpec(ctx)
	if err != nil {
		return fmt.Errorf("build sidecar spec: %w", err)
	}
	for _, issue := range sidecarIssues {
		if issue.Severity == "error" {
			return fmt.Errorf("build sidecar spec: %s: %s", issue.Path, issue.Message)
		}
		s.infra.Logger.Warn("sidecar config issue", "path", issue.Path, "message", issue.Message)
	}
	if sidecarSpec.Enabled {
		supervisor := sidecarruntime.NewSupervisor(sidecarSpec, s.infra.Logger)
		status, err := supervisor.Start(ctx)
		if err != nil {
			return fmt.Errorf("start sidecar: %w", err)
		}
		s.sidecar.SetSupervisor(supervisor)
		sidecarStatus = &status
	}
	memoryOpen, err := s.config.BuildMemoryCoreOpenConfig(ctx, sidecarStatus)
	if err != nil {
		return fmt.Errorf("build memorycore config: %w", err)
	}
	memoryHost, err := memoryhost.OpenFromConfigWithOptions(ctx, memoryhost.OpenConfigOptions{
		ConfigPath:       memoryOpen.ConfigPath,
		Overrides:        memoryOpen.Overrides,
		ProviderRegistry: memoryOpen.ProviderRegistry,
		Runtime:          memoryOpen.Runtime,
		NaturalMemory:    memoryOpen.NaturalMemory,
		Logger:           s.infra.Logger,
	})
	if err != nil {
		return fmt.Errorf("open memorycore: %w", err)
	}
	memoryHost.ConfigureExtractionPolicy(memoryExtractionHostConfig(memoryOpen.Memory.Extraction))
	s.host = memoryHost
	s.manualRules = manualRules
	s.writeRuntimeSnapshot(memoryOpen.Memory, memoryOpen.MemoryCore, sidecarStatus)
	return nil
}

func (s *MemoryService) StartBackground(ctx context.Context) {
	startMemoryExtractionBackground(ctx, s.host, s.infra.DB, s.infra.Logger, s.infra.Config.Memory.Extraction)
	s.natural = startNaturalMemoryBackground(ctx, s.host, s.infra.Logger, s.infra.Config.Memory.NaturalMemory)
}

func (s *MemoryService) Bridge() *memoryhost.Bridge {
	return memoryhost.NewBridge(s.host, s.infra.DB, s.infra.Logger, s.manualRules, memoryRetrievalPolicy(s.infra.Config.Memory.Retrieval))
}

func (s *MemoryService) writeRuntimeSnapshot(memory config.MemoryConfig, memoryCore *configcenter.MemoryCoreEffective, sidecarStatus *sidecarruntime.Status) {
	snapshot := memoryruntime.BuildSnapshot(memoryruntime.Input{
		Memory:        memory,
		MemoryCore:    memoryCore,
		SidecarStatus: sidecarStatus,
	})
	if err := memoryruntime.WriteSnapshot(memoryruntime.DefaultSnapshotPath, snapshot); err != nil && s.infra.Logger != nil {
		s.infra.Logger.Warn("write memory runtime snapshot failed", "path", memoryruntime.DefaultSnapshotPath, "error", err)
	}
}

func (s *MemoryService) QueueExtraction(ctx context.Context, req web.MemoryExtractionRequest) (web.MemoryExtractionQueueResponse, error) {
	if s.infra.DB == nil {
		return web.MemoryExtractionQueueResponse{}, fmt.Errorf("database is not configured")
	}
	scope := strings.TrimSpace(req.Scope)
	switch scope {
	case "", "session", "segment", "eligible", "all":
	default:
		return web.MemoryExtractionQueueResponse{}, fmt.Errorf("scope must be session, segment, eligible, or all")
	}
	if scope == "session" && strings.TrimSpace(req.SessionID) == "" {
		return web.MemoryExtractionQueueResponse{}, fmt.Errorf("session_id is required for session scope")
	}
	if scope == "segment" && strings.TrimSpace(req.SegmentID) == "" {
		return web.MemoryExtractionQueueResponse{}, fmt.Errorf("segment_id is required for segment scope")
	}
	if s.infra.Config != nil {
		extraction := s.infra.Config.Memory.Extraction
		if !extraction.Enabled {
			return web.MemoryExtractionQueueResponse{}, fmt.Errorf("memory extraction is disabled")
		}
		if !extraction.Async.Enabled {
			return web.MemoryExtractionQueueResponse{}, fmt.Errorf("memory extraction async queue is disabled")
		}
		if !extraction.Async.WorkerEnabled {
			return web.MemoryExtractionQueueResponse{}, fmt.Errorf("memory extraction worker is disabled")
		}
		manual := extraction.Manual
		if !manual.Enabled {
			return web.MemoryExtractionQueueResponse{}, fmt.Errorf("memory extraction manual trigger is disabled")
		}
		if req.Force && !manual.AllowForce {
			return web.MemoryExtractionQueueResponse{}, fmt.Errorf("memory extraction force is disabled")
		}
		if strings.TrimSpace(req.SegmentID) != "" && !manual.AllowSegmentSelection {
			return web.MemoryExtractionQueueResponse{}, fmt.Errorf("memory extraction segment selection is disabled")
		}
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" && s.infra.Config != nil {
		mode = s.infra.Config.Memory.Extraction.Manual.Mode
	}
	if mode == "" {
		mode = "apply"
	}
	switch normalizeAppMemoryExtractionMode(mode) {
	case "validate", "dry-run", "apply":
	default:
		return web.MemoryExtractionQueueResponse{}, fmt.Errorf("mode must be validate, dry_run, or apply")
	}
	mode = string(memoryExtractionMode(mode))

	var segments []storage.MemorySegment
	if strings.TrimSpace(req.SegmentID) != "" {
		segment, err := s.infra.DB.GetMemorySegment(ctx, req.SegmentID)
		if err != nil {
			return web.MemoryExtractionQueueResponse{}, err
		}
		if segment == nil {
			return web.MemoryExtractionQueueResponse{}, fmt.Errorf("segment_id not found")
		}
		segments = []storage.MemorySegment{*segment}
	} else if strings.TrimSpace(req.SessionID) != "" {
		session, err := s.infra.DB.GetSession(ctx, req.SessionID)
		if err != nil {
			return web.MemoryExtractionQueueResponse{}, err
		}
		if session == nil {
			return web.MemoryExtractionQueueResponse{}, ErrSessionNotFound
		}
		list, err := s.infra.DB.ListMemorySegments(ctx, storage.ListMemorySegmentsFilter{ChatSessionID: req.SessionID, Limit: 100})
		if err != nil {
			return web.MemoryExtractionQueueResponse{}, err
		}
		segments = list
	} else {
		idleAfter := 15 * time.Minute
		limit := 20
		minEpisodes := 1
		includeActive := true
		includeFinalized := true
		if s.infra.Config != nil {
			idleAfter = time.Duration(s.infra.Config.Memory.Extraction.Idle.IdleAfterSeconds) * time.Second
			limit = s.infra.Config.Memory.Extraction.Idle.MaxSegmentsPerSweep
			minEpisodes = s.infra.Config.Memory.Extraction.Idle.MinEpisodeCount
			includeActive = s.infra.Config.Memory.Extraction.Idle.IncludeActiveSegments
			includeFinalized = s.infra.Config.Memory.Extraction.Idle.IncludeFinalizedSegments
		}
		list, err := s.infra.DB.ScanEligibleMemorySegments(ctx, storage.ScanEligibleMemorySegmentsParams{
			Now:                      time.Now().UTC(),
			IdleAfter:                idleAfter,
			IncludeActiveSegments:    includeActive,
			IncludeFinalizedSegments: includeFinalized,
			MinEpisodeCount:          minEpisodes,
			Limit:                    limit,
		})
		if err != nil {
			return web.MemoryExtractionQueueResponse{}, err
		}
		segments = list
	}

	resp := web.MemoryExtractionQueueResponse{Status: "queued"}
	for _, segment := range segments {
		if strings.TrimSpace(segment.LastUserEpisodeID) == "" && strings.TrimSpace(segment.LastAssistantEpisodeID) == "" {
			resp.SkippedCount++
			continue
		}
		if !req.Force && !manualExtractionEligible(segment.ExtractionStatus) {
			resp.SkippedCount++
			continue
		}
		personaID := strings.TrimSpace(req.PersonaID)
		if personaID == "" {
			personaID = s.memorySegmentPersona(ctx, segment.ChatSessionID)
		}
		trigger := storage.MemoryExtractionTriggerManualScan
		if strings.TrimSpace(req.SegmentID) != "" {
			trigger = storage.MemoryExtractionTriggerManualSegmentScan
		}
		maxAttempts := 3
		if s.infra.Config != nil && s.infra.Config.Memory.Extraction.Async.MaxAttempts > 0 {
			maxAttempts = s.infra.Config.Memory.Extraction.Async.MaxAttempts
		}
		job, enqueued, err := s.infra.DB.EnqueueMemoryExtractionJob(ctx, storage.EnqueueMemoryExtractionJobParams{
			PersonaID:       personaID,
			ChatSessionID:   segment.ChatSessionID,
			SegmentID:       segment.ID,
			MemorySessionID: segment.MemorySessionID,
			Trigger:         trigger,
			Scope:           storage.MemoryExtractionScopeSegment,
			Mode:            mode,
			RequestedBy:     "user",
			Priority:        20,
			Force:           req.Force,
			UntilAt:         segment.LastActivityAt,
			EpisodeLimit:    extractionLimit(s.infra.Config),
			MaxAttempts:     maxAttempts,
			RunAfter:        time.Now().UTC(),
		})
		if err != nil {
			return resp, err
		}
		if enqueued {
			resp.EnqueuedCount++
		} else {
			resp.SkippedCount++
		}
		if job != nil {
			resp.Jobs = append(resp.Jobs, *job)
		}
	}
	return resp, nil
}

func (s *MemoryService) ListExtractions(ctx context.Context, req web.MemoryExtractionListRequest) ([]storage.MemoryExtractionJob, error) {
	if s.infra.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	return s.infra.DB.ListMemoryExtractionJobs(ctx, storage.ListMemoryExtractionJobsFilter{
		ChatSessionID: req.SessionID,
		SegmentID:     req.SegmentID,
		Status:        req.Status,
		Limit:         req.Limit,
	})
}

func (s *MemoryService) ListSegments(ctx context.Context, sessionID string) ([]storage.MemorySegment, error) {
	if s.infra.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	return s.infra.DB.ListMemorySegments(ctx, storage.ListMemorySegmentsFilter{ChatSessionID: sessionID, Limit: 100})
}

func (s *MemoryService) RunNatural(ctx context.Context, req web.NaturalMemoryRunRequest) (memoryhost.NaturalMemoryRunResponse, error) {
	runner, err := s.ensureNaturalMemoryRunner()
	if err != nil {
		return memoryhost.NaturalMemoryRunResponse{}, err
	}
	resp, err := runner.RunManual(ctx, memoryhost.NaturalMemoryManualRunRequest{
		PersonaID:      req.PersonaID,
		Mode:           req.Mode,
		DryRun:         req.DryRun,
		Force:          req.Force,
		Explain:        req.Explain,
		LocalDate:      req.LocalDate,
		LocalTime:      req.LocalTime,
		Timezone:       req.Timezone,
		MarkSleepCycle: req.MarkSleepCycle,
	})
	if resp == nil {
		return memoryhost.NaturalMemoryRunResponse{}, err
	}
	return *resp, err
}

func (s *MemoryService) LatestNatural(context.Context) (*memoryhost.NaturalMemoryRunResponse, error) {
	runner, err := s.ensureNaturalMemoryRunner()
	if err != nil {
		return nil, err
	}
	return runner.Latest(), nil
}

func (s *MemoryService) ensureNaturalMemoryRunner() (*memoryhost.NaturalMemoryRunner, error) {
	if s == nil || s.infra.Config == nil || !s.infra.Config.Memory.Enabled || !s.infra.Config.Memory.NaturalMemory.Enabled {
		return nil, fmt.Errorf("natural memory is disabled")
	}
	if s.host == nil || s.host.Core == nil {
		return nil, fmt.Errorf("memorycore is not configured")
	}
	if s.natural == nil {
		s.natural = memoryhost.NewNaturalMemoryRunner(s.host, memoryNaturalMemoryRunnerConfig(s.infra.Config.Memory.NaturalMemory), s.infra.Logger)
	}
	return s.natural, nil
}

func (s *MemoryService) memorySegmentPersona(ctx context.Context, chatSessionID string) string {
	if s.infra.DB == nil {
		return "default"
	}
	link, err := s.infra.DB.GetMemoryChatLink(ctx, chatSessionID)
	if err != nil || link == nil || strings.TrimSpace(link.PersonaID) == "" {
		return "default"
	}
	return link.PersonaID
}

func (s *MemoryService) Close(ctx context.Context) error {
	var closeErr error
	if s.natural != nil {
		if err := s.natural.Stop(ctx); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("stop natural memory runner: %w", err))
		}
		s.natural = nil
	}
	if s.host != nil {
		if err := s.host.Close(); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close memorycore: %w", err))
		} else {
			s.host = nil
		}
	}
	s.manualRules = nil
	return closeErr
}

func memoryRetrievalPolicy(cfg config.MemoryRetrievalConfig) memorycore.RetrievalPolicy {
	return memoryruntime.ChatPromptRetrievalPolicy(cfg)
}

func manualExtractionEligible(status string) bool {
	switch strings.TrimSpace(status) {
	case "", storage.MemorySegmentExtractionStatusNever, storage.MemorySegmentExtractionStatusStale, storage.MemorySegmentExtractionStatusFailed, storage.MemorySegmentExtractionStatusSkipped:
		return true
	default:
		return false
	}
}

func extractionLimit(cfg *config.Config) int {
	if cfg != nil && cfg.Memory.Extraction.Limit > 0 {
		return cfg.Memory.Extraction.Limit
	}
	return 50
}

func normalizeAppMemoryExtractionMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "dry_run":
		return "dry-run"
	default:
		return strings.TrimSpace(mode)
	}
}
