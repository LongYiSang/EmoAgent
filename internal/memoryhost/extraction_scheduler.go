package memoryhost

import (
	"context"
	"log/slog"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/storage"
)

type IdleExtractionSchedulerConfig struct {
	IdleAfter                time.Duration
	SweepInterval            time.Duration
	MinEpisodeCount          int
	MaxSegmentsPerSweep      int
	IncludeFinalizedSegments bool
	IncludeActiveSegments    bool
	Mode                     memorycore.ExtractionRunMode
}

type IdleExtractionScheduler struct {
	host   *Host
	db     *storage.DB
	logger *slog.Logger
	cfg    IdleExtractionSchedulerConfig
}

func NewIdleExtractionScheduler(host *Host, db *storage.DB, logger *slog.Logger, cfg IdleExtractionSchedulerConfig) *IdleExtractionScheduler {
	if cfg.IdleAfter <= 0 {
		cfg.IdleAfter = 15 * time.Minute
	}
	if cfg.SweepInterval <= 0 {
		cfg.SweepInterval = time.Minute
	}
	if cfg.MinEpisodeCount <= 0 {
		cfg.MinEpisodeCount = 2
	}
	if cfg.MaxSegmentsPerSweep <= 0 {
		cfg.MaxSegmentsPerSweep = 20
	}
	if !cfg.IncludeActiveSegments && !cfg.IncludeFinalizedSegments {
		cfg.IncludeActiveSegments = true
		cfg.IncludeFinalizedSegments = true
	}
	if cfg.Mode == "" {
		cfg.Mode = memorycore.ExtractionRunModeApply
	}
	return &IdleExtractionScheduler{host: host, db: db, logger: logger, cfg: cfg}
}

func (s *IdleExtractionScheduler) Run(ctx context.Context) {
	interval := s.cfg.SweepInterval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.RunOnce(ctx); err != nil && s.logger != nil {
				s.logger.Warn("memory idle extraction sweep failed", "error", err)
			}
		}
	}
}

func (s *IdleExtractionScheduler) RunOnce(ctx context.Context) (int, error) {
	if s == nil || s.host == nil || s.db == nil || !s.host.ExtractionEnabled() || !s.host.extractionPolicy.AsyncEnabled {
		return 0, nil
	}
	policy := s.host.extractionPolicy.normalized()
	segments, err := s.db.ScanEligibleMemorySegments(ctx, storage.ScanEligibleMemorySegmentsParams{
		Now:                      time.Now().UTC(),
		IdleAfter:                s.cfg.IdleAfter,
		IncludeActiveSegments:    s.cfg.IncludeActiveSegments,
		IncludeFinalizedSegments: s.cfg.IncludeFinalizedSegments,
		MinEpisodeCount:          s.cfg.MinEpisodeCount,
		MaxFailedAttempts:        policy.MaxAttempts,
		Limit:                    s.cfg.MaxSegmentsPerSweep,
	})
	if err != nil {
		return 0, err
	}
	queued := 0
	for _, segment := range segments {
		job, enqueued, err := s.db.EnqueueMemoryExtractionJob(ctx, storage.EnqueueMemoryExtractionJobParams{
			PersonaID:       defaultPersonaID(segmentPersona(&segment, s.db, ctx)),
			ChatSessionID:   segment.ChatSessionID,
			SegmentID:       segment.ID,
			MemorySessionID: segment.MemorySessionID,
			Trigger:         storage.MemoryExtractionTriggerIdleDetect,
			Scope:           storage.MemoryExtractionScopeSegment,
			Mode:            string(s.cfg.Mode),
			RequestedBy:     "system",
			Priority:        100,
			UntilAt:         segment.LastActivityAt,
			EpisodeLimit:    policy.limitOrDefault(),
			MaxAttempts:     policy.MaxAttempts,
			RunAfter:        time.Now().UTC(),
		})
		if err != nil {
			return queued, err
		}
		if enqueued {
			queued++
		}
		if s.logger != nil && job != nil {
			s.logger.Info("idle memory extraction queued", "chat_session_id", segment.ChatSessionID, "segment_id", segment.ID, "job_id", job.ID, "enqueued", enqueued)
		}
	}
	return queued, nil
}
