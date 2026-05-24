package memory

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenSQLiteOnlyService(t *testing.T) {
	svc, err := Open(context.Background(), Options{
		DBPath:      filepath.Join(t.TempDir(), "memory.db"),
		AutoMigrate: true,
		EnableFTS:   true,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
