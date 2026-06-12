package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type PluginStore struct {
	RootDir string
}

func NewPluginStore(rootDir string) (*PluginStore, error) {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return nil, fmt.Errorf("plugin store root_dir is required")
	}
	return &PluginStore{RootDir: rootDir}, nil
}

func (s *PluginStore) PackageDir(pluginID, version string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("plugin store is nil")
	}
	if !validPluginID(pluginID) {
		return "", fmt.Errorf("invalid plugin id %q", pluginID)
	}
	if !validSemver(version) {
		return "", fmt.Errorf("invalid plugin version %q", version)
	}
	return filepath.Join(s.RootDir, "packages", pluginID, version), nil
}

func (s *PluginStore) StateDir(pluginID string) (string, error) {
	return s.scopedDir("state", pluginID)
}

func (s *PluginStore) CacheDir(pluginID string) (string, error) {
	return s.scopedDir("cache", pluginID)
}

func (s *PluginStore) RunDir(pluginID string) (string, error) {
	return s.scopedDir("run", pluginID)
}

func (s *PluginStore) WorkspaceDir(pluginID string) (string, error) {
	return s.scopedDir("workspaces", pluginID)
}

func (s *PluginStore) scopedDir(kind, pluginID string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("plugin store is nil")
	}
	if !validPluginID(pluginID) {
		return "", fmt.Errorf("invalid plugin id %q", pluginID)
	}
	return filepath.Join(s.RootDir, kind, pluginID), nil
}

func (s *PluginStore) CreateImmutablePackageDir(pluginID, version string) (string, error) {
	dir, err := s.PackageDir(pluginID, version)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(dir); err == nil {
		return "", fmt.Errorf("plugin package %s@%s already exists", pluginID, version)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func (s *PluginStore) PrepareRuntimeDirs(pluginID string) error {
	for _, fn := range []func(string) (string, error){s.StateDir, s.CacheDir, s.RunDir} {
		dir, err := fn(pluginID)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}
