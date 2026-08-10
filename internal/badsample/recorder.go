package badsample

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/storage"
)

// ErrEmptyReason is returned when no reason was supplied. The reason is the one
// thing the frozen scene cannot supply on its own: "it forgot I hate cilantro"
// is not derivable from a transcript where that fact never appeared. Recording
// a sample without it would produce an unattributable dead record.
var ErrEmptyReason = errors.New("badsample: reason is required")

type MessageReader interface {
	RecentSessionMessages(ctx context.Context, sessionID string, limit int) ([]storage.BadSampleMessage, error)
}

type SnapshotReader interface {
	RecentPromptSnapshots(ctx context.Context, sessionID string, limit int) ([]storage.BadSamplePromptSnapshot, error)
}

type AffectReader interface {
	SessionAffectSnapshot(ctx context.Context, sessionID string, evalLimit int) (storage.BadSampleAffect, error)
}

type TurnReader interface {
	RecentTurns(ctx context.Context, sessionID string, limit int) ([]storage.BadSampleTurn, error)
	LatestCompletedTurnID(ctx context.Context, sessionID string) (string, error)
}

type Writer interface {
	InsertBadSample(ctx context.Context, record storage.BadSampleRecord) (storage.BadSampleRecord, error)
	CountBadSamples(ctx context.Context) (int, error)
}

// Store is the full dependency set. *storage.DB satisfies it; tests use fakes.
type Store interface {
	MessageReader
	SnapshotReader
	AffectReader
	TurnReader
	Writer
}

type Recorder struct {
	store Store
	now   func() time.Time
}

func NewRecorder(store Store) *Recorder {
	return &Recorder{store: store, now: time.Now}
}

type RecordRequest struct {
	SessionID  string
	OriginKey  string
	PersonaKey string
	Reason     string
}

type Result struct {
	SampleID string
	// TotalCount is the global number of samples, surfaced in the receipt so the
	// user can tell how much material has accumulated.
	TotalCount int
	// FrozenMessages and FrozenSnapshots report what was ACTUALLY captured, not
	// the configured maximums: the receipt must never look rosier than the
	// scene. Snapshots can legitimately be 0 when retention has pruned them.
	FrozenMessages  int
	FrozenSnapshots int
	Truncated       bool
	BlockErrors     []BlockError
}

// Record freezes the current scene and stores it. Scene blocks are collected
// independently: any block that fails is left empty and noted in Errors while
// the rest is still frozen. Only failing to write the row itself is fatal.
func (r *Recorder) Record(ctx context.Context, req RecordRequest) (Result, error) {
	if r == nil || r.store == nil {
		return Result{}, errors.New("badsample: recorder not configured")
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return Result{}, ErrEmptyReason
	}

	frozen := FrozenContext{
		ContextSchemaVersion: ContextSchemaVersion,
		Messages:             []FrozenMessage{},
		PromptSnapshots:      []FrozenPromptSnapshot{},
		Turns:                []FrozenTurn{},
		Affect:               FrozenAffect{RecentEvaluations: []FrozenAffectEvaluation{}},
	}

	messages, err := r.store.RecentSessionMessages(ctx, req.SessionID, MaxMessages)
	frozen.addError("messages", err)
	if err == nil {
		frozen.Messages = convertMessages(messages)
	}

	snapshots, err := r.store.RecentPromptSnapshots(ctx, req.SessionID, MaxPromptSnapshots)
	frozen.addError("prompt_snapshots", err)
	if err == nil {
		frozen.PromptSnapshots = convertSnapshots(snapshots)
	}

	affect, err := r.store.SessionAffectSnapshot(ctx, req.SessionID, MaxAffectEvals)
	frozen.addError("affect", err)
	if err == nil {
		frozen.Affect = convertAffect(affect)
	}

	turns, err := r.store.RecentTurns(ctx, req.SessionID, MaxTurns)
	frozen.addError("turns", err)
	if err == nil {
		frozen.Turns = convertTurns(turns)
	}

	targetTurnID, err := r.store.LatestCompletedTurnID(ctx, req.SessionID)
	frozen.addError("target_turn", err)

	frozen.Meta = FrozenMeta{
		CapturedAt:   r.now().UTC().Format(time.RFC3339Nano),
		SessionID:    req.SessionID,
		PersonaKey:   req.PersonaKey,
		OriginKey:    req.OriginKey,
		TargetTurnID: targetTurnID,
		Model:        newestSnapshotModel(frozen.PromptSnapshots),
	}

	encoded, err := encodeWithinLimit(&frozen)
	if err != nil {
		return Result{}, err
	}

	record, err := r.store.InsertBadSample(ctx, storage.BadSampleRecord{
		Reason:               reason,
		SessionID:            req.SessionID,
		OriginKey:            req.OriginKey,
		PersonaKey:           req.PersonaKey,
		TargetTurnID:         targetTurnID,
		ContextJSON:          string(encoded),
		ContextSchemaVersion: ContextSchemaVersion,
	})
	if err != nil {
		return Result{}, err
	}

	total, err := r.store.CountBadSamples(ctx)
	if err != nil {
		// The row is already safe; a failed count only costs the receipt its
		// number, so report zero rather than failing the whole capture.
		total = 0
	}

	return Result{
		SampleID:        record.ID,
		TotalCount:      total,
		FrozenMessages:  len(frozen.Messages),
		FrozenSnapshots: len(frozen.PromptSnapshots),
		Truncated:       frozen.Truncated,
		BlockErrors:     frozen.Errors,
	}, nil
}

// newestSnapshotModel picks the model from the most recent snapshot that names
// one. Snapshots come back newest-first.
func newestSnapshotModel(snapshots []FrozenPromptSnapshot) string {
	for _, s := range snapshots {
		if s.Model != "" {
			return s.Model
		}
	}
	return ""
}
