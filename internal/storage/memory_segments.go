package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type MemorySegmentRef struct {
	SegmentID       string
	MemorySessionID string
}

type MemoryChatLink struct {
	ChatSessionID          string
	PersonaID              string
	CurrentMemorySessionID string
	CreatedAt              string
	UpdatedAt              string
}

type MemorySegment struct {
	ID                              string
	ChatSessionID                   string
	MemorySessionID                 string
	SegmentIndex                    int
	StartedAt                       string
	LastActivityAt                  string
	FinalizedAt                     string
	FinalizeReason                  string
	Summary                         string
	LastUserEpisodeID               string
	LastAssistantEpisodeID          string
	LastExtractedAt                 string
	LastExtractedUntilAt            string
	LastExtractedUserEpisodeID      string
	LastExtractedAssistantEpisodeID string
	LastExtractionJobID             string
	LastExtractionErrorCode         string
	LastExtractionErrorMessage      string
	ExtractionAttemptCount          int
	ExtractionStatus                string
}

type CreateMemorySegmentParams struct {
	ID              string
	ChatSessionID   string
	PersonaID       string
	MemorySessionID string
}

func (d *DB) GetMemoryChatLink(ctx context.Context, chatSessionID string) (*MemoryChatLink, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT chat_session_id, persona_id, COALESCE(current_memory_session_id, ''), created_at, updated_at
		FROM memory_chat_links
		WHERE chat_session_id = ?
	`, chatSessionID)

	var link MemoryChatLink
	if err := row.Scan(&link.ChatSessionID, &link.PersonaID, &link.CurrentMemorySessionID, &link.CreatedAt, &link.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &link, nil
}

func (d *DB) CreateMemorySegment(ctx context.Context, params CreateMemorySegmentParams) (*MemorySegment, error) {
	if params.ID == "" {
		return nil, errors.New("memory segment id is required")
	}
	if params.ChatSessionID == "" {
		return nil, errors.New("chat session id is required")
	}
	if params.PersonaID == "" {
		return nil, errors.New("persona id is required")
	}
	if params.MemorySessionID == "" {
		return nil, errors.New("memory session id is required")
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := nowUTC()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO memory_chat_links (chat_session_id, persona_id, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(chat_session_id) DO UPDATE SET
			persona_id = excluded.persona_id,
			updated_at = excluded.updated_at
	`, params.ChatSessionID, params.PersonaID, now, now); err != nil {
		return nil, err
	}

	var index int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(segment_index), 0) + 1
		FROM memory_segments
		WHERE chat_session_id = ?
	`, params.ChatSessionID).Scan(&index); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO memory_segments (
			id, chat_session_id, memory_session_id, segment_index,
			started_at, last_activity_at
		)
		VALUES (?, ?, ?, ?, ?, ?)
	`, params.ID, params.ChatSessionID, params.MemorySessionID, index, now, now); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE memory_chat_links
		SET current_memory_session_id = ?, updated_at = ?
		WHERE chat_session_id = ?
	`, params.MemorySessionID, now, params.ChatSessionID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d.GetMemorySegment(ctx, params.ID)
}

func memorySegmentSelectSQL() string {
	return `
		SELECT id, chat_session_id, memory_session_id, segment_index,
		       started_at, last_activity_at, COALESCE(finalized_at, ''),
		       COALESCE(finalize_reason, ''), COALESCE(summary, ''),
		       COALESCE(last_user_episode_id, ''), COALESCE(last_assistant_episode_id, ''),
		       COALESCE(last_extracted_at, ''), COALESCE(last_extracted_until_at, ''),
		       COALESCE(last_extracted_user_episode_id, ''), COALESCE(last_extracted_assistant_episode_id, ''),
		       COALESCE(last_extraction_job_id, ''), COALESCE(last_extraction_error_code, ''),
		       COALESCE(last_extraction_error_message, ''), COALESCE(extraction_attempt_count, 0),
		       COALESCE(extraction_status, 'never')
		FROM memory_segments`
}

func (d *DB) GetCurrentMemorySegment(ctx context.Context, chatSessionID string) (*MemorySegment, error) {
	row := d.db.QueryRowContext(ctx, memorySegmentSelectSQL()+`
		WHERE chat_session_id = ? AND finalized_at IS NULL
	`, chatSessionID)
	return scanMemorySegment(row)
}

func (d *DB) GetMemorySegment(ctx context.Context, segmentID string) (*MemorySegment, error) {
	row := d.db.QueryRowContext(ctx, memorySegmentSelectSQL()+`
		WHERE id = ?
	`, segmentID)
	return scanMemorySegment(row)
}

func (d *DB) FinalizeMemorySegment(ctx context.Context, segmentID string, reason string, summary string) error {
	if segmentID == "" {
		return errors.New("memory segment id is required")
	}
	if reason == "" {
		return errors.New("finalize reason is required")
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var chatSessionID, memorySessionID string
	if err := tx.QueryRowContext(ctx, `
		SELECT chat_session_id, memory_session_id
		FROM memory_segments
		WHERE id = ?
	`, segmentID).Scan(&chatSessionID, &memorySessionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("memory segment not found: %s", segmentID)
		}
		return err
	}

	now := nowUTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE memory_segments
		SET finalized_at = COALESCE(finalized_at, ?),
		    finalize_reason = ?,
		    summary = ?,
		    last_activity_at = ?
		WHERE id = ?
	`, now, reason, summary, now, segmentID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE memory_chat_links
		SET current_memory_session_id = NULL,
		    updated_at = ?
		WHERE chat_session_id = ? AND current_memory_session_id = ?
	`, now, chatSessionID, memorySessionID); err != nil {
		return err
	}

	return tx.Commit()
}

func (d *DB) UpdateMemorySegmentEpisode(ctx context.Context, segmentID string, role string, episodeID string) error {
	if segmentID == "" {
		return errors.New("memory segment id is required")
	}
	if episodeID == "" {
		return errors.New("memory episode id is required")
	}

	column := ""
	switch role {
	case "user":
		column = "last_user_episode_id"
	case "assistant":
		column = "last_assistant_episode_id"
	default:
		return fmt.Errorf("unsupported memory episode role: %s", role)
	}

	now := nowUTC()
	result, err := d.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE memory_segments
		SET %s = ?,
		    last_activity_at = ?,
		    extraction_status = CASE
		        WHEN extraction_status IN ('succeeded', 'skipped')
		         AND COALESCE(last_extracted_until_at, '') <> ''
		         AND last_extracted_until_at < ?
		        THEN 'stale'
		        ELSE extraction_status
		    END
		WHERE id = ?
	`, column), episodeID, now, now, segmentID)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return fmt.Errorf("memory segment not found: %s", segmentID)
	}
	return nil
}

type MemorySegmentExtractionCompleted struct {
	JobID                       string
	Status                      string
	ExtractedUntilAt            string
	ExtractedUserEpisodeID      string
	ExtractedAssistantEpisodeID string
}

func (d *DB) UpdateMemorySegmentExtractionCompleted(ctx context.Context, segmentID string, completed MemorySegmentExtractionCompleted) error {
	if segmentID == "" {
		return errors.New("memory segment id is required")
	}
	status := completed.Status
	if status == "" {
		status = MemorySegmentExtractionStatusSucceeded
	}
	now := nowUTC()
	result, err := d.db.ExecContext(ctx, `
		UPDATE memory_segments
		SET last_extracted_at = ?,
		    last_extracted_until_at = NULLIF(?, ''),
		    last_extracted_user_episode_id = NULLIF(?, ''),
		    last_extracted_assistant_episode_id = NULLIF(?, ''),
		    last_extraction_job_id = NULLIF(?, ''),
		    last_extraction_error_code = NULL,
		    last_extraction_error_message = NULL,
		    extraction_status = ?,
		    extraction_attempt_count = 0
		WHERE id = ?
	`, now, completed.ExtractedUntilAt, completed.ExtractedUserEpisodeID, completed.ExtractedAssistantEpisodeID, completed.JobID, status, segmentID)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return fmt.Errorf("memory segment not found: %s", segmentID)
	}
	return nil
}

func scanMemorySegment(row scanner) (*MemorySegment, error) {
	var segment MemorySegment
	if err := row.Scan(
		&segment.ID,
		&segment.ChatSessionID,
		&segment.MemorySessionID,
		&segment.SegmentIndex,
		&segment.StartedAt,
		&segment.LastActivityAt,
		&segment.FinalizedAt,
		&segment.FinalizeReason,
		&segment.Summary,
		&segment.LastUserEpisodeID,
		&segment.LastAssistantEpisodeID,
		&segment.LastExtractedAt,
		&segment.LastExtractedUntilAt,
		&segment.LastExtractedUserEpisodeID,
		&segment.LastExtractedAssistantEpisodeID,
		&segment.LastExtractionJobID,
		&segment.LastExtractionErrorCode,
		&segment.LastExtractionErrorMessage,
		&segment.ExtractionAttemptCount,
		&segment.ExtractionStatus,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &segment, nil
}
