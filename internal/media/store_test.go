package media

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/storage"
)

func TestLocalStorePutPersistsMetadataWithoutBase64(t *testing.T) {
	db := openTestDB(t)
	store := NewLocalStore(db.SqlDB(), filepath.Join(t.TempDir(), "media"), StoreOptions{MaxBytes: 1024 * 1024})

	asset, err := store.Put(context.Background(), bytes.NewReader(tinyPNG()), UploadMeta{
		OriginalFilename: "tiny.png",
		CreatedByRole:    "user",
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if asset.ID == "" || asset.MimeType != "image/png" || asset.ByteSize == 0 || asset.Width != 1 || asset.Height != 1 {
		t.Fatalf("asset = %#v, want populated PNG metadata", asset)
	}

	var storageURI string
	if err := db.SqlDB().QueryRow(`SELECT storage_uri FROM media_assets WHERE id = ?`, asset.ID).Scan(&storageURI); err != nil {
		t.Fatalf("query storage_uri: %v", err)
	}
	if strings.Contains(storageURI, "base64") || strings.Contains(storageURI, "iVBOR") {
		t.Fatalf("storage_uri contains base64-ish data: %q", storageURI)
	}
}

func TestLocalStoreRejectsUnsupportedImageMime(t *testing.T) {
	db := openTestDB(t)
	store := NewLocalStore(db.SqlDB(), filepath.Join(t.TempDir(), "media"), StoreOptions{MaxBytes: 1024 * 1024})

	_, err := store.Put(context.Background(), strings.NewReader("not an image"), UploadMeta{CreatedByRole: "user"})
	if err == nil {
		t.Fatal("Put succeeded for unsupported MIME, want error")
	}
}

func openTestDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "emo.db"), slog.Default())
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func tinyPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}
}

var _ *sql.DB
