package resource

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/longyisang/emoagent/internal/config"
)

func buildRootCatalog(cfg config.HostResourcesConfig) ([]config.HostResourceRoot, error) {
	roots := []config.HostResourceRoot{}
	if cfg.Enabled {
		roots = append(roots, discoverPlatformRoots(cfg.DefaultProfile)...)
	}
	seenExplicit := map[string]struct{}{}
	for _, root := range cfg.Roots {
		id := strings.TrimSpace(root.ID)
		if _, exists := seenExplicit[id]; exists {
			return nil, fmt.Errorf("duplicate host resource root %q", id)
		}
		seenExplicit[id] = struct{}{}
		roots = upsertRoot(roots, root)
	}
	return roots, nil
}

func discoverPlatformRoots(defaultProfile string) []config.HostResourceRoot {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return nil
	}
	return discoverPlatformRootsForHome(home, defaultProfile)
}

func discoverPlatformRootsForHome(home, defaultProfile string) []config.HostResourceRoot {
	home = filepath.Clean(strings.TrimSpace(home))
	if home == "." || home == "" {
		return nil
	}
	roots := []config.HostResourceRoot{{
		ID:        "home",
		Path:      home,
		Access:    discoveredRootAccess(defaultProfile, "home"),
		Recursive: true,
	}}
	for _, child := range []struct {
		id  string
		dir string
	}{
		{id: "desktop", dir: "Desktop"},
		{id: "documents", dir: "Documents"},
		{id: "downloads", dir: "Downloads"},
	} {
		path := filepath.Join(home, child.dir)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			roots = append(roots, config.HostResourceRoot{
				ID:        child.id,
				Path:      path,
				Access:    discoveredRootAccess(defaultProfile, child.id),
				Recursive: true,
			})
		}
	}
	return roots
}

func discoveredRootAccess(defaultProfile, id string) string {
	switch strings.TrimSpace(defaultProfile) {
	case "read", "allow":
		return "read"
	case "ask", "personal_ask":
		return "ask"
	case "deny":
		return "deny"
	case "personal_read", "":
		if id == "home" {
			return "ask"
		}
		return "read"
	default:
		return "ask"
	}
}

func upsertRoot(roots []config.HostResourceRoot, root config.HostResourceRoot) []config.HostResourceRoot {
	id := strings.TrimSpace(root.ID)
	for i := range roots {
		if roots[i].ID == id {
			roots[i] = root
			return roots
		}
	}
	return append(roots, root)
}
