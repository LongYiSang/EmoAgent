package app

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/media"
)

type MediaService struct {
	infra *Infra
	mu    sync.Mutex
	store *media.LocalStore
}

func (s *MediaService) Store() *media.LocalStore {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store != nil {
		return s.store
	}
	cfg := config.DefaultConfig().Media
	if s.infra.Config != nil {
		cfg = s.infra.Config.Media
	}
	root := strings.TrimSpace(cfg.StorageDir)
	if root == "" {
		root = config.DefaultConfig().Media.StorageDir
	}
	if !filepath.IsAbs(root) && strings.TrimSpace(s.infra.ProjectRoot) != "" {
		root = filepath.Join(s.infra.ProjectRoot, root)
	}
	s.store = media.NewLocalStore(s.infra.DB.SqlDB(), root, media.StoreOptions{
		MaxBytes:  cfg.MaxBytes,
		MaxPixels: cfg.MaxPixels,
	})
	return s.store
}

func (s *MediaService) Upload(ctx context.Context, r io.Reader, meta media.UploadMeta) (*media.MediaAsset, error) {
	store := s.Store()
	if store == nil {
		return nil, fmt.Errorf("media store database is required")
	}
	return store.Put(ctx, r, meta)
}

func (s *MediaService) Get(ctx context.Context, mediaAssetID string) (*media.MediaAsset, error) {
	store := s.Store()
	if store == nil {
		return nil, fmt.Errorf("media store database is required")
	}
	return store.Get(ctx, mediaAssetID)
}

func (s *MediaService) Open(ctx context.Context, mediaAssetID string) (io.ReadCloser, *media.MediaAsset, error) {
	store := s.Store()
	if store == nil {
		return nil, nil, fmt.Errorf("media store database is required")
	}
	return store.Open(ctx, mediaAssetID)
}
