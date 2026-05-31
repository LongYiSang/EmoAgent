package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	MemoryExtractionJobStatusPending   = "pending"
	MemoryExtractionJobStatusRunning   = "running"
	MemoryExtractionJobStatusSucceeded = "succeeded"
	MemoryExtractionJobStatusSkipped   = "skipped"
	MemoryExtractionJobStatusFailed    = "failed"
	MemoryExtractionJobStatusCancelled = "cancelled"

	MemorySegmentExtractionStatusNever     = "never"
	MemorySegmentExtractionStatusPending   = "pending"
	MemorySegmentExtractionStatusRunning   = "running"
	MemorySegmentExtractionStatusSucceeded = "succeeded"
	MemorySegmentExtractionStatusSkipped   = "skipped"
	MemorySegmentExtractionStatusFailed    = "failed"
	MemorySegmentExtractionStatusStale     = "stale"

	MemoryExtractionTriggerIdleDetect        = "idle_detect"
	MemoryExtractionTriggerSessionEnd        = "session_end"
	MemoryExtractionTriggerManualPin         = "manual_pin"
	MemoryExtractionTriggerManualScan        = "manual_scan"
	MemoryExtractionTriggerManualSegmentScan = "manual_segment_scan"
	MemoryExtractionTriggerPeriodicSweep     = "periodic_sweep"
	MemoryExtractionTriggerReprocess         = "reprocess"

	MemoryExtractionScopeSegment     = "segment"
	MemoryExtractionScopeChatSession = "chat_session"
	MemoryExtractionScopePersona     = "persona"
)

type MemoryExtractionJob struct {
	ID                   string
	PersonaID            string
	ChatSessionID        string
	SegmentID            string
	MemorySessionID      string
	Trigger              string
	Scope                string
	Mode                 string
	RequestedBy          string
	Priority             int
	Force                bool
	EpisodeIDs           []string
	SinceAt              string
	UntilAt              string
	EpisodeLimit         int
	Status               string
	Attempts             int
	MaxAttempts          int
	RunAfter             string
	ClaimedBy            string
	ClaimedUntil         string
	RequestJSON          string
	ResultJSON           string
	MirrorSyncResultJSON string
	ErrorCode            string
	ErrorMessage         string
	DedupeKey            string
	CreatedAt            string
	UpdatedAt            string
	StartedAt            string
	FinishedAt           string
}

type EnqueueMemoryExtractionJobParams struct {
	ID              string
	PersonaID       string
	ChatSessionID   string
	SegmentID       string
	MemorySessionID string
	Trigger         string
	Scope           string
	Mode            string
	RequestedBy     string
	Priority        int
	Force           bool
	EpisodeIDs      []string
	SinceAt         string
	UntilAt         string
	EpisodeLimit    int
	MaxAttempts     int
	RunAfter        time.Time
	RequestJSON     string
}

type CompleteMemoryExtractionJobParams struct {
	Status               string
	ResultJSON           string
	MirrorSyncResultJSON string
	ExtractedUntilAt     string
	ExpectedClaimedBy    string
}

type FailMemoryExtractionJobParams struct {
	ErrorCode         string
	ErrorMessage      string
	Retry             bool
	NextRunAfter      time.Time
	ExpectedClaimedBy string
}

type ListMemoryExtractionJobsFilter struct {
	ChatSessionID string
	SegmentID     string
	Status        string
	Limit         int
}

type ListMemorySegmentsFilter struct {
	ChatSessionID string
	Limit         int
}

type ScanEligibleMemorySegmentsParams struct {
	Now                      time.Time
	IdleAfter                time.Duration
	IncludeActiveSegments    bool
	IncludeFinalizedSegments bool
	MinEpisodeCount          int
	MaxFailedAttempts        int
	Limit                    int
}

func (d *DB) EnqueueMemoryExtractionJob(ctx context.Context, params EnqueueMemoryExtractionJobParams) (*MemoryExtractionJob, bool, error) {
	if strings.TrimSpace(params.PersonaID) == "" {
		return nil, false, errors.New("persona id is required")
	}
	if strings.TrimSpace(params.Trigger) == "" {
		return nil, false, errors.New("memory extraction trigger is required")
	}
	if strings.TrimSpace(params.Scope) == "" {
		params.Scope = MemoryExtractionScopeSegment
	}
	if strings.TrimSpace(params.Mode) == "" {
		params.Mode = "apply"
	}
	if strings.TrimSpace(params.RequestedBy) == "" {
		params.RequestedBy = "system"
	}
	if params.Priority == 0 {
		params.Priority = 100
	}
	if params.EpisodeLimit == 0 {
		params.EpisodeLimit = 50
	}
	if params.MaxAttempts == 0 {
		params.MaxAttempts = 3
	}
	if params.RunAfter.IsZero() {
		params.RunAfter = time.Now().UTC()
	}
	if strings.TrimSpace(params.ID) == "" {
		params.ID = uuid.NewString()
	}
	dedupeKey := memoryExtractionDedupeKey(params)
	if existing, err := d.getActiveMemoryExtractionJobByDedupeKey(ctx, dedupeKey); err != nil || existing != nil {
		if err != nil || existing == nil {
			return existing, false, err
		}
		merged, mergeErr := d.mergeActiveMemoryExtractionJob(ctx, existing, params)
		return merged, false, mergeErr
	}

	episodeIDsJSON, err := json.Marshal(params.EpisodeIDs)
	if err != nil {
		return nil, false, fmt.Errorf("marshal episode ids: %w", err)
	}
	now := nowUTC()
	_, err = d.db.ExecContext(ctx, `
		INSERT INTO memory_extraction_jobs (
			id, persona_id, chat_session_id, segment_id, memory_session_id,
			trigger, scope, mode, requested_by, priority, force,
			episode_ids_json, since_at, until_at, episode_limit,
			status, attempts, max_attempts, run_after, request_json, dedupe_key,
			created_at, updated_at
		)
		VALUES (?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''),
		        ?, ?, ?, ?, ?, ?,
		        ?, NULLIF(?, ''), NULLIF(?, ''), ?,
		        'pending', 0, ?, ?, NULLIF(?, ''), ?,
		        ?, ?)
	`, params.ID, strings.TrimSpace(params.PersonaID), strings.TrimSpace(params.ChatSessionID), strings.TrimSpace(params.SegmentID), strings.TrimSpace(params.MemorySessionID),
		strings.TrimSpace(params.Trigger), strings.TrimSpace(params.Scope), strings.TrimSpace(params.Mode), strings.TrimSpace(params.RequestedBy), params.Priority, boolInt(params.Force),
		string(episodeIDsJSON), strings.TrimSpace(params.SinceAt), strings.TrimSpace(params.UntilAt), params.EpisodeLimit,
		params.MaxAttempts, params.RunAfter.UTC().Format(time.RFC3339Nano), strings.TrimSpace(params.RequestJSON), dedupeKey,
		now, now)
	if err != nil {
		if existing, getErr := d.getActiveMemoryExtractionJobByDedupeKey(ctx, dedupeKey); getErr == nil && existing != nil {
			return existing, false, nil
		}
		return nil, false, err
	}
	if params.SegmentID != "" {
		_, err = d.db.ExecContext(ctx, `
			UPDATE memory_segments
			SET extraction_status = 'pending',
			    last_extraction_job_id = ?
			WHERE id = ?
		`, params.ID, params.SegmentID)
		if err != nil {
			return nil, false, err
		}
	}
	job, err := d.GetMemoryExtractionJob(ctx, params.ID)
	return job, true, err
}

func (d *DB) GetMemoryExtractionJob(ctx context.Context, jobID string) (*MemoryExtractionJob, error) {
	row := d.db.QueryRowContext(ctx, memoryExtractionJobSelectSQL()+` WHERE id = ?`, jobID)
	return scanMemoryExtractionJob(row)
}

func (d *DB) ListMemoryExtractionJobs(ctx context.Context, filter ListMemoryExtractionJobsFilter) ([]MemoryExtractionJob, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	var conditions []string
	var args []any
	if strings.TrimSpace(filter.ChatSessionID) != "" {
		conditions = append(conditions, "chat_session_id = ?")
		args = append(args, strings.TrimSpace(filter.ChatSessionID))
	}
	if strings.TrimSpace(filter.SegmentID) != "" {
		conditions = append(conditions, "segment_id = ?")
		args = append(args, strings.TrimSpace(filter.SegmentID))
	}
	if strings.TrimSpace(filter.Status) != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, strings.TrimSpace(filter.Status))
	}
	query := memoryExtractionJobSelectSQL()
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []MemoryExtractionJob
	for rows.Next() {
		job, err := scanMemoryExtractionJob(rows)
		if err != nil {
			return nil, err
		}
		if job != nil {
			jobs = append(jobs, *job)
		}
	}
	return jobs, rows.Err()
}

func (d *DB) ClaimMemoryExtractionJobs(ctx context.Context, workerID string, limit int, claimTTL time.Duration, now time.Time) ([]MemoryExtractionJob, error) {
	if strings.TrimSpace(workerID) == "" {
		return nil, errors.New("worker id is required")
	}
	if limit <= 0 {
		return []MemoryExtractionJob{}, nil
	}
	if claimTTL <= 0 {
		claimTTL = 5 * time.Minute
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	nowText := now.UTC().Format(time.RFC3339Nano)
	claimedUntil := now.Add(claimTTL).UTC().Format(time.RFC3339Nano)

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM memory_extraction_jobs
		WHERE (status = 'pending' AND run_after <= ?)
		   OR (status = 'running' AND COALESCE(claimed_until, '') < ?)
		ORDER BY priority ASC, created_at ASC
		LIMIT ?
	`, nowText, nowText, limit)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	var claimedIDs []string
	for _, id := range ids {
		result, err := tx.ExecContext(ctx, `
			UPDATE memory_extraction_jobs
			SET status = 'running',
			    attempts = attempts + 1,
			    claimed_by = ?,
			    claimed_until = ?,
			    started_at = COALESCE(started_at, ?),
			    updated_at = ?
			WHERE id = ?
			  AND ((status = 'pending' AND run_after <= ?)
			       OR (status = 'running' AND COALESCE(claimed_until, '') < ?))
		`, workerID, claimedUntil, nowText, nowText, id, nowText, nowText)
		if err != nil {
			return nil, err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if rows == 0 {
			continue
		}
		claimedIDs = append(claimedIDs, id)
		if _, err := tx.ExecContext(ctx, `
			UPDATE memory_segments
			SET extraction_status = 'running',
			    last_extraction_job_id = ?
			WHERE id = (SELECT segment_id FROM memory_extraction_jobs WHERE id = ?)
		`, id, id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	claimed := make([]MemoryExtractionJob, 0, len(claimedIDs))
	for _, id := range claimedIDs {
		job, err := d.GetMemoryExtractionJob(ctx, id)
		if err != nil {
			return nil, err
		}
		if job != nil {
			claimed = append(claimed, *job)
		}
	}
	return claimed, nil
}

func (d *DB) CompleteMemoryExtractionJob(ctx context.Context, jobID string, params CompleteMemoryExtractionJobParams) error {
	if strings.TrimSpace(jobID) == "" {
		return errors.New("memory extraction job id is required")
	}
	status := strings.TrimSpace(params.Status)
	if status == "" {
		status = MemoryExtractionJobStatusSucceeded
	}
	if status != MemoryExtractionJobStatusSucceeded && status != MemoryExtractionJobStatusSkipped {
		return fmt.Errorf("unsupported memory extraction completion status: %s", status)
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var segmentID, untilAt, segmentLastActivityAt string
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(j.segment_id, ''), COALESCE(j.until_at, ''), COALESCE(s.last_activity_at, '')
		FROM memory_extraction_jobs j
		LEFT JOIN memory_segments s ON s.id = j.segment_id
		WHERE j.id = ?
	`, jobID).Scan(&segmentID, &untilAt, &segmentLastActivityAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("memory extraction job not found: %s", jobID)
		}
		return err
	}
	if strings.TrimSpace(params.ExtractedUntilAt) != "" {
		untilAt = strings.TrimSpace(params.ExtractedUntilAt)
	}
	expectedClaimedBy := strings.TrimSpace(params.ExpectedClaimedBy)
	now := nowUTC()
	result, err := tx.ExecContext(ctx, `
		UPDATE memory_extraction_jobs
		SET status = ?,
		    result_json = NULLIF(?, ''),
		    mirror_sync_result_json = NULLIF(?, ''),
		    claimed_by = NULL,
		    claimed_until = NULL,
		    error_code = NULL,
		    error_message = NULL,
		    finished_at = ?,
		    updated_at = ?
		WHERE id = ?
		  AND status = 'running'
		  AND (? = '' OR claimed_by = ?)
	`, status, strings.TrimSpace(params.ResultJSON), strings.TrimSpace(params.MirrorSyncResultJSON), now, now, jobID, expectedClaimedBy, expectedClaimedBy)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil {
		return err
	} else if rows == 0 {
		return fmt.Errorf("memory extraction job claim lost: %s", jobID)
	}
	if segmentID != "" {
		segmentStatus := MemorySegmentExtractionStatusSucceeded
		if status == MemoryExtractionJobStatusSkipped {
			segmentStatus = MemorySegmentExtractionStatusSkipped
		}
		if extractionWindowStale(untilAt, segmentLastActivityAt) {
			segmentStatus = MemorySegmentExtractionStatusStale
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE memory_segments
			SET last_extracted_at = ?,
			    last_extracted_until_at = NULLIF(?, ''),
			    last_extraction_job_id = ?,
			    last_extraction_error_code = NULL,
			    last_extraction_error_message = NULL,
			    extraction_attempt_count = 0,
			    extraction_status = ?
			WHERE id = ?
		`, now, untilAt, jobID, segmentStatus, segmentID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) FailMemoryExtractionJob(ctx context.Context, jobID string, params FailMemoryExtractionJobParams) error {
	if strings.TrimSpace(jobID) == "" {
		return errors.New("memory extraction job id is required")
	}
	code := strings.TrimSpace(params.ErrorCode)
	if code == "" {
		code = "unknown"
	}
	now := nowUTC()
	status := MemoryExtractionJobStatusFailed
	runAfter := ""
	finishedAt := now
	if params.Retry {
		status = MemoryExtractionJobStatusPending
		if params.NextRunAfter.IsZero() {
			params.NextRunAfter = time.Now().UTC()
		}
		runAfter = params.NextRunAfter.UTC().Format(time.RFC3339Nano)
		finishedAt = ""
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	expectedClaimedBy := strings.TrimSpace(params.ExpectedClaimedBy)
	var segmentID string
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(segment_id, '')
		FROM memory_extraction_jobs
		WHERE id = ?
	`, jobID).Scan(&segmentID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("memory extraction job not found: %s", jobID)
		}
		return err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE memory_extraction_jobs
		SET status = ?,
		    run_after = CASE WHEN ? = '' THEN run_after ELSE ? END,
		    claimed_by = NULL,
		    claimed_until = NULL,
		    error_code = ?,
		    error_message = NULLIF(?, ''),
		    finished_at = NULLIF(?, ''),
		    updated_at = ?
		WHERE id = ?
		  AND status = 'running'
		  AND (? = '' OR claimed_by = ?)
	`, status, runAfter, runAfter, code, strings.TrimSpace(params.ErrorMessage), finishedAt, now, jobID, expectedClaimedBy, expectedClaimedBy)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil {
		return err
	} else if rows == 0 {
		return fmt.Errorf("memory extraction job claim lost: %s", jobID)
	}
	if segmentID != "" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE memory_segments
			SET extraction_status = 'failed',
			    extraction_attempt_count = extraction_attempt_count + 1,
			    last_extraction_job_id = ?,
			    last_extraction_error_code = ?,
			    last_extraction_error_message = NULLIF(?, '')
			WHERE id = ?
		`, jobID, code, strings.TrimSpace(params.ErrorMessage), segmentID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (d *DB) ListMemorySegments(ctx context.Context, filter ListMemorySegmentsFilter) ([]MemorySegment, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	query := memorySegmentSelectSQL()
	var args []any
	if strings.TrimSpace(filter.ChatSessionID) != "" {
		query += " WHERE chat_session_id = ?"
		args = append(args, strings.TrimSpace(filter.ChatSessionID))
	}
	query += " ORDER BY chat_session_id, segment_index LIMIT ?"
	args = append(args, limit)

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var segments []MemorySegment
	for rows.Next() {
		segment, err := scanMemorySegment(rows)
		if err != nil {
			return nil, err
		}
		if segment != nil {
			segments = append(segments, *segment)
		}
	}
	return segments, rows.Err()
}

func (d *DB) ScanEligibleMemorySegments(ctx context.Context, params ScanEligibleMemorySegmentsParams) ([]MemorySegment, error) {
	if params.Limit <= 0 {
		return []MemorySegment{}, nil
	}
	if params.Now.IsZero() {
		params.Now = time.Now().UTC()
	}
	if params.IdleAfter <= 0 {
		params.IdleAfter = 15 * time.Minute
	}
	if params.MinEpisodeCount <= 0 {
		params.MinEpisodeCount = 1
	}
	if !params.IncludeActiveSegments && !params.IncludeFinalizedSegments {
		return []MemorySegment{}, nil
	}
	idleBefore := params.Now.Add(-params.IdleAfter).UTC().Format(time.RFC3339Nano)
	query := memorySegmentSelectSQL() + `
		WHERE last_activity_at <= ?
		  AND (
		      COALESCE(last_extracted_until_at, '') = ''
		      OR last_extracted_until_at < last_activity_at
		      OR COALESCE(extraction_status, 'never') = 'failed'
		  )
		  AND COALESCE(extraction_status, 'never') NOT IN ('pending', 'running')
		  AND (
		      COALESCE(extraction_status, 'never') != 'failed'
		      OR ? <= 0
		      OR COALESCE(extraction_attempt_count, 0) < ?
		  )
		  AND (
		      (CASE WHEN COALESCE(last_user_episode_id, '') = '' THEN 0 ELSE 1 END) +
		      (CASE WHEN COALESCE(last_assistant_episode_id, '') = '' THEN 0 ELSE 1 END)
		  ) >= ?
		  AND NOT EXISTS (
		      SELECT 1 FROM memory_extraction_jobs j
		      WHERE j.segment_id = memory_segments.id
		        AND j.status IN ('pending', 'running')
		  )
	`
	args := []any{idleBefore, params.MaxFailedAttempts, params.MaxFailedAttempts, params.MinEpisodeCount}
	if params.IncludeActiveSegments && !params.IncludeFinalizedSegments {
		query += " AND finalized_at IS NULL"
	}
	if params.IncludeFinalizedSegments && !params.IncludeActiveSegments {
		query += " AND finalized_at IS NOT NULL"
	}
	query += " ORDER BY last_activity_at ASC LIMIT ?"
	args = append(args, params.Limit)

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var segments []MemorySegment
	for rows.Next() {
		segment, err := scanMemorySegment(rows)
		if err != nil {
			return nil, err
		}
		if segment != nil {
			segments = append(segments, *segment)
		}
	}
	return segments, rows.Err()
}

func (d *DB) getActiveMemoryExtractionJobByDedupeKey(ctx context.Context, dedupeKey string) (*MemoryExtractionJob, error) {
	row := d.db.QueryRowContext(ctx, memoryExtractionJobSelectSQL()+`
		WHERE dedupe_key = ? AND status IN ('pending', 'running')
		ORDER BY created_at ASC
		LIMIT 1
	`, dedupeKey)
	return scanMemoryExtractionJob(row)
}

func (d *DB) mergeActiveMemoryExtractionJob(ctx context.Context, existing *MemoryExtractionJob, params EnqueueMemoryExtractionJobParams) (*MemoryExtractionJob, error) {
	if existing == nil || existing.Status == MemoryExtractionJobStatusRunning {
		return existing, nil
	}
	trigger := existing.Trigger
	episodeIDs := append([]string(nil), existing.EpisodeIDs...)
	episodeLimit := maxInt(existing.EpisodeLimit, params.EpisodeLimit)
	if episodeLimit == 0 {
		episodeLimit = 50
	}
	if params.Trigger == MemoryExtractionTriggerManualPin {
		trigger = MemoryExtractionTriggerManualPin
		if isFullSegmentExtractionTrigger(existing.Trigger) {
			episodeIDs = nil
		} else {
			episodeIDs = mergeEpisodeIDs(episodeIDs, params.EpisodeIDs)
		}
	} else if existing.Trigger == MemoryExtractionTriggerManualPin && isFullSegmentExtractionTrigger(params.Trigger) {
		trigger = MemoryExtractionTriggerManualPin
		episodeIDs = nil
	} else if params.Priority > 0 && params.Priority < existing.Priority {
		trigger = params.Trigger
	}
	episodeIDsJSON, err := json.Marshal(episodeIDs)
	if err != nil {
		return nil, fmt.Errorf("marshal episode ids: %w", err)
	}

	priority := existing.Priority
	if params.Priority > 0 && (priority == 0 || params.Priority < priority) {
		priority = params.Priority
	}
	mode := strongerExtractionMode(existing.Mode, params.Mode)
	requestedBy := existing.RequestedBy
	if strings.TrimSpace(params.RequestedBy) == "user" {
		requestedBy = "user"
	}
	sinceAt := firstNonEmpty(existing.SinceAt, params.SinceAt)
	untilAt := laterJobTime(existing.UntilAt, params.UntilAt)
	runAfter := earlierJobTime(existing.RunAfter, params.RunAfter)
	maxAttempts := maxInt(existing.MaxAttempts, params.MaxAttempts)
	if maxAttempts == 0 {
		maxAttempts = 3
	}
	now := nowUTC()
	_, err = d.db.ExecContext(ctx, `
		UPDATE memory_extraction_jobs
		SET trigger = ?,
		    mode = ?,
		    requested_by = ?,
		    priority = ?,
		    force = ?,
		    episode_ids_json = ?,
		    since_at = NULLIF(?, ''),
		    until_at = NULLIF(?, ''),
		    episode_limit = ?,
		    max_attempts = ?,
		    run_after = ?,
		    updated_at = ?
		WHERE id = ? AND status = 'pending'
	`, trigger, mode, requestedBy, priority, boolInt(existing.Force || params.Force), string(episodeIDsJSON),
		sinceAt, untilAt, episodeLimit, maxAttempts, runAfter, now, existing.ID)
	if err != nil {
		return nil, err
	}
	return d.GetMemoryExtractionJob(ctx, existing.ID)
}

func memoryExtractionDedupeKey(params EnqueueMemoryExtractionJobParams) string {
	keyID := strings.TrimSpace(params.SegmentID)
	if keyID == "" {
		keyID = strings.TrimSpace(params.MemorySessionID)
	}
	if keyID == "" {
		keyID = strings.TrimSpace(params.ChatSessionID)
	}
	if keyID == "" {
		keyID = strings.TrimSpace(params.PersonaID)
	}
	return strings.Join([]string{strings.TrimSpace(params.PersonaID), strings.TrimSpace(params.Scope), keyID}, "\x00")
}

func extractionWindowStale(extractedUntilAt string, lastActivityAt string) bool {
	extractedUntilAt = strings.TrimSpace(extractedUntilAt)
	lastActivityAt = strings.TrimSpace(lastActivityAt)
	if extractedUntilAt == "" || lastActivityAt == "" {
		return false
	}
	extractedUntil, errUntil := time.Parse(time.RFC3339Nano, extractedUntilAt)
	lastActivity, errActivity := time.Parse(time.RFC3339Nano, lastActivityAt)
	if errUntil == nil && errActivity == nil {
		return extractedUntil.Before(lastActivity)
	}
	return extractedUntilAt < lastActivityAt
}

func isFullSegmentExtractionTrigger(trigger string) bool {
	switch strings.TrimSpace(trigger) {
	case MemoryExtractionTriggerSessionEnd, MemoryExtractionTriggerIdleDetect, MemoryExtractionTriggerManualScan, MemoryExtractionTriggerManualSegmentScan, MemoryExtractionTriggerPeriodicSweep, MemoryExtractionTriggerReprocess:
		return true
	default:
		return false
	}
}

func mergeEpisodeIDs(base []string, extra []string) []string {
	seen := make(map[string]bool, len(base)+len(extra))
	merged := make([]string, 0, len(base)+len(extra))
	for _, id := range append(base, extra...) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		merged = append(merged, id)
	}
	return merged
}

func strongerExtractionMode(existing string, incoming string) string {
	existing = strings.TrimSpace(existing)
	incoming = strings.TrimSpace(incoming)
	if incoming == "" {
		return existing
	}
	if existing == "" || extractionModeRank(incoming) > extractionModeRank(existing) {
		return incoming
	}
	return existing
}

func extractionModeRank(mode string) int {
	switch strings.TrimSpace(mode) {
	case "apply":
		return 3
	case "dry-run", "dry_run":
		return 2
	case "validate":
		return 1
	default:
		return 0
	}
}

func laterJobTime(existing string, incoming string) string {
	return chooseJobTime(existing, incoming, true)
}

func earlierJobTime(existing string, incoming time.Time) string {
	if incoming.IsZero() {
		return strings.TrimSpace(existing)
	}
	return chooseJobTime(existing, incoming.UTC().Format(time.RFC3339Nano), false)
}

func chooseJobTime(existing string, incoming string, later bool) string {
	existing = strings.TrimSpace(existing)
	incoming = strings.TrimSpace(incoming)
	if existing == "" {
		return incoming
	}
	if incoming == "" {
		return existing
	}
	existingTime, existingErr := time.Parse(time.RFC3339Nano, existing)
	incomingTime, incomingErr := time.Parse(time.RFC3339Nano, incoming)
	if existingErr == nil && incomingErr == nil {
		if later && incomingTime.After(existingTime) {
			return incoming
		}
		if !later && incomingTime.Before(existingTime) {
			return incoming
		}
		return existing
	}
	if later && incoming > existing {
		return incoming
	}
	if !later && incoming < existing {
		return incoming
	}
	return existing
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func maxInt(values ...int) int {
	max := 0
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}

func memoryExtractionJobSelectSQL() string {
	return `
		SELECT id, persona_id, COALESCE(chat_session_id, ''), COALESCE(segment_id, ''),
		       COALESCE(memory_session_id, ''), trigger, scope, mode, requested_by,
		       priority, force, episode_ids_json, COALESCE(since_at, ''), COALESCE(until_at, ''),
		       episode_limit, status, attempts, max_attempts, run_after,
		       COALESCE(claimed_by, ''), COALESCE(claimed_until, ''),
		       COALESCE(request_json, ''), COALESCE(result_json, ''), COALESCE(mirror_sync_result_json, ''),
		       COALESCE(error_code, ''), COALESCE(error_message, ''), dedupe_key,
		       created_at, updated_at, COALESCE(started_at, ''), COALESCE(finished_at, '')
		FROM memory_extraction_jobs`
}

func scanMemoryExtractionJob(row scanner) (*MemoryExtractionJob, error) {
	var job MemoryExtractionJob
	var force int
	var episodeIDsJSON string
	if err := row.Scan(
		&job.ID,
		&job.PersonaID,
		&job.ChatSessionID,
		&job.SegmentID,
		&job.MemorySessionID,
		&job.Trigger,
		&job.Scope,
		&job.Mode,
		&job.RequestedBy,
		&job.Priority,
		&force,
		&episodeIDsJSON,
		&job.SinceAt,
		&job.UntilAt,
		&job.EpisodeLimit,
		&job.Status,
		&job.Attempts,
		&job.MaxAttempts,
		&job.RunAfter,
		&job.ClaimedBy,
		&job.ClaimedUntil,
		&job.RequestJSON,
		&job.ResultJSON,
		&job.MirrorSyncResultJSON,
		&job.ErrorCode,
		&job.ErrorMessage,
		&job.DedupeKey,
		&job.CreatedAt,
		&job.UpdatedAt,
		&job.StartedAt,
		&job.FinishedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	job.Force = force != 0
	if strings.TrimSpace(episodeIDsJSON) != "" {
		if err := json.Unmarshal([]byte(episodeIDsJSON), &job.EpisodeIDs); err != nil {
			return nil, fmt.Errorf("unmarshal episode ids: %w", err)
		}
	}
	return &job, nil
}
