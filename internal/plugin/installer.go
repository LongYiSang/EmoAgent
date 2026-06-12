package plugin

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/longyisang/emoagent/internal/config"
)

const manifestFileName = "emo_plugin.yaml"

type PluginInstaller struct {
	Store  *PluginStore
	Config config.PluginInstallerConfig
	Client *http.Client
}

type InstallResult struct {
	PluginID        string
	Version         string
	Name            string
	Manifest        ManifestV2
	ManifestJSON    string
	SourceType      string
	SourceRef       string
	PackageDigest   string
	ManifestDigest  string
	SignatureStatus string
	PublisherID     string
	StorePath       string
}

func NewPluginInstaller(store *PluginStore, cfg config.PluginInstallerConfig) *PluginInstaller {
	return &PluginInstaller{Store: store, Config: cfg, Client: http.DefaultClient}
}

func (i *PluginInstaller) InstallFromZip(ctx context.Context, path string) (InstallResult, error) {
	if err := ctx.Err(); err != nil {
		return InstallResult{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return InstallResult{}, err
	}
	packageDigest := sha256Digest(data)
	tempDir, err := os.MkdirTemp("", "emoagent-plugin-zip-*")
	if err != nil {
		return InstallResult{}, err
	}
	defer os.RemoveAll(tempDir)

	if err := extractZip(data, tempDir); err != nil {
		return InstallResult{}, err
	}
	descriptor, descriptorFound, err := readReleaseDescriptor(path+".sig.yaml", filepath.Join(tempDir, "emo_plugin.signature.yaml"))
	if err != nil {
		return InstallResult{}, err
	}
	return i.installFromPreparedDir(ctx, tempDir, "local_zip", path, packageDigest, descriptor, descriptorFound)
}

func (i *PluginInstaller) InstallFromDirectory(ctx context.Context, path string) (InstallResult, error) {
	if err := ctx.Err(); err != nil {
		return InstallResult{}, err
	}
	descriptor, descriptorFound, err := readReleaseDescriptor(filepath.Join(path, "emo_plugin.signature.yaml"))
	if err != nil {
		return InstallResult{}, err
	}
	return i.installFromPreparedDir(ctx, path, "local_dir", path, "", descriptor, descriptorFound)
}

func (i *PluginInstaller) InstallFromGitHubRelease(ctx context.Context, owner, repo, tag, asset string) (InstallResult, error) {
	if !i.Config.GithubEnabled {
		return InstallResult{}, fmt.Errorf("github plugin installs are disabled")
	}
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(repo) == "" || strings.TrimSpace(tag) == "" || strings.TrimSpace(asset) == "" {
		return InstallResult{}, fmt.Errorf("owner, repo, tag, and asset are required")
	}
	url := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", owner, repo, tag, asset)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return InstallResult{}, err
	}
	client := i.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return InstallResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return InstallResult{}, fmt.Errorf("download github release asset: status %d", resp.StatusCode)
	}
	temp, err := os.CreateTemp("", "emoagent-plugin-release-*.zip")
	if err != nil {
		return InstallResult{}, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	defer temp.Close()
	if _, err := io.Copy(temp, resp.Body); err != nil {
		return InstallResult{}, err
	}
	if err := temp.Close(); err != nil {
		return InstallResult{}, err
	}
	result, err := i.InstallFromZip(ctx, tempPath)
	if err != nil {
		return InstallResult{}, err
	}
	result.SourceType = "github_release"
	result.SourceRef = url
	return result, nil
}

func (i *PluginInstaller) installFromPreparedDir(ctx context.Context, sourceDir, sourceType, sourceRef, packageDigest string, descriptor PluginReleaseDescriptor, descriptorFound bool) (InstallResult, error) {
	if i == nil || i.Store == nil {
		return InstallResult{}, fmt.Errorf("plugin installer store is not configured")
	}
	manifestRaw, err := os.ReadFile(filepath.Join(sourceDir, manifestFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return InstallResult{}, fmt.Errorf("plugin package missing %s", manifestFileName)
		}
		return InstallResult{}, err
	}
	manifestDigest := sha256Digest(manifestRaw)
	manifest, err := DecodeManifestV2YAML(manifestRaw, ManifestValidationOptions{})
	if err != nil {
		return InstallResult{}, err
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return InstallResult{}, err
	}
	signatureStatus, publisherID, err := i.verifyInstallSignature(descriptor, descriptorFound, manifest.ID, manifest.Version, packageDigest, manifestDigest, sourceType)
	if err != nil {
		return InstallResult{}, err
	}
	storePath, err := i.Store.PackageDir(manifest.ID, manifest.Version)
	if err != nil {
		return InstallResult{}, err
	}
	if _, err := os.Stat(storePath); err == nil {
		return InstallResult{}, fmt.Errorf("plugin package %s@%s already exists", manifest.ID, manifest.Version)
	} else if !os.IsNotExist(err) {
		return InstallResult{}, err
	}
	parent := filepath.Dir(storePath)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return InstallResult{}, err
	}
	tempStorePath, err := os.MkdirTemp(parent, ".tmp-"+manifest.ID+"-"+manifest.Version+"-*")
	if err != nil {
		return InstallResult{}, err
	}
	if err := copyPackageDir(sourceDir, tempStorePath); err != nil {
		_ = os.RemoveAll(tempStorePath)
		return InstallResult{}, err
	}
	if err := i.Store.PrepareRuntimeDirs(manifest.ID); err != nil {
		_ = os.RemoveAll(tempStorePath)
		return InstallResult{}, err
	}
	if err := os.Rename(tempStorePath, storePath); err != nil {
		_ = os.RemoveAll(tempStorePath)
		return InstallResult{}, err
	}
	return InstallResult{
		PluginID:        manifest.ID,
		Version:         manifest.Version,
		Name:            manifest.Name,
		Manifest:        manifest,
		ManifestJSON:    string(manifestJSON),
		SourceType:      sourceType,
		SourceRef:       sourceRef,
		PackageDigest:   packageDigest,
		ManifestDigest:  manifestDigest,
		SignatureStatus: signatureStatus,
		PublisherID:     publisherID,
		StorePath:       storePath,
	}, ctx.Err()
}

func (i *PluginInstaller) verifyInstallSignature(descriptor PluginReleaseDescriptor, found bool, pluginID, version, packageDigest, manifestDigest, sourceType string) (string, string, error) {
	if !found {
		if i.Config.AllowUnsignedDev && sourceType == "local_dir" {
			return SignatureStatusUnsignedDev, "", nil
		}
		if i.Config.AllowUnsignedDev && sourceType == "local_zip" {
			return SignatureStatusUnsignedDev, "", nil
		}
		if i.Config.RequireSignature {
			return SignatureStatusMissingSignature, "", fmt.Errorf("plugin signature is required")
		}
		return SignatureStatusMissingSignature, "", nil
	}
	publishers, err := LoadTrustedPublishers(i.Config.TrustedPublishersPath)
	if err != nil {
		return "", "", err
	}
	status, err := VerifyReleaseDescriptor(descriptor, publishers, pluginID, version, packageDigest, manifestDigest)
	if err != nil {
		return status, descriptor.PublisherID, err
	}
	return status, descriptor.PublisherID, nil
}

func readReleaseDescriptor(paths ...string) (PluginReleaseDescriptor, bool, error) {
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return PluginReleaseDescriptor{}, false, err
		}
		descriptor, err := DecodeReleaseDescriptor(data)
		if err != nil {
			return PluginReleaseDescriptor{}, false, err
		}
		return descriptor, true, nil
	}
	return PluginReleaseDescriptor{}, false, nil
}

func extractZip(data []byte, target string) error {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, file := range reader.File {
		name, err := validatePackagePath(file.Name)
		if err != nil {
			return err
		}
		info := file.FileInfo()
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("zip symlink %q is not allowed", file.Name)
		}
		dst := filepath.Join(target, filepath.FromSlash(name))
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			src.Close()
			return err
		}
		_, copyErr := io.Copy(out, src)
		closeErr := out.Close()
		src.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func copyPackageDir(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		cleanRel, err := validatePackagePath(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink %q is not allowed", rel)
		}
		dst := filepath.Join(target, filepath.FromSlash(cleanRel))
		if entry.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("package entry %q is not a regular file", rel)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}

func validatePackagePath(name string) (string, error) {
	name = strings.TrimSpace(filepath.ToSlash(name))
	if name == "" {
		return "", fmt.Errorf("package path is empty")
	}
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) || filepath.IsAbs(name) {
		return "", fmt.Errorf("package path %q must be relative", name)
	}
	cleaned := filepath.ToSlash(filepath.Clean(name))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("package path %q is unsafe", name)
	}
	return cleaned, nil
}
