package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/memoryhost"
	"github.com/longyisang/emoagent/internal/web"
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
	if host == nil || host.Service == nil || !cfg.Enabled {
		return nil
	}
	runner := memoryhost.NewNaturalMemoryRunner(host, memoryNaturalMemoryRunnerConfig(cfg), logger)
	if cfg.SchedulerEnabled {
		runner.Start(ctx)
	}
	return runner
}

func (a *App) RunNaturalMemory(ctx context.Context, req web.NaturalMemoryRunRequest) (memoryhost.NaturalMemoryRunResponse, error) {
	runner, err := a.ensureNaturalMemoryRunner()
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

func (a *App) LatestNaturalMemoryRun(context.Context) (*memoryhost.NaturalMemoryRunResponse, error) {
	runner, err := a.ensureNaturalMemoryRunner()
	if err != nil {
		return nil, err
	}
	return runner.Latest(), nil
}

func (a *App) ensureNaturalMemoryRunner() (*memoryhost.NaturalMemoryRunner, error) {
	if a == nil || a.Config == nil || !a.Config.Memory.Enabled || !a.Config.Memory.NaturalMemory.Enabled {
		return nil, fmt.Errorf("natural memory is disabled")
	}
	if a.Memory == nil || a.Memory.Service == nil {
		return nil, fmt.Errorf("memorycore is not configured")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.NaturalMemory == nil {
		a.NaturalMemory = memoryhost.NewNaturalMemoryRunner(a.Memory, memoryNaturalMemoryRunnerConfig(a.Config.Memory.NaturalMemory), a.Logger)
	}
	return a.NaturalMemory, nil
}
