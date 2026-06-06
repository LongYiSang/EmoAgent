package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/memoryhost"
	"github.com/longyisang/emoagent/internal/web"
)

func TestNaturalMemoryRunnerConfigMapsHostSettings(t *testing.T) {
	cfg := config.MemoryNaturalMemoryConfig{
		Enabled:             true,
		SchedulerEnabled:    true,
		TickIntervalSeconds: 90,
		LocalTime:           "03:30",
		Timezone:            "Asia/Shanghai",
		RunMissedOnStart:    true,
		MirrorSyncAfterRun:  true,
		MirrorSyncLimit:     25,
		FailOnSyncError:     true,
		Manual: config.MemoryNaturalMemoryManualConfig{
			Enabled:                 true,
			AllowDryRun:             true,
			AllowForce:              false,
			MarkSleepCycleByDefault: true,
		},
	}

	hostCfg := memoryNaturalMemoryRunnerConfig(cfg)
	if !hostCfg.Enabled || !hostCfg.SchedulerEnabled || hostCfg.TickInterval != 90*time.Second {
		t.Fatalf("runner enabled/scheduler = %#v", hostCfg)
	}
	if hostCfg.LocalTime != "03:30" || hostCfg.Timezone != "Asia/Shanghai" || !hostCfg.RunMissedOnStart {
		t.Fatalf("runner schedule = %#v", hostCfg)
	}
	if !hostCfg.MirrorSyncAfterRun || hostCfg.MirrorSyncLimit != 25 || !hostCfg.FailOnSyncError {
		t.Fatalf("runner mirror = %#v", hostCfg)
	}
	if !hostCfg.ManualEnabled || !hostCfg.AllowDryRun || hostCfg.AllowForce || !hostCfg.MarkSleepCycleByDefault {
		t.Fatalf("runner manual = %#v", hostCfg)
	}
}

func TestRunNaturalMemoryManualDryRunCallsMemoryHost(t *testing.T) {
	service := &appNaturalMemoryTestService{
		cycleResult: &memorycore.RunNaturalMemoryCycleResult{
			RunID:     "run-app-dry",
			PersonaID: "default",
			RunKind:   memorycore.NaturalMemoryRunManual,
			DryRun:    true,
			Status:    memorycore.NaturalMemoryRunStatusCompleted,
		},
	}
	cfg := config.DefaultConfig()
	cfg.Memory.Enabled = true
	cfg.Memory.NaturalMemory.Enabled = true
	cfg.Memory.NaturalMemory.Manual.Enabled = true
	cfg.Memory.NaturalMemory.Manual.AllowDryRun = true
	cfg.Memory.NaturalMemory.Manual.AllowForce = true
	a := newTestAppWithMemory(cfg, &memoryhost.Host{Core: service}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	resp, err := a.RunNaturalMemory(context.Background(), web.NaturalMemoryRunRequest{
		PersonaID: "default",
		Mode:      "manual",
		DryRun:    true,
		Explain:   true,
	})
	if err != nil {
		t.Fatalf("RunNaturalMemory: %v", err)
	}
	if resp.NaturalRun == nil || resp.NaturalRun.RunID != "run-app-dry" {
		t.Fatalf("NaturalRun = %#v, want run-app-dry", resp.NaturalRun)
	}
	if len(service.cycleCalls) != 1 {
		t.Fatalf("RunNaturalMemoryCycle calls = %d, want 1", len(service.cycleCalls))
	}
	req := service.cycleCalls[0]
	if req.PersonaID != "default" || req.RunKind != memorycore.NaturalMemoryRunManual || !req.DryRun || !req.Explain {
		t.Fatalf("cycle request = %#v", req)
	}
}

func TestRunNaturalMemoryRejectsForceWhenDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory.Enabled = true
	cfg.Memory.NaturalMemory.Enabled = true
	cfg.Memory.NaturalMemory.Manual.Enabled = true
	cfg.Memory.NaturalMemory.Manual.AllowForce = false
	a := newTestAppWithMemory(cfg, &memoryhost.Host{Core: &appNaturalMemoryTestService{}}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, err := a.RunNaturalMemory(context.Background(), web.NaturalMemoryRunRequest{Mode: "manual", Force: true})
	if err == nil || !strings.Contains(err.Error(), "natural memory force is disabled") {
		t.Fatalf("RunNaturalMemory error = %v, want force disabled", err)
	}
}

func TestRunNaturalMemoryReturnsMirrorFailureWhenStrict(t *testing.T) {
	service := &appNaturalMemoryTestService{
		cycleResult: &memorycore.RunNaturalMemoryCycleResult{
			RunID:                 "run-app-strict",
			PersonaID:             "default",
			RunKind:               memorycore.NaturalMemoryRunManual,
			Status:                memorycore.NaturalMemoryRunStatusCompleted,
			MirrorUpdatesEnqueued: 1,
		},
		mirrorErr: errors.New("sidecar failed"),
	}
	cfg := config.DefaultConfig()
	cfg.Memory.Enabled = true
	cfg.Memory.NaturalMemory.Enabled = true
	cfg.Memory.NaturalMemory.Manual.Enabled = true
	cfg.Memory.NaturalMemory.MirrorSyncAfterRun = true
	cfg.Memory.NaturalMemory.FailOnSyncError = true
	a := newTestAppWithMemory(cfg, &memoryhost.Host{Core: service}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, err := a.RunNaturalMemory(context.Background(), web.NaturalMemoryRunRequest{Mode: "manual"})
	if err == nil || !strings.Contains(err.Error(), "mirror_sync_failed") {
		t.Fatalf("RunNaturalMemory error = %v, want mirror_sync_failed", err)
	}
}

type appNaturalMemoryTestService struct {
	memoryhost.CoreClient

	cycleCalls      []memorycore.RunNaturalMemoryCycleRequest
	mirrorSyncCalls []memorycore.RunMirrorSyncRequest
	cycleResult     *memorycore.RunNaturalMemoryCycleResult
	mirrorResult    *memorycore.RunMirrorSyncResult
	cycleErr        error
	mirrorErr       error
}

func (f *appNaturalMemoryTestService) RunNaturalMemoryCycle(_ context.Context, req memorycore.RunNaturalMemoryCycleRequest) (*memorycore.RunNaturalMemoryCycleResult, error) {
	f.cycleCalls = append(f.cycleCalls, req)
	if f.cycleResult != nil || f.cycleErr != nil {
		return f.cycleResult, f.cycleErr
	}
	return &memorycore.RunNaturalMemoryCycleResult{Status: memorycore.NaturalMemoryRunStatusCompleted}, nil
}

func (f *appNaturalMemoryTestService) RunMirrorSync(_ context.Context, req memorycore.RunMirrorSyncRequest) (*memorycore.RunMirrorSyncResult, error) {
	f.mirrorSyncCalls = append(f.mirrorSyncCalls, req)
	if f.mirrorResult != nil || f.mirrorErr != nil {
		return f.mirrorResult, f.mirrorErr
	}
	return &memorycore.RunMirrorSyncResult{}, nil
}
