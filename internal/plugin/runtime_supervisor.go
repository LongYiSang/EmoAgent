package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/tool"
)

type RuntimeStatus struct {
	PluginID     string `json:"plugin_id"`
	Status       string `json:"status"`
	LastError    string `json:"last_error,omitempty"`
	RestartCount int    `json:"restart_count"`
	StderrTail   string `json:"stderr_tail,omitempty"`
}

type RuntimeSupervisor struct {
	store             *PluginStore
	cfg               config.PluginRuntimeConfig
	hostHandler       JSONRPCHandler
	hostHandlerFor    func(string) JSONRPCHandler
	enabled           func(context.Context, string) bool
	blockedEnvNames   []string
	additionalEnvVars []string

	mu       sync.Mutex
	plugins  map[string]ManifestV2
	runtimes map[string]*supervisedRuntime
}

type supervisedRuntime struct {
	manifest     ManifestV2
	runtime      *ProcessRuntime
	tools        []ProcessToolSpec
	status       string
	lastError    string
	restartCount int
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
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
	Scope       tool.Scope      `json:"scope"`
	Permission  tool.Permission `json:"permission"`
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
		s.blockedEnvNames = append([]string(nil), values...)
	}
}

func (s *RuntimeSupervisor) SetAdditionalEnvVars(values []string) {
	if s != nil {
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

func (s *RuntimeSupervisor) EnsureReady(ctx context.Context, pluginID string) (*ProcessRuntime, error) {
	if s == nil {
		return nil, fmt.Errorf("runtime supervisor is nil")
	}
	s.mu.Lock()
	existing := s.runtimes[pluginID]
	if existing != nil && existing.runtime != nil && existing.status == "running" {
		runtime := existing.runtime
		s.mu.Unlock()
		return runtime, nil
	}
	manifest, ok := s.plugins[pluginID]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("plugin %q is not registered with supervisor", pluginID)
	}
	if s.enabled != nil && !s.enabled(ctx, pluginID) {
		return nil, fmt.Errorf("plugin %q is disabled", pluginID)
	}
	if !s.cfg.ProcessEnabled {
		return nil, fmt.Errorf("plugin process runtime is disabled")
	}
	if manifest.Runtime.Kind != RuntimePythonProcess && manifest.Runtime.Kind != RuntimeProcess {
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
	python := s.cfg.PythonExecutable
	if python == "" {
		python = "python3"
	}
	handler := s.hostHandler
	if s.hostHandlerFor != nil {
		handler = s.hostHandlerFor(manifest.ID)
	}
	runtime, err := StartProcessRuntime(ctx, ProcessLaunchConfig{
		PluginID:          manifest.ID,
		Version:           manifest.Version,
		WorkDir:           packageDir,
		Entry:             manifest.Runtime.Entry,
		PythonExecutable:  python,
		StateDir:          stateDir,
		CacheDir:          cacheDir,
		RunDir:            runDir,
		StartupTimeout:    time.Duration(s.cfg.StartupTimeoutMS) * time.Millisecond,
		ShutdownTimeout:   time.Duration(s.cfg.ShutdownTimeoutMS) * time.Millisecond,
		MaxStderrBytes:    s.cfg.MaxStderrBytes,
		BlockedEnvNames:   append([]string(nil), s.blockedEnvNames...),
		AdditionalEnvVars: append([]string(nil), s.additionalEnvVars...),
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
		manifest:     manifest,
		runtime:      runtime,
		tools:        initResp.Tools,
		status:       "running",
		restartCount: restarts,
	}
	return runtime, nil
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
	defer s.mu.Unlock()
	record := s.runtimes[pluginID]
	if record == nil {
		return RuntimeStatus{PluginID: pluginID, Status: "stopped"}
	}
	status := RuntimeStatus{
		PluginID:     pluginID,
		Status:       record.status,
		LastError:    record.lastError,
		RestartCount: record.restartCount,
	}
	if record.runtime != nil {
		status.StderrTail = record.runtime.StderrTail()
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
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.runtimes[pluginID]
	if record == nil {
		record = &supervisedRuntime{status: "failed"}
		s.runtimes[pluginID] = record
	}
	record.status = "failed"
	record.lastError = err.Error()
}
