package memoryhost

import (
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/storage"
)

func TestIdleExtractionSchedulerQueuesEligibleSegmentsOnce(t *testing.T) {
	fixture := openFacadeBridgeFixture(t, "chat-idle-scheduler", &fakeMemoryService{})
	if _, err := fixture.bridge.AppendUserEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-user", "hello"); err != nil {
		t.Fatalf("AppendUserEpisode: %v", err)
	}
	if _, err := fixture.bridge.AppendAssistantEpisode(fixture.ctx, fixture.segment.SegmentID, "msg-assistant", "hi"); err != nil {
		t.Fatalf("AppendAssistantEpisode: %v", err)
	}
	idleAt := time.Now().UTC().Add(-20 * time.Minute).Format(time.RFC3339Nano)
	if _, err := fixture.db.SqlDB().Exec(`
		UPDATE memory_segments
		SET last_activity_at = ?,
		    extraction_status = 'never'
		WHERE id = ?
	`, idleAt, fixture.segment.SegmentID); err != nil {
		t.Fatalf("set idle segment: %v", err)
	}
	scheduler := NewIdleExtractionScheduler(fixture.serviceHost(), fixture.db, testMemoryLogger(), IdleExtractionSchedulerConfig{
		IdleAfter:                15 * time.Minute,
		MaxSegmentsPerSweep:      10,
		MinEpisodeCount:          2,
		IncludeActiveSegments:    true,
		IncludeFinalizedSegments: true,
		Mode:                     memorycore.ExtractionRunModeApply,
	})

	queued, err := scheduler.RunOnce(fixture.ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if queued != 1 {
		t.Fatalf("queued = %d, want 1", queued)
	}
	jobs, err := fixture.db.ListMemoryExtractionJobs(fixture.ctx, storage.ListMemoryExtractionJobsFilter{SegmentID: fixture.segment.SegmentID, Limit: 10})
	if err != nil {
		t.Fatalf("ListMemoryExtractionJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Trigger != storage.MemoryExtractionTriggerIdleDetect || jobs[0].Status != storage.MemoryExtractionJobStatusPending {
		t.Fatalf("jobs = %#v, want one pending idle_detect job", jobs)
	}

	queued, err = scheduler.RunOnce(fixture.ctx)
	if err != nil {
		t.Fatalf("RunOnce(second): %v", err)
	}
	if queued != 0 {
		t.Fatalf("queued second sweep = %d, want 0", queued)
	}
}
