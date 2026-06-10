package agentaffect

import (
	"context"
	"time"
)

type Store interface {
	EnsureProfile(ctx context.Context, personaID string) (AffectProfile, error)
	UpsertProfile(ctx context.Context, profile AffectProfile) (AffectProfile, error)
	GetLatestState(ctx context.Context, personaID string, sessionID string) (*MoodSnapshot, error)
	GetLatestStateByOwner(ctx context.Context, personaID string, owner MoodOwner) (*MoodSnapshot, error)
	InsertState(ctx context.Context, state MoodSnapshot) error
	InsertEvaluation(ctx context.Context, eval AffectEvaluationRecord) error
	MarkEvaluationCommitted(ctx context.Context, evaluationID string, afterStateID string) error
	InsertEvent(ctx context.Context, event AffectEventRecord) error
	CommitStateEvent(ctx context.Context, state MoodSnapshot, event AffectEventRecord) error
	EnqueueTurnEvaluationJob(ctx context.Context, req EnqueueTurnEvaluationJobRequest) (AffectJobRecord, error)
	EnqueueAffectJob(ctx context.Context, req EnqueueAffectJobRequest) (AffectJobRecord, error)
	ClaimNextBatch(ctx context.Context, workerID string, now time.Time, opts ClaimBatchOptions) (*AffectJobBatchRecord, error)
	ListJobsByBatch(ctx context.Context, batchID string) ([]AffectJobRecord, error)
	ListJobs(ctx context.Context, q JobQueueQuery) ([]AffectJobRecord, error)
	ListBatches(ctx context.Context, q BatchQuery) ([]AffectJobBatchRecord, error)
	CommitBatchEvaluation(ctx context.Context, req CommitBatchEvaluationRequest) error
	MarkBatchDone(ctx context.Context, req MarkBatchDoneRequest) error
	MarkBatchFailed(ctx context.Context, req MarkBatchFailedRequest) error
	SupersedePendingJobs(ctx context.Context, req SupersedePendingJobsRequest) (int, error)
	ClearFailedJobs(ctx context.Context, q JobQueueQuery) (int, error)
	InsertPluginWrite(ctx context.Context, write PluginWriteRecord) error
	ListRecentEvaluations(ctx context.Context, q RecentEvaluationsQuery) ([]AffectEvaluationRecord, error)
	ListRecentEvents(ctx context.Context, q RecentEventsQuery) ([]AffectEventRecord, error)
	ListPluginWrites(ctx context.Context, q PluginWritesQuery) ([]PluginWriteRecord, error)
}
