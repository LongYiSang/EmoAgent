package memoryhost

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func TestNaturalMemoryRunnerTickRunsAndSyncsMirror(t *testing.T) {
	now := time.Date(2026, 6, 6, 3, 30, 0, 0, time.UTC)
	service := &naturalMemoryTestService{
		tickResult: &memorycore.RunNaturalMemoryCycleResult{
			RunID:                 "run-1",
			PersonaID:             "default",
			RunKind:               memorycore.NaturalMemoryRunSleepCycle,
			Status:                memorycore.NaturalMemoryRunStatusCompleted,
			MirrorUpdatesEnqueued: 2,
		},
		mirrorResult: &memorycore.RunMirrorSyncResult{Claimed: 2, Completed: 2},
	}
	runner := NewNaturalMemoryRunner(&Host{Service: service}, NaturalMemoryHostConfig{
		Enabled:            true,
		SchedulerEnabled:   true,
		TickInterval:       time.Minute,
		LocalTime:          "03:30",
		Timezone:           "Asia/Shanghai",
		MirrorSyncAfterRun: true,
		MirrorSyncLimit:    42,
	}, testMemoryLogger())

	resp, err := runner.Tick(context.Background(), now)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if resp.NaturalRun == nil || resp.NaturalRun.RunID != "run-1" {
		t.Fatalf("NaturalRun = %#v, want run-1", resp.NaturalRun)
	}
	if len(service.tickCalls) != 1 {
		t.Fatalf("RunNaturalMemoryTick calls = %d, want 1", len(service.tickCalls))
	}
	req := service.tickCalls[0]
	if req.PersonaID != "default" || !req.Now.Equal(now) || req.LocalTime != "03:30" || req.Timezone != "Asia/Shanghai" {
		t.Fatalf("tick request = %#v", req)
	}
	if len(service.mirrorSyncCalls) != 1 {
		t.Fatalf("RunMirrorSync calls = %d, want 1", len(service.mirrorSyncCalls))
	}
	if service.mirrorSyncCalls[0].Limit != 42 || service.mirrorSyncCalls[0].PersonaID != "default" {
		t.Fatalf("mirror request = %#v", service.mirrorSyncCalls[0])
	}
	if resp.MirrorSync == nil || !resp.MirrorSync.Attempted || resp.MirrorSync.Status != "completed" {
		t.Fatalf("MirrorSync = %#v, want completed", resp.MirrorSync)
	}
}

func TestNaturalMemoryRunnerManualDryRunDoesNotSyncMirror(t *testing.T) {
	service := &naturalMemoryTestService{
		cycleResult: &memorycore.RunNaturalMemoryCycleResult{
			RunID:                 "run-dry",
			PersonaID:             "default",
			RunKind:               memorycore.NaturalMemoryRunManual,
			DryRun:                true,
			Status:                memorycore.NaturalMemoryRunStatusCompleted,
			MirrorUpdatesEnqueued: 3,
		},
	}
	runner := NewNaturalMemoryRunner(&Host{Service: service}, NaturalMemoryHostConfig{
		Enabled:            true,
		MirrorSyncAfterRun: true,
		MirrorSyncLimit:    100,
		ManualEnabled:      true,
		AllowDryRun:        true,
		AllowForce:         true,
	}, testMemoryLogger())

	resp, err := runner.RunManual(context.Background(), NaturalMemoryManualRunRequest{
		PersonaID: "default",
		DryRun:    true,
		Explain:   true,
	})
	if err != nil {
		t.Fatalf("RunManual: %v", err)
	}
	if resp.NaturalRun == nil || !resp.NaturalRun.DryRun {
		t.Fatalf("NaturalRun = %#v, want dry run", resp.NaturalRun)
	}
	if len(service.cycleCalls) != 1 {
		t.Fatalf("RunNaturalMemoryCycle calls = %d, want 1", len(service.cycleCalls))
	}
	req := service.cycleCalls[0]
	if req.RunKind != memorycore.NaturalMemoryRunManual || !req.DryRun || !req.Explain {
		t.Fatalf("cycle request = %#v", req)
	}
	if len(service.mirrorSyncCalls) != 0 {
		t.Fatalf("RunMirrorSync calls = %d, want 0 for dry-run", len(service.mirrorSyncCalls))
	}
}

func TestNaturalMemoryRunnerSkippedTickDoesNotReplaceLatestCompletedRun(t *testing.T) {
	service := &naturalMemoryTestService{
		cycleResult: &memorycore.RunNaturalMemoryCycleResult{
			RunID:     "run-completed",
			PersonaID: "default",
			RunKind:   memorycore.NaturalMemoryRunManual,
			Status:    memorycore.NaturalMemoryRunStatusCompleted,
		},
		tickResult: &memorycore.RunNaturalMemoryCycleResult{
			RunID:   "run-skipped",
			RunKind: memorycore.NaturalMemoryRunSleepCycle,
			Status:  memorycore.NaturalMemoryRunStatusSkipped,
		},
	}
	runner := NewNaturalMemoryRunner(&Host{Service: service}, NaturalMemoryHostConfig{
		Enabled:       true,
		ManualEnabled: true,
		AllowDryRun:   true,
		AllowForce:    true,
	}, testMemoryLogger())

	if _, err := runner.RunManual(context.Background(), NaturalMemoryManualRunRequest{PersonaID: "default"}); err != nil {
		t.Fatalf("RunManual: %v", err)
	}
	if _, err := runner.Tick(context.Background(), time.Date(2026, 6, 6, 3, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	latest := runner.Latest()
	if latest == nil || latest.NaturalRun == nil || latest.NaturalRun.RunID != "run-completed" {
		t.Fatalf("Latest = %#v, want completed run", latest)
	}
}

func TestNaturalMemoryRunnerMirrorFailureDegradesByDefault(t *testing.T) {
	service := &naturalMemoryTestService{
		cycleResult: &memorycore.RunNaturalMemoryCycleResult{
			RunID:                 "run-apply",
			PersonaID:             "default",
			RunKind:               memorycore.NaturalMemoryRunManual,
			Status:                memorycore.NaturalMemoryRunStatusCompleted,
			MirrorUpdatesEnqueued: 1,
		},
		mirrorErr: errors.New("sidecar down with raw path D:/private"),
	}
	runner := NewNaturalMemoryRunner(&Host{Service: service}, NaturalMemoryHostConfig{
		Enabled:            true,
		MirrorSyncAfterRun: true,
		MirrorSyncLimit:    100,
		ManualEnabled:      true,
		AllowDryRun:        true,
		AllowForce:         true,
	}, testMemoryLogger())

	resp, err := runner.RunManual(context.Background(), NaturalMemoryManualRunRequest{PersonaID: "default"})
	if err != nil {
		t.Fatalf("RunManual: %v", err)
	}
	if resp.NaturalRun == nil || resp.NaturalRun.RunID != "run-apply" {
		t.Fatalf("NaturalRun = %#v, want run-apply", resp.NaturalRun)
	}
	if resp.MirrorSync == nil || resp.MirrorSync.Status != "degraded" || resp.MirrorSync.ErrorCode != "mirror_sync_failed" {
		t.Fatalf("MirrorSync = %#v, want degraded mirror_sync_failed", resp.MirrorSync)
	}
	body, err := json.Marshal(resp.MirrorSync)
	if err != nil {
		t.Fatalf("Marshal mirror sync: %v", err)
	}
	if string(body) != `{"attempted":true,"status":"degraded","error_code":"mirror_sync_failed"}` {
		t.Fatalf("mirror sync JSON = %s, want sanitized degraded payload", body)
	}
}

type naturalMemoryTestService struct {
	memorycore.Service

	tickCalls       []memorycore.RunNaturalMemoryTickRequest
	cycleCalls      []memorycore.RunNaturalMemoryCycleRequest
	mirrorSyncCalls []memorycore.RunMirrorSyncRequest
	tickResult      *memorycore.RunNaturalMemoryCycleResult
	cycleResult     *memorycore.RunNaturalMemoryCycleResult
	mirrorResult    *memorycore.RunMirrorSyncResult
	tickErr         error
	cycleErr        error
	mirrorErr       error
}

func (f *naturalMemoryTestService) RunNaturalMemoryTick(_ context.Context, req memorycore.RunNaturalMemoryTickRequest) (*memorycore.RunNaturalMemoryCycleResult, error) {
	f.tickCalls = append(f.tickCalls, req)
	if f.tickResult != nil || f.tickErr != nil {
		return f.tickResult, f.tickErr
	}
	return &memorycore.RunNaturalMemoryCycleResult{Status: memorycore.NaturalMemoryRunStatusSkipped}, nil
}

func (f *naturalMemoryTestService) RunNaturalMemoryCycle(_ context.Context, req memorycore.RunNaturalMemoryCycleRequest) (*memorycore.RunNaturalMemoryCycleResult, error) {
	f.cycleCalls = append(f.cycleCalls, req)
	if f.cycleResult != nil || f.cycleErr != nil {
		return f.cycleResult, f.cycleErr
	}
	return &memorycore.RunNaturalMemoryCycleResult{Status: memorycore.NaturalMemoryRunStatusCompleted}, nil
}

func (f *naturalMemoryTestService) RunMirrorSync(_ context.Context, req memorycore.RunMirrorSyncRequest) (*memorycore.RunMirrorSyncResult, error) {
	f.mirrorSyncCalls = append(f.mirrorSyncCalls, req)
	if f.mirrorResult != nil || f.mirrorErr != nil {
		return f.mirrorResult, f.mirrorErr
	}
	return &memorycore.RunMirrorSyncResult{}, nil
}
