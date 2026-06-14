package promptcenter

import "context"

type Store interface {
	GetOverride(ctx context.Context, componentID, scopeType, scopeID string) (*OverrideRecord, error)
	ListOverrides(ctx context.Context) ([]OverrideRecord, error)
	UpsertOverride(ctx context.Context, req UpsertOverrideRequest) error
	DeleteOverride(ctx context.Context, componentID, scopeType, scopeID string) error
	SaveRenderSnapshot(ctx context.Context, snapshot RenderSnapshot) error
	ListRenderSnapshots(ctx context.Context, filter SnapshotFilter) ([]RenderSnapshotSummary, error)
	GetRenderSnapshot(ctx context.Context, id string) (*RenderSnapshot, error)
}

type SnapshotCleaner interface {
	CleanupRenderSnapshots(ctx context.Context, retentionDays int, maxRows int) (CleanupResult, error)
}
