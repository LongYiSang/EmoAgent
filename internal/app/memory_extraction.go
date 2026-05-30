package app

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/memoryhost"
	"github.com/longyisang/emoagent/internal/storage"
)

func memoryExtractionHostConfig(cfg config.MemoryExtractionConfig) memoryhost.ExtractionHostPolicy {
	return memoryhost.ExtractionHostPolicy{
		Enabled:                  cfg.Enabled,
		AsyncEnabled:             cfg.Async.Enabled,
		TriggerOnFinalizeSegment: cfg.TriggerOnFinalizeSegment,
		TriggerOnManualPin:       cfg.TriggerOnManualPin,
		SessionEndMode:           memoryExtractionMode(firstMemoryExtractionMode(cfg.SessionEndMode, cfg.Mode)),
		ManualPinMode:            memoryExtractionMode(firstMemoryExtractionMode(cfg.ManualPinMode, "apply")),
		Limit:                    cfg.Limit,
		Timezone:                 cfg.Timezone,
		MaxAttempts:              cfg.Async.MaxAttempts,
		AllowInference:           cfg.AllowInference,
		AllowSensitiveExtraction: cfg.AllowSensitiveExtraction,
		MaxFacts:                 cfg.MaxFacts,
		MaxLinks:                 cfg.MaxLinks,
		SemanticDedup: memorycore.SemanticDedupOptions{
			Enabled:          cfg.SemanticDedup.Enabled,
			Shadow:           cfg.SemanticDedup.Shadow,
			Enforce:          cfg.SemanticDedup.Enforce,
			CandidateLimit:   cfg.SemanticDedup.CandidateLimit,
			ThresholdProfile: cfg.SemanticDedup.ThresholdProfile,
		},
	}
}

func memoryExtractionMode(mode string) memorycore.ExtractionRunMode {
	switch mode {
	case "validate":
		return memorycore.ExtractionRunModeValidate
	case "apply":
		return memorycore.ExtractionRunModeApply
	case "dry-run", "dry_run":
		return memorycore.ExtractionRunModeDryRun
	default:
		return memorycore.ExtractionRunMode(mode)
	}
}

func firstMemoryExtractionMode(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func memoryExtractionWorkerConfig(cfg config.MemoryExtractionConfig, workerIndex int) memoryhost.ExtractionWorkerConfig {
	workerID := "memory-extraction-worker"
	if workerIndex > 0 {
		workerID += "-" + strconv.Itoa(workerIndex)
	}
	return memoryhost.ExtractionWorkerConfig{
		WorkerID:                  workerID,
		ClaimLimit:                1,
		ClaimTTL:                  time.Duration(cfg.Async.QueueClaimTTLSeconds) * time.Second,
		RetryBaseDelay:            time.Duration(cfg.Async.RetryBaseDelaySeconds) * time.Second,
		RetryMaxDelay:             time.Duration(cfg.Async.RetryMaxDelaySeconds) * time.Second,
		MirrorSyncAfterApply:      cfg.MirrorSync.AfterApply,
		MirrorSyncLimit:           cfg.MirrorSync.Limit,
		FailExtractionOnSyncError: cfg.MirrorSync.FailExtractionOnSyncError,
	}
}

func memoryExtractionIdleSchedulerConfig(cfg config.MemoryExtractionConfig) memoryhost.IdleExtractionSchedulerConfig {
	return memoryhost.IdleExtractionSchedulerConfig{
		IdleAfter:                time.Duration(cfg.Idle.IdleAfterSeconds) * time.Second,
		SweepInterval:            time.Duration(cfg.Idle.SweepIntervalSeconds) * time.Second,
		MinEpisodeCount:          cfg.Idle.MinEpisodeCount,
		MaxSegmentsPerSweep:      cfg.Idle.MaxSegmentsPerSweep,
		IncludeFinalizedSegments: cfg.Idle.IncludeFinalizedSegments,
		IncludeActiveSegments:    cfg.Idle.IncludeActiveSegments,
		Mode:                     memoryExtractionMode(firstMemoryExtractionMode(cfg.SessionEndMode, cfg.Mode)),
	}
}

func startMemoryExtractionBackground(ctx context.Context, host *memoryhost.Host, db *storage.DB, logger *slog.Logger, cfg config.MemoryExtractionConfig) {
	if host == nil || db == nil || !cfg.Enabled || !cfg.Async.Enabled {
		return
	}
	if cfg.Async.WorkerEnabled {
		concurrency := cfg.Async.WorkerConcurrency
		if concurrency <= 0 {
			concurrency = 1
		}
		for i := 1; i <= concurrency; i++ {
			worker := memoryhost.NewExtractionWorker(host, db, logger, memoryExtractionWorkerConfig(cfg, i))
			go worker.Run(ctx, time.Second)
		}
	}
	if cfg.Idle.Enabled {
		scheduler := memoryhost.NewIdleExtractionScheduler(host, db, logger, memoryExtractionIdleSchedulerConfig(cfg))
		go scheduler.Run(ctx)
	}
	if cfg.MirrorSync.PeriodicEnabled {
		go runMemoryMirrorSyncLoop(ctx, host, logger, cfg.MirrorSync)
	}
}

func runMemoryMirrorSyncLoop(ctx context.Context, host *memoryhost.Host, logger *slog.Logger, cfg config.MemoryExtractionMirrorConfig) {
	if host == nil || host.Service == nil {
		return
	}
	interval := time.Duration(cfg.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Minute
	}
	limit := cfg.Limit
	if limit <= 0 {
		limit = 100
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := host.Service.RunMirrorSync(ctx, memorycore.RunMirrorSyncRequest{Limit: limit}); err != nil && logger != nil {
				logger.Warn("periodic memory mirror sync failed", "error_code", "mirror_sync_failed")
			}
		}
	}
}
