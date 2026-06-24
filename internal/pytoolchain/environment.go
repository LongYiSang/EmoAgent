package pytoolchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

type EnvironmentOwnerKind string

const (
	OwnerMemorySidecar EnvironmentOwnerKind = "memory_sidecar"
	OwnerPlugin        EnvironmentOwnerKind = "plugin"
)

type EnvState string

const (
	EnvMissing   EnvState = "missing"
	EnvNeedsSync EnvState = "needs_sync"
	EnvSyncing   EnvState = "syncing"
	EnvReady     EnvState = "ready"
	EnvBroken    EnvState = "broken"
)

const environmentMarkerName = ".emoagent-python-env.json"

type EnvironmentOwner struct {
	Kind       EnvironmentOwnerKind `json:"kind"`
	ID         string               `json:"id"`
	Version    string               `json:"version,omitempty"`
	ProjectDir string               `json:"project_dir"`
	EnvDir     string               `json:"env_dir"`
}

type EnvironmentSummary struct {
	Owner       EnvironmentOwner  `json:"owner"`
	Status      EnvironmentStatus `json:"status"`
	Enabled     bool              `json:"enabled"`
	RuntimeKind string            `json:"runtime_kind,omitempty"`
}

type EnvironmentStatus struct {
	State  EnvState           `json:"state"`
	Marker *EnvironmentMarker `json:"marker,omitempty"`
	Reason string             `json:"reason,omitempty"`
}

type EnvironmentMarker struct {
	SchemaVersion        string `json:"schema_version"`
	OwnerKind            string `json:"owner_kind"`
	OwnerID              string `json:"owner_id"`
	OwnerVersion         string `json:"owner_version"`
	ToolchainFingerprint string `json:"toolchain_fingerprint"`
	ProjectPathHash      string `json:"project_path_hash"`
	PyprojectHash        string `json:"pyproject_hash"`
	UVLockHash           string `json:"uv_lock_hash"`
	EnvironmentPython    string `json:"environment_python"`
	SyncStatus           string `json:"sync_status"`
	SyncedAt             string `json:"synced_at"`
}

type EnvironmentManagerOptions struct {
	Runner      Runner
	Toolchain   ProbeResult
	ProcessArch string
}

type EnvironmentManager struct {
	cfg         config.PythonToolchainConfig
	runner      Runner
	toolchain   ProbeResult
	processArch string

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewEnvironmentManager(cfg config.PythonToolchainConfig, opts EnvironmentManagerOptions) *EnvironmentManager {
	applyManagerDefaults(&cfg)
	runner := opts.Runner
	if runner == nil {
		runner = osExecRunner{}
	}
	processArch := strings.TrimSpace(opts.ProcessArch)
	if processArch == "" {
		processArch = defaultProcessArchitecture()
	}
	return &EnvironmentManager{
		cfg:         cfg,
		runner:      runner,
		toolchain:   opts.Toolchain,
		processArch: processArch,
		locks:       map[string]*sync.Mutex{},
	}
}

func (m *EnvironmentManager) Ensure(ctx context.Context, owner EnvironmentOwner) (EnvironmentStatus, error) {
	if m == nil {
		return EnvironmentStatus{State: EnvBroken, Reason: "environment manager is nil"}, fmt.Errorf("environment manager is nil")
	}
	if err := owner.validate(); err != nil {
		return EnvironmentStatus{State: EnvBroken, Reason: err.Error()}, err
	}
	lock := m.lockFor(owner.EnvDir)
	lock.Lock()
	defer lock.Unlock()

	status, needsSync, err := m.inspect(ctx, owner)
	if err != nil && !needsSync {
		return status, err
	}
	if !needsSync {
		return status, nil
	}
	status = EnvironmentStatus{State: EnvSyncing, Reason: status.Reason}
	marker, syncErr := m.sync(ctx, owner)
	if syncErr != nil {
		return EnvironmentStatus{State: EnvBroken, Reason: syncErr.Error()}, syncErr
	}
	return EnvironmentStatus{State: EnvReady, Marker: marker}, nil
}

func (m *EnvironmentManager) Status(ctx context.Context, owner EnvironmentOwner) (EnvironmentStatus, error) {
	if m == nil {
		return EnvironmentStatus{State: EnvBroken, Reason: "environment manager is nil"}, fmt.Errorf("environment manager is nil")
	}
	if err := owner.validate(); err != nil {
		return EnvironmentStatus{State: EnvBroken, Reason: err.Error()}, err
	}
	status, _, err := m.inspect(ctx, owner)
	return status, err
}

func (m *EnvironmentManager) inspect(ctx context.Context, owner EnvironmentOwner) (EnvironmentStatus, bool, error) {
	expected, err := m.expectedMarker(owner, "")
	if err != nil {
		return EnvironmentStatus{State: EnvBroken, Reason: err.Error()}, false, err
	}
	marker, err := ReadEnvironmentMarker(owner.EnvDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return EnvironmentStatus{State: EnvMissing, Reason: "environment marker missing"}, true, nil
		}
		return EnvironmentStatus{State: EnvNeedsSync, Reason: "environment marker invalid"}, true, nil
	}
	if marker.OwnerKind != expected.OwnerKind ||
		marker.OwnerID != expected.OwnerID ||
		marker.OwnerVersion != expected.OwnerVersion ||
		marker.ToolchainFingerprint != expected.ToolchainFingerprint ||
		marker.ProjectPathHash != expected.ProjectPathHash ||
		marker.PyprojectHash != expected.PyprojectHash ||
		marker.UVLockHash != expected.UVLockHash ||
		marker.SyncStatus != string(EnvReady) {
		return EnvironmentStatus{State: EnvNeedsSync, Marker: marker, Reason: "environment marker is stale"}, true, nil
	}
	if _, err := m.probeEnvironmentPython(ctx, marker.EnvironmentPython); err != nil {
		return EnvironmentStatus{State: EnvNeedsSync, Marker: marker, Reason: err.Error()}, true, nil
	}
	return EnvironmentStatus{State: EnvReady, Marker: marker}, false, nil
}

func (m *EnvironmentManager) sync(ctx context.Context, owner EnvironmentOwner) (*EnvironmentMarker, error) {
	parent := filepath.Dir(owner.EnvDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	tempDir := filepath.Join(parent, ".sync-"+filepath.Base(owner.EnvDir)+"-"+strconvTime())
	previousDir := owner.EnvDir + ".previous"
	_ = os.RemoveAll(tempDir)
	_ = os.RemoveAll(previousDir)
	defer os.RemoveAll(tempDir)

	if err := m.runUV(ctx, []string{"lock", "--check", "--project", owner.ProjectDir}, "", "uv lock --check"); err != nil {
		return nil, err
	}
	if err := m.runUV(ctx, []string{"sync", "--locked", "--no-dev", "--project", owner.ProjectDir}, tempDir, "uv sync --locked --no-dev"); err != nil {
		return nil, err
	}
	envPython := environmentPythonPath(tempDir)
	if _, err := m.probeEnvironmentPython(ctx, envPython); err != nil {
		return nil, fmt.Errorf("environment python self-test: %w", err)
	}
	marker, err := m.expectedMarker(owner, environmentPythonPath(owner.EnvDir))
	if err != nil {
		return nil, err
	}
	if err := writeEnvironmentMarker(tempDir, marker); err != nil {
		return nil, err
	}
	if _, err := os.Stat(owner.EnvDir); err == nil {
		if err := os.Rename(owner.EnvDir, previousDir); err != nil {
			return nil, fmt.Errorf("preserve previous environment: %w", err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.Rename(tempDir, owner.EnvDir); err != nil {
		if _, statErr := os.Stat(previousDir); statErr == nil {
			_ = os.Rename(previousDir, owner.EnvDir)
		}
		return nil, fmt.Errorf("activate synced environment: %w", err)
	}
	_ = os.RemoveAll(previousDir)
	return marker, nil
}

func (m *EnvironmentManager) runUV(ctx context.Context, args []string, targetEnv string, label string) error {
	env := []string(nil)
	if targetEnv != "" {
		env = m.uvEnv(targetEnv)
	}
	result := m.runner.Run(ctx, Command{
		Path:    strings.TrimSpace(m.cfg.UVExecutable),
		Args:    args,
		Env:     env,
		Timeout: m.commandTimeout(),
	})
	if result.Err != nil {
		return fmt.Errorf("%s failed: %w", label, result.Err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("%s failed with exit code %d: %s", label, result.ExitCode, sanitizeLogText(result.Stderr))
	}
	return nil
}

func (m *EnvironmentManager) uvEnv(targetEnv string) []string {
	manager := NewManager(m.cfg, WithRunner(m.runner), WithProcessArchitecture(m.processArch))
	return manager.uvCommandEnv(targetEnv)
}

func (m *EnvironmentManager) probeEnvironmentPython(ctx context.Context, python string) (PythonProbeResult, error) {
	manager := NewManager(m.cfg, WithRunner(m.runner), WithProcessArchitecture(m.processArch))
	return manager.probePython(ctx, python)
}

func (m *EnvironmentManager) expectedMarker(owner EnvironmentOwner, envPython string) (*EnvironmentMarker, error) {
	projectDir, err := filepath.Abs(owner.ProjectDir)
	if err != nil {
		return nil, err
	}
	pyprojectHash, err := fileSHA256(filepath.Join(projectDir, "pyproject.toml"))
	if err != nil {
		return nil, err
	}
	lockHash, err := fileSHA256(filepath.Join(projectDir, "uv.lock"))
	if err != nil {
		return nil, err
	}
	if envPython == "" {
		envPython = environmentPythonPath(owner.EnvDir)
	}
	return &EnvironmentMarker{
		SchemaVersion:        "emoagent.python_env.v1",
		OwnerKind:            string(owner.Kind),
		OwnerID:              strings.TrimSpace(owner.ID),
		OwnerVersion:         strings.TrimSpace(owner.Version),
		ToolchainFingerprint: fingerprintDigest(m.toolchain.Fingerprint),
		ProjectPathHash:      stringSHA256(projectDir),
		PyprojectHash:        pyprojectHash,
		UVLockHash:           lockHash,
		EnvironmentPython:    envPython,
		SyncStatus:           string(EnvReady),
		SyncedAt:             time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

func (m *EnvironmentManager) commandTimeout() time.Duration {
	if m != nil && m.cfg.SyncTimeoutSeconds > 0 {
		return time.Duration(m.cfg.SyncTimeoutSeconds) * time.Second
	}
	return 600 * time.Second
}

func (m *EnvironmentManager) lockFor(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	lock := m.locks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		m.locks[key] = lock
	}
	return lock
}

func (o EnvironmentOwner) validate() error {
	if o.Kind != OwnerMemorySidecar && o.Kind != OwnerPlugin {
		return fmt.Errorf("environment owner kind is required")
	}
	if strings.TrimSpace(o.ID) == "" {
		return fmt.Errorf("environment owner id is required")
	}
	if strings.TrimSpace(o.ProjectDir) == "" {
		return fmt.Errorf("environment project dir is required")
	}
	if strings.TrimSpace(o.EnvDir) == "" {
		return fmt.Errorf("environment dir is required")
	}
	return nil
}

func ReadEnvironmentMarker(envDir string) (*EnvironmentMarker, error) {
	body, err := os.ReadFile(filepath.Join(envDir, environmentMarkerName))
	if err != nil {
		return nil, err
	}
	var marker EnvironmentMarker
	if err := json.Unmarshal(body, &marker); err != nil {
		return nil, err
	}
	return &marker, nil
}

func writeEnvironmentMarker(envDir string, marker *EnvironmentMarker) error {
	if marker == nil {
		return fmt.Errorf("environment marker is nil")
	}
	body, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(envDir, environmentMarkerName), append(body, '\n'), 0o644)
}

func environmentPythonPath(envDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(envDir, "Scripts", "python.exe")
	}
	return filepath.Join(envDir, "bin", "python")
}

func EnvironmentPythonPath(envDir string) string {
	return environmentPythonPath(envDir)
}

func fileSHA256(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func stringSHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func fingerprintDigest(fingerprint ToolchainFingerprint) string {
	body, _ := json.Marshal(fingerprint)
	return stringSHA256(string(body))
}

func strconvTime() string {
	return strconvFormatInt(time.Now().UnixNano())
}

func strconvFormatInt(value int64) string {
	return fmt.Sprintf("%d", value)
}
