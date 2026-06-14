package app

import (
	"context"

	"github.com/longyisang/emoagent/internal/promptcenter"
)

func (a *App) ListPromptComponents(ctx context.Context, agentID string) (promptcenter.PromptComponentsResponse, error) {
	services, err := a.services()
	if err != nil {
		return promptcenter.PromptComponentsResponse{}, err
	}
	return services.PromptCenter.ListComponents(ctx, agentID)
}

func (a *App) GetPromptComponent(ctx context.Context, id, agentID string) (promptcenter.PromptComponentDetail, error) {
	services, err := a.services()
	if err != nil {
		return promptcenter.PromptComponentDetail{}, err
	}
	return services.PromptCenter.GetComponent(ctx, id, agentID)
}

func (a *App) UpsertPromptOverride(ctx context.Context, req promptcenter.UpsertOverrideRequest) error {
	services, err := a.services()
	if err != nil {
		return err
	}
	return services.PromptCenter.UpsertOverride(ctx, req)
}

func (a *App) DeletePromptOverride(ctx context.Context, req promptcenter.DeleteOverrideRequest) error {
	services, err := a.services()
	if err != nil {
		return err
	}
	return services.PromptCenter.DeleteOverride(ctx, req)
}

func (a *App) PreviewPrompt(ctx context.Context, req promptcenter.PromptPreviewRequest) (promptcenter.PromptPreviewResponse, error) {
	services, err := a.services()
	if err != nil {
		return promptcenter.PromptPreviewResponse{}, err
	}
	return services.PromptCenter.Preview(ctx, req)
}

func (a *App) ListPromptSnapshots(ctx context.Context, req promptcenter.PromptSnapshotListRequest) (promptcenter.PromptSnapshotListResponse, error) {
	services, err := a.services()
	if err != nil {
		return promptcenter.PromptSnapshotListResponse{}, err
	}
	return services.PromptCenter.ListSnapshots(ctx, req)
}

func (a *App) GetPromptSnapshot(ctx context.Context, id string) (promptcenter.PromptSnapshotDetail, error) {
	services, err := a.services()
	if err != nil {
		return promptcenter.PromptSnapshotDetail{}, err
	}
	return services.PromptCenter.GetSnapshot(ctx, id)
}
