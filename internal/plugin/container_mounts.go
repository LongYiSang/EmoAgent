package plugin

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/longyisang/emoagent/internal/storage"
)

type MountPlan struct {
	Mounts []Mount `json:"mounts"`
}

type Mount struct {
	HostPath      string `json:"host_path"`
	ContainerPath string `json:"container_path"`
	Mode          string `json:"mode"`
}

type MountPlanValidation struct {
	ProjectRoot        string
	MemoryCorePath     string
	ProviderConfigPath string
}

func BuildContainerMountPlan(record storage.PluginRuntimeRecord, store PluginStore) (MountPlan, error) {
	return BuildContainerMountPlanForManifest(record, ManifestV2{}, store, MountPlanValidation{})
}

func BuildContainerMountPlanForManifest(record storage.PluginRuntimeRecord, manifest ManifestV2, store PluginStore, validation MountPlanValidation) (MountPlan, error) {
	if strings.TrimSpace(record.PluginID) == "" {
		record.PluginID = manifest.ID
	}
	if strings.TrimSpace(record.Version) == "" {
		record.Version = manifest.Version
	}
	if !validPluginID(record.PluginID) {
		return MountPlan{}, fmt.Errorf("invalid plugin id %q", record.PluginID)
	}
	if !validSemver(record.Version) {
		return MountPlan{}, fmt.Errorf("invalid plugin version %q", record.Version)
	}
	if len(manifest.Container.Mounts) > 0 {
		for _, declared := range manifest.Container.Mounts {
			if err := rejectDeclaredMount(declared, validation); err != nil {
				return MountPlan{}, err
			}
		}
		return MountPlan{}, fmt.Errorf("plugin-declared host mounts are not allowed")
	}
	storePtr := &store
	packageDir, err := storePtr.PackageDir(record.PluginID, record.Version)
	if err != nil {
		return MountPlan{}, err
	}
	stateDir, err := storePtr.StateDir(record.PluginID)
	if err != nil {
		return MountPlan{}, err
	}
	cacheDir, err := storePtr.CacheDir(record.PluginID)
	if err != nil {
		return MountPlan{}, err
	}
	runDir, err := storePtr.RunDir(record.PluginID)
	if err != nil {
		return MountPlan{}, err
	}
	mounts := []Mount{
		{HostPath: packageDir, ContainerPath: "/plugin", Mode: "ro"},
		{HostPath: stateDir, ContainerPath: "/data", Mode: "rw"},
		{HostPath: cacheDir, ContainerPath: "/cache", Mode: "rw"},
		{HostPath: runDir, ContainerPath: "/run", Mode: "rw"},
	}
	if manifest.Container.Workspace.Enabled {
		mode := strings.TrimSpace(manifest.Container.Workspace.Mode)
		if mode == "" {
			mode = "ro"
		}
		if mode != "ro" && mode != "rw" {
			return MountPlan{}, fmt.Errorf("workspace mount mode must be ro or rw")
		}
		workspaceDir, err := storePtr.WorkspaceDir(record.PluginID)
		if err != nil {
			return MountPlan{}, err
		}
		mounts = append(mounts, Mount{HostPath: workspaceDir, ContainerPath: "/workspace", Mode: mode})
	}
	for _, mount := range mounts {
		if err := validateGeneratedMount(mount, store.RootDir, validation); err != nil {
			return MountPlan{}, err
		}
	}
	return MountPlan{Mounts: mounts}, nil
}

func rejectDeclaredMount(mount ManifestV2Mount, validation MountPlanValidation) error {
	host := strings.TrimSpace(mount.HostPath)
	if host == "" {
		return fmt.Errorf("declared mount host_path is required")
	}
	clean := filepath.Clean(host)
	if filepath.IsAbs(host) {
		return fmt.Errorf("plugin-declared absolute host mounts are not allowed")
	}
	if clean == ".." || strings.HasPrefix(filepath.ToSlash(clean), "../") || strings.Contains(filepath.ToSlash(clean), "/../") {
		return fmt.Errorf("plugin-declared host mounts must not contain ..")
	}
	for _, protected := range []string{validation.ProjectRoot, validation.MemoryCorePath, validation.ProviderConfigPath} {
		if protected != "" && sameOrUnder(host, protected) {
			return fmt.Errorf("plugin-declared host mount targets protected path %q", protected)
		}
	}
	return fmt.Errorf("plugin-declared host mounts are not allowed")
}

func validateGeneratedMount(mount Mount, root string, validation MountPlanValidation) error {
	if mount.ContainerPath == "" || !strings.HasPrefix(mount.ContainerPath, "/") {
		return fmt.Errorf("container path %q must be absolute", mount.ContainerPath)
	}
	if mount.Mode != "ro" && mount.Mode != "rw" {
		return fmt.Errorf("mount mode %q is unsupported", mount.Mode)
	}
	if !sameOrUnder(mount.HostPath, root) {
		return fmt.Errorf("mount host path %q escapes plugin store", mount.HostPath)
	}
	for _, protected := range []string{validation.ProjectRoot, validation.MemoryCorePath, validation.ProviderConfigPath} {
		if protected != "" && sameOrUnder(mount.HostPath, protected) {
			return fmt.Errorf("mount host path %q targets protected path %q", mount.HostPath, protected)
		}
	}
	return nil
}

func sameOrUnder(path, root string) bool {
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathClean := strings.ToLower(filepath.Clean(pathAbs))
	rootClean := strings.ToLower(filepath.Clean(rootAbs))
	if pathClean == rootClean {
		return true
	}
	rel, err := filepath.Rel(rootClean, pathClean)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
