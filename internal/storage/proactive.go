package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Candidate status values. A candidate starts pending, becomes skipped when the
// gate declines it (it stays visible to later gate runs and to the injector),
// consumed once it backs a delivered message, and expired past its TTL.
const (
	ProactiveCandidateStatusPending  = "pending"
	ProactiveCandidateStatusSkipped  = "skipped"
	ProactiveCandidateStatusConsumed = "consumed"
	ProactiveCandidateStatusExpired  = "expired"
)

const (
	ProactiveDecisionSpeak = "speak"
	ProactiveDecisionSkip  = "skip"
)

// ProactiveCandidateRecord is one proposed reason to speak up, submitted by a
// plugin or an internal trigger source. Candidates never carry a delivery
// target: which origin to reach the user on is resolved at dispatch time.
type ProactiveCandidateRecord struct {
	ID             string
	SourcePluginID string
	PersonaKey     string
	EventType      string
	Summary        string
	ObservedFrom   string
	ObservedTo     string
	Importance     float64
	PayloadJSON    string
	Status         string
	SkipCount      int
	LastDecisionID string
	CreatedAt      string
	ExpiresAt      string
}

// ProactiveDecisionRecord is the audit trail of every gate verdict, including
// the skips. Skips must be observable: with the agent silent most of the time,
// an unrecorded skip is indistinguishable from a broken pipeline.
type ProactiveDecisionRecord struct {
	ID                string
	PersonaKey        string
	OriginKey         string
	CandidateIDs      []string
	Decision          string
	Reason            string
	Urgency           float64
	Hint              string
	GateModel         string
	GateLatencyMS     int64
	GateTokens        int
	TurnID            string
	SilencedByEmotion bool
	UserRepliedAt     string
	CreatedAt         string
}

type ProactiveCandidateFilter struct {
	PersonaKey string
	Statuses   []string
	Since      time.Time
	Limit      int
}

func (d *DB) InsertProactiveCandidate(ctx context.Context, record ProactiveCandidateRecord) error {
	if strings.TrimSpace(record.ID) == "" {
		return errors.New("candidate id is required")
	}
	if strings.TrimSpace(record.PersonaKey) == "" {
		return errors.New("persona key is required")
	}
	if strings.TrimSpace(record.EventType) == "" {
		return errors.New("event type is required")
	}
	if strings.TrimSpace(record.Summary) == "" {
		return errors.New("summary is required")
	}
	if record.PayloadJSON == "" {
		record.PayloadJSON = "{}"
	}
	if err := validateJSONObject(record.PayloadJSON, "payload_json"); err != nil {
		return err
	}
	if record.Status == "" {
		record.Status = ProactiveCandidateStatusPending
	}
	record.Importance = clampUnit(record.Importance)
	now := d.nowText()
	if record.CreatedAt == "" {
		record.CreatedAt = now
	}
	if record.ExpiresAt == "" {
		return errors.New("expires_at is required")
	}
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO proactive_candidates (
			id, source_plugin_id, persona_key, event_type, summary,
			observed_from, observed_to, importance, payload_json,
			status, skip_count, last_decision_id, created_at, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, record.ID, record.SourcePluginID, record.PersonaKey, record.EventType, record.Summary,
		record.ObservedFrom, record.ObservedTo, record.Importance, record.PayloadJSON,
		record.Status, record.SkipCount, record.LastDecisionID, record.CreatedAt, record.ExpiresAt)
	return err
}

// CountProactiveCandidates reports how many candidates are in the given
// statuses. Used for the max_pending backpressure check at propose time.
func (d *DB) CountProactiveCandidates(ctx context.Context, personaKey string, statuses []string) (int, error) {
	query := `SELECT COUNT(*) FROM proactive_candidates WHERE persona_key = ?`
	args := []any{personaKey}
	if len(statuses) > 0 {
		query += ` AND status IN (` + placeholders(len(statuses)) + `)`
		for _, status := range statuses {
			args = append(args, status)
		}
	}
	var count int
	if err := d.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (d *DB) ListProactiveCandidates(ctx context.Context, filter ProactiveCandidateFilter) ([]ProactiveCandidateRecord, error) {
	query := `
		SELECT id, source_plugin_id, persona_key, event_type, summary,
		       observed_from, observed_to, importance, payload_json,
		       status, skip_count, last_decision_id, created_at, expires_at
		FROM proactive_candidates
		WHERE persona_key = ?`
	args := []any{filter.PersonaKey}
	if len(filter.Statuses) > 0 {
		query += ` AND status IN (` + placeholders(len(filter.Statuses)) + `)`
		for _, status := range filter.Statuses {
			args = append(args, status)
		}
	}
	if !filter.Since.IsZero() {
		query += ` AND created_at >= ?`
		args = append(args, d.formatTime(filter.Since))
	}
	query += ` ORDER BY created_at DESC`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []ProactiveCandidateRecord
	for rows.Next() {
		var record ProactiveCandidateRecord
		if err := rows.Scan(
			&record.ID, &record.SourcePluginID, &record.PersonaKey, &record.EventType, &record.Summary,
			&record.ObservedFrom, &record.ObservedTo, &record.Importance, &record.PayloadJSON,
			&record.Status, &record.SkipCount, &record.LastDecisionID, &record.CreatedAt, &record.ExpiresAt,
		); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// MarkProactiveCandidates moves candidates to a new status and links them to the
// decision that caused the move. A skip also bumps skip_count so the gate can
// see it has already declined this candidate N times.
func (d *DB) MarkProactiveCandidates(ctx context.Context, ids []string, status string, decisionID string) error {
	if len(ids) == 0 {
		return nil
	}
	if strings.TrimSpace(status) == "" {
		return errors.New("status is required")
	}
	skipIncrement := 0
	if status == ProactiveCandidateStatusSkipped {
		skipIncrement = 1
	}
	args := []any{status, decisionID, skipIncrement}
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := d.db.ExecContext(ctx, `
		UPDATE proactive_candidates
		SET status = ?, last_decision_id = ?, skip_count = skip_count + ?
		WHERE id IN (`+placeholders(len(ids))+`)
	`, args...)
	return err
}

// ExpireProactiveCandidates retires candidates past their TTL so neither the
// gate nor the injector keeps reacting to stale activity.
func (d *DB) ExpireProactiveCandidates(ctx context.Context, now time.Time) (int, error) {
	result, err := d.db.ExecContext(ctx, `
		UPDATE proactive_candidates
		SET status = ?
		WHERE status IN (?, ?) AND expires_at <= ?
	`, ProactiveCandidateStatusExpired,
		ProactiveCandidateStatusPending, ProactiveCandidateStatusSkipped,
		d.formatTime(now))
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	return int(affected), err
}

func (d *DB) InsertProactiveDecision(ctx context.Context, record ProactiveDecisionRecord) error {
	if strings.TrimSpace(record.ID) == "" {
		return errors.New("decision id is required")
	}
	if strings.TrimSpace(record.PersonaKey) == "" {
		return errors.New("persona key is required")
	}
	switch record.Decision {
	case ProactiveDecisionSpeak, ProactiveDecisionSkip:
	default:
		return fmt.Errorf("unsupported proactive decision: %s", record.Decision)
	}
	idsJSON, err := json.Marshal(nonNilStrings(record.CandidateIDs))
	if err != nil {
		return fmt.Errorf("encode candidate ids: %w", err)
	}
	if record.CreatedAt == "" {
		record.CreatedAt = d.nowText()
	}
	var repliedAt any
	if strings.TrimSpace(record.UserRepliedAt) != "" {
		repliedAt = record.UserRepliedAt
	}
	_, err = d.db.ExecContext(ctx, `
		INSERT INTO proactive_decisions (
			id, persona_key, origin_key, candidate_ids_json, decision, reason,
			urgency, hint, gate_model, gate_latency_ms, gate_tokens, turn_id,
			silenced_by_emotion, user_replied_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, record.ID, record.PersonaKey, record.OriginKey, string(idsJSON), record.Decision, record.Reason,
		record.Urgency, record.Hint, record.GateModel, record.GateLatencyMS, record.GateTokens, record.TurnID,
		boolToInt(record.SilencedByEmotion), repliedAt, record.CreatedAt)
	return err
}

// UpdateProactiveDecisionOutcome records what the turn produced: the turn id and
// whether Emotion chose to stay silent after the gate had allowed speaking.
func (d *DB) UpdateProactiveDecisionOutcome(ctx context.Context, decisionID string, turnID string, silencedByEmotion bool) error {
	if strings.TrimSpace(decisionID) == "" {
		return errors.New("decision id is required")
	}
	_, err := d.db.ExecContext(ctx, `
		UPDATE proactive_decisions
		SET turn_id = ?, silenced_by_emotion = ?
		WHERE id = ?
	`, turnID, boolToInt(silencedByEmotion), decisionID)
	return err
}

func (d *DB) ListProactiveDecisions(ctx context.Context, personaKey string, limit int) ([]ProactiveDecisionRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, persona_key, origin_key, candidate_ids_json, decision, reason,
		       urgency, hint, gate_model, gate_latency_ms, gate_tokens, turn_id,
		       silenced_by_emotion, user_replied_at, created_at
		FROM proactive_decisions
		WHERE persona_key = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, personaKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []ProactiveDecisionRecord
	for rows.Next() {
		var (
			record    ProactiveDecisionRecord
			idsJSON   string
			silenced  int
			repliedAt sql.NullString
		)
		if err := rows.Scan(
			&record.ID, &record.PersonaKey, &record.OriginKey, &idsJSON, &record.Decision, &record.Reason,
			&record.Urgency, &record.Hint, &record.GateModel, &record.GateLatencyMS, &record.GateTokens, &record.TurnID,
			&silenced, &repliedAt, &record.CreatedAt,
		); err != nil {
			return nil, err
		}
		record.SilencedByEmotion = silenced != 0
		if repliedAt.Valid {
			record.UserRepliedAt = repliedAt.String
		}
		if strings.TrimSpace(idsJSON) != "" {
			_ = json.Unmarshal([]byte(idsJSON), &record.CandidateIDs)
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// CountProactiveSpeakSince backs the daily hard cap. The cap is evaluated before
// the gate runs so that no model verdict can talk past it.
func (d *DB) CountProactiveSpeakSince(ctx context.Context, personaKey string, since time.Time) (int, error) {
	var count int
	err := d.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM proactive_decisions
		WHERE persona_key = ? AND decision = ? AND created_at >= ?
	`, personaKey, ProactiveDecisionSpeak, d.formatTime(since)).Scan(&count)
	return count, err
}

// BackfillProactiveUserReplied attributes a user message to the most recent
// delivered proactive message on the same origin, when it lands inside the
// attribution window. This is the only real feedback signal the gate has.
func (d *DB) BackfillProactiveUserReplied(ctx context.Context, originKey string, repliedAt time.Time, window time.Duration) (bool, error) {
	if strings.TrimSpace(originKey) == "" || window <= 0 {
		return false, nil
	}
	result, err := d.db.ExecContext(ctx, `
		UPDATE proactive_decisions
		SET user_replied_at = ?
		WHERE id = (
			SELECT id FROM proactive_decisions
			WHERE origin_key = ?
			  AND decision = ?
			  AND silenced_by_emotion = 0
			  AND user_replied_at IS NULL
			  AND created_at >= ?
			ORDER BY created_at DESC
			LIMIT 1
		)
	`, d.formatTime(repliedAt), originKey, ProactiveDecisionSpeak, d.formatTime(repliedAt.Add(-window)))
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

// CountProactiveConsecutiveIgnored counts how many of the most recent delivered
// proactive messages went unanswered, newest first, stopping at the first reply.
func (d *DB) CountProactiveConsecutiveIgnored(ctx context.Context, personaKey string, limit int) (int, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := d.db.QueryContext(ctx, `
		SELECT user_replied_at FROM proactive_decisions
		WHERE persona_key = ? AND decision = ? AND silenced_by_emotion = 0
		ORDER BY created_at DESC
		LIMIT ?
	`, personaKey, ProactiveDecisionSpeak, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	ignored := 0
	for rows.Next() {
		var repliedAt sql.NullString
		if err := rows.Scan(&repliedAt); err != nil {
			return 0, err
		}
		if repliedAt.Valid && strings.TrimSpace(repliedAt.String) != "" {
			break
		}
		ignored++
	}
	return ignored, rows.Err()
}

// ProactiveTargetCandidate is a place the agent could reach the user, ordered by
// how recently they actually talked there.
type ProactiveTargetCandidate struct {
	OriginKey              string
	SourceType             string
	AdapterInstanceID      string
	PlatformID             string
	ChannelType            string
	ExternalConversationID string
	ExternalActorID        string
	DisplayName            string
	SessionID              string
	LastMessageAt          string
}

// ListProactiveTargetCandidates returns the origins bound to a persona that have
// real conversation history, newest first.
//
// Origins with no messages are excluded on purpose: a proactive message must
// never be the first thing the agent ever says on a channel.
func (d *DB) ListProactiveTargetCandidates(ctx context.Context, personaKey string) ([]ProactiveTargetCandidate, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT o.origin_key, o.source_type, o.adapter_instance_id, o.platform_id, o.channel_type,
		       o.external_conversation_id, o.external_actor_id, o.display_name,
		       b.current_session_id,
		       (SELECT MAX(m.created_at) FROM messages m WHERE m.session_id = b.current_session_id) AS last_message_at
		FROM conversation_bindings b
		JOIN conversation_origins o ON o.origin_key = b.origin_key
		WHERE b.persona_key = ?
		ORDER BY last_message_at DESC
	`, personaKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []ProactiveTargetCandidate
	for rows.Next() {
		var (
			candidate     ProactiveTargetCandidate
			lastMessageAt sql.NullString
		)
		if err := rows.Scan(
			&candidate.OriginKey, &candidate.SourceType, &candidate.AdapterInstanceID, &candidate.PlatformID,
			&candidate.ChannelType, &candidate.ExternalConversationID, &candidate.ExternalActorID,
			&candidate.DisplayName, &candidate.SessionID, &lastMessageAt,
		); err != nil {
			return nil, err
		}
		if !lastMessageAt.Valid || strings.TrimSpace(lastMessageAt.String) == "" {
			continue
		}
		candidate.LastMessageAt = lastMessageAt.String
		candidates = append(candidates, candidate)
	}
	return candidates, rows.Err()
}

// LastMessageTimes reports when the user and the agent last spoke in a session,
// feeding the interruption-cost block of the gate input.
func (d *DB) LastMessageTimes(ctx context.Context, sessionID string) (lastUser string, lastAgent string, err error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT
			(SELECT MAX(created_at) FROM messages WHERE session_id = ? AND role = 'user'),
			(SELECT MAX(created_at) FROM messages WHERE session_id = ? AND role = 'assistant')
	`, sessionID, sessionID)
	var user, agent sql.NullString
	if err := row.Scan(&user, &agent); err != nil {
		return "", "", err
	}
	if user.Valid {
		lastUser = user.String
	}
	if agent.Valid {
		lastAgent = agent.String
	}
	return lastUser, lastAgent, nil
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func clampUnit(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
