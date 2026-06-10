package agentaffect

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent/internal/config"
)

const affectJobSelectColumns = `
seq, id, persona_id, COALESCE(session_id, ''), COALESCE(turn_id, ''),
mood_owner_scope, mood_owner_id, job_type, batchable, barrier_kind,
status, priority, run_after, attempts, max_attempts,
COALESCE(claimed_by, ''), COALESCE(claimed_until, ''),
trigger_json, input_mode, COALESCE(user_text, ''), COALESCE(assistant_text, ''),
COALESCE(input_summary, ''), COALESCE(memory_prompt_block, ''),
COALESCE(base_state_id, ''), COALESCE(base_state_updated_at, ''),
COALESCE(batch_id, ''), COALESCE(result_evaluation_id, ''), COALESCE(result_event_id, ''),
COALESCE(error_message, ''), created_at, COALESCE(started_at, ''), COALESCE(finished_at, '')
`

const affectBatchSelectColumns = `
id, persona_id, mood_owner_scope, mood_owner_id, job_type, status,
job_count, first_job_seq, last_job_seq, job_ids_json, session_ids_json, turn_ids_json,
batch_input_summary, COALESCE(context_window_snapshot_json, ''),
COALESCE(evaluation_id, ''), COALESCE(affect_event_id, ''), COALESCE(error_message, ''),
COALESCE(claimed_by, ''), started_at, COALESCE(finished_at, '')
`

var ErrAffectBatchNotRunning = errors.New("agent affect batch is not running")

type sqlScanner interface {
	Scan(dest ...any) error
}

func (s *SQLiteStore) EnqueueTurnEvaluationJob(ctx context.Context, req EnqueueTurnEvaluationJobRequest) (AffectJobRecord, error) {
	owner := req.MoodOwner
	if owner.Scope == "" || owner.ID == "" {
		owner = ResolveMoodOwner(config.DefaultConfig().AgentAffect, req.PersonaID, req.SessionID)
	}
	return s.EnqueueAffectJob(ctx, EnqueueAffectJobRequest{
		PersonaID:          req.PersonaID,
		SessionID:          req.SessionID,
		TurnID:             req.TurnID,
		MoodOwner:          owner,
		JobType:            AffectJobTypeTurnEvaluate,
		Batchable:          true,
		Status:             AffectJobStatusPending,
		RunAfter:           req.RunAfter,
		MaxAttempts:        req.MaxAttempts,
		Trigger:            req.Trigger,
		InputMode:          "mixed",
		UserText:           req.UserText,
		AssistantText:      req.AssistantText,
		InputSummary:       req.InputSummary,
		MemoryPromptBlock:  req.MemoryPromptBlock,
		BaseStateID:        req.BaseStateID,
		BaseStateUpdatedAt: req.BaseStateUpdatedAt,
	})
}

func (s *SQLiteStore) EnqueueAffectJob(ctx context.Context, req EnqueueAffectJobRequest) (AffectJobRecord, error) {
	if req.PersonaID == "" {
		return AffectJobRecord{}, fmt.Errorf("persona_id is required")
	}
	owner := req.MoodOwner
	if owner.Scope == "" || owner.ID == "" {
		owner = ResolveMoodOwner(config.DefaultConfig().AgentAffect, req.PersonaID, req.SessionID)
	}
	jobID := req.ID
	if jobID == "" {
		jobID = uuid.NewString()
	}
	jobType := defaultString(req.JobType, AffectJobTypeTurnEvaluate)
	status := defaultString(req.Status, AffectJobStatusPending)
	priority := req.Priority
	if priority == 0 {
		priority = 100
	}
	maxAttempts := req.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 3
	}
	runAfter := req.RunAfter
	if runAfter.IsZero() {
		runAfter = time.Now().UTC()
	}
	inputMode := defaultString(req.InputMode, "mixed")
	triggerJSON := string(req.TriggerJSONRaw)
	if triggerJSON == "" {
		triggerJSON = mustJSON(normalizeTrigger(req.Trigger))
	}
	baseStateUpdatedAt := ""
	if !req.BaseStateUpdatedAt.IsZero() {
		baseStateUpdatedAt = dbTime(req.BaseStateUpdatedAt)
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO agent_affect_jobs (
    id, persona_id, session_id, turn_id,
    mood_owner_scope, mood_owner_id,
    job_type, batchable, barrier_kind, status, priority, run_after,
    max_attempts, trigger_json, input_mode,
    user_text, assistant_text, input_summary, memory_prompt_block,
    base_state_id, base_state_updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, jobID, req.PersonaID, nilIfEmpty(req.SessionID), nilIfEmpty(req.TurnID),
		owner.Scope, owner.ID,
		jobType, boolInt(req.Batchable), req.BarrierKind, status, priority, dbTime(runAfter),
		maxAttempts, triggerJSON, inputMode,
		nilIfEmpty(req.UserText), nilIfEmpty(req.AssistantText), nilIfEmpty(req.InputSummary), nilIfEmpty(req.MemoryPromptBlock),
		nilIfEmpty(req.BaseStateID), nilIfEmpty(baseStateUpdatedAt),
	)
	if err != nil {
		return AffectJobRecord{}, fmt.Errorf("enqueue affect job: %w", err)
	}
	return s.getJobByID(ctx, jobID)
}

func (s *SQLiteStore) ClaimNextBatch(ctx context.Context, workerID string, now time.Time, opts ClaimBatchOptions) (*AffectJobBatchRecord, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	maxJobs := opts.MaxJobs
	if maxJobs <= 0 {
		maxJobs = 1
	}
	claimTTL := opts.ClaimTTL
	if claimTTL <= 0 {
		claimTTL = 5 * time.Minute
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin affect batch claim: %w", err)
	}
	defer tx.Rollback()

	if err := releaseExpiredAffectClaims(ctx, tx, now); err != nil {
		return nil, err
	}
	first, ok, err := getFirstClaimableJob(ctx, tx, now, opts.MinWait)
	if err != nil {
		return nil, err
	}
	if !ok {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit empty affect batch claim: %w", err)
		}
		return nil, nil
	}
	jobs, err := selectContiguousClaimJobs(ctx, tx, first, now, opts)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit empty affect batch candidates: %w", err)
		}
		return nil, nil
	}
	batch := buildBatchRecord(jobs, workerID, now)
	if err := insertBatch(ctx, tx, batch); err != nil {
		return nil, err
	}
	claimedUntil := now.Add(claimTTL)
	for _, job := range jobs {
		if _, err := tx.ExecContext(ctx, `
UPDATE agent_affect_jobs
SET status = ?, batch_id = ?, claimed_by = ?, claimed_until = ?, started_at = ?, attempts = attempts + 1
WHERE id = ? AND status = ?
`, AffectJobStatusRunning, batch.ID, workerID, dbTime(claimedUntil), dbTime(now), job.ID, AffectJobStatusPending); err != nil {
			return nil, fmt.Errorf("mark affect job running: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit affect batch claim: %w", err)
	}
	return &batch, nil
}

func (s *SQLiteStore) MarkBatchDone(ctx context.Context, req MarkBatchDoneRequest) error {
	if req.BatchID == "" {
		return fmt.Errorf("batch_id is required")
	}
	finishedAt := req.FinishedAt
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin mark affect batch done: %w", err)
	}
	defer tx.Rollback()
	req.FinishedAt = finishedAt
	if err := markBatchDoneTx(ctx, tx, req); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit mark affect batch done: %w", err)
	}
	return nil
}

func releaseExpiredAffectClaims(ctx context.Context, tx *sql.Tx, now time.Time) error {
	rows, err := tx.QueryContext(ctx, `
SELECT DISTINCT COALESCE(batch_id, '')
FROM agent_affect_jobs
WHERE status = ? AND claimed_until IS NOT NULL AND claimed_until <= ?
`, AffectJobStatusRunning, dbTime(now))
	if err != nil {
		return fmt.Errorf("select expired affect claims: %w", err)
	}
	var batchIDs []string
	for rows.Next() {
		var batchID string
		if err := rows.Scan(&batchID); err != nil {
			rows.Close()
			return fmt.Errorf("scan expired affect batch: %w", err)
		}
		if batchID != "" {
			batchIDs = append(batchIDs, batchID)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate expired affect batches: %w", err)
	}
	rows.Close()
	for _, batchID := range batchIDs {
		if _, err := tx.ExecContext(ctx, `
UPDATE agent_affect_job_batches
SET status = ?, error_message = ?, finished_at = ?
WHERE id = ? AND status = ?
`, AffectBatchStatusFailed, "claim_expired", dbTime(now), batchID, AffectBatchStatusRunning); err != nil {
			return fmt.Errorf("mark expired affect batch failed: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE agent_affect_jobs
SET status = ?, batch_id = NULL, claimed_by = NULL, claimed_until = NULL, started_at = NULL, error_message = ?
WHERE status = ? AND claimed_until IS NOT NULL AND claimed_until <= ?
`, AffectJobStatusPending, "claim_expired", AffectJobStatusRunning, dbTime(now)); err != nil {
		return fmt.Errorf("release expired affect jobs: %w", err)
	}
	return nil
}

func markBatchDoneTx(ctx context.Context, tx *sql.Tx, req MarkBatchDoneRequest) error {
	finishedAt := req.FinishedAt
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE agent_affect_job_batches
SET status = ?, evaluation_id = ?, affect_event_id = ?, finished_at = ?, error_message = NULL
WHERE id = ?
`, AffectBatchStatusDone, nilIfEmpty(req.EvaluationID), nilIfEmpty(req.EventID), dbTime(finishedAt), req.BatchID); err != nil {
		return fmt.Errorf("mark affect batch done: %w", err)
	}
	updateSQL := `
UPDATE agent_affect_jobs
SET status = ?, result_evaluation_id = ?, result_event_id = ?, finished_at = ?, error_message = NULL
WHERE batch_id = ?
`
	args := []any{AffectJobStatusDone, nilIfEmpty(req.EvaluationID), nilIfEmpty(req.EventID), dbTime(finishedAt), req.BatchID}
	if req.ClearRaw {
		updateSQL = `
UPDATE agent_affect_jobs
SET status = ?, result_evaluation_id = ?, result_event_id = ?, finished_at = ?, error_message = NULL,
    user_text = NULL, assistant_text = NULL, memory_prompt_block = NULL
WHERE batch_id = ?
`
	}
	if _, err := tx.ExecContext(ctx, updateSQL, args...); err != nil {
		return fmt.Errorf("mark affect jobs done: %w", err)
	}
	return nil
}

func (s *SQLiteStore) MarkBatchFailed(ctx context.Context, req MarkBatchFailedRequest) error {
	if req.BatchID == "" {
		return fmt.Errorf("batch_id is required")
	}
	finishedAt := req.FinishedAt
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin mark affect batch failed: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
UPDATE agent_affect_job_batches
SET status = ?, error_message = ?, finished_at = ?
WHERE id = ?
`, AffectBatchStatusFailed, req.ErrorMessage, dbTime(finishedAt), req.BatchID); err != nil {
		return fmt.Errorf("mark affect batch failed: %w", err)
	}
	if req.Retry {
		retryAt := req.RetryAt
		if retryAt.IsZero() {
			retryAt = finishedAt
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE agent_affect_jobs
SET status = ?, batch_id = NULL, claimed_by = NULL, claimed_until = NULL, run_after = ?, error_message = ?, started_at = NULL, finished_at = NULL
WHERE batch_id = ? AND attempts < max_attempts
`, AffectJobStatusPending, dbTime(retryAt), req.ErrorMessage, req.BatchID); err != nil {
			return fmt.Errorf("schedule affect jobs retry: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE agent_affect_jobs
SET status = ?, error_message = ?, finished_at = ?
WHERE batch_id = ? AND attempts >= max_attempts
`, AffectJobStatusFailed, req.ErrorMessage, dbTime(finishedAt), req.BatchID); err != nil {
			return fmt.Errorf("mark exhausted affect jobs failed: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
UPDATE agent_affect_jobs
SET status = ?, error_message = ?, finished_at = ?
WHERE batch_id = ?
`, AffectJobStatusFailed, req.ErrorMessage, dbTime(finishedAt), req.BatchID); err != nil {
			return fmt.Errorf("mark affect jobs failed: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit mark affect batch failed: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListJobsByBatch(ctx context.Context, batchID string) ([]AffectJobRecord, error) {
	if batchID == "" {
		return nil, fmt.Errorf("batch_id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT `+affectJobSelectColumns+`
FROM agent_affect_jobs
WHERE batch_id = ?
ORDER BY seq ASC
`, batchID)
	if err != nil {
		return nil, fmt.Errorf("list affect jobs by batch: %w", err)
	}
	defer rows.Close()
	return scanAffectJobs(rows)
}

func (s *SQLiteStore) ListJobs(ctx context.Context, q JobQueueQuery) ([]AffectJobRecord, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	conditions := []string{"1 = 1"}
	args := []any{}
	if q.PersonaID != "" {
		conditions = append(conditions, "persona_id = ?")
		args = append(args, q.PersonaID)
	}
	if q.SessionID != "" {
		conditions = append(conditions, "COALESCE(session_id, '') = ?")
		args = append(args, q.SessionID)
	}
	if q.MoodOwnerScope != "" {
		conditions = append(conditions, "mood_owner_scope = ?")
		args = append(args, q.MoodOwnerScope)
	}
	if q.MoodOwnerID != "" {
		conditions = append(conditions, "mood_owner_id = ?")
		args = append(args, q.MoodOwnerID)
	}
	if q.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, q.Status)
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `
SELECT `+affectJobSelectColumns+`
FROM agent_affect_jobs
WHERE `+strings.Join(conditions, " AND ")+`
ORDER BY seq DESC
LIMIT ?
`, args...)
	if err != nil {
		return nil, fmt.Errorf("list affect jobs: %w", err)
	}
	defer rows.Close()
	return scanAffectJobs(rows)
}

func (s *SQLiteStore) ListBatches(ctx context.Context, q BatchQuery) ([]AffectJobBatchRecord, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 30
	}
	conditions := []string{"1 = 1"}
	args := []any{}
	if q.PersonaID != "" {
		conditions = append(conditions, "persona_id = ?")
		args = append(args, q.PersonaID)
	}
	if q.MoodOwnerScope != "" {
		conditions = append(conditions, "mood_owner_scope = ?")
		args = append(args, q.MoodOwnerScope)
	}
	if q.MoodOwnerID != "" {
		conditions = append(conditions, "mood_owner_id = ?")
		args = append(args, q.MoodOwnerID)
	}
	if q.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, q.Status)
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `
SELECT `+affectBatchSelectColumns+`
FROM agent_affect_job_batches
WHERE `+strings.Join(conditions, " AND ")+`
ORDER BY started_at DESC
LIMIT ?
`, args...)
	if err != nil {
		return nil, fmt.Errorf("list affect batches: %w", err)
	}
	defer rows.Close()
	var out []AffectJobBatchRecord
	for rows.Next() {
		batch, err := scanAffectBatch(rows)
		if err != nil {
			return nil, fmt.Errorf("scan affect batch: %w", err)
		}
		out = append(out, batch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate affect batches: %w", err)
	}
	return out, nil
}

func (s *SQLiteStore) CommitBatchEvaluation(ctx context.Context, req CommitBatchEvaluationRequest) error {
	if req.BatchID == "" {
		return fmt.Errorf("batch_id is required")
	}
	finishedAt := req.FinishedAt
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin commit affect batch evaluation: %w", err)
	}
	defer tx.Rollback()

	var expectedJobs int
	err = tx.QueryRowContext(ctx, `
SELECT job_count
FROM agent_affect_job_batches
WHERE id = ? AND status = ?
`, req.BatchID, AffectBatchStatusRunning).Scan(&expectedJobs)
	if err == sql.ErrNoRows {
		return ErrAffectBatchNotRunning
	}
	if err != nil {
		return fmt.Errorf("read affect batch status: %w", err)
	}
	var runningJobs int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM agent_affect_jobs
WHERE batch_id = ? AND status = ?
`, req.BatchID, AffectJobStatusRunning).Scan(&runningJobs); err != nil {
		return fmt.Errorf("count running affect batch jobs: %w", err)
	}
	if expectedJobs <= 0 || runningJobs != expectedJobs {
		return ErrAffectBatchNotRunning
	}

	if err := insertEvaluation(ctx, tx, req.Evaluation); err != nil {
		return err
	}
	if err := insertState(ctx, tx, req.State); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, req.Event); err != nil {
		return err
	}
	if err := markBatchDoneTx(ctx, tx, MarkBatchDoneRequest{
		BatchID:      req.BatchID,
		EvaluationID: req.Evaluation.ID,
		EventID:      req.Event.ID,
		FinishedAt:   finishedAt,
		ClearRaw:     req.ClearRaw,
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit affect batch evaluation: %w", err)
	}
	return nil
}

func (s *SQLiteStore) SupersedePendingJobs(ctx context.Context, req SupersedePendingJobsRequest) (int, error) {
	finishedAt := req.SupersededAt
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}
	conditions := []string{"status = ?"}
	args := []any{AffectJobStatusPending}
	if req.MoodOwner.Scope != "" && req.MoodOwner.ID != "" {
		conditions = append(conditions, "mood_owner_scope = ?", "mood_owner_id = ?")
		args = append(args, req.MoodOwner.Scope, req.MoodOwner.ID)
	} else if req.PersonaID != "" {
		conditions = append(conditions, "persona_id = ?")
		args = append(args, req.PersonaID)
	} else if !req.All {
		return 0, fmt.Errorf("mood_owner, persona_id, or all=true is required")
	}
	args = append([]any{AffectJobStatusSuperseded, req.Reason, dbTime(finishedAt)}, args...)
	result, err := s.db.ExecContext(ctx, `
UPDATE agent_affect_jobs
SET status = ?, error_message = ?, finished_at = ?
WHERE `+strings.Join(conditions, " AND "), args...)
	if err != nil {
		return 0, fmt.Errorf("supersede pending affect jobs: %w", err)
	}
	count, _ := result.RowsAffected()
	return int(count), nil
}

func (s *SQLiteStore) ClearFailedJobs(ctx context.Context, q JobQueueQuery) (int, error) {
	conditions := []string{"status = ?"}
	args := []any{AffectJobStatusFailed}
	if q.PersonaID != "" {
		conditions = append(conditions, "persona_id = ?")
		args = append(args, q.PersonaID)
	}
	if q.SessionID != "" {
		conditions = append(conditions, "COALESCE(session_id, '') = ?")
		args = append(args, q.SessionID)
	}
	if q.MoodOwnerScope != "" {
		conditions = append(conditions, "mood_owner_scope = ?")
		args = append(args, q.MoodOwnerScope)
	}
	if q.MoodOwnerID != "" {
		conditions = append(conditions, "mood_owner_id = ?")
		args = append(args, q.MoodOwnerID)
	}
	result, err := s.db.ExecContext(ctx, `
DELETE FROM agent_affect_jobs
WHERE `+strings.Join(conditions, " AND "), args...)
	if err != nil {
		return 0, fmt.Errorf("clear failed affect jobs: %w", err)
	}
	count, _ := result.RowsAffected()
	return int(count), nil
}

func (s *SQLiteStore) getJobByID(ctx context.Context, id string) (AffectJobRecord, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+affectJobSelectColumns+" FROM agent_affect_jobs WHERE id = ?", id)
	job, err := scanAffectJob(row)
	if err != nil {
		return AffectJobRecord{}, fmt.Errorf("get affect job: %w", err)
	}
	return job, nil
}

func getFirstClaimableJob(ctx context.Context, tx *sql.Tx, now time.Time, minWait time.Duration) (AffectJobRecord, bool, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT `+affectJobSelectColumns+`
FROM agent_affect_jobs
WHERE status = ? AND run_after <= ?
ORDER BY priority ASC, seq ASC
LIMIT 100
`, AffectJobStatusPending, dbTime(now))
	if err != nil {
		return AffectJobRecord{}, false, fmt.Errorf("select first eligible affect jobs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		job, err := scanAffectJob(rows)
		if err != nil {
			return AffectJobRecord{}, false, fmt.Errorf("scan first eligible affect job: %w", err)
		}
		if minWait > 0 && !job.CreatedAt.IsZero() && now.Sub(job.CreatedAt) < minWait {
			continue
		}
		var running int
		if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM agent_affect_jobs
WHERE mood_owner_scope = ? AND mood_owner_id = ? AND status = ?
`, job.MoodOwnerScope, job.MoodOwnerID, AffectJobStatusRunning).Scan(&running); err != nil {
			return AffectJobRecord{}, false, fmt.Errorf("check running affect jobs: %w", err)
		}
		if running == 0 {
			return job, true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return AffectJobRecord{}, false, fmt.Errorf("iterate first eligible affect jobs: %w", err)
	}
	return AffectJobRecord{}, false, nil
}

func selectContiguousClaimJobs(ctx context.Context, tx *sql.Tx, first AffectJobRecord, now time.Time, opts ClaimBatchOptions) ([]AffectJobRecord, error) {
	maxJobs := opts.MaxJobs
	if maxJobs <= 0 {
		maxJobs = 1
	}
	rows, err := tx.QueryContext(ctx, `
SELECT `+affectJobSelectColumns+`
FROM agent_affect_jobs
WHERE status = ? AND run_after <= ? AND seq >= ?
ORDER BY seq ASC
LIMIT ?
`, AffectJobStatusPending, dbTime(now), first.Seq, maxJobs+1)
	if err != nil {
		return nil, fmt.Errorf("select affect batch candidates: %w", err)
	}
	defer rows.Close()

	jobs := make([]AffectJobRecord, 0, maxJobs)
	tokenBudget := opts.MaxInputTokens
	usedTokens := 0
	for rows.Next() {
		job, err := scanAffectJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan affect batch candidate: %w", err)
		}
		if job.MoodOwnerScope != first.MoodOwnerScope || job.MoodOwnerID != first.MoodOwnerID || job.JobType != first.JobType {
			break
		}
		if opts.SplitSessions && job.SessionID != first.SessionID {
			break
		}
		if opts.MaxAge > 0 && !job.CreatedAt.IsZero() && !first.CreatedAt.IsZero() && job.CreatedAt.Sub(first.CreatedAt) > opts.MaxAge {
			break
		}
		jobTokens := roughJobTokenEstimate(job)
		if tokenBudget > 0 && len(jobs) > 0 && usedTokens+jobTokens > tokenBudget {
			break
		}
		if job.JobType == AffectJobTypeBarrier || !job.Batchable {
			if len(jobs) == 0 {
				jobs = append(jobs, job)
			}
			break
		}
		jobs = append(jobs, job)
		usedTokens += jobTokens
		if len(jobs) >= maxJobs {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate affect batch candidates: %w", err)
	}
	return jobs, nil
}

func buildBatchRecord(jobs []AffectJobRecord, workerID string, startedAt time.Time) AffectJobBatchRecord {
	first := jobs[0]
	last := jobs[len(jobs)-1]
	jobIDs := make([]string, 0, len(jobs))
	sessionIDs := make([]string, 0, len(jobs))
	turnIDs := make([]string, 0, len(jobs))
	for _, job := range jobs {
		jobIDs = append(jobIDs, job.ID)
		if job.SessionID != "" {
			sessionIDs = append(sessionIDs, job.SessionID)
		}
		if job.TurnID != "" {
			turnIDs = append(turnIDs, job.TurnID)
		}
	}
	return AffectJobBatchRecord{
		ID:                uuid.NewString(),
		PersonaID:         first.PersonaID,
		MoodOwnerScope:    first.MoodOwnerScope,
		MoodOwnerID:       first.MoodOwnerID,
		JobType:           first.JobType,
		Status:            AffectBatchStatusRunning,
		JobCount:          len(jobs),
		FirstJobSeq:       first.Seq,
		LastJobSeq:        last.Seq,
		JobIDs:            jobIDs,
		SessionIDs:        sessionIDs,
		TurnIDs:           turnIDs,
		BatchInputSummary: summarizeJobsForBatch(jobs, 2000),
		ClaimedBy:         workerID,
		StartedAt:         startedAt,
	}
}

func insertBatch(ctx context.Context, tx *sql.Tx, batch AffectJobBatchRecord) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO agent_affect_job_batches (
    id, persona_id, mood_owner_scope, mood_owner_id, job_type, status,
    job_count, first_job_seq, last_job_seq, job_ids_json, session_ids_json, turn_ids_json,
    batch_input_summary, context_window_snapshot_json, claimed_by, started_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, batch.ID, batch.PersonaID, batch.MoodOwnerScope, batch.MoodOwnerID, batch.JobType, batch.Status,
		batch.JobCount, batch.FirstJobSeq, batch.LastJobSeq, mustJSON(batch.JobIDs), mustJSON(batch.SessionIDs), mustJSON(batch.TurnIDs),
		batch.BatchInputSummary, nilIfEmpty(batch.ContextWindowSnapshotJSON), nilIfEmpty(batch.ClaimedBy), dbTime(batch.StartedAt),
	)
	if err != nil {
		return fmt.Errorf("insert affect job batch: %w", err)
	}
	return nil
}

func scanAffectJobs(rows *sql.Rows) ([]AffectJobRecord, error) {
	var out []AffectJobRecord
	for rows.Next() {
		job, err := scanAffectJob(rows)
		if err != nil {
			return nil, fmt.Errorf("scan affect job: %w", err)
		}
		out = append(out, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate affect jobs: %w", err)
	}
	return out, nil
}

func scanAffectJob(scanner sqlScanner) (AffectJobRecord, error) {
	var job AffectJobRecord
	var batchable int
	var runAfter, claimedUntil, triggerJSON, baseStateUpdatedAt, createdAt, startedAt, finishedAt string
	err := scanner.Scan(
		&job.Seq,
		&job.ID,
		&job.PersonaID,
		&job.SessionID,
		&job.TurnID,
		&job.MoodOwnerScope,
		&job.MoodOwnerID,
		&job.JobType,
		&batchable,
		&job.BarrierKind,
		&job.Status,
		&job.Priority,
		&runAfter,
		&job.Attempts,
		&job.MaxAttempts,
		&job.ClaimedBy,
		&claimedUntil,
		&triggerJSON,
		&job.InputMode,
		&job.UserText,
		&job.AssistantText,
		&job.InputSummary,
		&job.MemoryPromptBlock,
		&job.BaseStateID,
		&baseStateUpdatedAt,
		&job.BatchID,
		&job.ResultEvaluationID,
		&job.ResultEventID,
		&job.ErrorMessage,
		&createdAt,
		&startedAt,
		&finishedAt,
	)
	if err != nil {
		return AffectJobRecord{}, err
	}
	job.Batchable = batchable != 0
	_ = json.Unmarshal([]byte(triggerJSON), &job.Trigger)
	job.RunAfter = parseDBTime(runAfter)
	job.ClaimedUntil = parseDBTime(claimedUntil)
	job.BaseStateUpdatedAt = parseDBTime(baseStateUpdatedAt)
	job.CreatedAt = parseDBTime(createdAt)
	job.StartedAt = parseDBTime(startedAt)
	job.FinishedAt = parseDBTime(finishedAt)
	return job, nil
}

func scanAffectBatch(scanner sqlScanner) (AffectJobBatchRecord, error) {
	var batch AffectJobBatchRecord
	var jobIDsJSON, sessionIDsJSON, turnIDsJSON, startedAt, finishedAt string
	err := scanner.Scan(
		&batch.ID,
		&batch.PersonaID,
		&batch.MoodOwnerScope,
		&batch.MoodOwnerID,
		&batch.JobType,
		&batch.Status,
		&batch.JobCount,
		&batch.FirstJobSeq,
		&batch.LastJobSeq,
		&jobIDsJSON,
		&sessionIDsJSON,
		&turnIDsJSON,
		&batch.BatchInputSummary,
		&batch.ContextWindowSnapshotJSON,
		&batch.EvaluationID,
		&batch.AffectEventID,
		&batch.ErrorMessage,
		&batch.ClaimedBy,
		&startedAt,
		&finishedAt,
	)
	if err != nil {
		return AffectJobBatchRecord{}, err
	}
	_ = json.Unmarshal([]byte(jobIDsJSON), &batch.JobIDs)
	_ = json.Unmarshal([]byte(sessionIDsJSON), &batch.SessionIDs)
	_ = json.Unmarshal([]byte(turnIDsJSON), &batch.TurnIDs)
	batch.StartedAt = parseDBTime(startedAt)
	batch.FinishedAt = parseDBTime(finishedAt)
	return batch, nil
}

func summarizeJobsForBatch(jobs []AffectJobRecord, maxChars int) string {
	var b strings.Builder
	for i, job := range jobs {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("Turn ")
		b.WriteString(job.TurnID)
		if job.SessionID != "" {
			b.WriteString(" (session ")
			b.WriteString(job.SessionID)
			b.WriteString(")")
		}
		b.WriteString("\nUser: ")
		b.WriteString(compactForSummary(job.UserText, 600))
		b.WriteString("\nAssistant: ")
		b.WriteString(compactForSummary(job.AssistantText, 800))
		if job.InputSummary != "" {
			b.WriteString("\nSummary: ")
			b.WriteString(compactForSummary(job.InputSummary, 500))
		}
		if job.MemoryPromptBlock != "" {
			b.WriteString("\nMemory context: ")
			b.WriteString(compactForSummary(job.MemoryPromptBlock, 500))
		}
		if maxChars > 0 && b.Len() > maxChars {
			out := b.String()
			return out[:maxChars]
		}
	}
	return b.String()
}

func compactForSummary(value string, maxChars int) string {
	value = strings.Join(strings.Fields(value), " ")
	if maxChars > 0 && len(value) > maxChars {
		return value[:maxChars]
	}
	return value
}

func roughJobTokenEstimate(job AffectJobRecord) int {
	chars := len(job.UserText) + len(job.AssistantText) + len(job.InputSummary) + len(job.MemoryPromptBlock)
	if chars == 0 {
		return 1
	}
	return chars/4 + 1
}
