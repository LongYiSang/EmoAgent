package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

type UploadMeta struct {
	OriginalFilename string
	CreatedByRole    string
}

type StoreOptions struct {
	MaxBytes  int64
	MaxPixels int
}

type MediaAsset struct {
	ID               string `json:"media_asset_id"`
	SHA256           string `json:"-"`
	Kind             string `json:"kind"`
	MimeType         string `json:"mime_type"`
	OriginalFilename string `json:"original_filename,omitempty"`
	FileExt          string `json:"file_ext,omitempty"`
	ByteSize         int64  `json:"byte_size"`
	Width            int    `json:"width,omitempty"`
	Height           int    `json:"height,omitempty"`
	StorageBackend   string `json:"-"`
	StorageURI       string `json:"-"`
	CreatedByRole    string `json:"-"`
	VisibilityStatus string `json:"-"`
}

type LocalStore struct {
	db      *sql.DB
	rootDir string
	opts    StoreOptions
}

func NewLocalStore(db *sql.DB, rootDir string, opts StoreOptions) *LocalStore {
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = 10 * 1024 * 1024
	}
	if opts.MaxPixels <= 0 {
		opts.MaxPixels = 20_000_000
	}
	return &LocalStore{db: db, rootDir: rootDir, opts: opts}
}

func (s *LocalStore) Put(ctx context.Context, r io.Reader, meta UploadMeta) (*MediaAsset, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("media store database is required")
	}
	if err := os.MkdirAll(s.rootDir, 0o755); err != nil {
		return nil, fmt.Errorf("create media dir: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(r, s.opts.MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read media: %w", err)
	}
	if int64(len(data)) > s.opts.MaxBytes {
		return nil, fmt.Errorf("media exceeds max size")
	}
	mimeType := http.DetectContentType(data)
	if mimeType != "image/png" && mimeType != "image/jpeg" {
		return nil, fmt.Errorf("unsupported media MIME type: %s", mimeType)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image metadata: %w", err)
	}
	if cfg.Width > 0 && cfg.Height > 0 && int64(cfg.Width)*int64(cfg.Height) > int64(s.opts.MaxPixels) {
		return nil, fmt.Errorf("image exceeds max pixels")
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	if existing, err := s.findByDigest(ctx, sha, int64(len(data))); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}

	ext := extForMime(mimeType)
	id := "med_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	storagePath := filepath.Join(s.rootDir, sha+ext)
	if err := os.WriteFile(storagePath, data, 0o644); err != nil {
		return nil, fmt.Errorf("write media file: %w", err)
	}
	role := strings.TrimSpace(meta.CreatedByRole)
	if role == "" {
		role = "user"
	}
	asset := &MediaAsset{
		ID:               id,
		SHA256:           sha,
		Kind:             "image",
		MimeType:         mimeType,
		OriginalFilename: meta.OriginalFilename,
		FileExt:          ext,
		ByteSize:         int64(len(data)),
		Width:            cfg.Width,
		Height:           cfg.Height,
		StorageBackend:   "local",
		StorageURI:       storagePath,
		CreatedByRole:    role,
		VisibilityStatus: "visible",
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO media_assets (
			id, sha256, kind, mime_type, original_filename, file_ext, byte_size, width, height,
			storage_backend, storage_uri, created_by_role, scan_status
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'clean')
	`, asset.ID, asset.SHA256, asset.Kind, asset.MimeType, nullString(asset.OriginalFilename), asset.FileExt, asset.ByteSize, asset.Width, asset.Height, asset.StorageBackend, asset.StorageURI, asset.CreatedByRole); err != nil {
		return nil, fmt.Errorf("insert media asset: %w", err)
	}
	return asset, nil
}

func (s *LocalStore) Get(ctx context.Context, mediaAssetID string) (*MediaAsset, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("media store database is required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, sha256, kind, mime_type, COALESCE(original_filename, ''), COALESCE(file_ext, ''),
		       byte_size, COALESCE(width, 0), COALESCE(height, 0), storage_backend, storage_uri, created_by_role,
		       visibility_status
		FROM media_assets
		WHERE id = ?
	`, mediaAssetID)
	return scanAsset(row)
}

func (s *LocalStore) Open(ctx context.Context, mediaAssetID string) (io.ReadCloser, *MediaAsset, error) {
	asset, err := s.Get(ctx, mediaAssetID)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(asset.StorageURI)
	if err != nil {
		return nil, nil, fmt.Errorf("open media file: %w", err)
	}
	return f, asset, nil
}

func (s *LocalStore) MarkPurged(ctx context.Context, mediaAssetID string, reason string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE media_assets
		SET visibility_status = 'purged', purged_at = datetime('now'), purge_reason = ?
		WHERE id = ?
	`, reason, mediaAssetID)
	return err
}

func (s *LocalStore) findByDigest(ctx context.Context, sha string, size int64) (*MediaAsset, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, sha256, kind, mime_type, COALESCE(original_filename, ''), COALESCE(file_ext, ''),
		       byte_size, COALESCE(width, 0), COALESCE(height, 0), storage_backend, storage_uri, created_by_role,
		       visibility_status
		FROM media_assets
		WHERE sha256 = ? AND byte_size = ?
	`, sha, size)
	asset, err := scanAsset(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return asset, err
}

func scanAsset(row interface{ Scan(dest ...any) error }) (*MediaAsset, error) {
	var asset MediaAsset
	if err := row.Scan(&asset.ID, &asset.SHA256, &asset.Kind, &asset.MimeType, &asset.OriginalFilename, &asset.FileExt, &asset.ByteSize, &asset.Width, &asset.Height, &asset.StorageBackend, &asset.StorageURI, &asset.CreatedByRole, &asset.VisibilityStatus); err != nil {
		return nil, err
	}
	if asset.VisibilityStatus == "" {
		asset.VisibilityStatus = "visible"
	}
	return &asset, nil
}

func extForMime(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	default:
		return ""
	}
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
