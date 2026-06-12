package plugin

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
)

func TestPluginInstallerInstallFromZipVerifiesSignatureAndDigest(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugin")
	writeExamplePlugin(t, pluginDir)
	zipPath := filepath.Join(dir, "echo.zip")
	writeZip(t, zipPath, map[string]string{
		manifestFileName: readFileString(t, filepath.Join(pluginDir, manifestFileName)),
		"main.py":        "print('ok')\n",
	})

	manifestRaw := []byte(readFileString(t, filepath.Join(pluginDir, manifestFileName)))
	packageRaw, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	trustedPath := filepath.Join(dir, "publishers.yaml")
	if err := os.WriteFile(trustedPath, []byte(`
publishers:
  - id: example
    display_name: Example
    public_keys:
      - id: main
        algorithm: ed25519
        public_key_base64: `+base64.StdEncoding.EncodeToString(publicKey)+`
`), 0o644); err != nil {
		t.Fatalf("write trusted publishers: %v", err)
	}
	descriptor := PluginReleaseDescriptor{
		PluginID:       "com.example.echo",
		Version:        "0.1.0",
		PackageDigest:  sha256Digest(packageRaw),
		ManifestDigest: sha256Digest(manifestRaw),
		PublisherID:    "example",
		KeyID:          "main",
	}
	writeSignedDescriptor(t, zipPath+".sig.yaml", descriptor, privateKey)

	store, err := NewPluginStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatalf("NewPluginStore: %v", err)
	}
	installer := NewPluginInstaller(store, config.PluginInstallerConfig{
		RequireSignature:      true,
		TrustedPublishersPath: trustedPath,
	})

	result, err := installer.InstallFromZip(t.Context(), zipPath)
	if err != nil {
		t.Fatalf("InstallFromZip: %v", err)
	}
	if result.SignatureStatus != SignatureStatusVerified {
		t.Fatalf("SignatureStatus = %q, want verified", result.SignatureStatus)
	}
	if result.PackageDigest != descriptor.PackageDigest || result.ManifestDigest != descriptor.ManifestDigest {
		t.Fatalf("digests = %q/%q, want %q/%q", result.PackageDigest, result.ManifestDigest, descriptor.PackageDigest, descriptor.ManifestDigest)
	}
	if _, err := os.Stat(filepath.Join(result.StorePath, "main.py")); err != nil {
		t.Fatalf("copied main.py missing: %v", err)
	}
}

func TestPluginInstallerInstallFromDirectoryAllowsUnsignedDev(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugin")
	writeExamplePlugin(t, pluginDir)
	store, err := NewPluginStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatalf("NewPluginStore: %v", err)
	}
	installer := NewPluginInstaller(store, config.PluginInstallerConfig{
		RequireSignature: true,
		AllowUnsignedDev: true,
	})

	result, err := installer.InstallFromDirectory(t.Context(), pluginDir)
	if err != nil {
		t.Fatalf("InstallFromDirectory: %v", err)
	}
	if result.SignatureStatus != SignatureStatusUnsignedDev {
		t.Fatalf("SignatureStatus = %q, want unsigned_dev", result.SignatureStatus)
	}
}

func TestPluginInstallerRejectsDigestMismatch(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugin")
	writeExamplePlugin(t, pluginDir)
	zipPath := filepath.Join(dir, "echo.zip")
	writeZip(t, zipPath, map[string]string{
		manifestFileName: readFileString(t, filepath.Join(pluginDir, manifestFileName)),
		"main.py":        "print('ok')\n",
	})
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	trustedPath := filepath.Join(dir, "publishers.yaml")
	if err := os.WriteFile(trustedPath, []byte(`
publishers:
  - id: example
    display_name: Example
    public_keys:
      - id: main
        algorithm: ed25519
        public_key_base64: `+base64.StdEncoding.EncodeToString(publicKey)+`
`), 0o644); err != nil {
		t.Fatalf("write trusted publishers: %v", err)
	}
	descriptor := PluginReleaseDescriptor{
		PluginID:       "com.example.echo",
		Version:        "0.1.0",
		PackageDigest:  "sha256:wrong",
		ManifestDigest: sha256Digest([]byte(readFileString(t, filepath.Join(pluginDir, manifestFileName)))),
		PublisherID:    "example",
		KeyID:          "main",
	}
	writeSignedDescriptor(t, zipPath+".sig.yaml", descriptor, privateKey)
	store, _ := NewPluginStore(filepath.Join(dir, "store"))
	installer := NewPluginInstaller(store, config.PluginInstallerConfig{
		RequireSignature:      true,
		TrustedPublishersPath: trustedPath,
	})

	_, err = installer.InstallFromZip(t.Context(), zipPath)
	if err == nil || !strings.Contains(err.Error(), "package digest mismatch") {
		t.Fatalf("InstallFromZip error = %v, want package digest mismatch", err)
	}
}

func TestPluginInstallerRejectsZipSlip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "bad.zip")
	writeZip(t, zipPath, map[string]string{
		"../evil.txt": "bad",
	})
	store, _ := NewPluginStore(filepath.Join(dir, "store"))
	installer := NewPluginInstaller(store, config.PluginInstallerConfig{AllowUnsignedDev: true})

	_, err := installer.InstallFromZip(t.Context(), zipPath)
	if err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("InstallFromZip error = %v, want unsafe zip path", err)
	}
}

func TestPluginStoreRejectsDuplicateImmutablePackage(t *testing.T) {
	store, err := NewPluginStore(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("NewPluginStore: %v", err)
	}
	if _, err := store.CreateImmutablePackageDir("com.example.echo", "0.1.0"); err != nil {
		t.Fatalf("CreateImmutablePackageDir first: %v", err)
	}
	if _, err := store.CreateImmutablePackageDir("com.example.echo", "0.1.0"); err == nil {
		t.Fatal("CreateImmutablePackageDir duplicate error = nil, want error")
	}
}

func writeExamplePlugin(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestFileName), []byte(`
schema_version: emoagent.plugin.v0.2
id: com.example.echo
name: Echo Plugin
version: 0.1.0
emoagent_version: ">=0.2.0"
runtime:
  kind: python_process
  entry: main.py
access:
  tier: runtime_safe
  capabilities:
    - turn.read
hooks:
  - name: after_turn_end
    mode: observe
    failure_policy: fail_open
    priority: 100
    timeout_ms: 200
`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("write main.py: %v", err)
	}
}

func writeSignedDescriptor(t *testing.T, path string, descriptor PluginReleaseDescriptor, privateKey ed25519.PrivateKey) {
	t.Helper()
	payload, err := descriptor.CanonicalPayload()
	if err != nil {
		t.Fatalf("CanonicalPayload: %v", err)
	}
	descriptor.SignatureBase64 = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
	data := "plugin_id: " + descriptor.PluginID + "\n" +
		"version: " + descriptor.Version + "\n" +
		"package_digest: " + descriptor.PackageDigest + "\n" +
		"manifest_digest: " + descriptor.ManifestDigest + "\n" +
		"publisher_id: " + descriptor.PublisherID + "\n" +
		"key_id: " + descriptor.KeyID + "\n" +
		"signature_base64: " + descriptor.SignatureBase64 + "\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write descriptor: %v", err)
	}
}

func writeZip(t *testing.T, path string, files map[string]string) {
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
		t.Fatalf("Close file: %v", err)
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return string(data)
}
