package storage

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestMemoryExtractionJobLifecycle(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	segment := createExtractionJobSegment(t, db, "segment-job", "chat-job", "memory-job")

	job, enqueued, err := db.EnqueueMemoryExtractionJob(ctx, EnqueueMemoryExtractionJobParams{
		PersonaID:       "default",
		ChatSessionID:   segment.ChatSessionID,
		SegmentID:       segment.ID,
		MemorySessionID: segment.MemorySessionID,
		Trigger:         MemoryExtractionTriggerSessionEnd,
		Scope:           MemoryExtractionScopeSegment,
		Mode:            "apply",
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
	if !enqueued || job.Status != MemoryExtractionJobStatusPending {
		t.Fatalf("job=%#v enqueued=%v, want pending new job", job, enqueued)
	}

	storedSegment, err := db.GetMemorySegment(ctx, segment.ID)
	if err != nil {
		t.Fatalf("GetMemorySegment(queued): %v", err)
	}
	if storedSegment.ExtractionStatus != MemorySegmentExtractionStatusPending || storedSegment.LastExtractionJobID != job.ID {
		t.Fatalf("queued segment = %#v", storedSegment)
	}

	duplicate, enqueued, err := db.EnqueueMemoryExtractionJob(ctx, EnqueueMemoryExtractionJobParams{
		PersonaID:       "default",
		ChatSessionID:   segment.ChatSessionID,
		SegmentID:       segment.ID,
		MemorySessionID: segment.MemorySessionID,
		Trigger:         MemoryExtractionTriggerSessionEnd,
		Scope:           MemoryExtractionScopeSegment,
		Mode:            "apply",
		RequestedBy:     "user",
		Priority:        50,
		UntilAt:         segment.LastActivityAt,
		RunAfter:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Enqueue duplicate: %v", err)
	}
	if enqueued || duplicate.ID != job.ID {
		t.Fatalf("duplicate job = %#v enqueued=%v, want existing %s", duplicate, enqueued, job.ID)
	}

	claimed, err := db.ClaimMemoryExtractionJobs(ctx, "worker-1", 1, 5*time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("ClaimMemoryExtractionJobs: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != job.ID {
		t.Fatalf("claimed = %#v, want %s", claimed, job.ID)
	}
	if claimed[0].Status != MemoryExtractionJobStatusRunning || claimed[0].Attempts != 1 || claimed[0].ClaimedBy != "worker-1" {
		t.Fatalf("claimed job = %#v, want running attempt 1 worker-1", claimed[0])
	}

	result := map[string]any{"status": "applied", "applied_count": 1}
	resultJSON, _ := json.Marshal(result)
	if err := db.CompleteMemoryExtractionJob(ctx, job.ID, CompleteMemoryExtractionJobParams{
		Status:           MemoryExtractionJobStatusSucceeded,
		ResultJSON:       string(resultJSON),
		ExtractedUntilAt: segment.LastActivityAt,
	}); err != nil {
		t.Fatalf("CompleteMemoryExtractionJob: %v", err)
	}
	completed, err := db.GetMemoryExtractionJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetMemoryExtractionJob: %v", err)
	}
	if completed.Status != MemoryExtractionJobStatusSucceeded || completed.ResultJSON == "" || completed.FinishedAt == "" {
		t.Fatalf("completed job = %#v", completed)
	}
	storedSegment, err = db.GetMemorySegment(ctx, segment.ID)
	if err != nil {
		t.Fatalf("GetMemorySegment(completed): %v", err)
	}
	if storedSegment.ExtractionStatus != MemorySegmentExtractionStatusSucceeded || storedSegment.LastExtractedAt == "" || storedSegment.LastExtractedUntilAt != segment.LastActivityAt {
		t.Fatalf("completed segment = %#v", storedSegment)
	}
}

func TestMemoryExtractionJobDedupeCoalescesManualPinIntoActiveJob(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	segment := createExtractionJobSegment(t, db, "segment-manual-distinct", "chat-manual-distinct", "memory-manual-distinct")

	sessionEnd, enqueued, err := db.EnqueueMemoryExtractionJob(ctx, EnqueueMemoryExtractionJobParams{
		PersonaID:       "default",
		ChatSessionID:   segment.ChatSessionID,
		SegmentID:       segment.ID,
		MemorySessionID: segment.MemorySessionID,
		Trigger:         MemoryExtractionTriggerSessionEnd,
		Scope:           MemoryExtractionScopeSegment,
		Mode:            "apply",
		Priority:        50,
		UntilAt:         segment.LastActivityAt,
		RunAfter:        time.Now().UTC(),
	})
	if err != nil || !enqueued {
		t.Fatalf("enqueue session_end job=%#v enqueued=%v err=%v", sessionEnd, enqueued, err)
	}
	manualPin, enqueued, err := db.EnqueueMemoryExtractionJob(ctx, EnqueueMemoryExtractionJobParams{
		PersonaID:       "default",
		ChatSessionID:   segment.ChatSessionID,
		SegmentID:       segment.ID,
		MemorySessionID: segment.MemorySessionID,
		Trigger:         MemoryExtractionTriggerManualPin,
		Scope:           MemoryExtractionScopeSegment,
		Mode:            "apply",
		Priority:        10,
		EpisodeIDs:      []string{segment.LastUserEpisodeID},
		UntilAt:         segment.LastActivityAt,
		RunAfter:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("enqueue manual_pin job=%#v enqueued=%v err=%v", manualPin, enqueued, err)
	}
	if enqueued || manualPin.ID != sessionEnd.ID {
		t.Fatalf("manual pin job=%#v enqueued=%v, want coalesced existing %s", manualPin, enqueued, sessionEnd.ID)
	}
	if manualPin.Trigger != MemoryExtractionTriggerManualPin || manualPin.Priority != 10 || len(manualPin.EpisodeIDs) != 0 {
		t.Fatalf("manual pin job = %#v, session_end = %#v", manualPin, sessionEnd)
	}
	duplicateManual, enqueued, err := db.EnqueueMemoryExtractionJob(ctx, EnqueueMemoryExtractionJobParams{
		PersonaID:       "default",
		ChatSessionID:   segment.ChatSessionID,
		SegmentID:       segment.ID,
		MemorySessionID: segment.MemorySessionID,
		Trigger:         MemoryExtractionTriggerManualPin,
		Scope:           MemoryExtractionScopeSegment,
		Mode:            "apply",
		Priority:        10,
		EpisodeIDs:      []string{segment.LastUserEpisodeID},
		UntilAt:         segment.LastActivityAt,
		RunAfter:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("enqueue duplicate manual_pin: %v", err)
	}
	if enqueued || duplicateManual.ID != manualPin.ID || duplicateManual.Trigger != MemoryExtractionTriggerManualPin {
		t.Fatalf("duplicate manual pin job=%#v enqueued=%v, want existing %s", duplicateManual, enqueued, manualPin.ID)
	}
}

func TestCompleteExtractionJobMarksSegmentStaleWhenActivityMovesPastWindow(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	segment := createExtractionJobSegment(t, db, "segment-stale-complete", "chat-stale-complete", "memory-stale-complete")
	untilAt := segment.LastActivityAt
	job, _, err := db.EnqueueMemoryExtractionJob(ctx, EnqueueMemoryExtractionJobParams{
		PersonaID:       "default",
		ChatSessionID:   segment.ChatSessionID,
		SegmentID:       segment.ID,
		MemorySessionID: segment.MemorySessionID,
		Trigger:         MemoryExtractionTriggerSessionEnd,
		Scope:           MemoryExtractionScopeSegment,
		Mode:            "apply",
		UntilAt:         untilAt,
		RunAfter:        time.Now().UTC().Add(-time.Second),
	})
	if err != nil {
		t.Fatalf("EnqueueMemoryExtractionJob: %v", err)
	}
	if _, err := db.ClaimMemoryExtractionJobs(ctx, "worker-1", 1, time.Minute, time.Now().UTC()); err != nil {
		t.Fatalf("ClaimMemoryExtractionJobs: %v", err)
	}
	time.Sleep(time.Millisecond)
	if err := db.UpdateMemorySegmentEpisode(ctx, segment.ID, "user", "episode-newer"); err != nil {
		t.Fatalf("UpdateMemorySegmentEpisode: %v", err)
	}
	if err := db.CompleteMemoryExtractionJob(ctx, job.ID, CompleteMemoryExtractionJobParams{
		Status:           MemoryExtractionJobStatusSucceeded,
		ExtractedUntilAt: untilAt,
	}); err != nil {
		t.Fatalf("CompleteMemoryExtractionJob: %v", err)
	}
	storedSegment, err := db.GetMemorySegment(ctx, segment.ID)
	if err != nil {
		t.Fatalf("GetMemorySegment: %v", err)
	}
	if storedSegment.ExtractionStatus != MemorySegmentExtractionStatusStale || storedSegment.LastExtractedUntilAt != untilAt {
		t.Fatalf("completed stale segment = %#v", storedSegment)
	}
}

func TestFailExtractionJobRetriesAndThenFails(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	segment := createExtractionJobSegment(t, db, "segment-fail", "chat-fail", "memory-fail")

	job, _, err := db.EnqueueMemoryExtractionJob(ctx, EnqueueMemoryExtractionJobParams{
		PersonaID:       "default",
		ChatSessionID:   segment.ChatSessionID,
		SegmentID:       segment.ID,
		MemorySessionID: segment.MemorySessionID,
		Trigger:         MemoryExtractionTriggerIdleDetect,
		Scope:           MemoryExtractionScopeSegment,
		Mode:            "apply",
		MaxAttempts:     2,
		RunAfter:        time.Now().UTC().Add(-time.Second),
	})
	if err != nil {
		t.Fatalf("EnqueueMemoryExtractionJob: %v", err)
	}
	if _, err := db.ClaimMemoryExtractionJobs(ctx, "worker-1", 1, time.Minute, time.Now().UTC()); err != nil {
		t.Fatalf("ClaimMemoryExtractionJobs(first): %v", err)
	}
	nextRun := time.Now().UTC().Add(time.Minute)
	if err := db.FailMemoryExtractionJob(ctx, job.ID, FailMemoryExtractionJobParams{
		ErrorCode:    "provider_failed",
		ErrorMessage: "sanitized",
		Retry:        true,
		NextRunAfter: nextRun,
	}); err != nil {
		t.Fatalf("FailMemoryExtractionJob(retry): %v", err)
	}
	retrying, err := db.GetMemoryExtractionJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetMemoryExtractionJob(retry): %v", err)
	}
	if retrying.Status != MemoryExtractionJobStatusPending || retrying.RunAfter == "" || retrying.ErrorCode != "provider_failed" {
		t.Fatalf("retrying job = %#v", retrying)
	}

	if _, err := db.ClaimMemoryExtractionJobs(ctx, "worker-2", 1, time.Minute, nextRun.Add(time.Second)); err != nil {
		t.Fatalf("ClaimMemoryExtractionJobs(second): %v", err)
	}
	if err := db.FailMemoryExtractionJob(ctx, job.ID, FailMemoryExtractionJobParams{
		ErrorCode:    "provider_failed",
		ErrorMessage: "sanitized",
		Retry:        false,
	}); err != nil {
		t.Fatalf("FailMemoryExtractionJob(final): %v", err)
	}
	failed, err := db.GetMemoryExtractionJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetMemoryExtractionJob(final): %v", err)
	}
	if failed.Status != MemoryExtractionJobStatusFailed || failed.FinishedAt == "" {
		t.Fatalf("failed job = %#v", failed)
	}
	storedSegment, err := db.GetMemorySegment(ctx, segment.ID)
	if err != nil {
		t.Fatalf("GetMemorySegment: %v", err)
	}
	if storedSegment.ExtractionStatus != MemorySegmentExtractionStatusFailed || storedSegment.LastExtractionErrorCode != "provider_failed" || storedSegment.ExtractionAttemptCount != 2 {
		t.Fatalf("failed segment = %#v", storedSegment)
	}
}

func TestScanEligibleMemorySegmentsForIdleExtraction(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	now := time.Now().UTC()
	idleAt := now.Add(-20 * time.Minute).Format(time.RFC3339Nano)
	recentAt := now.Add(-2 * time.Minute).Format(time.RFC3339Nano)

	active := createExtractionJobSegment(t, db, "segment-active-idle", "chat-active-idle", "memory-active-idle")
	finalized := createExtractionJobSegment(t, db, "segment-finalized-idle", "chat-finalized-idle", "memory-finalized-idle")
	recent := createExtractionJobSegment(t, db, "segment-recent", "chat-recent", "memory-recent")
	upToDate := createExtractionJobSegment(t, db, "segment-up-to-date", "chat-up-to-date", "memory-up-to-date")
	failed := createExtractionJobSegment(t, db, "segment-failed", "chat-failed", "memory-failed")
	pending := createExtractionJobSegment(t, db, "segment-pending", "chat-pending", "memory-pending")

	setSegmentActivityForTest(t, db, active.ID, idleAt, "", "", MemorySegmentExtractionStatusNever)
	setSegmentActivityForTest(t, db, finalized.ID, idleAt, "", "", MemorySegmentExtractionStatusNever)
	setSegmentActivityForTest(t, db, recent.ID, recentAt, "", "", MemorySegmentExtractionStatusNever)
	setSegmentActivityForTest(t, db, upToDate.ID, idleAt, idleAt, "", MemorySegmentExtractionStatusSucceeded)
	setSegmentActivityForTest(t, db, failed.ID, idleAt, idleAt, "", MemorySegmentExtractionStatusFailed)
	setSegmentActivityForTest(t, db, pending.ID, idleAt, "", "job-pending", MemorySegmentExtractionStatusPending)
	if err := db.FinalizeMemorySegment(ctx, finalized.ID, "session_end", ""); err != nil {
		t.Fatalf("FinalizeMemorySegment: %v", err)
	}
	setSegmentActivityForTest(t, db, finalized.ID, idleAt, "", "", MemorySegmentExtractionStatusNever)

	got, err := db.ScanEligibleMemorySegments(ctx, ScanEligibleMemorySegmentsParams{
		Now:                      now,
		IdleAfter:                15 * time.Minute,
		IncludeActiveSegments:    true,
		IncludeFinalizedSegments: true,
		MinEpisodeCount:          2,
		Limit:                    10,
	})
	if err != nil {
		t.Fatalf("ScanEligibleMemorySegments: %v", err)
	}
	ids := map[string]bool{}
	for _, segment := range got {
		ids[segment.ID] = true
	}
	for _, want := range []string{active.ID, finalized.ID, failed.ID} {
		if !ids[want] {
			t.Fatalf("eligible ids = %#v, missing %s", ids, want)
		}
	}
	for _, unwanted := range []string{recent.ID, upToDate.ID, pending.ID} {
		if ids[unwanted] {
			t.Fatalf("eligible ids = %#v, should not include %s", ids, unwanted)
		}
	}
}

func TestScanEligibleMemorySegmentsSkipsExhaustedFailedSegments(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	now := time.Now().UTC()
	idleAt := now.Add(-20 * time.Minute).Format(time.RFC3339Nano)

	failed := createExtractionJobSegment(t, db, "segment-failed-exhausted", "chat-failed-exhausted", "memory-failed-exhausted")
	setSegmentActivityForTest(t, db, failed.ID, idleAt, idleAt, "", MemorySegmentExtractionStatusFailed)
	_, err := db.SqlDB().Exec(`
		UPDATE memory_segments
		SET extraction_attempt_count = 3
		WHERE id = ?
	`, failed.ID)
	if err != nil {
		t.Fatalf("set segment attempts: %v", err)
	}

	got, err := db.ScanEligibleMemorySegments(ctx, ScanEligibleMemorySegmentsParams{
		Now:                      now,
		IdleAfter:                15 * time.Minute,
		IncludeActiveSegments:    true,
		IncludeFinalizedSegments: true,
		MinEpisodeCount:          2,
		MaxFailedAttempts:        3,
		Limit:                    10,
	})
	if err != nil {
		t.Fatalf("ScanEligibleMemorySegments: %v", err)
	}
	for _, segment := range got {
		if segment.ID == failed.ID {
			t.Fatalf("eligible ids include exhausted failed segment: %#v", got)
		}
	}
}

func createExtractionJobSegment(t *testing.T, db *DB, segmentID string, chatSessionID string, memorySessionID string) *MemorySegment {
	t.Helper()
	ctx := context.Background()
	if err := db.CreateSession(ctx, chatSessionID, "default"); err != nil {
		t.Fatalf("CreateSession(%s): %v", chatSessionID, err)
	}
	segment, err := db.CreateMemorySegment(ctx, CreateMemorySegmentParams{
		ID:              segmentID,
		ChatSessionID:   chatSessionID,
		PersonaID:       "default",
		MemorySessionID: memorySessionID,
	})
	if err != nil {
		t.Fatalf("CreateMemorySegment(%s): %v", segmentID, err)
	}
	if err := db.UpdateMemorySegmentEpisode(ctx, segmentID, "user", segmentID+"-user"); err != nil {
		t.Fatalf("UpdateMemorySegmentEpisode(user): %v", err)
	}
	if err := db.UpdateMemorySegmentEpisode(ctx, segmentID, "assistant", segmentID+"-assistant"); err != nil {
		t.Fatalf("UpdateMemorySegmentEpisode(assistant): %v", err)
	}
	segment, err = db.GetMemorySegment(ctx, segmentID)
	if err != nil {
		t.Fatalf("GetMemorySegment(%s): %v", segmentID, err)
	}
	return segment
}

func setSegmentActivityForTest(t *testing.T, db *DB, segmentID string, lastActivityAt string, lastExtractedUntilAt string, jobID string, status string) {
	t.Helper()
	_, err := db.SqlDB().Exec(`
		UPDATE memory_segments
		SET last_activity_at = ?,
		    last_extracted_until_at = NULLIF(?, ''),
		    last_extraction_job_id = NULLIF(?, ''),
		    extraction_status = ?
		WHERE id = ?
	`, lastActivityAt, lastExtractedUntilAt, jobID, status, segmentID)
	if err != nil {
		t.Fatalf("set segment activity: %v", err)
	}
}
