package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/longyisang/emoagent/internal/resource"
)

type ResourceService struct {
	infra *Infra
}

func (s *ResourceService) grantStore() (*resource.SQLiteGrantStore, error) {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return nil, fmt.Errorf("resource grant store is unavailable")
	}
	return resource.NewSQLiteGrantStore(s.infra.DB.SqlDB()), nil
}

func (s *ResourceService) ListGrants(ctx context.Context, filter resource.GrantListFilter) ([]resource.GrantEnvelope, error) {
	store, err := s.grantStore()
	if err != nil {
		return nil, err
	}
	return store.List(ctx, filter)
}

func (s *ResourceService) RevokeGrant(ctx context.Context, id string) (resource.GrantEnvelope, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return resource.GrantEnvelope{}, fmt.Errorf("resource grant id is required")
	}
	store, err := s.grantStore()
	if err != nil {
		return resource.GrantEnvelope{}, err
	}
	grant, ok, err := store.Get(ctx, id)
	if err != nil {
		return resource.GrantEnvelope{}, err
	}
	if !ok {
		return resource.GrantEnvelope{}, fmt.Errorf("%w: %s", resource.ErrGrantNotFound, id)
	}
	return store.Revoke(ctx, id, grant.Principal)
}

func (s *ResourceService) changeSetManager() (*resource.ChangeSetManager, error) {
	if s == nil || s.infra == nil || s.infra.Config == nil || s.infra.DB == nil {
		return nil, fmt.Errorf("host resource changeset manager is unavailable")
	}
	if !s.infra.Config.HostResources.Enabled {
		return nil, fmt.Errorf("host resources are disabled")
	}
	grantStore := resource.NewSQLiteGrantStore(s.infra.DB.SqlDB())
	broker, err := resource.NewBrokerWithGrantStore(s.infra.Config.HostResources, grantStore)
	if err != nil {
		return nil, err
	}
	return resource.NewChangeSetManager(broker, resource.NewSQLiteChangeSetStore(s.infra.DB.SqlDB()), resource.ChangeSetManagerOptions{
		StagingDir:    appResourceDataDir(s.infra.ProjectRoot, s.infra.Config.HostResources.StagingDir),
		QuarantineDir: appResourceDataDir(s.infra.ProjectRoot, s.infra.Config.HostResources.QuarantineDir),
		MaxBytes:      s.infra.Config.HostResources.MaxReadBytes,
	})
}

func (s *ResourceService) ListChangeSets(ctx context.Context, statuses []resource.ChangeSetStatus) ([]resource.ChangeSet, error) {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return nil, fmt.Errorf("host resource changeset store is unavailable")
	}
	store := resource.NewSQLiteChangeSetStore(s.infra.DB.SqlDB())
	return store.List(ctx, statuses)
}

func (s *ResourceService) GetChangeSet(ctx context.Context, id string) (resource.ChangeSet, error) {
	manager, err := s.changeSetManager()
	if err != nil {
		return resource.ChangeSet{}, err
	}
	cs, err := manager.PreviewChange(ctx, id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return resource.ChangeSet{}, fmt.Errorf("%w: %s", resource.ErrChangeSetNotFound, id)
		}
		return resource.ChangeSet{}, err
	}
	return cs, nil
}

func (s *ResourceService) CancelChangeSet(ctx context.Context, id string) (resource.ChangeSet, error) {
	manager, err := s.changeSetManager()
	if err != nil {
		return resource.ChangeSet{}, err
	}
	cs, err := manager.CancelChange(ctx, id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return resource.ChangeSet{}, fmt.Errorf("%w: %s", resource.ErrChangeSetNotFound, id)
		}
		return resource.ChangeSet{}, err
	}
	return cs, nil
}

func appResourceDataDir(projectRoot, dir string) string {
	if filepath.IsAbs(dir) || projectRoot == "" {
		return dir
	}
	return filepath.Join(projectRoot, dir)
}
