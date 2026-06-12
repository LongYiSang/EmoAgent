package plugin

import (
	"path/filepath"
	"testing"

	"github.com/longyisang/emoagent/internal/storage"
)

func TestBuildContainerMountPlanEmitsFixedMounts(t *testing.T) {
	store := PluginStore{RootDir: filepath.Join(t.TempDir(), "store")}
	manifest := ManifestV2{
		ID:      "com.example.echo",
		Version: "0.1.0",
		Container: ManifestV2Container{Workspace: ManifestV2WorkspaceMount{
			Enabled: true,
			Mode:    "rw",
		}},
	}
	plan, err := BuildContainerMountPlanForManifest(storage.PluginRuntimeRecord{
		PluginID: "com.example.echo",
		Version:  "0.1.0",
	}, manifest, store, MountPlanValidation{})
	if err != nil {
		t.Fatalf("BuildContainerMountPlanForManifest: %v", err)
	}
	if len(plan.Mounts) != 5 {
		t.Fatalf("mounts = %#v, want 5", plan.Mounts)
	}
	want := map[string]string{
		"/plugin":    "ro",
		"/data":      "rw",
		"/cache":     "rw",
		"/run":       "rw",
		"/workspace": "rw",
	}
	for _, mount := range plan.Mounts {
		if want[mount.ContainerPath] != mount.Mode {
			t.Fatalf("mount = %#v, want mode %q", mount, want[mount.ContainerPath])
		}
		if !sameOrUnder(mount.HostPath, store.RootDir) {
			t.Fatalf("mount host path %q escaped store %q", mount.HostPath, store.RootDir)
		}
	}
}

func TestBuildContainerMountPlanRejectsPluginDeclaredHostMounts(t *testing.T) {
	store := PluginStore{RootDir: filepath.Join(t.TempDir(), "store")}
	protected := filepath.Join(t.TempDir(), "project")
	cases := []ManifestV2Mount{
		{HostPath: "../memory.db", ContainerPath: "/x", Mode: "ro"},
		{HostPath: filepath.Join(protected, "config.yaml"), ContainerPath: "/x", Mode: "ro"},
		{HostPath: "logs", ContainerPath: "/x", Mode: "ro"},
	}
	for _, declared := range cases {
		manifest := ManifestV2{
			ID:      "com.example.echo",
			Version: "0.1.0",
			Container: ManifestV2Container{
				Mounts: []ManifestV2Mount{declared},
			},
		}
		_, err := BuildContainerMountPlanForManifest(storage.PluginRuntimeRecord{
			PluginID: "com.example.echo",
			Version:  "0.1.0",
		}, manifest, store, MountPlanValidation{
			ProjectRoot:        protected,
			MemoryCorePath:     filepath.Join(protected, "memory.db"),
			ProviderConfigPath: filepath.Join(protected, "config.yaml"),
		})
		if err == nil {
			t.Fatalf("BuildContainerMountPlanForManifest(%#v) error = nil", declared)
		}
	}
}
