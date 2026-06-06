package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/memoryhost"
)

func memoryNaturalMemoryRunnerConfig(cfg config.MemoryNaturalMemoryConfig) memoryhost.NaturalMemoryHostConfig {
	return memoryhost.NaturalMemoryHostConfig{
		Enabled:                 cfg.Enabled,
		SchedulerEnabled:        cfg.SchedulerEnabled,
		TickInterval:            time.Duration(cfg.TickIntervalSeconds) * time.Second,
		LocalTime:               cfg.LocalTime,
		Timezone:                cfg.Timezone,
		RunMissedOnStart:        cfg.RunMissedOnStart,
		MirrorSyncAfterRun:      cfg.MirrorSyncAfterRun,
		MirrorSyncLimit:         cfg.MirrorSyncLimit,
		FailOnSyncError:         cfg.FailOnSyncError,
		ManualEnabled:           cfg.Manual.Enabled,
		AllowDryRun:             cfg.Manual.AllowDryRun,
		AllowForce:              cfg.Manual.AllowForce,
		MarkSleepCycleByDefault: cfg.Manual.MarkSleepCycleByDefault || cfg.MarkSleepCycleByDefault,
	}
}

func startNaturalMemoryBackground(ctx context.Context, host *memoryhost.Host, logger *slog.Logger, cfg config.MemoryNaturalMemoryConfig) *memoryhost.NaturalMemoryRunner {
	if host == nil || host.Core == nil || !cfg.Enabled {
		return nil
	}
	runner := memoryhost.NewNaturalMemoryRunner(host, memoryNaturalMemoryRunnerConfig(cfg), logger)
	if cfg.SchedulerEnabled {
		runner.Start(ctx)
	}
	return runner
}
