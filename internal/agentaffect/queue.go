package agentaffect

import (
	"context"
)

func (r *Runtime) QueueStatus(ctx context.Context, q JobQueueQuery) (QueueStatusResponse, error) {
	if r.store == nil {
		return QueueStatusResponse{}, nil
	}
	q = r.normalizeQueueQuery(q)
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	jobs, err := r.store.ListJobs(ctx, q)
	if err != nil {
		return QueueStatusResponse{}, err
	}
	batches, err := r.store.ListBatches(ctx, BatchQuery{
		PersonaID:      q.PersonaID,
		MoodOwnerScope: q.MoodOwnerScope,
		MoodOwnerID:    q.MoodOwnerID,
		Limit:          20,
	})
	if err != nil {
		return QueueStatusResponse{}, err
	}
	var latest *AffectJobBatchRecord
	if len(batches) > 0 {
		copy := batches[0]
		latest = &copy
	}
	pending, err := r.countJobs(ctx, q, AffectJobStatusPending)
	if err != nil {
		return QueueStatusResponse{}, err
	}
	running, err := r.countJobs(ctx, q, AffectJobStatusRunning)
	if err != nil {
		return QueueStatusResponse{}, err
	}
	failed, err := r.countJobs(ctx, q, AffectJobStatusFailed)
	if err != nil {
		return QueueStatusResponse{}, err
	}
	return QueueStatusResponse{
		PendingJobs: pending,
		RunningJobs: running,
		FailedJobs:  failed,
		LatestBatch: latest,
		Jobs:        jobs,
		Batches:     batches,
	}, nil
}

func (r *Runtime) ClearFailedJobs(ctx context.Context, q JobQueueQuery) (ClearFailedJobsResponse, error) {
	if r.store == nil {
		return ClearFailedJobsResponse{}, nil
	}
	cleared, err := r.store.ClearFailedJobs(ctx, r.normalizeQueueQuery(q))
	if err != nil {
		return ClearFailedJobsResponse{}, err
	}
	return ClearFailedJobsResponse{Cleared: cleared}, nil
}

func (r *Runtime) SupersedePendingQueue(ctx context.Context, q JobQueueQuery, reason string) (SupersedePendingJobsResponse, error) {
	if r.store == nil {
		return SupersedePendingJobsResponse{}, nil
	}
	q = r.normalizeQueueQuery(q)
	req := SupersedePendingJobsRequest{
		MoodOwner:    MoodOwner{Scope: q.MoodOwnerScope, ID: q.MoodOwnerID},
		PersonaID:    q.PersonaID,
		Reason:       defaultString(reason, "admin_supersede_pending"),
		SupersededAt: r.now(),
	}
	count, err := r.store.SupersedePendingJobs(ctx, req)
	if err != nil {
		return SupersedePendingJobsResponse{}, err
	}
	return SupersedePendingJobsResponse{Superseded: count}, nil
}

func (r *Runtime) SupersedeAllPending(ctx context.Context, reason string) (int, error) {
	if r.store == nil {
		return 0, nil
	}
	return r.store.SupersedePendingJobs(ctx, SupersedePendingJobsRequest{
		All:          true,
		Reason:       defaultString(reason, "config_change"),
		SupersededAt: r.now(),
	})
}

func (r *Runtime) normalizeQueueQuery(q JobQueueQuery) JobQueueQuery {
	if q.PersonaID == "" {
		q.PersonaID = "default"
	}
	if q.MoodOwnerScope == "" || q.MoodOwnerID == "" {
		owner := ResolveMoodOwner(r.cfg, q.PersonaID, q.SessionID)
		q.MoodOwnerScope = owner.Scope
		q.MoodOwnerID = owner.ID
	}
	if q.Limit <= 0 {
		q.Limit = 50
	}
	return q
}

func (r *Runtime) countJobs(ctx context.Context, base JobQueueQuery, status string) (int, error) {
	base.Status = status
	base.Limit = 100000
	jobs, err := r.store.ListJobs(ctx, base)
	if err != nil {
		return 0, err
	}
	return len(jobs), nil
}
