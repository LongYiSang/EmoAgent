package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const dependencyLockFileName = "emo_dependencies.lock.json"
const dependencyLockMarkerFileName = ".emoagent_dependency_lock"

var dependencyProvisionLocks sync.Map

type DependencyProvisionResult struct {
	Installed        bool
	DependencyEnvDir string
	LockDigest       string
	PackageCount     int
}

type DependencyLockSummary struct {
	Present      bool                       `json:"present"`
	LockDigest   string                     `json:"lock_digest,omitempty"`
	PackageCount int                        `json:"package_count"`
	Packages     []DependencyPackageSummary `json:"packages,omitempty"`
}

type DependencyPackageSummary struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type dependencyLockFile struct {
	Version  int                     `json:"version"`
	Packages []dependencyLockPackage `json:"packages"`
}

type dependencyLockPackage struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func ProvisionPluginDependencies(store *PluginStore, manifest ManifestV2) (DependencyProvisionResult, error) {
	packageDir, err := store.PackageDir(manifest.ID, manifest.Version)
	if err != nil {
		return DependencyProvisionResult{}, err
	}
	dependencyEnvDir, err := store.DependencyEnvDir(manifest.ID, manifest.Version)
	if err != nil {
		return DependencyProvisionResult{}, err
	}
	lockKey, err := filepath.Abs(dependencyEnvDir)
	if err != nil {
		lockKey = dependencyEnvDir
	}
	mu := dependencyProvisionLock(lockKey)
	mu.Lock()
	defer mu.Unlock()
	lockPath := filepath.Join(packageDir, dependencyLockFileName)
	lockRaw, err := os.ReadFile(lockPath)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(dependencyEnvDir, 0o755); err != nil {
			return DependencyProvisionResult{}, err
		}
		return DependencyProvisionResult{DependencyEnvDir: dependencyEnvDir}, nil
	}
	if err != nil {
		return DependencyProvisionResult{}, err
	}
	lock, err := decodeDependencyLock(lockRaw)
	if err != nil {
		return DependencyProvisionResult{}, err
	}
	lockDigest := sha256Digest(lockRaw)
	if existingDigest, err := os.ReadFile(filepath.Join(dependencyEnvDir, dependencyLockMarkerFileName)); err == nil {
		if strings.TrimSpace(string(existingDigest)) == lockDigest {
			return DependencyProvisionResult{
				DependencyEnvDir: dependencyEnvDir,
				LockDigest:       lockDigest,
				PackageCount:     len(lock.Packages),
			}, nil
		}
	}
	parent := filepath.Dir(dependencyEnvDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return DependencyProvisionResult{}, err
	}
	tempDir, err := os.MkdirTemp(parent, ".deps-install-*")
	if err != nil {
		return DependencyProvisionResult{}, err
	}
	defer os.RemoveAll(tempDir)
	for _, pkg := range lock.Packages {
		if err := installLockedDependencyPackage(packageDir, tempDir, pkg); err != nil {
			return DependencyProvisionResult{}, err
		}
	}
	if err := os.WriteFile(filepath.Join(tempDir, dependencyLockMarkerFileName), []byte(lockDigest+"\n"), 0o644); err != nil {
		return DependencyProvisionResult{}, err
	}
	backupDir := dependencyEnvDir + ".previous"
	_ = os.RemoveAll(backupDir)
	if _, err := os.Stat(dependencyEnvDir); err == nil {
		if err := os.Rename(dependencyEnvDir, backupDir); err != nil {
			return DependencyProvisionResult{}, fmt.Errorf("replace dependency env: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return DependencyProvisionResult{}, err
	}
	if err := os.Rename(tempDir, dependencyEnvDir); err != nil {
		if _, statErr := os.Stat(backupDir); statErr == nil {
			_ = os.Rename(backupDir, dependencyEnvDir)
		}
		return DependencyProvisionResult{}, fmt.Errorf("install dependency env: %w", err)
	}
	if _, err := os.Stat(backupDir); err == nil {
		_ = os.RemoveAll(backupDir)
	}
	return DependencyProvisionResult{
		Installed:        true,
		DependencyEnvDir: dependencyEnvDir,
		LockDigest:       lockDigest,
		PackageCount:     len(lock.Packages),
	}, nil
}

func DependencyLockDigest(store *PluginStore, manifest ManifestV2) (string, error) {
	summary, err := DependencyLockSummaryForPackage(store, manifest)
	if err != nil || !summary.Present {
		return "", err
	}
	return summary.LockDigest, nil
}

func DependencyLockSummaryForPackage(store *PluginStore, manifest ManifestV2) (DependencyLockSummary, error) {
	packageDir, err := store.PackageDir(manifest.ID, manifest.Version)
	if err != nil {
		return DependencyLockSummary{}, err
	}
	lockRaw, err := os.ReadFile(filepath.Join(packageDir, dependencyLockFileName))
	if os.IsNotExist(err) {
		return DependencyLockSummary{}, nil
	}
	if err != nil {
		return DependencyLockSummary{}, err
	}
	lock, err := decodeDependencyLock(lockRaw)
	if err != nil {
		return DependencyLockSummary{}, err
	}
	packages := make([]DependencyPackageSummary, 0, len(lock.Packages))
	for _, pkg := range lock.Packages {
		packages = append(packages, DependencyPackageSummary{
			Name:   pkg.Name,
			Kind:   pkg.Kind,
			Path:   pkg.Path,
			SHA256: strings.TrimSpace(pkg.SHA256),
		})
	}
	return DependencyLockSummary{
		Present:      true,
		LockDigest:   sha256Digest(lockRaw),
		PackageCount: len(lock.Packages),
		Packages:     packages,
	}, nil
}

func dependencyProvisionLock(key string) *sync.Mutex {
	value, _ := dependencyProvisionLocks.LoadOrStore(key, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func decodeDependencyLock(data []byte) (dependencyLockFile, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var lock dependencyLockFile
	if err := decoder.Decode(&lock); err != nil {
		return dependencyLockFile{}, fmt.Errorf("decode %s: %w", dependencyLockFileName, err)
	}
	if lock.Version != 1 {
		return dependencyLockFile{}, fmt.Errorf("%s version must be 1", dependencyLockFileName)
	}
	for i, pkg := range lock.Packages {
		if strings.TrimSpace(pkg.Name) == "" {
			return dependencyLockFile{}, fmt.Errorf("%s package[%d].name is required", dependencyLockFileName, i)
		}
		if strings.TrimSpace(pkg.Kind) != "python_module_zip" {
			return dependencyLockFile{}, fmt.Errorf("%s package[%d].kind must be python_module_zip", dependencyLockFileName, i)
		}
		if _, err := validatePackagePath(pkg.Path); err != nil {
			return dependencyLockFile{}, err
		}
		digest := strings.TrimSpace(pkg.SHA256)
		validDigest := len(digest) == len("sha256:")+64 && strings.HasPrefix(digest, "sha256:")
		if validDigest {
			for _, r := range digest[len("sha256:"):] {
				if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
					validDigest = false
					break
				}
			}
		}
		if !validDigest {
			return dependencyLockFile{}, fmt.Errorf("%s package[%d].sha256 must be sha256:<64 hex chars>", dependencyLockFileName, i)
		}
	}
	return lock, nil
}

func installLockedDependencyPackage(packageDir, targetDir string, pkg dependencyLockPackage) error {
	relPath, err := validatePackagePath(pkg.Path)
	if err != nil {
		return err
	}
	artifactPath := filepath.Join(packageDir, filepath.FromSlash(relPath))
	info, err := os.Lstat(artifactPath)
	if err != nil {
		return fmt.Errorf("read dependency artifact %q: %w", relPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("dependency artifact %q is a symlink", relPath)
	}
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return fmt.Errorf("read dependency artifact %q: %w", relPath, err)
	}
	actualDigest := sha256Digest(data)
	if !strings.EqualFold(actualDigest, strings.TrimSpace(pkg.SHA256)) {
		return fmt.Errorf("dependency artifact %q digest mismatch: got %s want %s", relPath, actualDigest, pkg.SHA256)
	}
	if err := extractZip(data, targetDir); err != nil {
		return fmt.Errorf("extract dependency artifact %q: %w", relPath, err)
	}
	return nil
}
