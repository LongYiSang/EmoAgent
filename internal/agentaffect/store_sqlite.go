package agentaffect

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type SQLiteStore struct {
	db *sql.DB
}

type sqlExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	if db != nil {
		db.SetMaxOpenConns(1)
	}
	return &SQLiteStore{db: db}
}

func (s *SQLiteStore) EnsureProfile(ctx context.Context, personaID string) (AffectProfile, error) {
	var p AffectProfile
	row := s.db.QueryRowContext(ctx, `
SELECT id, persona_id, profile_name,
       baseline_valence, baseline_arousal, baseline_dominance, baseline_energy,
       baseline_warmth, baseline_concern, baseline_curiosity, baseline_playfulness,
       baseline_attachment, baseline_frustration, baseline_uncertainty,
       created_at, updated_at
FROM agent_affect_profiles
WHERE persona_id = ? AND profile_name = 'default'
`, personaID)
	var createdAt string
	var updatedAt sql.NullString
	err := row.Scan(
		&p.ID,
		&p.PersonaID,
		&p.ProfileName,
		&p.Baseline.Valence,
		&p.Baseline.Arousal,
		&p.Baseline.Dominance,
		&p.Baseline.Energy,
		&p.Baseline.Warmth,
		&p.Baseline.Concern,
		&p.Baseline.Curiosity,
		&p.Baseline.Playfulness,
		&p.Baseline.Attachment,
		&p.Baseline.Frustration,
		&p.Baseline.Uncertainty,
		&createdAt,
		&updatedAt,
	)
	if err == nil {
		p.CreatedAt = parseDBTime(createdAt)
		if updatedAt.Valid {
			updated := parseDBTime(updatedAt.String)
			p.UpdatedAt = &updated
		}
		return p, nil
	}
	if err != sql.ErrNoRows {
		return AffectProfile{}, fmt.Errorf("get affect profile: %w", err)
	}
	p = AffectProfile{
		ID:          uuid.NewString(),
		PersonaID:   personaID,
		ProfileName: "default",
		Baseline: MoodVector{
			Valence:     0,
			Arousal:     0.2,
			Dominance:   0,
			Energy:      0.5,
			Warmth:      0.6,
			Concern:     0.3,
			Curiosity:   0.3,
			Playfulness: 0.2,
			Attachment:  0,
			Frustration: 0,
			Uncertainty: 0.1,
		},
		CreatedAt: time.Now().UTC(),
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO agent_affect_profiles (
    id, persona_id, profile_name,
    baseline_valence, baseline_arousal, baseline_dominance, baseline_energy,
    baseline_warmth, baseline_concern, baseline_curiosity, baseline_playfulness,
    baseline_attachment, baseline_frustration, baseline_uncertainty
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, p.ID, p.PersonaID, p.ProfileName,
		p.Baseline.Valence, p.Baseline.Arousal, p.Baseline.Dominance, p.Baseline.Energy,
		p.Baseline.Warmth, p.Baseline.Concern, p.Baseline.Curiosity, p.Baseline.Playfulness,
		p.Baseline.Attachment, p.Baseline.Frustration, p.Baseline.Uncertainty,
	); err != nil {
		return AffectProfile{}, fmt.Errorf("insert affect profile: %w", err)
	}
	return p, nil
}

func (s *SQLiteStore) GetLatestState(ctx context.Context, personaID string, sessionID string) (*MoodSnapshot, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, persona_id, COALESCE(session_id, ''),
       valence, arousal, dominance, energy, warmth, concern, curiosity, playfulness,
       attachment, frustration, uncertainty,
       COALESCE(label, ''), confidence, cause_summary, visible_cause_summary,
       cause_stack_json, updated_at
FROM agent_affect_states
WHERE persona_id = ? AND COALESCE(session_id, '') = ?
ORDER BY updated_at DESC
LIMIT 1
`, personaID, sessionID)
	var snapshot MoodSnapshot
	var causeStackJSON string
	var updatedAt string
	err := row.Scan(
		&snapshot.StateID,
		&snapshot.PersonaID,
		&snapshot.SessionID,
		&snapshot.Vector.Valence,
		&snapshot.Vector.Arousal,
		&snapshot.Vector.Dominance,
		&snapshot.Vector.Energy,
		&snapshot.Vector.Warmth,
		&snapshot.Vector.Concern,
		&snapshot.Vector.Curiosity,
		&snapshot.Vector.Playfulness,
		&snapshot.Vector.Attachment,
		&snapshot.Vector.Frustration,
		&snapshot.Vector.Uncertainty,
		&snapshot.Label,
		&snapshot.Confidence,
		&snapshot.CauseSummary,
		&snapshot.VisibleCauseSummary,
		&causeStackJSON,
		&updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get latest affect state: %w", err)
	}
	_ = json.Unmarshal([]byte(causeStackJSON), &snapshot.CauseStack)
	snapshot.UpdatedAt = parseDBTime(updatedAt)
	return &snapshot, nil
}

func (s *SQLiteStore) InsertState(ctx context.Context, state MoodSnapshot) error {
	return insertState(ctx, s.db, state)
}

func insertState(ctx context.Context, exec sqlExecer, state MoodSnapshot) error {
	stateVectorJSON := mustJSON(state.Vector)
	causeStackJSON := mustJSON(state.CauseStack)
	_, err := exec.ExecContext(ctx, `
INSERT INTO agent_affect_states (
    id, persona_id, session_id,
    valence, arousal, dominance, energy, warmth, concern, curiosity, playfulness,
    attachment, frustration, uncertainty,
    label, confidence, state_vector_json, cause_summary, visible_cause_summary,
    cause_stack_json, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, state.StateID, state.PersonaID, nilIfEmpty(state.SessionID),
		state.Vector.Valence, state.Vector.Arousal, state.Vector.Dominance, state.Vector.Energy,
		state.Vector.Warmth, state.Vector.Concern, state.Vector.Curiosity, state.Vector.Playfulness,
		state.Vector.Attachment, state.Vector.Frustration, state.Vector.Uncertainty,
		nilIfEmpty(state.Label), state.Confidence, stateVectorJSON, state.CauseSummary, state.VisibleCauseSummary,
		causeStackJSON, dbTime(state.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert affect state: %w", err)
	}
	return nil
}

func (s *SQLiteStore) InsertEvaluation(ctx context.Context, eval AffectEvaluationRecord) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO agent_affect_evaluations (
    id, persona_id, session_id, turn_id,
    trigger_type, custom_type, custom_type_desc, source_kind, source_ref_type,
    source_ref_id, source_ref_hash, plugin_id,
    input_mode, input_text, input_summary, context_window_policy_json, context_window_snapshot_json,
    before_state_id, before_state_json,
    llm_provider, llm_model, llm_thinking_enabled, prompt_version, prompt_hash, prompt_snapshot, response_json,
    proposed_delta_json, clamped_delta_json, predicted_state_json,
    cause_summary, visible_cause_summary, confidence, clamp_notes_json, status, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, eval.ID, eval.PersonaID, nilIfEmpty(eval.SessionID), nilIfEmpty(eval.TurnID),
		eval.Trigger.TriggerType, nilIfEmpty(eval.Trigger.CustomType), nilIfEmpty(eval.Trigger.CustomTypeDesc), eval.Trigger.SourceKind, nilIfEmpty(eval.Trigger.SourceRefType),
		nilIfEmpty(eval.Trigger.SourceRefID), nilIfEmpty(eval.Trigger.SourceRefHash), nilIfEmpty(eval.Trigger.PluginID),
		defaultString(eval.Input.Mode, "raw"), nilIfEmpty(eval.Input.Text), nilIfEmpty(eval.Input.Summary), defaultString(eval.ContextWindowPolicyJSON, "{}"), nilIfEmpty(eval.ContextWindowSnapshotJSON),
		nilIfEmpty(eval.BeforeStateID), defaultString(eval.BeforeStateJSON, "{}"),
		nilIfEmpty(eval.LLMProvider), nilIfEmpty(eval.LLMModel), boolInt(eval.LLMThinkingEnabled), defaultString(eval.PromptVersion, "agent_affect_v2.prompt.v1"), eval.PromptHash, nilIfEmpty(eval.PromptSnapshot), nilIfEmpty(eval.ResponseJSON),
		mustJSON(eval.ProposedDelta), mustJSON(eval.ClampedDelta), mustJSON(eval.PredictedState),
		eval.CauseSummary, eval.VisibleCauseSummary, eval.Confidence, mustJSON(eval.ClampNotes), defaultString(eval.Status, EvaluationStatusPreview), dbTime(eval.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert affect evaluation: %w", err)
	}
	return nil
}

func (s *SQLiteStore) MarkEvaluationCommitted(ctx context.Context, evaluationID string, afterStateID string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE agent_affect_evaluations SET status = ? WHERE id = ?", EvaluationStatusCommitted, evaluationID)
	if err != nil {
		return fmt.Errorf("mark affect evaluation committed: %w", err)
	}
	return nil
}

func (s *SQLiteStore) InsertEvent(ctx context.Context, event AffectEventRecord) error {
	return insertEvent(ctx, s.db, event)
}

func insertEvent(ctx context.Context, exec sqlExecer, event AffectEventRecord) error {
	_, err := exec.ExecContext(ctx, `
INSERT INTO agent_affect_events (
    id, persona_id, session_id, turn_id, evaluation_id, trigger_type, custom_type, plugin_id,
    before_state_id, after_state_id,
    proposed_delta_json, clamped_delta_json, committed_delta_json,
    label_before, label_after, cause_summary, significance, confidence, committed_by, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, event.ID, event.PersonaID, nilIfEmpty(event.SessionID), nilIfEmpty(event.TurnID), nilIfEmpty(event.EvaluationID),
		event.Trigger.TriggerType, nilIfEmpty(event.Trigger.CustomType), nilIfEmpty(event.Trigger.PluginID),
		nilIfEmpty(event.BeforeStateID), nilIfEmpty(event.AfterStateID),
		mustJSON(event.ProposedDelta), mustJSON(event.ClampedDelta), mustJSON(event.CommittedDelta),
		nilIfEmpty(event.LabelBefore), nilIfEmpty(event.LabelAfter), event.CauseSummary, event.Significance, event.Confidence, defaultString(event.CommittedBy, "core"), dbTime(event.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert affect event: %w", err)
	}
	return nil
}

func (s *SQLiteStore) CommitStateEvent(ctx context.Context, state MoodSnapshot, event AffectEventRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin affect state event commit: %w", err)
	}
	if err := insertState(ctx, tx, state); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := insertEvent(ctx, tx, event); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit affect state event: %w", err)
	}
	return nil
}

func (s *SQLiteStore) InsertPluginWrite(ctx context.Context, write PluginWriteRecord) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO agent_affect_plugin_writes (
    id, persona_id, session_id, turn_id, plugin_id, capability, request_kind, request_json,
    accepted, rejection_reason, clamp_notes_json, evaluation_id, affect_event_id, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, write.ID, write.PersonaID, nilIfEmpty(write.SessionID), nilIfEmpty(write.TurnID), write.PluginID, write.Capability, write.RequestKind, defaultString(write.RequestJSON, "{}"),
		boolInt(write.Accepted), nilIfEmpty(write.RejectionReason), mustJSON(write.ClampNotes), nilIfEmpty(write.EvaluationID), nilIfEmpty(write.AffectEventID), dbTime(write.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert affect plugin write: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListRecentEvaluations(ctx context.Context, q RecentEvaluationsQuery) ([]AffectEvaluationRecord, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, persona_id, COALESCE(session_id, ''), COALESCE(turn_id, ''),
       trigger_type, COALESCE(custom_type, ''), source_kind,
       input_mode, COALESCE(input_summary, ''), cause_summary, visible_cause_summary,
       proposed_delta_json, clamped_delta_json, predicted_state_json, confidence, status, created_at
FROM agent_affect_evaluations
WHERE persona_id = ? AND COALESCE(session_id, '') = ?
ORDER BY created_at DESC
LIMIT ?
`, q.PersonaID, q.SessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent affect evaluations: %w", err)
	}
	defer rows.Close()
	var out []AffectEvaluationRecord
	for rows.Next() {
		var rec AffectEvaluationRecord
		var proposed, clamped, predicted string
		var createdAt string
		if err := rows.Scan(
			&rec.ID, &rec.PersonaID, &rec.SessionID, &rec.TurnID,
			&rec.Trigger.TriggerType, &rec.Trigger.CustomType, &rec.Trigger.SourceKind,
			&rec.Input.Mode, &rec.Input.Summary, &rec.CauseSummary, &rec.VisibleCauseSummary,
			&proposed, &clamped, &predicted, &rec.Confidence, &rec.Status, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scan affect evaluation: %w", err)
		}
		_ = json.Unmarshal([]byte(proposed), &rec.ProposedDelta)
		_ = json.Unmarshal([]byte(clamped), &rec.ClampedDelta)
		_ = json.Unmarshal([]byte(predicted), &rec.PredictedState)
		rec.CreatedAt = parseDBTime(createdAt)
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate affect evaluations: %w", err)
	}
	return out, nil
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func nilIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func defaultString(v string, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func dbTime(t time.Time) string {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseDBTime(v string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
		return t
	}
	return time.Time{}
}
