package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	LastUsedAt                string      `json:"last_used_at,omitempty"`
	InFlight                  int         `json:"in_flight,omitempty"`
	ConsecutiveFailures       int         `json:"consecutive_failures,omitempty"`
	NextStartAt               string      `json:"next_start_at,omitempty"`
	StableSince               string      `json:"stable_since,omitempty"`
	StderrTail                string      `json:"stderr_tail,omitempty"`
	PythonExecutablePath      string      `json:"python_executable_path,omitempty"`
	PythonExecutableSource    string      `json:"python_executable_source,omitempty"`
	PythonExecutableAvailable *bool       `json:"python_executable_available,omitempty"`
	PythonEnvironmentDir      string      `json:"python_environment_dir,omitempty"`
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
	managedEnv        func(context.Context, ManifestV2) (ManagedPythonEnvironment, error)
	blockedEnvNames   []string
	blockedEnvNamesFn func() []string
	additionalEnvVars []string

	mu             sync.Mutex
	nextGeneration uint64
	plugins        map[string]ManifestV2
	runtimes       map[string]*runtimeSlot
	now            func() time.Time
	reaperCancel   context.CancelFunc
	reaperDone     chan struct{}
	reaperStopOnce sync.Once
}

type RuntimeState string

const (
	RuntimeStateStopped     RuntimeState = "stopped"
	RuntimeStateStarting    RuntimeState = "starting"
	RuntimeStateRunning     RuntimeState = "running"
	RuntimeStateFailed      RuntimeState = "failed"
	RuntimeStateBackoff     RuntimeState = "backoff"
	RuntimeStateQuarantined RuntimeState = "quarantined"
)

type runtimeSlot struct {
	manifest                  ManifestV2
	state                     RuntimeState
	runtime                   *ProcessRuntime
	tools                     []ProcessToolSpec
	ready                     chan struct{}
	generation                uint64
	startErr                  error
	lastError                 string
	restartCount              int
	lastUsedAt                time.Time
	inFlight                  int
	consecutiveFailures       int
	nextStartAt               time.Time
	stableSince               time.Time
	runtimeKind               RuntimeKind
	pythonExecutablePath      string
	pythonExecutableSource    string
	pythonExecutableAvailable *bool
	pythonEnvironmentDir      string
	dependencyEnvDir          string
	pid                       int
	processGuardKind          string
	processGuardAttached      bool
	processGuardError         string
}

type runtimeStartResult struct {
	runtime                   *ProcessRuntime
	tools                     []ProcessToolSpec
	runtimeKind               RuntimeKind
	pythonExecutablePath      string
	pythonExecutableSource    string
	pythonExecutableAvailable *bool
	pythonEnvironmentDir      string
	dependencyEnvDir          string
}

const (
	PythonExecutableSourceToolchainUV = "python_toolchain_uv_environment"
	PythonExecutableSourceLegacy      = "legacy_python_executable"
)

type ManagedPythonEnvironment struct {
	PythonExecutable string
	EnvironmentDir   string
}

const (
	runtimeReaperInterval      = 30 * time.Second
	runtimeStableResetDuration = 30 * time.Second
)

type RuntimeBackoffError struct {
	PluginID    string
	NextStartAt time.Time
	Err         error
}

func (e *RuntimeBackoffError) Error() string {
	if e == nil {
		return ""
	}
	message := fmt.Sprintf("plugin %q runtime is in backoff until %s", e.PluginID, e.NextStartAt.UTC().Format(time.RFC3339Nano))
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	return message
}

func (e *RuntimeBackoffError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

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

type CommandInvokeResult struct {
	Status     string         `json:"status,omitempty"`
	Content    string         `json:"content"`
	Payload    map[string]any `json:"payload,omitempty"`
	OutputMode string         `json:"output_mode,omitempty"`
}

type CommandInvokeContext struct {
	OriginKey  string `json:"origin_key"`
	SessionID  string `json:"session_id"`
	PersonaKey string `json:"persona_key"`
	ActorRole  string `json:"actor_role"`
}

type CommandInvokeRequest struct {
	CommandID string               `json:"command_id"`
	Handler   string               `json:"handler"`
	Args      []string             `json:"args"`
	Flags     map[string]string    `json:"flags"`
	Context   CommandInvokeContext `json:"context"`
}

type ProcessToolSpec struct {
	Name             string                 `json:"name"`
	Description      string                 `json:"description"`
	Parameters       json.RawMessage        `json:"parameters"`
	Scope            tool.Scope             `json:"scope"`
	Permission       tool.Permission        `json:"permission"`
	InvocationPolicy InvocationPolicy       `json:"invocation,omitempty"`
	Trust            resultv2.ContentLabels `json:"trust,omitempty"`
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
	supervisor := &RuntimeSupervisor{
		store:       store,
		cfg:         cfg,
		hostHandler: handler,
		plugins:     map[string]ManifestV2{},
		runtimes:    map[string]*runtimeSlot{},
		now:         time.Now,
	}
	supervisor.startIdleReaper()
	return supervisor
}

func (s *RuntimeSupervisor) SetEnabledChecker(checker func(context.Context, string) bool) {
	if s != nil {
		s.enabled = checker
	}
}

func (s *RuntimeSupervisor) SetManagedPythonEnvironmentResolver(resolver func(context.Context, ManifestV2) (ManagedPythonEnvironment, error)) {
	if s != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.managedEnv = resolver
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

func (s *RuntimeSupervisor) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *RuntimeSupervisor) startIdleReaper() {
	if s == nil || s.cfg.IdleTimeoutSeconds <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.reaperCancel = cancel
	s.reaperDone = make(chan struct{})
	go func() {
		defer close(s.reaperDone)
		s.runIdleReaper(ctx)
	}()
}

func (s *RuntimeSupervisor) stopIdleReaper() {
	if s == nil {
		return
	}
	s.reaperStopOnce.Do(func() {
		if s.reaperCancel != nil {
			s.reaperCancel()
		}
		if s.reaperDone != nil {
			<-s.reaperDone
		}
	})
}

func (s *RuntimeSupervisor) runIdleReaper(ctx context.Context) {
	ticker := time.NewTicker(runtimeReaperInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reapIdleRuntimes(ctx)
		}
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

	for {
		s.mu.Lock()
		now := s.currentTime()
		slot := s.runtimes[pluginID]
		manifest, ok := s.plugins[pluginID]
		if slot != nil {
			switch slot.state {
			case RuntimeStateRunning:
				if !ok {
					runtime := slot.runtime
					delete(s.runtimes, pluginID)
					s.mu.Unlock()
					_ = runtime.Stop(context.Background())
					return nil, fmt.Errorf("plugin %q is not registered with supervisor", pluginID)
				}
				if !sameManifestVersion(manifest, slot.manifest) {
					runtime := slot.runtime
					delete(s.runtimes, pluginID)
					s.mu.Unlock()
					if err := runtime.Stop(context.Background()); err != nil {
						s.recordRuntimeError(pluginID, err)
						return nil, err
					}
					continue
				}
				s.resetFailuresIfStableLocked(slot, now)
				slot.lastUsedAt = now
				runtime := slot.runtime
				s.mu.Unlock()
				return runtime, nil
			case RuntimeStateStarting:
				if !ok {
					err := fmt.Errorf("plugin %q is not registered with supervisor", pluginID)
					s.supersedeStartingSlotLocked(slot, err)
					delete(s.runtimes, pluginID)
					s.mu.Unlock()
					return nil, err
				}
				if !sameManifestVersion(manifest, slot.manifest) {
					s.supersedeStartingSlotLocked(slot, fmt.Errorf("plugin %q runtime start was superseded", pluginID))
					delete(s.runtimes, pluginID)
					s.mu.Unlock()
					continue
				}
				ready := slot.ready
				s.mu.Unlock()
				return s.waitForRuntimeSlot(ctx, pluginID, slot, ready)
			case RuntimeStateBackoff:
				if !ok {
					err := fmt.Errorf("plugin %q is not registered with supervisor", pluginID)
					delete(s.runtimes, pluginID)
					s.mu.Unlock()
					return nil, err
				}
				if !sameManifestVersion(manifest, slot.manifest) {
					delete(s.runtimes, pluginID)
					s.mu.Unlock()
					continue
				}
				if !slot.nextStartAt.IsZero() && now.Before(slot.nextStartAt) {
					err := &RuntimeBackoffError{PluginID: pluginID, NextStartAt: slot.nextStartAt, Err: slot.startErr}
					s.mu.Unlock()
					return nil, err
				}
			case RuntimeStateQuarantined:
				if ok && !sameManifestVersion(manifest, slot.manifest) {
					delete(s.runtimes, pluginID)
					s.mu.Unlock()
					continue
				}
				err := fmt.Errorf("plugin %q runtime is quarantined", pluginID)
				s.mu.Unlock()
				return nil, err
			}
		}
		if !ok {
			s.mu.Unlock()
			return nil, fmt.Errorf("plugin %q is not registered with supervisor", pluginID)
		}
		if !s.cfg.ProcessEnabled {
			s.mu.Unlock()
			return nil, fmt.Errorf("plugin process runtime is disabled")
		}
		if !launchableRuntimeKind(manifest.Runtime.Kind) {
			s.mu.Unlock()
			return nil, fmt.Errorf("plugin runtime kind %q cannot be launched as process", manifest.Runtime.Kind)
		}
		generation := s.nextRuntimeGenerationLocked()
		restarts := 0
		consecutiveFailures := 0
		tools := []ProcessToolSpec(nil)
		if slot != nil {
			restarts = slot.restartCount + 1
			consecutiveFailures = slot.consecutiveFailures
			tools = append([]ProcessToolSpec(nil), slot.tools...)
		}
		starting := &runtimeSlot{
			manifest:            manifest,
			state:               RuntimeStateStarting,
			tools:               tools,
			ready:               make(chan struct{}),
			generation:          generation,
			restartCount:        restarts,
			consecutiveFailures: consecutiveFailures,
			runtimeKind:         manifest.Runtime.Kind,
		}
		s.runtimes[pluginID] = starting
		s.mu.Unlock()

		result, err := s.startRuntime(ctx, manifest, generation)
		return s.completeRuntimeStart(pluginID, starting, generation, result, err)
	}
}

func (s *RuntimeSupervisor) waitForRuntimeSlot(ctx context.Context, pluginID string, slot *runtimeSlot, ready chan struct{}) (*ProcessRuntime, error) {
	if ready == nil {
		return nil, fmt.Errorf("plugin %q runtime is not ready", pluginID)
	}
	select {
	case <-ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if slot.startErr != nil {
		return nil, slot.startErr
	}
	if slot.state == RuntimeStateRunning && slot.runtime != nil {
		return slot.runtime, nil
	}
	return nil, fmt.Errorf("plugin %q runtime is not ready", pluginID)
}

func (s *RuntimeSupervisor) completeRuntimeStart(pluginID string, slot *runtimeSlot, generation uint64, result *runtimeStartResult, startErr error) (*ProcessRuntime, error) {
	var diag pythonExecutableDiagnostics
	if startErr != nil {
		diag = s.pythonDiagnosticsForKind(slot.manifest.Runtime.Kind)
	}
	s.mu.Lock()
	now := s.currentTime()
	current := s.runtimes[pluginID]
	if current != slot || slot.generation != generation || slot.state != RuntimeStateStarting {
		err := slot.startErr
		if err == nil {
			err = fmt.Errorf("plugin %q runtime start was superseded", pluginID)
			slot.startErr = err
			slot.lastError = err.Error()
		}
		closeRuntimeSlotReadyLocked(slot)
		s.mu.Unlock()
		if result != nil && result.runtime != nil {
			_ = result.runtime.Stop(context.Background())
		}
		return nil, err
	}
	if startErr != nil {
		slot.startErr = startErr
		slot.lastError = startErr.Error()
		slot.runtimeKind = slot.manifest.Runtime.Kind
		slot.pythonExecutablePath = diag.path
		slot.pythonExecutableSource = diag.source
		slot.pythonExecutableAvailable = cloneBoolPtr(diag.available)
		s.markRuntimeFailureLocked(slot, startErr, now)
		closeRuntimeSlotReadyLocked(slot)
		s.mu.Unlock()
		return nil, startErr
	}
	if result == nil || result.runtime == nil {
		err := fmt.Errorf("plugin %q runtime start returned no runtime", pluginID)
		slot.startErr = err
		slot.lastError = err.Error()
		s.markRuntimeFailureLocked(slot, err, now)
		closeRuntimeSlotReadyLocked(slot)
		s.mu.Unlock()
		return nil, err
	}
	slot.runtime = result.runtime
	slot.tools = result.tools
	slot.state = RuntimeStateRunning
	slot.startErr = nil
	slot.lastError = ""
	slot.nextStartAt = time.Time{}
	slot.lastUsedAt = now
	slot.stableSince = now
	slot.runtimeKind = result.runtimeKind
	slot.pythonExecutablePath = result.pythonExecutablePath
	slot.pythonExecutableSource = result.pythonExecutableSource
	slot.pythonExecutableAvailable = cloneBoolPtr(result.pythonExecutableAvailable)
	slot.pythonEnvironmentDir = result.pythonEnvironmentDir
	slot.dependencyEnvDir = result.dependencyEnvDir
	applyProcessRuntimeDiagnosticsToRecord(slot, result.runtime)
	closeRuntimeSlotReadyLocked(slot)
	s.mu.Unlock()
	return result.runtime, nil
}

func (s *RuntimeSupervisor) markRuntimeFailureLocked(slot *runtimeSlot, err error, now time.Time) {
	if slot == nil {
		return
	}
	if err == nil {
		err = fmt.Errorf("plugin runtime failed")
	}
	s.resetFailuresIfStableLocked(slot, now)
	slot.consecutiveFailures++
	slot.startErr = err
	slot.lastError = err.Error()
	slot.nextStartAt = time.Time{}
	if threshold := s.crashQuarantineThreshold(); threshold > 0 && slot.consecutiveFailures >= threshold {
		slot.state = RuntimeStateQuarantined
		return
	}
	backoff := s.crashBackoffDuration(slot.consecutiveFailures)
	if backoff <= 0 {
		slot.state = RuntimeStateFailed
		return
	}
	slot.state = RuntimeStateBackoff
	slot.nextStartAt = now.Add(backoff)
}

func (s *RuntimeSupervisor) resetFailuresIfStableLocked(slot *runtimeSlot, now time.Time) {
	if slot == nil || slot.state != RuntimeStateRunning || slot.consecutiveFailures == 0 || slot.stableSince.IsZero() {
		return
	}
	if now.Sub(slot.stableSince) >= runtimeStableResetDuration {
		slot.consecutiveFailures = 0
	}
}

func (s *RuntimeSupervisor) crashBackoffDuration(failures int) time.Duration {
	if failures <= 0 || s == nil || s.cfg.CrashBackoffInitialSeconds <= 0 {
		return 0
	}
	initial := time.Duration(s.cfg.CrashBackoffInitialSeconds) * time.Second
	maximum := time.Duration(s.cfg.CrashBackoffMaxSeconds) * time.Second
	if maximum < initial {
		maximum = initial
	}
	backoff := initial
	for i := 1; i < failures; i++ {
		if backoff >= maximum/2 {
			backoff = maximum
			break
		}
		backoff *= 2
	}
	if backoff > maximum {
		return maximum
	}
	return backoff
}

func (s *RuntimeSupervisor) crashQuarantineThreshold() int {
	if s == nil {
		return 0
	}
	return s.cfg.CrashQuarantineThreshold
}

func (s *RuntimeSupervisor) nextRuntimeGenerationLocked() uint64 {
	s.nextGeneration++
	return s.nextGeneration
}

func (s *RuntimeSupervisor) supersedeStartingSlotLocked(slot *runtimeSlot, err error) {
	if slot == nil || slot.state != RuntimeStateStarting {
		return
	}
	slot.state = RuntimeStateFailed
	slot.startErr = err
	slot.lastError = err.Error()
	closeRuntimeSlotReadyLocked(slot)
}

func closeRuntimeSlotReadyLocked(slot *runtimeSlot) {
	if slot == nil || slot.ready == nil {
		return
	}
	close(slot.ready)
	slot.ready = nil
}

func sameManifestVersion(current ManifestV2, slot ManifestV2) bool {
	return current.Version == "" || slot.Version == "" || current.Version == slot.Version
}

func launchableRuntimeKind(kind RuntimeKind) bool {
	return kind == RuntimeManagedPythonProcess || kind == RuntimePythonProcess || kind == RuntimeProcess
}

func (s *RuntimeSupervisor) startRuntime(ctx context.Context, manifest ManifestV2, generation uint64) (*runtimeStartResult, error) {
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
	pythonEnvironmentDir := ""
	var pythonDiag pythonExecutableDiagnostics
	if manifest.Runtime.Kind == RuntimeManagedPythonProcess {
		envResolver := s.managedEnvironmentResolver()
		if envResolver != nil {
			env, err := envResolver(ctx, manifest)
			if err != nil {
				return nil, err
			}
			pythonDiag = pythonExecutableDiagnostics{path: strings.TrimSpace(env.PythonExecutable), source: PythonExecutableSourceToolchainUV, available: boolPtr(false)}
			if !filepath.IsAbs(pythonDiag.path) {
				return nil, fmt.Errorf("managed python environment executable must be an absolute path")
			}
			if _, err := os.Stat(pythonDiag.path); err != nil {
				return nil, fmt.Errorf("managed python environment unavailable at %q: %w", pythonDiag.path, err)
			}
			pythonDiag.available = boolPtr(true)
			pythonEnvironmentDir = strings.TrimSpace(env.EnvironmentDir)
		} else {
			return nil, fmt.Errorf("managed python runtime requires a Python Toolchain uv environment resolver")
		}
	} else {
		var err error
		pythonDiag, err = s.resolvePythonExecutable(manifest.Runtime.Kind)
		if err != nil {
			return nil, err
		}
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
			s.recordRuntimeErrorForGeneration(manifest.ID, generation, fmt.Errorf("plugin protocol error: %w", err))
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
	return &runtimeStartResult{
		runtime:                   runtime,
		tools:                     initResp.Tools,
		runtimeKind:               manifest.Runtime.Kind,
		pythonExecutablePath:      pythonDiag.path,
		pythonExecutableSource:    pythonDiag.source,
		pythonExecutableAvailable: cloneBoolPtr(pythonDiag.available),
		pythonEnvironmentDir:      pythonEnvironmentDir,
		dependencyEnvDir:          dependencyEnvDir,
	}, nil
}

func (s *RuntimeSupervisor) managedEnvironmentResolver() func(context.Context, ManifestV2) (ManagedPythonEnvironment, error) {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.managedEnv
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
		return pythonExecutableDiagnostics{source: PythonExecutableSourceToolchainUV, available: boolPtr(false)}, fmt.Errorf("managed python runtime requires a Python Toolchain uv environment resolver")
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

func (s *RuntimeSupervisor) InvokeHook(ctx context.Context, pluginID string, hook HookName, hc HookContext) (HookResult, error) {
	runtime, err := s.EnsureReady(ctx, pluginID)
	if err != nil {
		return HookResult{}, err
	}
	generation, err := s.beginRuntimeUse(pluginID, runtime)
	if err != nil {
		return HookResult{}, err
	}
	var result HookResult
	if err := runtime.Call(ctx, "invoke_hook", hookInvokeRequest{Hook: hook, Context: hc}, &result); err != nil {
		s.finishRuntimeUse(pluginID, runtime, generation, err)
		return HookResult{}, err
	}
	s.finishRuntimeUse(pluginID, runtime, generation, nil)
	return result, nil
}

func (s *RuntimeSupervisor) InvokeTool(ctx context.Context, pluginID string, name string, input json.RawMessage) (json.RawMessage, error) {
	runtime, err := s.EnsureReady(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	generation, err := s.beginRuntimeUse(pluginID, runtime)
	if err != nil {
		return nil, err
	}
	var result json.RawMessage
	if err := runtime.Call(ctx, "invoke_tool", toolInvokeRequest{Tool: name, Input: input}, &result); err != nil {
		s.finishRuntimeUse(pluginID, runtime, generation, err)
		return nil, err
	}
	s.finishRuntimeUse(pluginID, runtime, generation, nil)
	return result, nil
}

func (s *RuntimeSupervisor) InvokeCommand(ctx context.Context, pluginID string, req CommandInvokeRequest) (CommandInvokeResult, error) {
	runtime, err := s.EnsureReady(ctx, pluginID)
	if err != nil {
		return CommandInvokeResult{}, err
	}
	generation, err := s.beginRuntimeUse(pluginID, runtime)
	if err != nil {
		return CommandInvokeResult{}, err
	}
	var result CommandInvokeResult
	if err := runtime.Call(ctx, "invoke_command", req, &result); err != nil {
		s.finishRuntimeUse(pluginID, runtime, generation, err)
		return CommandInvokeResult{}, err
	}
	s.finishRuntimeUse(pluginID, runtime, generation, nil)
	return result, nil
}

func (s *RuntimeSupervisor) beginRuntimeUse(pluginID string, runtime *ProcessRuntime) (uint64, error) {
	if s == nil || runtime == nil {
		return 0, fmt.Errorf("plugin %q runtime is not ready", pluginID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	slot := s.runtimes[pluginID]
	if slot == nil || slot.state != RuntimeStateRunning || slot.runtime != runtime {
		return 0, fmt.Errorf("plugin %q runtime is not ready", pluginID)
	}
	slot.inFlight++
	slot.lastUsedAt = s.currentTime()
	return slot.generation, nil
}

func (s *RuntimeSupervisor) finishRuntimeUse(pluginID string, runtime *ProcessRuntime, generation uint64, callErr error) {
	if s == nil || runtime == nil {
		return
	}
	var runtimeToStop *ProcessRuntime
	s.mu.Lock()
	now := s.currentTime()
	slot := s.runtimes[pluginID]
	if slot != nil && slot.generation == generation && slot.runtime == runtime {
		if slot.inFlight > 0 {
			slot.inFlight--
		}
		slot.lastUsedAt = now
		if callErr != nil {
			runtimeToStop = slot.runtime
			slot.runtime = nil
			slot.pid = 0
			slot.processGuardAttached = false
			slot.processGuardError = ""
			s.markRuntimeFailureLocked(slot, callErr, now)
		} else {
			s.resetFailuresIfStableLocked(slot, now)
		}
	}
	s.mu.Unlock()
	if runtimeToStop != nil {
		_ = runtimeToStop.Stop(context.Background())
	}
}

func (s *RuntimeSupervisor) reapIdleRuntimes(ctx context.Context) {
	if s == nil || s.cfg.IdleTimeoutSeconds <= 0 {
		return
	}
	idleTimeout := time.Duration(s.cfg.IdleTimeoutSeconds) * time.Second
	now := s.currentTime()
	type idleRuntime struct {
		runtime *ProcessRuntime
	}
	var idle []idleRuntime
	s.mu.Lock()
	for _, slot := range s.runtimes {
		if slot == nil || slot.state != RuntimeStateRunning || slot.runtime == nil {
			continue
		}
		s.resetFailuresIfStableLocked(slot, now)
		if slot.inFlight > 0 {
			continue
		}
		if slot.lastUsedAt.IsZero() {
			slot.lastUsedAt = now
			continue
		}
		if now.Sub(slot.lastUsedAt) < idleTimeout {
			continue
		}
		idle = append(idle, idleRuntime{runtime: slot.runtime})
		slot.runtime = nil
		slot.pid = 0
		slot.processGuardAttached = false
		slot.processGuardError = ""
		slot.state = RuntimeStateStopped
	}
	s.mu.Unlock()
	for _, item := range idle {
		if item.runtime != nil {
			_ = item.runtime.Stop(ctx)
		}
	}
}

func (s *RuntimeSupervisor) Stop(ctx context.Context, pluginID string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	record := s.runtimes[pluginID]
	delete(s.runtimes, pluginID)
	if record != nil && record.state == RuntimeStateStarting {
		record.startErr = fmt.Errorf("plugin %q runtime start was stopped", pluginID)
		record.lastError = record.startErr.Error()
		record.state = RuntimeStateFailed
		closeRuntimeSlotReadyLocked(record)
	}
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
	s.stopIdleReaper()
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
	s.resetFailuresIfStableLocked(record, s.currentTime())
	status := RuntimeStatus{
		PluginID:                  pluginID,
		Version:                   record.manifest.Version,
		RuntimeKind:               firstRuntimeKind(record.runtimeKind, record.manifest.Runtime.Kind),
		Status:                    string(record.state),
		LastError:                 record.lastError,
		RestartCount:              record.restartCount,
		LastUsedAt:                formatRuntimeTime(record.lastUsedAt),
		InFlight:                  record.inFlight,
		ConsecutiveFailures:       record.consecutiveFailures,
		NextStartAt:               formatRuntimeTime(record.nextStartAt),
		StableSince:               formatRuntimeTime(record.stableSince),
		PythonExecutablePath:      record.pythonExecutablePath,
		PythonExecutableSource:    record.pythonExecutableSource,
		PythonExecutableAvailable: cloneBoolPtr(record.pythonExecutableAvailable),
		PythonEnvironmentDir:      record.pythonEnvironmentDir,
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
	s.recordRuntimeErrorForGeneration(pluginID, 0, err)
}

func (s *RuntimeSupervisor) recordRuntimeErrorForGeneration(pluginID string, generation uint64, err error) {
	var runtimeToStop *ProcessRuntime
	s.mu.Lock()
	record := s.runtimes[pluginID]
	if generation != 0 && (record == nil || record.generation != generation) {
		s.mu.Unlock()
		return
	}
	if record == nil {
		record = &runtimeSlot{state: RuntimeStateFailed}
		s.runtimes[pluginID] = record
	}
	if record.state != RuntimeStateRunning && record.state != RuntimeStateStarting {
		if err != nil {
			record.lastError = err.Error()
		}
		s.mu.Unlock()
		return
	}
	if record.runtime != nil {
		runtimeToStop = record.runtime
		applyProcessRuntimeDiagnosticsToRecord(record, record.runtime)
	}
	record.runtime = nil
	record.pid = 0
	record.processGuardAttached = false
	record.processGuardError = ""
	s.markRuntimeFailureLocked(record, err, s.currentTime())
	s.mu.Unlock()
	if runtimeToStop != nil {
		_ = runtimeToStop.Stop(context.Background())
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
	return ""
}

func applyPythonDiagnostics(status *RuntimeStatus, diag pythonExecutableDiagnostics) {
	if status == nil {
		return
	}
	status.PythonExecutablePath = diag.path
	status.PythonExecutableSource = diag.source
	status.PythonExecutableAvailable = cloneBoolPtr(diag.available)
}

func formatRuntimeTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
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

func applyProcessRuntimeDiagnosticsToRecord(record *runtimeSlot, runtime *ProcessRuntime) {
	if record == nil || runtime == nil {
		return
	}
	record.pid = runtime.PID()
	snapshot := runtime.ProcessGuardSnapshot()
	record.processGuardKind = snapshot.Kind
	record.processGuardAttached = snapshot.Attached
	record.processGuardError = snapshot.Error
}
