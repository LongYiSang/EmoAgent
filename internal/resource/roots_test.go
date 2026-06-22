package resource

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
)

func TestDiscoverPlatformRootsPersonalRead(t *testing.T) {
	home := t.TempDir()
	for _, dir := range []string{"Desktop", "Documents", "Downloads"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	roots := discoverPlatformRootsForHome(home, "personal_read")
	byID := map[string]config.HostResourceRoot{}
	for _, root := range roots {
		byID[root.ID] = root
	}
	if byID["home"].Access != "ask" {
		t.Fatalf("home access = %q, want ask", byID["home"].Access)
	}
	for _, id := range []string{"desktop", "documents", "downloads"} {
		if byID[id].Access != "read" {
			t.Fatalf("%s access = %q, want read", id, byID[id].Access)
		}
	}
}

func TestBuildRootCatalogExplicitRootOverridesDiscoveredRoot(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "Documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	explicit := t.TempDir()

	roots := discoverPlatformRootsForHome(home, "personal_read")
	roots = upsertRoot(roots, config.HostResourceRoot{
		ID:        "documents",
		Path:      explicit,
		Access:    "ask",
		Recursive: false,
	})
	byID := map[string]config.HostResourceRoot{}
	for _, root := range roots {
		byID[root.ID] = root
	}
	if byID["documents"].Path != explicit || byID["documents"].Access != "ask" || byID["documents"].Recursive {
		t.Fatalf("documents root = %#v", byID["documents"])
	}
}
