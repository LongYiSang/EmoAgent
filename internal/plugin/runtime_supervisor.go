package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/tool/resultv2"
)

type RuntimeStatus struct {
	PluginID                  string      `json:"plugin_id"`
	Version                   string      `json:"version,omitempty"`
	RuntimeKind               RuntimeKind `json:"runtime_kind,omitempty"`
	Status                    string      `json:"status"`
	LastError                 string      `json:"last_error,omitempty"`
	RestartCount              int         `json:"restart_count"`
	StderrTail                string      `json:"stderr_tail,omitempty"`
	PythonExecutablePath      string      `json:"python_executable_path,omitempty"`
	PythonExecutableSource    string      `json:"python_executable_source,omitempty"`
	PythonExecutableAvailable *bool       `json:"python_executable_available,omitempty"`
	DependencyEnvDir          string      `json:"dependency_env_dir,omitempty"`
	PID                       int         `json:"pid,omitempty"`
	ProcessGuardKind          string      `json:"process_guard_kind,omitempty"`
	ProcessGuardAttached      bool        `json:"process_guard_attached,omitempty"`
	ProcessGuardError         string      `json:"process_guard_error,omitempty"`
}

type RuntimeSupervisor struct {
	store             *PluginStore
	cfg               config.PluginRuntimeConfig
	hostHandler       JSONRPCHandler
	hostHandlerFor    func(string) JSONRPCHandler
	enabled           func(context.Context, string) bool
	blockedEnvNames   []string
	blockedEnvNamesFn func() []string
	additionalEnvVars []string

	mu       sync.Mutex
	plugins  map[string]ManifestV2
	runtimes map[string]*supervisedRuntime
}

type supervisedRuntime struct {
	manifest                  ManifestV2
	runtime                   *ProcessRuntime
	tools                     []ProcessToolSpec
	status                    string
	lastError                 string
	restartCount              int
	runtimeKind               RuntimeKind
	pythonExecutablePath      string
	pythonExecutableSource    string
	pythonExecutableAvailable *bool
	dependencyEnvDir          string
	pid                       int
	processGuardKind          string
	processGuardAttached      bool
	processGuardError         string
}

const (
	PythonExecutableSourcePrivate      = "private_python_executable"
	PythonExecutableSourceStorePrivate = "store_private_runtime"
	PythonExecutableSourceLegacy       = "legacy_python_executable"
)

type pythonExecutableDiagnostics struct {
	path      string
	source    string
	available *bool
}

type InitializeRequest struct {
	PluginID     string     `json:"plugin_id"`
	Version      string     `json:"version"`
	Manifest     ManifestV2 `json:"manifest"`
	Protocol     string     `json:"protocol"`
	Capabilities []string   `json:"capabilities"`
}

type InitializeResponse struct {
	Tools []ProcessToolSpec `json:"tools"`
}

type ProcessToolSpec struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  json.RawMessage        `json:"parameters"`
	Scope       tool.Scope             `json:"scope"`
	Permission  tool.Permission        `json:"permission"`
	Trust       resultv2.ContentLabels `json:"trust,omitempty"`
}

type hookInvokeRequest struct {
	Hook    HookName    `json:"hook"`
	Context HookContext `json:"context"`
}

type toolInvokeRequest struct {
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
}

func NewRuntimeSupervisor(store *PluginStore, cfg config.PluginRuntimeConfig, handler JSONRPCHandler) *RuntimeSupervisor {
	return &RuntimeSupervisor{
		store:       store,
		cfg:         cfg,
		hostHandler: handler,
		plugins:     map[string]ManifestV2{},
		runtimes:    map[string]*supervisedRuntime{},
	}
}

func (s *RuntimeSupervisor) SetEnabledChecker(checker func(context.Context, string) bool) {
	if s != nil {
		s.enabled = checker
	}
}

func (s *RuntimeSupervisor) SetHostHandlerForPlugin(handler func(string) JSONRPCHandler) {
	if s != nil {
		s.hostHandlerFor = handler
	}
}

func (s *RuntimeSupervisor) SetBlockedEnvNames(values []string) {
	if s != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.blockedEnvNames = append([]string(nil), values...)
	}
}

func (s *RuntimeSupervisor) SetBlockedEnvNamesProvider(fn func() []string) {
	if s != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.blockedEnvNamesFn = fn
	}
}

func (s *RuntimeSupervisor) SetAdditionalEnvVars(values []string) {
	if s != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.additionalEnvVars = append([]string(nil), values...)
	}
}

func (s *RuntimeSupervisor) AddPlugin(manifest ManifestV2) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plugins[manifest.ID] = manifest
}

func (s *RuntimeSupervisor) RemovePlugin(pluginID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.plugins, pluginID)
}

func (s *RuntimeSupervisor) EnsureReady(ctx context.Context, pluginID string) (*ProcessRuntime, error) {
	if s == nil {
		return nil, fmt.Errorf("runtime supervisor is nil")
	}
	if s.enabled != nil && !s.enabled(ctx, pluginID) {
		return nil, fmt.Errorf("plugin %q is disabled", pluginID)
	}
	s.mu.Lock()
	existing := s.runtimes[pluginID]
	manifest, ok := s.plugins[pluginID]
	if existing != nil && existing.runtime != nil && existing.status == "running" {
		if !ok {
			runtime := existing.runtime
			delete(s.runtimes, pluginID)
			s.mu.Unlock()
			_ = runtime.Stop(context.Background())
			return nil, fmt.Errorf("plugin %q is not registered with supervisor", pluginID)
		}
		if manifest.Version != "" && existing.manifest.Version != "" && manifest.Version != existing.manifest.Version {
			runtime := existing.runtime
			delete(s.runtimes, pluginID)
			s.mu.Unlock()
			if err := runtime.Stop(context.Background()); err != nil {
				s.recordRuntimeError(pluginID, err)
				return nil, err
			}
		} else {
			runtime := existing.runtime
			s.mu.Unlock()
			return runtime, nil
		}
	} else {
		s.mu.Unlock()
	}
	if !ok {
		return nil, fmt.Errorf("plugin %q is not registered with supervisor", pluginID)
	}
	if !s.cfg.ProcessEnabled {
		return nil, fmt.Errorf("plugin process runtime is disabled")
	}
	if manifest.Runtime.Kind != RuntimeManagedPythonProcess && manifest.Runtime.Kind != RuntimePythonProcess && manifest.Runtime.Kind != RuntimeProcess {
		return nil, fmt.Errorf("plugin runtime kind %q cannot be launched as process", manifest.Runtime.Kind)
	}
	runtime, err := s.startRuntime(ctx, manifest)
	if err != nil {
		s.recordRuntimeError(pluginID, err)
		return nil, err
	}
	return runtime, nil
}

func (s *RuntimeSupervisor) startRuntime(ctx context.Context, manifest ManifestV2) (*ProcessRuntime, error) {
	packageDir, err := s.store.PackageDir(manifest.ID, manifest.Version)
	if err != nil {
		return nil, err
	}
	stateDir, err := s.store.StateDir(manifest.ID)
	if err != nil {
		return nil, err
	}
	cacheDir, err := s.store.CacheDir(manifest.ID)
	if err != nil {
		return nil, err
	}
	runDir, err := s.store.RunDir(manifest.ID)
	if err != nil {
		return nil, err
	}
	if err := s.store.PrepareRuntimeDirs(manifest.ID); err != nil {
		return nil, err
	}
	dependencyEnvDir := ""
	if manifest.Runtime.Kind == RuntimeManagedPythonProcess {
		provisionResult, err := ProvisionPluginDependencies(s.store, manifest)
		if err != nil {
			return nil, err
		}
		dependencyEnvDir = provisionResult.DependencyEnvDir
	}
	pythonDiag, err := s.resolvePythonExecutable(manifest.Runtime.Kind)
	if err != nil {
		return nil, err
	}
	python := pythonDiag.path
	handler := s.hostHandler
	if s.hostHandlerFor != nil {
		handler = s.hostHandlerFor(manifest.ID)
	}
	blockedEnvNames, additionalEnvVars := s.launchEnvSnapshot()
	runtime, err := StartProcessRuntime(ctx, ProcessLaunchConfig{
		PluginID:          manifest.ID,
		Version:           manifest.Version,
		WorkDir:           packageDir,
		Entry:             manifest.Runtime.Entry,
		PythonExecutable:  python,
		StateDir:          stateDir,
		CacheDir:          cacheDir,
		RunDir:            runDir,
		DependencyEnvDir:  dependencyEnvDir,
		ManagedPython:     manifest.Runtime.Kind == RuntimeManagedPythonProcess,
		StartupTimeout:    time.Duration(s.cfg.StartupTimeoutMS) * time.Millisecond,
		ShutdownTimeout:   time.Duration(s.cfg.ShutdownTimeoutMS) * time.Millisecond,
		MaxStderrBytes:    s.cfg.MaxStderrBytes,
		MaxProcesses:      s.cfg.MaxProcesses,
		MemoryBytes:       int64(s.cfg.MemoryMB) << 20,
		CPUQuota:          s.cfg.CPUs,
		BlockedEnvNames:   blockedEnvNames,
		AdditionalEnvVars: additionalEnvVars,
		OnProtocolError: func(err error) {
			s.recordRuntimeError(manifest.ID, fmt.Errorf("plugin protocol error: %w", err))
		},
	}, handler)
	if err != nil {
		return nil, err
	}
	initCtx := ctx
	if _, ok := initCtx.Deadline(); !ok && s.cfg.StartupTimeoutMS > 0 {
		var cancel context.CancelFunc
		initCtx, cancel = context.WithTimeout(ctx, time.Duration(s.cfg.StartupTimeoutMS)*time.Millisecond)
		defer cancel()
	}
	var initResp InitializeResponse
	if err := runtime.Call(initCtx, "initialize", InitializeRequest{
		PluginID: manifest.ID,
		Version:  manifest.Version,
		Manifest: manifest,
		Protocol: "emoagent.plugin.stdio_jsonrpc.v0.2",
	}, &initResp); err != nil {
		_ = runtime.Stop(context.Background())
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.runtimes[manifest.ID]
	restarts := 0
	if previous != nil {
		restarts = previous.restartCount + 1
	}
	s.runtimes[manifest.ID] = &supervisedRuntime{
		manifest:                  manifest,
		runtime:                   runtime,
		tools:                     initResp.Tools,
		status:                    "running",
		restartCount:              restarts,
		runtimeKind:               manifest.Runtime.Kind,
		pythonExecutablePath:      pythonDiag.path,
		pythonExecutableSource:    pythonDiag.source,
		pythonExecutableAvailable: cloneBoolPtr(pythonDiag.available),
		dependencyEnvDir:          dependencyEnvDir,
	}
	applyProcessRuntimeDiagnosticsToRecord(s.runtimes[manifest.ID], runtime)
	return runtime, nil
}

func (s *RuntimeSupervisor) pythonExecutableFor(kind RuntimeKind) (string, error) {
	diag, err := s.resolvePythonExecutable(kind)
	if err != nil {
		return "", err
	}
	return diag.path, nil
}

func (s *RuntimeSupervisor) launchEnvSnapshot() ([]string, []string) {
	s.mu.Lock()
	blocked := append([]string(nil), s.blockedEnvNames...)
	provider := s.blockedEnvNamesFn
	additional := append([]string(nil), s.additionalEnvVars...)
	s.mu.Unlock()
	if provider != nil {
		blocked = append(blocked, provider()...)
	}
	return blocked, additional
}

func (s *RuntimeSupervisor) resolvePythonExecutable(kind RuntimeKind) (pythonExecutableDiagnostics, error) {
	switch kind {
	case RuntimeManagedPythonProcess:
		python := strings.TrimSpace(s.cfg.PrivatePythonExecutable)
		if python != "" {
			diag := pythonExecutableDiagnostics{path: python, source: PythonExecutableSourcePrivate, available: boolPtr(false)}
			if !filepath.IsAbs(python) {
				return diag, fmt.Errorf("private_python_executable must be an absolute path")
			}
			if _, err := os.Stat(python); err != nil {
				return diag, fmt.Errorf("managed python runtime unavailable at %q: %w", python, err)
			}
			diag.available = boolPtr(true)
			return diag, nil
		}
		python, err := s.defaultPrivatePythonExecutable()
		diag := pythonExecutableDiagnostics{path: python, source: PythonExecutableSourceStorePrivate, available: boolPtr(false)}
		if err != nil {
			return diag, err
		}
		if _, err := os.Stat(python); err != nil {
			return diag, fmt.Errorf("managed python runtime unavailable at %q: %w", python, err)
		}
		diag.available = boolPtr(true)
		return diag, nil
	case RuntimePythonProcess, RuntimeProcess:
		python := strings.TrimSpace(s.cfg.PythonExecutable)
		diag := pythonExecutableDiagnostics{path: python, source: PythonExecutableSourceLegacy}
		if python == "" {
			return diag, fmt.Errorf("python_executable is required for legacy python_process")
		}
		return diag, nil
	default:
		return pythonExecutableDiagnostics{}, fmt.Errorf("plugin runtime kind %q cannot be launched as process", kind)
	}
}

func (s *RuntimeSupervisor) defaultPrivatePythonExecutable() (string, error) {
	if s == nil || s.store == nil || strings.TrimSpace(s.store.RootDir) == "" {
		return "", fmt.Errorf("managed python runtime unavailable: plugin store is not configured")
	}
	return filepath.Join(s.store.RootDir, "runtime", "python", privatePythonExecutableName(runtime.GOOS)), nil
}

func privatePythonExecutableName(goos string) string {
	if strings.EqualFold(goos, "windows") {
		return "python.exe"
	}
	return "python"
}

func (s *RuntimeSupervisor) InvokeHook(ctx context.Context, pluginID string, hook HookName, hc HookContext) (HookResult, error) {
	runtime, err := s.EnsureReady(ctx, pluginID)
	if err != nil {
		return HookResult{}, err
	}
	var result HookResult
	if err := runtime.Call(ctx, "invoke_hook", hookInvokeRequest{Hook: hook, Context: hc}, &result); err != nil {
		s.recordRuntimeError(pluginID, err)
		return HookResult{}, err
	}
	return result, nil
}

func (s *RuntimeSupervisor) InvokeTool(ctx context.Context, pluginID string, name string, input json.RawMessage) (json.RawMessage, error) {
	runtime, err := s.EnsureReady(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	var result json.RawMessage
	if err := runtime.Call(ctx, "invoke_tool", toolInvokeRequest{Tool: name, Input: input}, &result); err != nil {
		s.recordRuntimeError(pluginID, err)
		return nil, err
	}
	return result, nil
}

func (s *RuntimeSupervisor) Stop(ctx context.Context, pluginID string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	record := s.runtimes[pluginID]
	delete(s.runtimes, pluginID)
	s.mu.Unlock()
	if record == nil || record.runtime == nil {
		return nil
	}
	err := record.runtime.Stop(ctx)
	if err != nil {
		s.recordRuntimeError(pluginID, err)
		return err
	}
	return nil
}

func (s *RuntimeSupervisor) StopAll(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	ids := make([]string, 0, len(s.runtimes))
	for id := range s.runtimes {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	var closeErr error
	for _, id := range ids {
		if err := s.Stop(ctx, id); err != nil {
			closeErr = err
		}
	}
	return closeErr
}

func (s *RuntimeSupervisor) Status(pluginID string) RuntimeStatus {
	if s == nil {
		return RuntimeStatus{PluginID: pluginID, Status: "stopped"}
	}
	s.mu.Lock()
	record := s.runtimes[pluginID]
	if record == nil {
		manifest, ok := s.plugins[pluginID]
		s.mu.Unlock()
		status := RuntimeStatus{PluginID: pluginID, Version: manifest.Version, Status: "stopped"}
		if ok {
			status.RuntimeKind = manifest.Runtime.Kind
			applyPythonDiagnostics(&status, s.pythonDiagnosticsForKind(manifest.Runtime.Kind))
			status.DependencyEnvDir = s.dependencyEnvDirForManifest(manifest)
		}
		return status
	}
	status := RuntimeStatus{
		PluginID:                  pluginID,
		Version:                   record.manifest.Version,
		RuntimeKind:               firstRuntimeKind(record.runtimeKind, record.manifest.Runtime.Kind),
		Status:                    record.status,
		LastError:                 record.lastError,
		RestartCount:              record.restartCount,
		PythonExecutablePath:      record.pythonExecutablePath,
		PythonExecutableSource:    record.pythonExecutableSource,
		PythonExecutableAvailable: cloneBoolPtr(record.pythonExecutableAvailable),
		DependencyEnvDir:          record.dependencyEnvDir,
		PID:                       record.pid,
		ProcessGuardKind:          record.processGuardKind,
		ProcessGuardAttached:      record.processGuardAttached,
		ProcessGuardError:         record.processGuardError,
	}
	if record.runtime != nil {
		applyProcessRuntimeDiagnosticsToRecord(record, record.runtime)
		status.PID = record.pid
		status.ProcessGuardKind = record.processGuardKind
		status.ProcessGuardAttached = record.processGuardAttached
		status.ProcessGuardError = record.processGuardError
		status.StderrTail = record.runtime.StderrTail()
	}
	s.mu.Unlock()
	if status.PythonExecutablePath == "" && status.RuntimeKind != "" {
		applyPythonDiagnostics(&status, s.pythonDiagnosticsForKind(status.RuntimeKind))
	}
	return status
}

func (s *RuntimeSupervisor) Tools(pluginID string) []ProcessToolSpec {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.runtimes[pluginID]
	if record == nil {
		return nil
	}
	return append([]ProcessToolSpec(nil), record.tools...)
}

func (s *RuntimeSupervisor) recordRuntimeError(pluginID string, err error) {
	runtimeKind, diag := s.diagnosticsForPlugin(pluginID)
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.runtimes[pluginID]
	if record == nil {
		record = &supervisedRuntime{status: "failed"}
		s.runtimes[pluginID] = record
	}
	record.status = "failed"
	record.lastError = err.Error()
	if record.runtime != nil {
		applyProcessRuntimeDiagnosticsToRecord(record, record.runtime)
	}
	if runtimeKind != "" {
		record.runtimeKind = runtimeKind
	}
	if diag.path != "" || diag.source != "" || diag.available != nil {
		record.pythonExecutablePath = diag.path
		record.pythonExecutableSource = diag.source
		record.pythonExecutableAvailable = cloneBoolPtr(diag.available)
	}
}

func (s *RuntimeSupervisor) diagnosticsForPlugin(pluginID string) (RuntimeKind, pythonExecutableDiagnostics) {
	if s == nil {
		return "", pythonExecutableDiagnostics{}
	}
	s.mu.Lock()
	record := s.runtimes[pluginID]
	manifest, ok := s.plugins[pluginID]
	runtimeKind := RuntimeKind("")
	diag := pythonExecutableDiagnostics{}
	if record != nil {
		runtimeKind = firstRuntimeKind(record.runtimeKind, record.manifest.Runtime.Kind)
		diag = pythonExecutableDiagnostics{
			path:      record.pythonExecutablePath,
			source:    record.pythonExecutableSource,
			available: cloneBoolPtr(record.pythonExecutableAvailable),
		}
	}
	if runtimeKind == "" && ok {
		runtimeKind = manifest.Runtime.Kind
	}
	s.mu.Unlock()
	if (diag.path != "" || diag.source != "" || diag.available != nil) || runtimeKind == "" {
		return runtimeKind, diag
	}
	return runtimeKind, s.pythonDiagnosticsForKind(runtimeKind)
}

func (s *RuntimeSupervisor) pythonDiagnosticsForKind(kind RuntimeKind) pythonExecutableDiagnostics {
	diag, _ := s.resolvePythonExecutable(kind)
	return diag
}

func (s *RuntimeSupervisor) dependencyEnvDirForManifest(manifest ManifestV2) string {
	if s == nil || s.store == nil {
		return ""
	}
	if manifest.Runtime.Kind != RuntimeManagedPythonProcess {
		return ""
	}
	dir, err := s.store.DependencyEnvDir(manifest.ID, manifest.Version)
	if err != nil {
		return ""
	}
	return dir
}

func applyPythonDiagnostics(status *RuntimeStatus, diag pythonExecutableDiagnostics) {
	if status == nil {
		return
	}
	status.PythonExecutablePath = diag.path
	status.PythonExecutableSource = diag.source
	status.PythonExecutableAvailable = cloneBoolPtr(diag.available)
}

func firstRuntimeKind(values ...RuntimeKind) RuntimeKind {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func boolPtr(value bool) *bool {
	return &value
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func applyProcessRuntimeDiagnosticsToRecord(record *supervisedRuntime, runtime *ProcessRuntime) {
	if record == nil || runtime == nil {
		return
	}
	record.pid = runtime.PID()
	snapshot := runtime.ProcessGuardSnapshot()
	record.processGuardKind = snapshot.Kind
	record.processGuardAttached = snapshot.Attached
	record.processGuardError = snapshot.Error
}
