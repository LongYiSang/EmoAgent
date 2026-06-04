package turn

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type SQLiteJournal struct {
	db  *sql.DB
	loc *time.Location
}

func NewSQLiteJournal(db *sql.DB) *SQLiteJournal {
	return NewSQLiteJournalWithTimezone(db, "Asia/Shanghai")
}

func NewSQLiteJournalWithTimezone(db *sql.DB, timezone string) *SQLiteJournal {
	return &SQLiteJournal{db: db, loc: loadLocation(timezone)}
}

func (j *SQLiteJournal) StartTurn(ctx context.Context, record TurnRecord) error {
	if j == nil || j.db == nil {
		return fmt.Errorf("sqlite journal database is required")
	}
	if record.TurnID == "" {
		return fmt.Errorf("turn id is required")
	}
	if record.State == "" {
		record.State = StateCreated
	}
	if record.Status == "" {
		record.Status = "running"
	}
	if record.StartedAt.IsZero() {
		record.StartedAt = time.Now()
	}
	now := time.Now()
	_, err := j.db.ExecContext(ctx, `
		INSERT INTO turns (
			id, idempotency_key, source, source_event_id, kind, session_id, persona_key,
			state, status, error_kind, started_at, updated_at, completed_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			idempotency_key = excluded.idempotency_key,
			source = excluded.source,
			source_event_id = excluded.source_event_id,
			kind = excluded.kind,
			session_id = excluded.session_id,
			persona_key = excluded.persona_key,
			state = excluded.state,
			status = excluded.status,
			error_kind = excluded.error_kind,
			started_at = excluded.started_at,
			updated_at = excluded.updated_at,
			completed_at = excluded.completed_at
	`, record.TurnID, nullEmpty(record.IdempotencyKey), string(record.Source), record.SourceEventID, string(record.Kind), record.SessionID, record.PersonaKey, string(record.State), record.Status, record.ErrorKind, j.formatTime(record.StartedAt), j.formatTime(now), j.nullableTime(record.CompletedAt))
	if err != nil {
		return fmt.Errorf("start turn: %w", err)
	}
	return nil
}

func (j *SQLiteJournal) RecordTransition(ctx context.Context, turnID string, from, to TurnState, metrics StageMetrics) error {
	if j == nil || j.db == nil {
		return fmt.Errorf("sqlite journal database is required")
	}
	if turnID == "" {
		return fmt.Errorf("turn id is required")
	}
	if metrics.Stage == "" {
		metrics.Stage = StageName(to)
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transition: %w", err)
	}
	defer tx.Rollback()

	now := time.Now()
	if _, err := tx.ExecContext(ctx, `UPDATE turns SET state = ?, updated_at = ? WHERE id = ?`, string(to), j.formatTime(now), turnID); err != nil {
		return fmt.Errorf("update turn transition: %w", err)
	}
	payload := map[string]any{
		"from":        string(from),
		"to":          string(to),
		"duration_ms": metrics.DurationMS,
	}
	seq, err := nextTurnEventSeq(ctx, tx, turnID)
	if err != nil {
		return err
	}
	if err := j.insertTurnEvent(ctx, tx, turnID, seq, metrics.Stage, "state_transition", payload, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transition: %w", err)
	}
	return nil
}

func (j *SQLiteJournal) RecordEvent(ctx context.Context, turnID string, event JournalEvent) error {
	if j == nil || j.db == nil {
		return fmt.Errorf("sqlite journal database is required")
	}
	if turnID == "" {
		return fmt.Errorf("turn id is required")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin event: %w", err)
	}
	defer tx.Rollback()

	seq := event.Seq
	if seq == 0 {
		seq, err = nextTurnEventSeq(ctx, tx, turnID)
		if err != nil {
			return err
		}
	}
	if err := j.insertTurnEvent(ctx, tx, turnID, seq, event.Stage, event.Type, sanitizePayload(event.Payload), event.CreatedAt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE turns SET updated_at = ? WHERE id = ?`, j.formatTime(time.Now()), turnID); err != nil {
		return fmt.Errorf("update turn event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit event: %w", err)
	}
	return nil
}

func (j *SQLiteJournal) RecordOutbound(ctx context.Context, turnID string, event OutboundEvent) error {
	if j == nil || j.db == nil {
		return fmt.Errorf("sqlite journal database is required")
	}
	if turnID == "" {
		return fmt.Errorf("turn id is required")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	event.TurnID = turnID
	event = sanitizeOutboundEvent(event)
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin outbound: %w", err)
	}
	defer tx.Rollback()

	seq := event.Seq
	if seq == 0 {
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM turn_outbound_events WHERE turn_id = ?`, turnID).Scan(&seq); err != nil {
			return fmt.Errorf("next outbound seq: %w", err)
		}
	}
	payload, err := marshalJSONObject(outboundEventPayload(event))
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO turn_outbound_events (id, turn_id, seq, event_type, payload_json, delivery_status, created_at)
		VALUES (?, ?, ?, ?, ?, 'delivered', ?)
	`, uuid.NewString(), turnID, seq, event.Type, payload, j.formatTime(event.CreatedAt)); err != nil {
		return fmt.Errorf("insert outbound event: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE turns SET updated_at = ? WHERE id = ?`, j.formatTime(time.Now()), turnID); err != nil {
		return fmt.Errorf("update turn outbound: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit outbound: %w", err)
	}
	return nil
}

func (j *SQLiteJournal) CompleteTurn(ctx context.Context, turnID, status, errorKind string) error {
	if j == nil || j.db == nil {
		return fmt.Errorf("sqlite journal database is required")
	}
	if turnID == "" {
		return fmt.Errorf("turn id is required")
	}
	if status == "" {
		status = "done"
	}
	now := time.Now()
	_, err := j.db.ExecContext(ctx, `
		UPDATE turns
		SET status = ?, error_kind = ?, updated_at = ?, completed_at = ?
		WHERE id = ?
	`, status, errorKind, j.formatTime(now), j.formatTime(now), turnID)
	if err != nil {
		return fmt.Errorf("complete turn: %w", err)
	}
	return nil
}

func (j *SQLiteJournal) GetTurn(ctx context.Context, turnID string) (TurnSnapshot, bool, error) {
	if j == nil || j.db == nil {
		return TurnSnapshot{}, false, fmt.Errorf("sqlite journal database is required")
	}
	row := j.db.QueryRowContext(ctx, `
		SELECT id, COALESCE(idempotency_key, ''), source, source_event_id, kind, session_id, persona_key,
		       state, status, error_kind, started_at, COALESCE(completed_at, '')
		FROM turns
		WHERE id = ?
	`, turnID)
	var record TurnRecord
	var source, kind, state, startedAt, completedAt string
	if err := row.Scan(&record.TurnID, &record.IdempotencyKey, &source, &record.SourceEventID, &kind, &record.SessionID, &record.PersonaKey, &state, &record.Status, &record.ErrorKind, &startedAt, &completedAt); err != nil {
		if err == sql.ErrNoRows {
			return TurnSnapshot{}, false, nil
		}
		return TurnSnapshot{}, false, fmt.Errorf("get turn: %w", err)
	}
	record.Source = InboundSource(source)
	record.Kind = InboundKind(kind)
	record.State = TurnState(state)
	record.StartedAt = parseTime(startedAt)
	record.CompletedAt = parseTime(completedAt)
	snapshot := TurnSnapshot{TurnRecord: record}

	rows, err := j.db.QueryContext(ctx, `
		SELECT seq, stage, event_type, payload_json, created_at
		FROM turn_events
		WHERE turn_id = ?
		ORDER BY seq ASC
	`, turnID)
	if err != nil {
		return TurnSnapshot{}, false, fmt.Errorf("list turn events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var seq int64
		var stage, eventType, payloadJSON, createdAt string
		if err := rows.Scan(&seq, &stage, &eventType, &payloadJSON, &createdAt); err != nil {
			return TurnSnapshot{}, false, fmt.Errorf("scan turn event: %w", err)
		}
		payload := decodePayload(payloadJSON)
		if eventType == "state_transition" {
			snapshot.Transitions = append(snapshot.Transitions, TurnTransition{
				From:       TurnState(stringFromPayload(payload, "from")),
				To:         TurnState(stringFromPayload(payload, "to")),
				Stage:      StageName(stage),
				DurationMS: int64FromPayload(payload, "duration_ms"),
				CreatedAt:  parseTime(createdAt),
			})
			continue
		}
		snapshot.Events = append(snapshot.Events, JournalEvent{
			Seq:       seq,
			Stage:     StageName(stage),
			Type:      eventType,
			Payload:   sanitizePayload(payload),
			CreatedAt: parseTime(createdAt),
		})
	}
	if err := rows.Err(); err != nil {
		return TurnSnapshot{}, false, fmt.Errorf("scan turn events: %w", err)
	}
	outbound, err := j.ListOutbound(ctx, turnID)
	if err != nil {
		return TurnSnapshot{}, false, err
	}
	snapshot.Outbound = outbound
	return snapshot, true, nil
}

func (j *SQLiteJournal) ListOutbound(ctx context.Context, turnID string) ([]OutboundEvent, error) {
	if j == nil || j.db == nil {
		return nil, fmt.Errorf("sqlite journal database is required")
	}
	rows, err := j.db.QueryContext(ctx, `
		SELECT seq, event_type, payload_json, created_at
		FROM turn_outbound_events
		WHERE turn_id = ?
		ORDER BY seq ASC
	`, turnID)
	if err != nil {
		return nil, fmt.Errorf("list outbound events: %w", err)
	}
	defer rows.Close()

	var events []OutboundEvent
	for rows.Next() {
		var event OutboundEvent
		var payloadJSON, createdAt string
		if err := rows.Scan(&event.Seq, &event.Type, &payloadJSON, &createdAt); err != nil {
			return nil, fmt.Errorf("scan outbound event: %w", err)
		}
		payload := decodePayload(payloadJSON)
		event.TurnID = turnID
		event.Payload = payloadMapFromPayload(payload, "payload")
		if event.Payload == nil {
			event.Payload = map[string]any{}
		}
		if bytes := int64FromPayload(payload, "content_bytes"); bytes > 0 {
			event.Payload["content_bytes"] = bytes
		}
		if hash := stringFromPayload(payload, "content_hash"); hash != "" {
			event.Payload["content_hash"] = hash
		}
		event.CreatedAt = parseTime(createdAt)
		events = append(events, sanitizeOutboundEvent(event))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan outbound events: %w", err)
	}
	return events, nil
}

func (j *SQLiteJournal) insertTurnEvent(ctx context.Context, tx *sql.Tx, turnID string, seq int64, stage StageName, eventType string, payload map[string]any, createdAt time.Time) error {
	payloadJSON, err := marshalJSONObject(payload)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO turn_events (id, turn_id, seq, stage, event_type, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, uuid.NewString(), turnID, seq, string(stage), eventType, payloadJSON, j.formatTime(createdAt)); err != nil {
		return fmt.Errorf("insert turn event: %w", err)
	}
	return nil
}

func nextTurnEventSeq(ctx context.Context, tx *sql.Tx, turnID string) (int64, error) {
	var seq int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM turn_events WHERE turn_id = ?`, turnID).Scan(&seq); err != nil {
		return 0, fmt.Errorf("next event seq: %w", err)
	}
	return seq, nil
}

func marshalJSONObject(payload map[string]any) (string, error) {
	if payload == nil {
		return "{}", nil
	}
	data, err := json.Marshal(sanitizePayload(payload))
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	if len(data) == 0 {
		return "{}", nil
	}
	return string(data), nil
}

func decodePayload(raw string) map[string]any {
	if raw == "" {
		return map[string]any{}
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return map[string]any{}
	}
	return sanitizePayload(payload)
}

func stringFromPayload(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

func int64FromPayload(payload map[string]any, key string) int64 {
	switch value := payload[key].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func payloadMapFromPayload(payload map[string]any, key string) map[string]any {
	value, _ := payload[key].(map[string]any)
	return sanitizePayload(value)
}

func (j *SQLiteJournal) formatTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.In(j.location()).Format(time.RFC3339Nano)
}

func parseTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	ts, _ := time.Parse(time.RFC3339Nano, raw)
	return ts
}

func (j *SQLiteJournal) nullableTime(ts time.Time) any {
	if ts.IsZero() {
		return nil
	}
	return j.formatTime(ts)
}

func (j *SQLiteJournal) location() *time.Location {
	if j != nil && j.loc != nil {
		return j.loc
	}
	return time.FixedZone("Asia/Shanghai", 8*60*60)
}

func loadLocation(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return loc
}

func nullEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
