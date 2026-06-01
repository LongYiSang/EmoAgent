package memoryhost

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/storage"
)

func TestExtractionWorkerRunsQueuedJobAndUpdatesSegment(t *testing.T) {
	fixture := openFacadeBridgeFixture(t, "chat-worker-success", &fakeMemoryService{
		runExtractionResult: &memorycore.ExtractionRunResult{
			Status:       memorycore.ExtractionRunStatusApplied,
			AppliedCount: 1,
		},
		mirrorSyncResult: &memorycore.RunMirrorSyncResult{Claimed: 1, Completed: 1},
	})
	job := enqueueWorkerJob(t, fixture, storage.MemoryExtractionTriggerSessionEnd)

	worker := NewExtractionWorker(fixture.serviceHost(), fixture.db, testMemoryLogger(), ExtractionWorkerConfig{
		WorkerID:             "worker-test",
		ClaimLimit:           1,
		ClaimTTL:             time.Minute,
		MirrorSyncAfterApply: true,
		MirrorSyncLimit:      100,
		RetryBaseDelay:       time.Second,
		RetryMaxDelay:        time.Minute,
	})
	processed, err := worker.RunOnce(fixture.ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d, want 1", processed)
	}
	if len(fixture.service.runExtractionCalls) != 1 {
		t.Fatalf("RunExtraction calls = %d, want 1", len(fixture.service.runExtractionCalls))
	}
	req := fixture.service.runExtractionCalls[0]
	if req.Trigger != memorycore.ExtractionTriggerSessionEnd || req.SessionID == nil || *req.SessionID != fixture.segment.MemorySessionID {
		t.Fatalf("RunExtraction request = %#v", req)
	}
	if req.Build == nil || req.Build.SessionID == nil || *req.Build.SessionID != fixture.segment.MemorySessionID || req.Build.Until == nil {
		t.Fatalf("RunExtraction build = %#v", req.Build)
	}
	if fixture.service.mirrorSyncCalls != 1 {
		t.Fatalf("RunMirrorSync calls = %d, want 1", fixture.service.mirrorSyncCalls)
	}
	completed, err := fixture.db.GetMemoryExtractionJob(fixture.ctx, job.ID)
	if err != nil {
		t.Fatalf("GetMemoryExtractionJob: %v", err)
	}
	if completed.Status != storage.MemoryExtractionJobStatusSucceeded || completed.ResultJSON == "" || completed.MirrorSyncResultJSON == "" {
		t.Fatalf("completed job = %#v", completed)
	}
	segment, err := fixture.db.GetMemorySegment(fixture.ctx, fixture.segment.SegmentID)
	if err != nil {
		t.Fatalf("GetMemorySegment: %v", err)
	}
	if segment.ExtractionStatus != storage.MemorySegmentExtractionStatusSucceeded || segment.LastExtractedAt == "" || segment.LastExtractedUntilAt != job.UntilAt {
		t.Fatalf("segment = %#v", segment)
	}
}

func TestExtractionWorkerMapsFingerprintSkipToSkipped(t *testing.T) {
	fixture := openFacadeBridgeFixture(t, "chat-worker-skip", &fakeMemoryService{
		runExtractionResult: &memorycore.ExtractionRunResult{
			Status:               memorycore.ExtractionRunStatusSkipped,
			SkippedByFingerprint: true,
		},
	})
	job := enqueueWorkerJob(t, fixture, storage.MemoryExtractionTriggerIdleDetect)

	worker := NewExtractionWorker(fixture.serviceHost(), fixture.db, testMemoryLogger(), ExtractionWorkerConfig{WorkerID: "worker-test", ClaimLimit: 1, ClaimTTL: time.Minute})
	if processed, err := worker.RunOnce(fixture.ctx); err != nil || processed != 1 {
		t.Fatalf("RunOnce processed=%d err=%v, want 1 nil", processed, err)
	}
	completed, err := fixture.db.GetMemoryExtractionJob(fixture.ctx, job.ID)
	if err != nil {
		t.Fatalf("GetMemoryExtractionJob: %v", err)
	}
	if completed.Status != storage.MemoryExtractionJobStatusSkipped {
		t.Fatalf("job status = %q, want skipped", completed.Status)
	}
	segment, err := fixture.db.GetMemorySegment(fixture.ctx, fixture.segment.SegmentID)
	if err != nil {
		t.Fatalf("GetMemorySegment: %v", err)
	}
	if segment.ExtractionStatus != storage.MemorySegmentExtractionStatusSkipped {
		t.Fatalf("segment status = %q, want skipped", segment.ExtractionStatus)
	}
}

func TestExtractionWorkerRetriesFailureWithSanitizedError(t *testing.T) {
	fixture := openFacadeBridgeFixture(t, "chat-worker-fail", &fakeMemoryService{
		runExtractionResult: &memorycore.ExtractionRunResult{
			Status:                memorycore.ExtractionRunStatusFailed,
			SanitizedErrorCode:    "provider_failed",
			SanitizedErrorMessage: "provider unavailable",
		},
		runExtractionErr: errors.New("raw provider failed with user text 我喜欢手冲咖啡"),
	})
	job := enqueueWorkerJob(t, fixture, storage.MemoryExtractionTriggerSessionEnd)

	worker := NewExtractionWorker(fixture.serviceHost(), fixture.db, testMemoryLogger(), ExtractionWorkerConfig{
		WorkerID:       "worker-test",
		ClaimLimit:     1,
		ClaimTTL:       time.Minute,
		RetryBaseDelay: time.Minute,
		RetryMaxDelay:  time.Minute,
	})
	if processed, err := worker.RunOnce(fixture.ctx); err != nil || processed != 1 {
		t.Fatalf("RunOnce processed=%d err=%v, want 1 nil", processed, err)
	}
	failed, err := fixture.db.GetMemoryExtractionJob(fixture.ctx, job.ID)
	if err != nil {
		t.Fatalf("GetMemoryExtractionJob: %v", err)
	}
	if failed.Status != storage.MemoryExtractionJobStatusPending || failed.ErrorCode != "provider_failed" {
		t.Fatalf("failed job = %#v, want pending retry with provider_failed", failed)
	}
	if strings.Contains(failed.ErrorMessage, "手冲咖啡") || strings.Contains(failed.ErrorMessage, "raw provider failed") {
		t.Fatalf("error message leaked raw provider text: %q", failed.ErrorMessage)
	}
}

func TestExtractionWorkerStoresCodedErrorMessageWhenResultIsNil(t *testing.T) {
	fixture := openFacadeBridgeFixture(t, "chat-worker-coded-fail", &fakeMemoryService{
		runExtractionErr: codedExtractionTestError{
			code:    "build_request_failed",
			message: "could not build extraction request",
		},
	})
	job := enqueueWorkerJob(t, fixture, storage.MemoryExtractionTriggerSessionEnd)

	worker := NewExtractionWorker(fixture.serviceHost(), fixture.db, testMemoryLogger(), ExtractionWorkerConfig{
		WorkerID:       "worker-test",
		ClaimLimit:     1,
		ClaimTTL:       time.Minute,
		RetryBaseDelay: time.Minute,
		RetryMaxDelay:  time.Minute,
	})
	if processed, err := worker.RunOnce(fixture.ctx); err != nil || processed != 1 {
		t.Fatalf("RunOnce processed=%d err=%v, want 1 nil", processed, err)
	}
	failed, err := fixture.db.GetMemoryExtractionJob(fixture.ctx, job.ID)
	if err != nil {
		t.Fatalf("GetMemoryExtractionJob: %v", err)
	}
	if failed.Status != storage.MemoryExtractionJobStatusPending || failed.ErrorCode != "build_request_failed" || failed.ErrorMessage != "could not build extraction request" {
		t.Fatalf("failed job = %#v, want pending retry with coded sanitized message", failed)
	}
	segment, err := fixture.db.GetMemorySegment(fixture.ctx, fixture.segment.SegmentID)
	if err != nil {
		t.Fatalf("GetMemorySegment: %v", err)
	}
	if segment.LastExtractionErrorCode != "build_request_failed" || segment.LastExtractionErrorMessage != "could not build extraction request" {
		t.Fatalf("segment = %#v, want coded sanitized error", segment)
	}
}

type codedExtractionTestError struct {
	code    string
	message string
}

func (e codedExtractionTestError) Error() string {
	return e.message
}

func (e codedExtractionTestError) ErrorCode() string {
	return e.code
}

func TestExtractionWorkerMirrorFailureDoesNotFailExtractionByDefault(t *testing.T) {
	fixture := openFacadeBridgeFixture(t, "chat-worker-mirror", &fakeMemoryService{
		runExtractionResult: &memorycore.ExtractionRunResult{
			Status:       memorycore.ExtractionRunStatusApplied,
			AppliedCount: 1,
		},
		mirrorSyncErr: errors.New("sidecar down with raw details"),
	})
	job := enqueueWorkerJob(t, fixture, storage.MemoryExtractionTriggerSessionEnd)

	worker := NewExtractionWorker(fixture.serviceHost(), fixture.db, testMemoryLogger(), ExtractionWorkerConfig{
		WorkerID:             "worker-test",
		ClaimLimit:           1,
		ClaimTTL:             time.Minute,
		MirrorSyncAfterApply: true,
		MirrorSyncLimit:      100,
		RetryBaseDelay:       time.Second,
		RetryMaxDelay:        time.Minute,
	})
	if processed, err := worker.RunOnce(fixture.ctx); err != nil || processed != 1 {
		t.Fatalf("RunOnce processed=%d err=%v, want 1 nil", processed, err)
	}
	completed, err := fixture.db.GetMemoryExtractionJob(fixture.ctx, job.ID)
	if err != nil {
		t.Fatalf("GetMemoryExtractionJob: %v", err)
	}
	if completed.Status != storage.MemoryExtractionJobStatusSucceeded {
		t.Fatalf("job status = %q, want succeeded", completed.Status)
	}
	var mirror map[string]any
	if err := json.Unmarshal([]byte(completed.MirrorSyncResultJSON), &mirror); err != nil {
		t.Fatalf("mirror result JSON: %v", err)
	}
	if mirror["status"] != "degraded" || mirror["error_code"] != "mirror_sync_failed" {
		t.Fatalf("mirror result = %#v, want degraded mirror_sync_failed", mirror)
	}
}

func enqueueWorkerJob(t *testing.T, fixture facadeBridgeFixture, trigger string) *storage.MemoryExtractionJob {
	t.Helper()
	segment, err := fixture.db.GetMemorySegment(fixture.ctx, fixture.segment.SegmentID)
	if err != nil {
		t.Fatalf("GetMemorySegment: %v", err)
	}
	job, _, err := fixture.db.EnqueueMemoryExtractionJob(fixture.ctx, storage.EnqueueMemoryExtractionJobParams{
		PersonaID:       "default",
		ChatSessionID:   segment.ChatSessionID,
		SegmentID:       segment.ID,
		MemorySessionID: segment.MemorySessionID,
		Trigger:         trigger,
		Scope:           storage.MemoryExtractionScopeSegment,
		Mode:            string(memorycore.ExtractionRunModeApply),
		RequestedBy:     "system",
		Priority:        50,
		UntilAt:         segment.LastActivityAt,
		EpisodeLimit:    50,
		MaxAttempts:     3,
		RunAfter:        time.Now().UTC().Add(-time.Second),
	})
	if err != nil {
		t.Fatalf("EnqueueMemoryExtractionJob: %v", err)
	}
	return job
}
