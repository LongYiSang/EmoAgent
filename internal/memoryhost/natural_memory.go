package memoryhost

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	memconfig "github.com/longyisang/emoagent-memorycore/config"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

type NaturalMemoryCoreOverrides struct {
	Configured              bool
	Enabled                 bool
	LocalTime               string
	Timezone                string
	RunMissedOnStart        bool
	ManualEnabled           bool
	AllowDryRun             bool
	AllowForce              bool
	MarkSleepCycleByDefault bool
}

type NaturalMemoryHostConfig struct {
	Enabled                 bool
	SchedulerEnabled        bool
	TickInterval            time.Duration
	LocalTime               string
	Timezone                string
	RunMissedOnStart        bool
	MirrorSyncAfterRun      bool
	MirrorSyncLimit         int
	FailOnSyncError         bool
	ManualEnabled           bool
	AllowDryRun             bool
	AllowForce              bool
	MarkSleepCycleByDefault bool
}

type NaturalMemoryManualRunRequest struct {
	PersonaID      string
	Mode           string
	Now            time.Time
	DryRun         bool
	Force          bool
	Explain        bool
	LocalDate      string
	LocalTime      string
	Timezone       string
	MarkSleepCycle bool
}

type NaturalMemoryRunResponse struct {
	NaturalRun *memorycore.RunNaturalMemoryCycleResult `json:"natural_run,omitempty"`
	MirrorSync *NaturalMemoryMirrorSyncResult          `json:"mirror_sync,omitempty"`
}

type NaturalMemoryMirrorSyncResult struct {
	Attempted bool                            `json:"attempted"`
	Status    string                          `json:"status"`
	ErrorCode string                          `json:"error_code,omitempty"`
	Result    *memorycore.RunMirrorSyncResult `json:"result,omitempty"`
}

type NaturalMemoryRunner struct {
	host   *Host
	cfg    NaturalMemoryHostConfig
	logger *slog.Logger

	mu       sync.RWMutex
	latest   *NaturalMemoryRunResponse
	stop     chan struct{}
	stopOnce sync.Once
}

func NewNaturalMemoryRunner(host *Host, cfg NaturalMemoryHostConfig, logger *slog.Logger) *NaturalMemoryRunner {
	cfg = normalizeNaturalMemoryHostConfig(cfg)
	return &NaturalMemoryRunner{host: host, cfg: cfg, logger: logger, stop: make(chan struct{})}
}

func (r *NaturalMemoryRunner) Start(ctx context.Context) {
	if r == nil || !r.cfg.Enabled || !r.cfg.SchedulerEnabled {
		return
	}
	if r.cfg.RunMissedOnStart {
		go func() {
			if _, err := r.tick(ctx, time.Now(), true); err != nil && r.logger != nil {
				r.logger.Warn("natural memory startup tick failed", "error_code", safeErrorCode(nil, err))
			}
		}()
	}
	go r.run(ctx)
}

func (r *NaturalMemoryRunner) Stop(context.Context) error {
	if r == nil {
		return nil
	}
	r.stopOnce.Do(func() { close(r.stop) })
	return nil
}

func (r *NaturalMemoryRunner) Tick(ctx context.Context, now time.Time) (*NaturalMemoryRunResponse, error) {
	return r.tick(ctx, now, false)
}

func (r *NaturalMemoryRunner) RunManual(ctx context.Context, req NaturalMemoryManualRunRequest) (*NaturalMemoryRunResponse, error) {
	if r == nil || r.host == nil || r.host.Service == nil {
		return nil, nil
	}
	if !r.cfg.Enabled {
		return nil, fmt.Errorf("natural memory is disabled")
	}
	if !r.cfg.ManualEnabled {
		return nil, fmt.Errorf("natural memory manual trigger is disabled")
	}
	if req.DryRun && !r.cfg.AllowDryRun {
		return nil, fmt.Errorf("natural memory dry-run is disabled")
	}
	if req.Force && !r.cfg.AllowForce {
		return nil, fmt.Errorf("natural memory force is disabled")
	}
	runKind, err := naturalMemoryRunKind(req.Mode)
	if err != nil {
		return nil, err
	}
	result, err := r.host.Service.RunNaturalMemoryCycle(ctx, memorycore.RunNaturalMemoryCycleRequest{
		PersonaID:      defaultPersonaID(req.PersonaID),
		Now:            req.Now,
		DryRun:         req.DryRun,
		Force:          req.Force,
		Explain:        req.Explain,
		RunKind:        runKind,
		LocalDate:      strings.TrimSpace(req.LocalDate),
		LocalTime:      firstNonEmpty(strings.TrimSpace(req.LocalTime), r.cfg.LocalTime),
		Timezone:       firstNonEmpty(strings.TrimSpace(req.Timezone), r.cfg.Timezone),
		MarkSleepCycle: req.MarkSleepCycle || r.cfg.MarkSleepCycleByDefault,
	})
	if err != nil {
		return nil, err
	}
	return r.finishRun(ctx, result)
}

func (r *NaturalMemoryRunner) Latest() *NaturalMemoryRunResponse {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.latest == nil {
		return nil
	}
	cp := *r.latest
	return &cp
}

func (r *NaturalMemoryRunner) run(ctx context.Context) {
	interval := r.cfg.TickInterval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stop:
			return
		case <-ticker.C:
			if _, err := r.Tick(ctx, time.Now()); err != nil && r.logger != nil {
				r.logger.Warn("natural memory tick failed", "error_code", safeErrorCode(nil, err))
			}
		}
	}
}

func (r *NaturalMemoryRunner) tick(ctx context.Context, now time.Time, startup bool) (*NaturalMemoryRunResponse, error) {
	if r == nil || r.host == nil || r.host.Service == nil {
		return nil, nil
	}
	if !r.cfg.Enabled {
		return nil, nil
	}
	result, err := r.host.Service.RunNaturalMemoryTick(ctx, memorycore.RunNaturalMemoryTickRequest{
		PersonaID: defaultPersonaID(""),
		Now:       now,
		Startup:   startup,
		LocalTime: r.cfg.LocalTime,
		Timezone:  r.cfg.Timezone,
	})
	if err != nil {
		return nil, err
	}
	return r.finishRun(ctx, result)
}

func (r *NaturalMemoryRunner) finishRun(ctx context.Context, result *memorycore.RunNaturalMemoryCycleResult) (*NaturalMemoryRunResponse, error) {
	resp := &NaturalMemoryRunResponse{NaturalRun: result}
	mirror, err := r.runMirrorSyncAfterNaturalRun(ctx, result)
	if mirror != nil {
		resp.MirrorSync = mirror
	}
	if err != nil {
		return resp, err
	}
	if result == nil || result.Status != memorycore.NaturalMemoryRunStatusSkipped {
		r.mu.Lock()
		r.latest = resp
		r.mu.Unlock()
	}
	return resp, nil
}

func (r *NaturalMemoryRunner) runMirrorSyncAfterNaturalRun(ctx context.Context, result *memorycore.RunNaturalMemoryCycleResult) (*NaturalMemoryMirrorSyncResult, error) {
	if r == nil || r.host == nil || r.host.Service == nil || result == nil {
		return nil, nil
	}
	if !r.cfg.MirrorSyncAfterRun || result.DryRun || result.MirrorUpdatesEnqueued <= 0 {
		return nil, nil
	}
	limit := r.cfg.MirrorSyncLimit
	if limit <= 0 {
		limit = 100
	}
	mirror, err := r.host.Service.RunMirrorSync(ctx, memorycore.RunMirrorSyncRequest{
		PersonaID: defaultPersonaID(result.PersonaID),
		Limit:     limit,
	})
	if err != nil {
		degraded := &NaturalMemoryMirrorSyncResult{Attempted: true, Status: "degraded", ErrorCode: "mirror_sync_failed"}
		if r.cfg.FailOnSyncError {
			return degraded, fmt.Errorf("mirror_sync_failed")
		}
		return degraded, nil
	}
	status := "completed"
	if mirror != nil && mirror.Failed > 0 {
		status = "degraded"
	}
	return &NaturalMemoryMirrorSyncResult{Attempted: true, Status: status, Result: mirror}, nil
}

func normalizeNaturalMemoryHostConfig(cfg NaturalMemoryHostConfig) NaturalMemoryHostConfig {
	if cfg.TickInterval <= 0 {
		cfg.TickInterval = time.Minute
	}
	if strings.TrimSpace(cfg.LocalTime) == "" {
		cfg.LocalTime = "03:30"
	}
	if cfg.MirrorSyncLimit <= 0 {
		cfg.MirrorSyncLimit = 100
	}
	return cfg
}

func ApplyNaturalMemoryCoreOverrides(cfg *memconfig.Config, overrides NaturalMemoryCoreOverrides) {
	if cfg == nil || !overrides.Configured {
		return
	}
	cfg.NaturalMemory.Enabled = overrides.Enabled
	if strings.TrimSpace(overrides.LocalTime) != "" {
		cfg.NaturalMemory.SleepCycle.LocalTime = strings.TrimSpace(overrides.LocalTime)
	}
	if strings.TrimSpace(overrides.Timezone) != "" {
		cfg.NaturalMemory.SleepCycle.Timezone = strings.TrimSpace(overrides.Timezone)
	}
	cfg.NaturalMemory.SleepCycle.RunMissedOnStart = overrides.RunMissedOnStart
	cfg.NaturalMemory.ManualTrigger.Enabled = overrides.Enabled && overrides.ManualEnabled
	cfg.NaturalMemory.ManualTrigger.AllowDryRun = overrides.AllowDryRun
	cfg.NaturalMemory.ManualTrigger.AllowForce = overrides.AllowForce
	cfg.NaturalMemory.ManualTrigger.MarkSleepCycleByDefault = overrides.MarkSleepCycleByDefault
}

func naturalMemoryRunKind(mode string) (memorycore.NaturalMemoryRunKind, error) {
	switch strings.TrimSpace(mode) {
	case "", string(memorycore.NaturalMemoryRunManual):
		return memorycore.NaturalMemoryRunManual, nil
	case string(memorycore.NaturalMemoryRunAPI):
		return memorycore.NaturalMemoryRunAPI, nil
	case string(memorycore.NaturalMemoryRunTest):
		return memorycore.NaturalMemoryRunTest, nil
	default:
		return "", fmt.Errorf("mode must be manual, api, or test")
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
