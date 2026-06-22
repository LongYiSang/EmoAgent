package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/longyisang/emoagent/internal/config"
)

type PrivatePythonProvisionResult struct {
	Installed      bool
	ExecutablePath string
	ArtifactDigest string
}

func ProvisionPrivatePythonRuntime(store *PluginStore, cfg config.PluginRuntimeConfig) (PrivatePythonProvisionResult, error) {
	artifactPath := strings.TrimSpace(cfg.PrivatePythonArtifactPath)
	if artifactPath == "" {
		return PrivatePythonProvisionResult{}, nil
	}
	expectedDigest := strings.TrimSpace(cfg.PrivatePythonArtifactSHA256)
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return PrivatePythonProvisionResult{}, fmt.Errorf("read private python artifact: %w", err)
	}
	actualDigest := sha256Digest(data)
	if !strings.EqualFold(actualDigest, expectedDigest) {
		return PrivatePythonProvisionResult{}, fmt.Errorf("private python artifact digest mismatch: got %s want %s", actualDigest, expectedDigest)
	}
	runtimeDir, err := store.PrivatePythonRuntimeDir()
	if err != nil {
		return PrivatePythonProvisionResult{}, err
	}
	executablePath, err := store.PrivatePythonExecutablePath(runtime.GOOS)
	if err != nil {
		return PrivatePythonProvisionResult{}, err
	}
	parent := filepath.Dir(runtimeDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return PrivatePythonProvisionResult{}, err
	}
	tempDir, err := os.MkdirTemp(parent, ".python-install-*")
	if err != nil {
		return PrivatePythonProvisionResult{}, err
	}
	defer os.RemoveAll(tempDir)
	if err := extractZip(data, tempDir); err != nil {
		return PrivatePythonProvisionResult{}, fmt.Errorf("extract private python artifact: %w", err)
	}
	tempExecutable := filepath.Join(tempDir, privatePythonExecutableName(runtime.GOOS))
	info, err := os.Stat(tempExecutable)
	if err != nil {
		return PrivatePythonProvisionResult{}, fmt.Errorf("private python artifact missing %s: %w", privatePythonExecutableName(runtime.GOOS), err)
	}
	if info.IsDir() {
		return PrivatePythonProvisionResult{}, fmt.Errorf("private python artifact %s is a directory", privatePythonExecutableName(runtime.GOOS))
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tempExecutable, 0o755); err != nil {
			return PrivatePythonProvisionResult{}, err
		}
	}
	backupDir := runtimeDir + ".previous"
	_ = os.RemoveAll(backupDir)
	if _, err := os.Stat(runtimeDir); err == nil {
		if err := os.Rename(runtimeDir, backupDir); err != nil {
			return PrivatePythonProvisionResult{}, fmt.Errorf("replace private python runtime: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return PrivatePythonProvisionResult{}, err
	}
	if err := os.Rename(tempDir, runtimeDir); err != nil {
		if _, statErr := os.Stat(backupDir); statErr == nil {
			_ = os.Rename(backupDir, runtimeDir)
		}
		return PrivatePythonProvisionResult{}, fmt.Errorf("install private python runtime: %w", err)
	}
	if _, err := os.Stat(backupDir); err == nil {
		_ = os.RemoveAll(backupDir)
	}
	return PrivatePythonProvisionResult{Installed: true, ExecutablePath: executablePath, ArtifactDigest: actualDigest}, nil
}
