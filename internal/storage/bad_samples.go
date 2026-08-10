package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// BadSampleRecord is a user-flagged unsatisfactory reply plus a frozen copy of
// the scene it happened in. The scene is copied rather than referenced so that
// samples survive prompt-snapshot retention pruning.
type BadSampleRecord struct {
	ID                   string
	CreatedAt            string
	Reason               string
	SessionID            string
	OriginKey            string
	PersonaKey           string
	TargetTurnID         string
	ContextJSON          string
	ContextSchemaVersion int
}

// BadSampleMessage carries metadata verbatim: assistant rows hold
// memory_pipeline.prompt_block (the memory block actually injected that turn)
// and reply_delivery, which is the primary material for attribution.
type BadSampleMessage struct {
	Role      string
	Content   string
	CreatedAt string
	Metadata  string
}

type BadSamplePromptSnapshot struct {
	TurnID         string
	Purpose        string
	Model          string
	CreatedAt      string
	ComponentsJSON string
	RenderedText   string
}

type BadSampleAffectState struct {
	ID              string
	PersonaID       string
	Label           string
	Confidence      float64
	StateVectorJSON string
	CauseSummary    string
	MoodDescription string
	MoodReason      string
	PromptMoodText  string
	UpdatedAt       string
}

// BadSampleAffectEvaluation deliberately omits the bulky columns of
// agent_affect_evaluations (prompt_snapshot, response_json,
// context_window_snapshot_json): they would dominate the sample's size while
// adding little to attribution.
type BadSampleAffectEvaluation struct {
	ID                string
	TurnID            string
	TriggerType       string
	Status            string
	LLMModel          string
	ClampedDeltaJSON  string
	CauseSummary      string
	MoodDescription   string
	MoodReason        string
	Confidence        float64
	CreatedAt         string
}

type BadSampleAffect struct {
	State             *BadSampleAffectState
	RecentEvaluations []BadSampleAffectEvaluation
}

type BadSampleTurn struct {
	ID           string
	Kind         string
	State        string
	Status       string
	ErrorKind    string
	ErrorMessage string
	StartedAt    string
	CompletedAt  string
}

// InsertBadSample writes the sample row. Callers assemble ContextJSON.
func (d *DB) InsertBadSample(ctx context.Context, record BadSampleRecord) (BadSampleRecord, error) {
	if d == nil || d.db == nil {
		return BadSampleRecord{}, errors.New("storage: nil db")
	}
	if record.ID == "" {
		record.ID = uuid.NewString()
	}
	if record.CreatedAt == "" {
		record.CreatedAt = d.nowText()
	}
	if record.ContextSchemaVersion == 0 {
		record.ContextSchemaVersion = 1
	}
	if record.ContextJSON == "" {
		record.ContextJSON = "{}"
	}
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO bad_samples (
			id, created_at, reason, session_id, origin_key, persona_key,
			target_turn_id, context_json, context_schema_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, record.ID, record.CreatedAt, record.Reason, record.SessionID, record.OriginKey,
		record.PersonaKey, record.TargetTurnID, record.ContextJSON, record.ContextSchemaVersion)
	if err != nil {
		return BadSampleRecord{}, fmt.Errorf("insert bad sample: %w", err)
	}
	return record, nil
}

// CountBadSamples returns the global total, shown in the command receipt so the
// user knows how much material has accumulated without querying the DB.
func (d *DB) CountBadSamples(ctx context.Context) (int, error) {
	if d == nil || d.db == nil {
		return 0, errors.New("storage: nil db")
	}
	var count int
	if err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bad_samples`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count bad samples: %w", err)
	}
	return count, nil
}

// ListBadSamples returns samples newest-first. Used by diagnostics, not by the
// capture path.
func (d *DB) ListBadSamples(ctx context.Context, limit int) ([]BadSampleRecord, error) {
	if d == nil || d.db == nil {
		return nil, errors.New("storage: nil db")
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, created_at, reason, session_id, origin_key, persona_key,
		       target_turn_id, context_json, context_schema_version
		FROM bad_samples ORDER BY created_at DESC, id DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list bad samples: %w", err)
	}
	defer rows.Close()
	items := make([]BadSampleRecord, 0, limit)
	for rows.Next() {
		var r BadSampleRecord
		if err := rows.Scan(&r.ID, &r.CreatedAt, &r.Reason, &r.SessionID, &r.OriginKey,
			&r.PersonaKey, &r.TargetTurnID, &r.ContextJSON, &r.ContextSchemaVersion); err != nil {
			return nil, fmt.Errorf("scan bad sample: %w", err)
		}
		items = append(items, r)
	}
	return items, rows.Err()
}

// RecentSessionMessages returns up to limit newest messages, re-ordered oldest
// first so the frozen scene reads as a conversation.
//
// messages has no turn_id, so aligning messages to turns would mean correlating
// by timestamp window — fragile. Taking the last N messages of the session is
// both sturdier and closer to what attribution actually needs.
func (d *DB) RecentSessionMessages(ctx context.Context, sessionID string, limit int) ([]BadSampleMessage, error) {
	if d == nil || d.db == nil {
		return nil, errors.New("storage: nil db")
	}
	if sessionID == "" || limit <= 0 {
		return nil, nil
	}
	rows, err := d.db.QueryContext(ctx, `
		SELECT role, COALESCE(content, ''), COALESCE(created_at, ''), COALESCE(metadata, '')
		FROM messages WHERE session_id = ?
		ORDER BY created_at DESC, id DESC LIMIT ?
	`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("recent session messages: %w", err)
	}
	defer rows.Close()
	collected := make([]BadSampleMessage, 0, limit)
	for rows.Next() {
		var m BadSampleMessage
		if err := rows.Scan(&m.Role, &m.Content, &m.CreatedAt, &m.Metadata); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		collected = append(collected, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(collected)-1; i < j; i, j = i+1, j-1 {
		collected[i], collected[j] = collected[j], collected[i]
	}
	return collected, nil
}

// RecentPromptSnapshots returns the newest rendered prompts for the session.
// These carry the mood block and the fully assembled prompt; they may be absent
// because prompt snapshots are pruned by retention.
func (d *DB) RecentPromptSnapshots(ctx context.Context, sessionID string, limit int) ([]BadSamplePromptSnapshot, error) {
	if d == nil || d.db == nil {
		return nil, errors.New("storage: nil db")
	}
	if sessionID == "" || limit <= 0 {
		return nil, nil
	}
	rows, err := d.db.QueryContext(ctx, `
		SELECT COALESCE(turn_id, ''), COALESCE(purpose, ''), COALESCE(model, ''),
		       COALESCE(created_at, ''), COALESCE(components_json, ''), COALESCE(rendered_text, '')
		FROM prompt_render_snapshots WHERE session_id = ?
		ORDER BY created_at DESC, id DESC LIMIT ?
	`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("recent prompt snapshots: %w", err)
	}
	defer rows.Close()
	items := make([]BadSamplePromptSnapshot, 0, limit)
	for rows.Next() {
		var s BadSamplePromptSnapshot
		if err := rows.Scan(&s.TurnID, &s.Purpose, &s.Model, &s.CreatedAt, &s.ComponentsJSON, &s.RenderedText); err != nil {
			return nil, fmt.Errorf("scan prompt snapshot: %w", err)
		}
		items = append(items, s)
	}
	return items, rows.Err()
}

// SessionAffectSnapshot returns the session's current mood plus recent
// evaluations. A missing state row is not an error: affect may never have been
// evaluated for a young session.
func (d *DB) SessionAffectSnapshot(ctx context.Context, sessionID string, evalLimit int) (BadSampleAffect, error) {
	if d == nil || d.db == nil {
		return BadSampleAffect{}, errors.New("storage: nil db")
	}
	if sessionID == "" {
		return BadSampleAffect{}, nil
	}
	var out BadSampleAffect
	var state BadSampleAffectState
	err := d.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(persona_id, ''), COALESCE(label, ''), COALESCE(confidence, 0),
		       COALESCE(state_vector_json, ''), COALESCE(cause_summary, ''),
		       COALESCE(mood_description, ''), COALESCE(mood_reason, ''),
		       COALESCE(prompt_mood_text, ''), COALESCE(updated_at, '')
		FROM agent_affect_states WHERE session_id = ?
		ORDER BY updated_at DESC LIMIT 1
	`, sessionID).Scan(&state.ID, &state.PersonaID, &state.Label, &state.Confidence,
		&state.StateVectorJSON, &state.CauseSummary, &state.MoodDescription,
		&state.MoodReason, &state.PromptMoodText, &state.UpdatedAt)
	switch {
	case err == nil:
		out.State = &state
	case errors.Is(err, sql.ErrNoRows):
		// leave State nil
	default:
		return BadSampleAffect{}, fmt.Errorf("session affect state: %w", err)
	}

	if evalLimit <= 0 {
		return out, nil
	}
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, COALESCE(turn_id, ''), COALESCE(trigger_type, ''), COALESCE(status, ''),
		       COALESCE(llm_model, ''), COALESCE(clamped_delta_json, ''), COALESCE(cause_summary, ''),
		       COALESCE(mood_description, ''), COALESCE(mood_reason, ''), COALESCE(confidence, 0),
		       COALESCE(created_at, '')
		FROM agent_affect_evaluations WHERE session_id = ?
		ORDER BY created_at DESC, id DESC LIMIT ?
	`, sessionID, evalLimit)
	if err != nil {
		return out, fmt.Errorf("session affect evaluations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var e BadSampleAffectEvaluation
		if err := rows.Scan(&e.ID, &e.TurnID, &e.TriggerType, &e.Status, &e.LLMModel,
			&e.ClampedDeltaJSON, &e.CauseSummary, &e.MoodDescription, &e.MoodReason,
			&e.Confidence, &e.CreatedAt); err != nil {
			return out, fmt.Errorf("scan affect evaluation: %w", err)
		}
		out.RecentEvaluations = append(out.RecentEvaluations, e)
	}
	return out, rows.Err()
}

// RecentTurns returns the newest turns for the session regardless of status.
//
// Deliberately unfiltered: failed turns are exactly what attribution wants to
// see — "it answered lazily" is often a stage that degraded silently, not the
// model. Contrast with LatestCompletedTurnID, which needs a successful turn.
func (d *DB) RecentTurns(ctx context.Context, sessionID string, limit int) ([]BadSampleTurn, error) {
	if d == nil || d.db == nil {
		return nil, errors.New("storage: nil db")
	}
	if sessionID == "" || limit <= 0 {
		return nil, nil
	}
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, COALESCE(kind, ''), COALESCE(state, ''), COALESCE(status, ''),
		       COALESCE(error_kind, ''), COALESCE(error_message, ''),
		       COALESCE(started_at, ''), COALESCE(completed_at, '')
		FROM turns WHERE session_id = ?
		ORDER BY started_at DESC, id DESC LIMIT ?
	`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("recent turns: %w", err)
	}
	defer rows.Close()
	items := make([]BadSampleTurn, 0, limit)
	for rows.Next() {
		var t BadSampleTurn
		if err := rows.Scan(&t.ID, &t.Kind, &t.State, &t.Status, &t.ErrorKind,
			&t.ErrorMessage, &t.StartedAt, &t.CompletedAt); err != nil {
			return nil, fmt.Errorf("scan turn: %w", err)
		}
		items = append(items, t)
	}
	return items, rows.Err()
}

// LatestCompletedTurnID returns the most recent successfully completed user
// turn, used as the flagged sample's target. Empty string when there is none —
// that is not an error, and must not block recording.
func (d *DB) LatestCompletedTurnID(ctx context.Context, sessionID string) (string, error) {
	if d == nil || d.db == nil {
		return "", errors.New("storage: nil db")
	}
	if sessionID == "" {
		return "", nil
	}
	var id string
	err := d.db.QueryRowContext(ctx, `
		SELECT id FROM turns
		WHERE session_id = ? AND kind = 'user_message' AND status = 'done'
		ORDER BY completed_at DESC, id DESC LIMIT 1
	`, sessionID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("latest completed turn: %w", err)
	}
	return id, nil
}
