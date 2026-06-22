package plugin

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
)

func TestProvisionPrivatePythonRuntimeFromZipVerifiesDigestAndInstallsStoreRuntime(t *testing.T) {
	store, err := NewPluginStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("NewPluginStore: %v", err)
	}
	zipPath := filepath.Join(t.TempDir(), "python-runtime.zip")
	exeName := privatePythonExecutableName(runtime.GOOS)
	writePrivatePythonRuntimeZip(t, zipPath, map[string]string{
		exeName:             "private-python",
		"Lib/site.py":       "# site",
		"runtime-note.txt":  "host-owned runtime artifact",
		"Scripts/helper.py": "print('helper')",
	})
	digest := sha256Digest(readFileBytes(t, zipPath))

	result, err := ProvisionPrivatePythonRuntime(store, config.PluginRuntimeConfig{
		PrivatePythonArtifactPath:   zipPath,
		PrivatePythonArtifactSHA256: digest,
	})
	if err != nil {
		t.Fatalf("ProvisionPrivatePythonRuntime: %v", err)
	}
	expectedExe := filepath.Join(store.RootDir, "runtime", "python", exeName)
	if !result.Installed || result.ExecutablePath != expectedExe || result.ArtifactDigest != digest {
		t.Fatalf("provision result = %#v, want installed executable %q digest %q", result, expectedExe, digest)
	}
	if got := readFileString(t, expectedExe); got != "private-python" {
		t.Fatalf("installed python executable content = %q", got)
	}
}

func TestProvisionPrivatePythonRuntimeRejectsDigestMismatchKeepsExistingRuntime(t *testing.T) {
	store, err := NewPluginStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("NewPluginStore: %v", err)
	}
	existingExe, err := store.PrivatePythonExecutablePath(runtime.GOOS)
	if err != nil {
		t.Fatalf("PrivatePythonExecutablePath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(existingExe), 0o755); err != nil {
		t.Fatalf("MkdirAll existing runtime: %v", err)
	}
	if err := os.WriteFile(existingExe, []byte("existing"), 0o755); err != nil {
		t.Fatalf("write existing runtime: %v", err)
	}
	zipPath := filepath.Join(t.TempDir(), "python-runtime.zip")
	writePrivatePythonRuntimeZip(t, zipPath, map[string]string{
		privatePythonExecutableName(runtime.GOOS): "new",
	})

	_, err = ProvisionPrivatePythonRuntime(store, config.PluginRuntimeConfig{
		PrivatePythonArtifactPath:   zipPath,
		PrivatePythonArtifactSHA256: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	})
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("ProvisionPrivatePythonRuntime error = %v, want digest mismatch", err)
	}
	if got := readFileString(t, existingExe); got != "existing" {
		t.Fatalf("existing runtime content = %q, want preserved", got)
	}
}

func TestProvisionPrivatePythonRuntimeRejectsDigestMismatchWithoutCreatingRuntime(t *testing.T) {
	store, err := NewPluginStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("NewPluginStore: %v", err)
	}
	runtimeDir, err := store.PrivatePythonRuntimeDir()
	if err != nil {
		t.Fatalf("PrivatePythonRuntimeDir: %v", err)
	}
	zipPath := filepath.Join(t.TempDir(), "python-runtime.zip")
	writePrivatePythonRuntimeZip(t, zipPath, map[string]string{
		privatePythonExecutableName(runtime.GOOS): "new",
	})

	_, err = ProvisionPrivatePythonRuntime(store, config.PluginRuntimeConfig{
		PrivatePythonArtifactPath:   zipPath,
		PrivatePythonArtifactSHA256: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	})
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("ProvisionPrivatePythonRuntime error = %v, want digest mismatch", err)
	}
	if _, statErr := os.Stat(runtimeDir); !os.IsNotExist(statErr) {
		t.Fatalf("runtime dir stat error = %v, want not exist", statErr)
	}
}

func TestProvisionPrivatePythonRuntimeRejectsUnsafeZipPathKeepsExistingRuntime(t *testing.T) {
	store, existingExe := writeExistingPrivatePythonRuntime(t, "existing")
	zipPath := filepath.Join(t.TempDir(), "python-runtime.zip")
	writePrivatePythonRuntimeZip(t, zipPath, map[string]string{
		"../" + privatePythonExecutableName(runtime.GOOS): "escaped",
	})
	digest := sha256Digest(readFileBytes(t, zipPath))

	_, err := ProvisionPrivatePythonRuntime(store, config.PluginRuntimeConfig{
		PrivatePythonArtifactPath:   zipPath,
		PrivatePythonArtifactSHA256: digest,
	})
	if err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("ProvisionPrivatePythonRuntime error = %v, want unsafe zip path", err)
	}
	if got := readFileString(t, existingExe); got != "existing" {
		t.Fatalf("existing runtime content = %q, want preserved", got)
	}
}

func TestProvisionPrivatePythonRuntimeRequiresExecutableInArtifact(t *testing.T) {
	store, existingExe := writeExistingPrivatePythonRuntime(t, "existing")
	zipPath := filepath.Join(t.TempDir(), "python-runtime.zip")
	writePrivatePythonRuntimeZip(t, zipPath, map[string]string{
		"README.txt": "missing executable",
	})
	digest := sha256Digest(readFileBytes(t, zipPath))

	_, err := ProvisionPrivatePythonRuntime(store, config.PluginRuntimeConfig{
		PrivatePythonArtifactPath:   zipPath,
		PrivatePythonArtifactSHA256: digest,
	})
	if err == nil || !strings.Contains(err.Error(), "missing "+privatePythonExecutableName(runtime.GOOS)) {
		t.Fatalf("ProvisionPrivatePythonRuntime error = %v, want missing executable", err)
	}
	if got := readFileString(t, existingExe); got != "existing" {
		t.Fatalf("existing runtime content = %q, want preserved", got)
	}
}

func writeExistingPrivatePythonRuntime(t *testing.T, content string) (*PluginStore, string) {
	t.Helper()
	store, err := NewPluginStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("NewPluginStore: %v", err)
	}
	existingExe, err := store.PrivatePythonExecutablePath(runtime.GOOS)
	if err != nil {
		t.Fatalf("PrivatePythonExecutablePath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(existingExe), 0o755); err != nil {
		t.Fatalf("MkdirAll existing runtime: %v", err)
	}
	if err := os.WriteFile(existingExe, []byte(content), 0o755); err != nil {
		t.Fatalf("write existing runtime: %v", err)
	}
	return store, existingExe
}

func writePrivatePythonRuntimeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	out, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create zip: %v", err)
	}
	archive := zip.NewWriter(out)
	for name, body := range files {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatalf("Create zip entry: %v", err)
		}
		if _, err := writer.Write([]byte(body)); err != nil {
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

func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	return data
}
