package agentaffect

import "context"

type Store interface {
	EnsureProfile(ctx context.Context, personaID string) (AffectProfile, error)
	GetLatestState(ctx context.Context, personaID string, sessionID string) (*MoodSnapshot, error)
	InsertState(ctx context.Context, state MoodSnapshot) error
	InsertEvaluation(ctx context.Context, eval AffectEvaluationRecord) error
	MarkEvaluationCommitted(ctx context.Context, evaluationID string, afterStateID string) error
	InsertEvent(ctx context.Context, event AffectEventRecord) error
	CommitStateEvent(ctx context.Context, state MoodSnapshot, event AffectEventRecord) error
	InsertPluginWrite(ctx context.Context, write PluginWriteRecord) error
	ListRecentEvaluations(ctx context.Context, q RecentEvaluationsQuery) ([]AffectEvaluationRecord, error)
}
