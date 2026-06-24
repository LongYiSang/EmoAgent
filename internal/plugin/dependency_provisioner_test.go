package plugin

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestProvisionPluginDependenciesFromLockInstallsZipArtifacts(t *testing.T) {
	store, manifest := writeProcessPluginPackage(t, normalPythonPluginSource())
	packageDir, err := store.PackageDir(manifest.ID, manifest.Version)
	if err != nil {
		t.Fatalf("PackageDir: %v", err)
	}
	writeDependencyZip(t, filepath.Join(packageDir, "deps", "depmod.zip"), map[string]string{
		"depmod.py": `VALUE = "locked-dependency"`,
	})
	writeDependencyLock(t, packageDir, "deps/depmod.zip")

	result, err := ProvisionPluginDependencies(store, manifest)
	if err != nil {
		t.Fatalf("ProvisionPluginDependencies: %v", err)
	}
	if !result.Installed || result.DependencyEnvDir == "" || result.LockDigest == "" {
		t.Fatalf("result = %#v, want installed dependency env with lock digest", result)
	}
	if got := readFileString(t, filepath.Join(result.DependencyEnvDir, "depmod.py")); got != `VALUE = "locked-dependency"` {
		t.Fatalf("installed depmod.py = %q", got)
	}
}

func TestDependencyLockSummaryReportsPackagesWithoutInstalling(t *testing.T) {
	store, manifest := writeProcessPluginPackage(t, normalPythonPluginSource())
	packageDir, err := store.PackageDir(manifest.ID, manifest.Version)
	if err != nil {
		t.Fatalf("PackageDir: %v", err)
	}
	writeDependencyZip(t, filepath.Join(packageDir, "deps", "depmod.zip"), map[string]string{
		"depmod.py": `VALUE = "locked-dependency"`,
	})
	writeDependencyLock(t, packageDir, "deps/depmod.zip")

	summary, err := DependencyLockSummaryForPackage(store, manifest)
	if err != nil {
		t.Fatalf("DependencyLockSummaryForPackage: %v", err)
	}
	if !summary.Present || summary.LockDigest == "" || summary.PackageCount != 1 {
		t.Fatalf("summary = %#v, want present lock with one package", summary)
	}
	if len(summary.Packages) != 1 ||
		summary.Packages[0].Name != "depmod" ||
		summary.Packages[0].Kind != "python_module_zip" ||
		summary.Packages[0].Path != "deps/depmod.zip" ||
		!strings.HasPrefix(summary.Packages[0].SHA256, "sha256:") {
		t.Fatalf("summary packages = %#v", summary.Packages)
	}
	depDir, err := store.DependencyEnvDir(manifest.ID, manifest.Version)
	if err != nil {
		t.Fatalf("DependencyEnvDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(depDir, "depmod.py")); !os.IsNotExist(err) {
		t.Fatalf("dependency summary installed artifacts unexpectedly: %v", err)
	}
}

func TestDependencyLockSummaryReportsMissingLock(t *testing.T) {
	store, manifest := writeProcessPluginPackage(t, normalPythonPluginSource())

	summary, err := DependencyLockSummaryForPackage(store, manifest)
	if err != nil {
		t.Fatalf("DependencyLockSummaryForPackage: %v", err)
	}
	if summary.Present || summary.LockDigest != "" || summary.PackageCount != 0 || len(summary.Packages) != 0 {
		t.Fatalf("summary without lock = %#v, want empty absent summary", summary)
	}
}

func TestDependencyLockSummaryReportsManagedUVProject(t *testing.T) {
	store, manifest := writeProcessPluginPackage(t, normalPythonPluginSource())
	manifest.Runtime.Kind = RuntimeManagedPythonProcess
	packageDir, err := store.PackageDir(manifest.ID, manifest.Version)
	if err != nil {
		t.Fatalf("PackageDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "pyproject.toml"), []byte("[project]\nname=\"test\"\nversion=\"0.1.0\"\n"), 0o644); err != nil {
		t.Fatalf("write pyproject: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "uv.lock"), []byte("lock-v1"), 0o644); err != nil {
		t.Fatalf("write uv.lock: %v", err)
	}

	summary, err := DependencyLockSummaryForPackage(store, manifest)
	if err != nil {
		t.Fatalf("DependencyLockSummaryForPackage: %v", err)
	}
	if !summary.Present || summary.Format != "uv" || summary.LockDigest == "" || summary.UVLockDigest != summary.LockDigest || summary.PyprojectDigest == "" {
		t.Fatalf("summary = %#v, want uv digests", summary)
	}
	if summary.LegacyDependency || summary.PackageCount != 0 || len(summary.Packages) != 0 {
		t.Fatalf("summary legacy fields = %#v", summary)
	}
}

func TestProvisionPluginDependenciesSkipsWhenLockDigestAlreadyInstalled(t *testing.T) {
	store, manifest := writeProcessPluginPackage(t, normalPythonPluginSource())
	packageDir, err := store.PackageDir(manifest.ID, manifest.Version)
	if err != nil {
		t.Fatalf("PackageDir: %v", err)
	}
	writeDependencyZip(t, filepath.Join(packageDir, "deps", "depmod.zip"), map[string]string{
		"depmod.py": `VALUE = "locked-dependency"`,
	})
	writeDependencyLock(t, packageDir, "deps/depmod.zip")
	first, err := ProvisionPluginDependencies(store, manifest)
	if err != nil {
		t.Fatalf("ProvisionPluginDependencies first: %v", err)
	}
	depFile := filepath.Join(first.DependencyEnvDir, "depmod.py")
	if err := os.WriteFile(depFile, []byte(`VALUE = "already-installed"`), 0o644); err != nil {
		t.Fatalf("mutate installed dependency: %v", err)
	}

	second, err := ProvisionPluginDependencies(store, manifest)
	if err != nil {
		t.Fatalf("ProvisionPluginDependencies second: %v", err)
	}
	if second.Installed {
		t.Fatalf("second result = %#v, want skip when lock digest marker matches", second)
	}
	if got := readFileString(t, depFile); got != `VALUE = "already-installed"` {
		t.Fatalf("dependency env was rebuilt, depmod.py = %q", got)
	}
}

func TestProvisionPluginDependenciesSerializesConcurrentInstalls(t *testing.T) {
	store, manifest := writeProcessPluginPackage(t, normalPythonPluginSource())
	packageDir, err := store.PackageDir(manifest.ID, manifest.Version)
	if err != nil {
		t.Fatalf("PackageDir: %v", err)
	}
	files := map[string]string{}
	for i := 0; i < 64; i++ {
		files[filepath.ToSlash(filepath.Join("pkg", "mod", fmt.Sprintf("file%02d.py", i)))] = "VALUE = 1"
	}
	files["depmod.py"] = `VALUE = "concurrent"`
	writeDependencyZip(t, filepath.Join(packageDir, "deps", "depmod.zip"), files)
	writeDependencyLock(t, packageDir, "deps/depmod.zip")

	const workers = 8
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := ProvisionPluginDependencies(store, manifest)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent ProvisionPluginDependencies error = %v", err)
		}
	}
	depDir, err := store.DependencyEnvDir(manifest.ID, manifest.Version)
	if err != nil {
		t.Fatalf("DependencyEnvDir: %v", err)
	}
	if got := readFileString(t, filepath.Join(depDir, "depmod.py")); got != `VALUE = "concurrent"` {
		t.Fatalf("installed depmod.py = %q", got)
	}
}

func TestProvisionPluginDependenciesRejectsDigestMismatchKeepsExistingEnv(t *testing.T) {
	store, manifest := writeProcessPluginPackage(t, normalPythonPluginSource())
	existingDep := writeExistingDependencyEnv(t, store, manifest, `VALUE = "existing"`)
	packageDir, err := store.PackageDir(manifest.ID, manifest.Version)
	if err != nil {
		t.Fatalf("PackageDir: %v", err)
	}
	writeDependencyZip(t, filepath.Join(packageDir, "deps", "depmod.zip"), map[string]string{
		"depmod.py": `VALUE = "new"`,
	})
	if err := os.WriteFile(filepath.Join(packageDir, dependencyLockFileName), []byte(`{
  "version": 1,
  "packages": [
    {
      "name": "depmod",
      "kind": "python_module_zip",
      "path": "deps/depmod.zip",
      "sha256": "sha256:0000000000000000000000000000000000000000000000000000000000000000"
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write dependency lock: %v", err)
	}

	_, err = ProvisionPluginDependencies(store, manifest)
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("ProvisionPluginDependencies error = %v, want digest mismatch", err)
	}
	if got := readFileString(t, existingDep); got != `VALUE = "existing"` {
		t.Fatalf("existing dependency content = %q, want preserved", got)
	}
}

func TestProvisionPluginDependenciesRejectsUnsafeArtifactPathKeepsExistingEnv(t *testing.T) {
	store, manifest := writeProcessPluginPackage(t, normalPythonPluginSource())
	existingDep := writeExistingDependencyEnv(t, store, manifest, `VALUE = "existing"`)
	packageDir, err := store.PackageDir(manifest.ID, manifest.Version)
	if err != nil {
		t.Fatalf("PackageDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, dependencyLockFileName), []byte(`{
  "version": 1,
  "packages": [
    {
      "name": "depmod",
      "kind": "python_module_zip",
      "path": "../depmod.zip",
      "sha256": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write dependency lock: %v", err)
	}

	_, err = ProvisionPluginDependencies(store, manifest)
	if err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("ProvisionPluginDependencies error = %v, want unsafe path", err)
	}
	if got := readFileString(t, existingDep); got != `VALUE = "existing"` {
		t.Fatalf("existing dependency content = %q, want preserved", got)
	}
}

func writeDependencyLock(t *testing.T, packageDir, artifactRel string) {
	t.Helper()
	data := readFileBytes(t, filepath.Join(packageDir, filepath.FromSlash(artifactRel)))
	if err := os.WriteFile(filepath.Join(packageDir, dependencyLockFileName), []byte(`{
  "version": 1,
  "packages": [
    {
      "name": "depmod",
      "kind": "python_module_zip",
      "path": "`+artifactRel+`",
      "sha256": "`+sha256Digest(data)+`"
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write dependency lock: %v", err)
	}
}

func writeDependencyZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll dependency zip dir: %v", err)
	}
	writeZipFiles(t, path, files)
}

func writeZipFiles(t *testing.T, path string, files map[string]string) {
	t.Helper()
	out, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create zip: %v", err)
	}
	archive := zip.NewWriter(out)
	for name, content := range files {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatalf("Create zip entry: %v", err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatalf("Write zip entry: %v", err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("Close zip: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("Close zip file: %v", err)
	}
}

func writeExistingDependencyEnv(t *testing.T, store *PluginStore, manifest ManifestV2, content string) string {
	t.Helper()
	depDir, err := store.DependencyEnvDir(manifest.ID, manifest.Version)
	if err != nil {
		t.Fatalf("DependencyEnvDir: %v", err)
	}
	depFile := filepath.Join(depDir, "depmod.py")
	if err := os.MkdirAll(filepath.Dir(depFile), 0o755); err != nil {
		t.Fatalf("MkdirAll dependency env: %v", err)
	}
	if err := os.WriteFile(depFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write existing dependency: %v", err)
	}
	return depFile
}

func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	return data
}
