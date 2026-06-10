package agentaffect

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/storage"
	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) (*SQLiteStore, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.ApplyMigrations(db); err != nil {
		t.Fatalf("ApplyMigrations: %v", err)
	}
	return NewSQLiteStore(db), db
}

func TestAffectJobSchemaCreated(t *testing.T) {
	_, db := newTestStore(t)
	for _, table := range []string{"agent_affect_jobs", "agent_affect_job_batches"} {
		var name string
		if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
			t.Fatalf("table %q not found: %v", table, err)
		}
	}
}

func TestEnqueueTurnEvaluationJobsAssignIncreasingSeq(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC)

	first := enqueueTurnJob(t, store, "default", "session-1", "turn-1", now)
	second := enqueueTurnJob(t, store, "default", "session-1", "turn-2", now)

	if first.Seq == 0 || second.Seq != first.Seq+1 {
		t.Fatalf("seq first=%d second=%d, want increasing", first.Seq, second.Seq)
	}
}

func TestClaimBatchMergesContiguousSameOwnerJobs(t *testing.T) {
	store, db := newTestStore(t)
	now := time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC)
	for i, sessionID := range []string{"session-1", "session-2", "session-3"} {
		enqueueTurnJob(t, store, "default", sessionID, "turn-"+string(rune('1'+i)), now)
	}

	batch, err := store.ClaimNextBatch(context.Background(), "worker-1", now.Add(time.Second), ClaimBatchOptions{
		MaxJobs:  6,
		ClaimTTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("ClaimNextBatch: %v", err)
	}
	if batch == nil || batch.JobCount != 3 || len(batch.JobIDs) != 3 {
		t.Fatalf("batch = %#v, want 3 jobs", batch)
	}

	var running int
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_jobs WHERE status = 'running' AND batch_id = ?", batch.ID).Scan(&running); err != nil {
		t.Fatalf("count running jobs: %v", err)
	}
	if running != 3 {
		t.Fatalf("running jobs = %d, want 3", running)
	}
}

func TestClaimBatchDoesNotMergeDifferentOwners(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC)
	enqueueTurnJob(t, store, "default", "session-1", "turn-1", now)
	enqueueTurnJob(t, store, "other", "session-2", "turn-2", now)
	enqueueTurnJob(t, store, "default", "session-3", "turn-3", now)

	batch, err := store.ClaimNextBatch(context.Background(), "worker-1", now.Add(time.Second), ClaimBatchOptions{
		MaxJobs:  6,
		ClaimTTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("ClaimNextBatch: %v", err)
	}
	if batch == nil || batch.JobCount != 1 || batch.MoodOwnerID != "persona:default" {
		t.Fatalf("batch = %#v, want first owner only", batch)
	}
}

func TestClaimBatchUsesSeqOrderBeforePriority(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC)
	first := enqueueTurnJob(t, store, "default", "session-1", "turn-1", now)
	second := enqueueTurnJob(t, store, "other", "session-2", "turn-2", now)
	if _, err := store.db.Exec("UPDATE agent_affect_jobs SET priority = 999 WHERE id = ?", first.ID); err != nil {
		t.Fatalf("raise first priority: %v", err)
	}
	if _, err := store.db.Exec("UPDATE agent_affect_jobs SET priority = 1 WHERE id = ?", second.ID); err != nil {
		t.Fatalf("lower second priority: %v", err)
	}

	batch, err := store.ClaimNextBatch(context.Background(), "worker-1", now.Add(time.Second), ClaimBatchOptions{
		MaxJobs:  6,
		ClaimTTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("ClaimNextBatch: %v", err)
	}
	if batch == nil || batch.MoodOwnerID != "persona:default" || batch.FirstJobSeq != first.Seq {
		t.Fatalf("batch = %#v, want earliest seq owner despite priority", batch)
	}
}

func TestClaimBatchStopsBeforeBarrier(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC)
	enqueueTurnJob(t, store, "default", "session-1", "turn-1", now)
	enqueueTurnJob(t, store, "default", "session-1", "turn-2", now)
	enqueueBarrierJob(t, store, "default", "session-1", "barrier-1", now)
	enqueueTurnJob(t, store, "default", "session-1", "turn-3", now)

	batch, err := store.ClaimNextBatch(context.Background(), "worker-1", now.Add(time.Second), ClaimBatchOptions{
		MaxJobs:  6,
		ClaimTTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("ClaimNextBatch: %v", err)
	}
	if batch == nil || batch.JobCount != 2 || len(batch.JobIDs) != 2 {
		t.Fatalf("batch = %#v, want two jobs before barrier", batch)
	}
}

func TestMarkBatchDoneUpdatesJobsAndClearsRawPayload(t *testing.T) {
	store, db := newTestStore(t)
	now := time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC)
	for _, turnID := range []string{"turn-1", "turn-2"} {
		enqueueTurnJob(t, store, "default", "session-1", turnID, now)
	}
	batch, err := store.ClaimNextBatch(context.Background(), "worker-1", now.Add(time.Second), ClaimBatchOptions{MaxJobs: 6, ClaimTTL: time.Minute})
	if err != nil {
		t.Fatalf("ClaimNextBatch: %v", err)
	}

	err = store.MarkBatchDone(context.Background(), MarkBatchDoneRequest{
		BatchID:      batch.ID,
		EvaluationID: "eval-1",
		EventID:      "event-1",
		FinishedAt:   now.Add(2 * time.Second),
		ClearRaw:     true,
	})
	if err != nil {
		t.Fatalf("MarkBatchDone: %v", err)
	}

	var done int
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_jobs WHERE status = 'done' AND result_evaluation_id = 'eval-1' AND result_event_id = 'event-1'").Scan(&done); err != nil {
		t.Fatalf("count done jobs: %v", err)
	}
	if done != 2 {
		t.Fatalf("done jobs = %d, want 2", done)
	}
	var rawCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_jobs WHERE COALESCE(user_text, '') <> '' OR COALESCE(assistant_text, '') <> '' OR COALESCE(memory_prompt_block, '') <> ''").Scan(&rawCount); err != nil {
		t.Fatalf("count raw payload: %v", err)
	}
	if rawCount != 0 {
		t.Fatalf("raw payload rows = %d, want cleared", rawCount)
	}
}

func TestSupersedePendingJobsForMoodOwner(t *testing.T) {
	store, db := newTestStore(t)
	now := time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC)
	enqueueTurnJob(t, store, "default", "session-1", "turn-1", now)
	enqueueTurnJob(t, store, "default", "session-2", "turn-2", now)
	enqueueTurnJob(t, store, "other", "session-3", "turn-3", now)

	owner := ResolveMoodOwner(config.DefaultConfig().AgentAffect, "default", "")
	count, err := store.SupersedePendingJobs(context.Background(), SupersedePendingJobsRequest{
		MoodOwner:    owner,
		Reason:       "manual_reset",
		SupersededAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("SupersedePendingJobs: %v", err)
	}
	if count != 2 {
		t.Fatalf("superseded count = %d, want 2", count)
	}
	var otherPending int
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_jobs WHERE mood_owner_id = 'persona:other' AND status = 'pending'").Scan(&otherPending); err != nil {
		t.Fatalf("count other pending: %v", err)
	}
	if otherPending != 1 {
		t.Fatalf("other pending = %d, want 1", otherPending)
	}
}

func TestSupersedePendingJobsAllOwners(t *testing.T) {
	store, db := newTestStore(t)
	now := time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC)
	enqueueTurnJob(t, store, "default", "session-1", "turn-1", now)
	enqueueTurnJob(t, store, "other", "session-2", "turn-2", now)

	count, err := store.SupersedePendingJobs(context.Background(), SupersedePendingJobsRequest{
		All:          true,
		Reason:       "config_change",
		SupersededAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("SupersedePendingJobs all: %v", err)
	}
	if count != 2 {
		t.Fatalf("superseded count = %d, want 2", count)
	}
	var pending int
	if err := db.QueryRow("SELECT COUNT(*) FROM agent_affect_jobs WHERE status = 'pending'").Scan(&pending); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if pending != 0 {
		t.Fatalf("pending = %d, want 0", pending)
	}
}

func enqueueTurnJob(t *testing.T, store *SQLiteStore, personaID, sessionID, turnID string, runAfter time.Time) AffectJobRecord {
	t.Helper()
	job, err := store.EnqueueTurnEvaluationJob(context.Background(), EnqueueTurnEvaluationJobRequest{
		PersonaID:         personaID,
		SessionID:         sessionID,
		TurnID:            turnID,
		UserText:          "user " + turnID,
		AssistantText:     "assistant " + turnID,
		MemoryPromptBlock: "[Memory]\ncontext",
		Trigger:           TriggerDescriptor{TriggerType: "user_message"},
		RunAfter:          runAfter,
		MaxAttempts:       3,
	})
	if err != nil {
		t.Fatalf("EnqueueTurnEvaluationJob: %v", err)
	}
	return job
}

func enqueueBarrierJob(t *testing.T, store *SQLiteStore, personaID, sessionID, barrierID string, runAfter time.Time) AffectJobRecord {
	t.Helper()
	owner := ResolveMoodOwner(config.DefaultConfig().AgentAffect, personaID, sessionID)
	job, err := store.EnqueueAffectJob(context.Background(), EnqueueAffectJobRequest{
		ID:             barrierID,
		PersonaID:      personaID,
		SessionID:      sessionID,
		MoodOwner:      owner,
		JobType:        AffectJobTypeBarrier,
		Batchable:      false,
		BarrierKind:    "manual_delta",
		Status:         AffectJobStatusPending,
		RunAfter:       runAfter,
		MaxAttempts:    1,
		Trigger:        TriggerDescriptor{TriggerType: "debug", CustomType: "manual_delta"},
		TriggerJSONRaw: json.RawMessage(`{"trigger_type":"debug","custom_type":"manual_delta"}`),
	})
	if err != nil {
		t.Fatalf("EnqueueAffectJob barrier: %v", err)
	}
	return job
}
