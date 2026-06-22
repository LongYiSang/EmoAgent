package app

import (
	"context"

	"github.com/longyisang/emoagent/internal/resource"
)

func (a *App) ListResourceGrants(ctx context.Context, filter resource.GrantListFilter) ([]resource.GrantEnvelope, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.Resource.ListGrants(ctx, filter)
}

func (a *App) RevokeResourceGrant(ctx context.Context, id string) (resource.GrantEnvelope, error) {
	services, err := a.services()
	if err != nil {
		return resource.GrantEnvelope{}, err
	}
	return services.Resource.RevokeGrant(ctx, id)
}

func (a *App) ListHostResourceChangeSets(ctx context.Context, statuses []resource.ChangeSetStatus) ([]resource.ChangeSet, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	return services.Resource.ListChangeSets(ctx, statuses)
}

func (a *App) GetHostResourceChangeSet(ctx context.Context, id string) (resource.ChangeSet, error) {
	services, err := a.services()
	if err != nil {
		return resource.ChangeSet{}, err
	}
	return services.Resource.GetChangeSet(ctx, id)
}

func (a *App) CancelHostResourceChangeSet(ctx context.Context, id string) (resource.ChangeSet, error) {
	services, err := a.services()
	if err != nil {
		return resource.ChangeSet{}, err
	}
	return services.Resource.CancelChangeSet(ctx, id)
}
