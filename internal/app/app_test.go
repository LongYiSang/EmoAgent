package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/apperrors"
	"github.com/longyisang/emoagent/internal/chat"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/configcenter"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/plugin"
	"github.com/longyisang/emoagent/internal/protocol"
	sidecarruntime "github.com/longyisang/emoagent/internal/sidecar"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/web"
)

type routeTestAdminApp struct {
	providers    []config.LLMProvider
	agentConfigs []config.AgentConfig
	activeAgent  *config.AgentConfig
	lastActive   string
}

func (a *routeTestAdminApp) ListLLMProviders() ([]config.LLMProvider, error) {
	return append([]config.LLMProvider(nil), a.providers...), nil
}
func (a *routeTestAdminApp) GetLLMProvider(id string) (*config.LLMProvider, error) {
	for i := range a.providers {
		if a.providers[i].ID == id {
			cp := a.providers[i]
			return &cp, nil
		}
	}
	return nil, ErrLLMProviderNotFound
}
func (a *routeTestAdminApp) CreateLLMProvider(provider config.LLMProvider) error { return nil }
func (a *routeTestAdminApp) UpdateLLMProvider(id string, provider config.LLMProvider) error {
	return nil
}
func (a *routeTestAdminApp) DeleteLLMProvider(id string) error { return nil }
func (a *routeTestAdminApp) RefreshLLMProviderModels(id string) ([]llm.ModelInfo, error) {
	return []llm.ModelInfo{}, nil
}
func (a *routeTestAdminApp) GetLLMProviderModels(id string) ([]llm.ModelInfo, error) {
	return []llm.ModelInfo{}, nil
}
func (a *routeTestAdminApp) GetLLMProviderEnvStatus(id string) (configcenter.ProviderEnvStatus, error) {
	return configcenter.ProviderEnvStatus{}, nil
}
func (a *routeTestAdminApp) ListAgentConfigs() ([]config.AgentConfig, error) {
	return append([]config.AgentConfig(nil), a.agentConfigs...), nil
}
func (a *routeTestAdminApp) GetAgentConfig(id string) (*config.AgentConfig, error) {
	for i := range a.agentConfigs {
		if a.agentConfigs[i].ID == id {
			cp := a.agentConfigs[i]
			return &cp, nil
		}
	}
	return nil, ErrAgentConfigNotFound
}
func (a *routeTestAdminApp) GetActiveAgentConfig() (*config.AgentConfig, bool, error) {
	if a.activeAgent == nil {
		return nil, false, nil
	}
	cp := *a.activeAgent
	return &cp, true, nil
}
func (a *routeTestAdminApp) CreateAgentConfig(agent config.AgentConfig) error { return nil }
func (a *routeTestAdminApp) UpdateAgentConfig(id string, agent config.AgentConfig) error {
	return nil
}
func (a *routeTestAdminApp) ActivateAgentConfig(id string) error {
	a.lastActive = id
	return nil
}
func (a *routeTestAdminApp) DeleteAgentConfig(id string) error { return nil }
func (a *routeTestAdminApp) ListPersonas() map[string]*config.Persona {
	return map[string]*config.Persona{}
}
func (a *routeTestAdminApp) GetPersona(name string) (*config.Persona, bool) { return nil, false }
func (a *routeTestAdminApp) CreatePersona(key string, p *config.Persona) error {
	return nil
}
func (a *routeTestAdminApp) UpdatePersona(key string, p *config.Persona) error { return nil }
func (a *routeTestAdminApp) DeletePersona(key string) error                    { return nil }
func (a *routeTestAdminApp) GetProgressPhrases(key string) (map[string][]string, error) {
	return map[string][]string{}, nil
}
func (a *routeTestAdminApp) UpdateProgressPhrases(key string, phrases map[string][]string) error {
	return nil
}
func (a *routeTestAdminApp) ListSessions(ctx context.Context, persona string, limit int) ([]storage.SessionSummary, error) {
	return nil, nil
}
func (a *routeTestAdminApp) GetLatestSession(ctx context.Context, persona string) (*storage.SessionSummary, error) {
	return nil, nil
}
func (a *routeTestAdminApp) GetSessionDetail(ctx context.Context, id string) (*storage.SessionRecord, []storage.MessageRecord, error) {
	return nil, nil, nil
}
func (a *routeTestAdminApp) DeleteSession(ctx context.Context, id string) error {
	return nil
}
func (a *routeTestAdminApp) ListSessionApprovals(ctx context.Context, sessionID string) ([]protocol.ApprovalRequest, error) {
	return nil, nil
}
func (a *routeTestAdminApp) QueueMemoryExtraction(ctx context.Context, req web.MemoryExtractionRequest) (web.MemoryExtractionQueueResponse, error) {
	return web.MemoryExtractionQueueResponse{}, nil
}
func (a *routeTestAdminApp) ListMemoryExtractions(ctx context.Context, req web.MemoryExtractionListRequest) ([]storage.MemoryExtractionJob, error) {
	return nil, nil
}
func (a *routeTestAdminApp) ListMemorySegments(ctx context.Context, sessionID string) ([]storage.MemorySegment, error) {
	return nil, nil
}
func (a *routeTestAdminApp) GetChatSettings() config.ChatConfig {
	return config.ChatConfig{}
}
func (a *routeTestAdminApp) UpdateChatSettings(settings config.ChatConfig) error {
	return nil
}
func (a *routeTestAdminApp) GetEffectiveConfig(ctx context.Context) (configcenter.EffectiveConfig, error) {
	return configcenter.EffectiveConfig{}, nil
}
func (a *routeTestAdminApp) ValidateConfig(ctx context.Context, req configcenter.ValidateRequest) (configcenter.ValidateResponse, error) {
	return configcenter.ValidateResponse{}, nil
}
func (a *routeTestAdminApp) ListConfigIssues(ctx context.Context) ([]configcenter.ConfigIssue, error) {
	return nil, nil
}
func (a *routeTestAdminApp) GetMemoryConfig(ctx context.Context) (configcenter.MemoryConfigResponse, error) {
	return configcenter.MemoryConfigResponse{}, nil
}
func (a *routeTestAdminApp) UpdateMemoryConfig(ctx context.Context, memory config.MemoryConfig) (configcenter.EffectiveConfig, error) {
	return configcenter.EffectiveConfig{}, nil
}
func (a *routeTestAdminApp) GetMemoryFeatures(ctx context.Context) (configcenter.MemoryConfigResponse, error) {
	return configcenter.MemoryConfigResponse{}, nil
}
func (a *routeTestAdminApp) UpdateMemoryFeatures(ctx context.Context, memory config.MemoryConfig) (configcenter.EffectiveConfig, error) {
	return configcenter.EffectiveConfig{}, nil
}
func (a *routeTestAdminApp) GetSidecarStatus(ctx context.Context) (sidecarruntime.Status, error) {
	return sidecarruntime.Status{}, nil
}
func (a *routeTestAdminApp) StartSidecar(ctx context.Context) (sidecarruntime.Status, error) {
	return sidecarruntime.Status{}, nil
}
func (a *routeTestAdminApp) StopSidecar(ctx context.Context) (sidecarruntime.Status, error) {
	return sidecarruntime.Status{}, nil
}
func (a *routeTestAdminApp) RestartSidecar(ctx context.Context) (sidecarruntime.Status, error) {
	return sidecarruntime.Status{}, nil
}
func (a *routeTestAdminApp) GetSidecarGeneratedConfig(ctx context.Context) (string, error) {
	return "", nil
}
func (a *routeTestAdminApp) GetSidecarLogs(ctx context.Context, maxBytes int) (string, error) {
	return "", nil
}

func TestRunAllowsStartupWithoutLLM(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := &App{
		Config: &config.Config{
			Server: config.ServerConfig{Host: "127.0.0.1", Port: 0},
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	if err := a.Run(ctx); err != nil {
		t.Fatalf("Run() with canceled context should still shut down cleanly, got %v", err)
	}
}

func TestInitRejectsOldLLMProfileOnlyConfigWhenDBIsEmpty(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	dbPath := filepath.Join(dir, "emo.db")
	personaDir := filepath.Join(dir, "personas")
	if err := os.MkdirAll(personaDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(personaDir, "default.yaml"), []byte("name: Default\nsystem_prompt: test\n"), 0o644); err != nil {
		t.Fatalf("WriteFile persona: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`
server:
  host: "127.0.0.1"
  port: 8081
llm:
  provider: openai
  base_url: https://api.openai.com
  model: gpt-4o
  max_tokens: 128
  temperature: 0.7
llm_profiles:
  - name: default
    provider: openai
    base_url: https://api.openai.com
    model: gpt-4o
    max_tokens: 128
    temperature: 0.7
db:
  path: "`+filepath.ToSlash(dbPath)+`"
personas:
  dir: "`+filepath.ToSlash(personaDir)+`"
`), 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	a := New()
	err := a.Init(context.Background(), configPath)
	if err == nil {
		t.Fatal("Init succeeded with legacy llm_profiles-only config, want error")
	}
	if !strings.Contains(err.Error(), "llm_providers and agent_configs") {
		t.Fatalf("Init error = %v, want missing new seed message", err)
	}
	_ = a.Shutdown()
}

func TestInitMemoryDisabledDoesNotRequireMemoryCoreConfig(t *testing.T) {
	dir := t.TempDir()
	missingMemoryConfig := filepath.Join(dir, "missing-memorycore.yaml")
	memoryDBPath := filepath.Join(dir, "memory.db")
	configPath := writeAppInitConfig(t, dir, false, missingMemoryConfig)

	a := New()
	t.Cleanup(func() { _ = a.Shutdown() })
	if err := a.Init(context.Background(), configPath); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if a.Memory != nil {
		t.Fatalf("App.Memory = %#v, want nil when memory.enabled=false", a.Memory)
	}
	if _, err := os.Stat(memoryDBPath); !os.IsNotExist(err) {
		t.Fatalf("memory db stat error = %v, want not exist", err)
	}
}

func TestInitMemoryEnabledOpensMemoryCore(t *testing.T) {
	dir := t.TempDir()
	memoryDBPath := filepath.Join(dir, "memory.db")
	memoryConfigPath := writeAppMemoryCoreConfig(t, dir, true, true, memoryDBPath)
	configPath := writeAppInitConfig(t, dir, true, memoryConfigPath)

	a := New()
	t.Cleanup(func() { _ = a.Shutdown() })
	if err := a.Init(context.Background(), configPath); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if a.Memory == nil {
		t.Fatal("App.Memory = nil, want initialized host")
	}
	if _, err := os.Stat(memoryDBPath); err != nil {
		t.Fatalf("memory db was not created: %v", err)
	}
}

func TestInitMemoryEnabledUsesProviderCenterForMemoryCore(t *testing.T) {
	dir := t.TempDir()
	memoryDBPath := filepath.Join(dir, "memory.db")
	memoryConfigPath := writeAppMemoryCoreConfigWithPipeline(t, dir, memoryDBPath, "moonshot")
	configPath := writeAppInitConfig(t, dir, true, memoryConfigPath)

	a := New()
	t.Cleanup(func() { _ = a.Shutdown() })
	if err := a.Init(context.Background(), configPath); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if a.Memory == nil {
		t.Fatal("App.Memory = nil, want initialized host")
	}
	if _, err := os.Stat(memoryDBPath); err != nil {
		t.Fatalf("memory db was not created: %v", err)
	}
}

func TestInitMemoryEnabledMissingProviderEnvReturnsClearError(t *testing.T) {
	dir := t.TempDir()
	memoryDBPath := filepath.Join(dir, "memory.db")
	memoryConfigPath := writeAppMemoryCoreConfigWithPipeline(t, dir, memoryDBPath, "moonshot")
	configPath := writeAppInitConfig(t, dir, true, memoryConfigPath)
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile config: %v", err)
	}
	body = []byte(strings.ReplaceAll(string(body), "api_key_env: MOONSHOT_API_KEY", "api_key_env: MISSING_MOONSHOT_API_KEY"))
	if err := os.WriteFile(configPath, body, 0o644); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	a := New()
	t.Cleanup(func() { _ = a.Shutdown() })
	err = a.Init(context.Background(), configPath)
	if err == nil {
		t.Fatal("Init succeeded, want missing env error")
	}
	if !strings.Contains(err.Error(), "provider \"moonshot\" requires env MISSING_MOONSHOT_API_KEY") {
		t.Fatalf("Init error = %v, want missing provider env", err)
	}
}

func TestInitMemorySidecarExternalInjectsURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("path = %q, want /health", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	memoryDBPath := filepath.Join(dir, "memory.db")
	memoryConfigPath := writeAppMemoryCoreConfigWithSidecar(t, dir, memoryDBPath)
	configPath := writeAppInitConfigWithMemorySidecar(t, dir, true, memoryConfigPath, fmt.Sprintf(`
  sidecar:
    enabled: true
    managed: false
    url: %q
    fail_open: false
`, server.URL))

	a := New()
	t.Cleanup(func() { _ = a.Shutdown() })
	if err := a.Init(context.Background(), configPath); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if a.Memory == nil {
		t.Fatal("App.Memory = nil, want initialized host")
	}
}

func TestInitMemorySidecarFailOpenDisablesMirror(t *testing.T) {
	dir := t.TempDir()
	memoryDBPath := filepath.Join(dir, "memory.db")
	memoryConfigPath := writeAppMemoryCoreConfigWithSidecar(t, dir, memoryDBPath)
	configPath := writeAppInitConfigWithMemorySidecar(t, dir, true, memoryConfigPath, `
  sidecar:
    enabled: true
    managed: false
    url: "http://127.0.0.1:1"
    fail_open: true
`)

	a := New()
	t.Cleanup(func() { _ = a.Shutdown() })
	if err := a.Init(context.Background(), configPath); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if a.Memory == nil {
		t.Fatal("App.Memory = nil, want initialized host")
	}
	if a.Memory.Source == "" {
		t.Fatal("memory source is empty")
	}
}

func TestInitMemorySidecarFailClosedReturnsError(t *testing.T) {
	dir := t.TempDir()
	memoryDBPath := filepath.Join(dir, "memory.db")
	memoryConfigPath := writeAppMemoryCoreConfigWithSidecar(t, dir, memoryDBPath)
	configPath := writeAppInitConfigWithMemorySidecar(t, dir, true, memoryConfigPath, `
  sidecar:
    enabled: true
    managed: false
    url: "http://127.0.0.1:1"
    fail_open: false
`)

	a := New()
	t.Cleanup(func() { _ = a.Shutdown() })
	err := a.Init(context.Background(), configPath)
	if err == nil {
		t.Fatal("Init succeeded, want sidecar error")
	}
	if !strings.Contains(err.Error(), "start sidecar") {
		t.Fatalf("Init error = %v, want start sidecar", err)
	}
}

func TestGetSidecarLogsUsesRuntimeSettingsLogPath(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	logPath := filepath.Join(t.TempDir(), "runtime-sidecar.log")
	if err := os.WriteFile(logPath, []byte("runtime sidecar log"), 0o644); err != nil {
		t.Fatalf("WriteFile log: %v", err)
	}
	if err := db.UpsertRuntimeSetting("memory.sidecar", "log_path", `"`+filepath.ToSlash(logPath)+`"`, "ui"); err != nil {
		t.Fatalf("UpsertRuntimeSetting: %v", err)
	}

	a := &App{Config: config.DefaultConfig(), DB: db, Logger: logger}
	logs, err := a.GetSidecarLogs(context.Background(), 1024)
	if err != nil {
		t.Fatalf("GetSidecarLogs: %v", err)
	}
	if logs != "runtime sidecar log" {
		t.Fatalf("logs = %q, want runtime sidecar log", logs)
	}
}

func TestInitMemoryEnabledWrapsStartupError(t *testing.T) {
	dir := t.TempDir()
	configPath := writeAppInitConfig(t, dir, true, filepath.Join(dir, "missing-memorycore.yaml"))

	a := New()
	err := a.Init(context.Background(), configPath)
	if err == nil {
		t.Fatal("Init succeeded with missing memorycore config, want error")
	}
	t.Cleanup(func() { _ = a.Shutdown() })
	if !strings.Contains(err.Error(), "open memorycore") {
		t.Fatalf("Init error = %v, want open memorycore", err)
	}
}

func TestInitMemoryEnabledWrapsManualRulesStartupError(t *testing.T) {
	dir := t.TempDir()
	memoryDBPath := filepath.Join(dir, "memory.db")
	memoryConfigPath := writeAppMemoryCoreConfig(t, dir, true, true, memoryDBPath)
	rulesPath := filepath.Join(dir, "bad-memory-rules.yaml")
	if err := os.WriteFile(rulesPath, []byte(`
pin_rules:
  - prefix: 记住
    predicate: likes
    fact_type: stable_preference
    content_summary: 用户喜欢对象。
`), 0o644); err != nil {
		t.Fatalf("WriteFile bad rules: %v", err)
	}
	configPath := writeAppInitConfigWithManualRules(t, dir, true, memoryConfigPath, rulesPath)

	a := New()
	err := a.Init(context.Background(), configPath)
	if err == nil {
		t.Fatal("Init succeeded with invalid manual rules, want error")
	}
	t.Cleanup(func() { _ = a.Shutdown() })
	if !strings.Contains(err.Error(), "load memory manual rules") {
		t.Fatalf("Init error = %v, want load memory manual rules", err)
	}
}

func TestGetDefaultPersonaName(t *testing.T) {
	a := &App{
		ActiveAgentRuntime: &ActiveAgentRuntime{PersonaKey: "default"},
	}

	if got := a.GetDefaultPersonaName(); got != "default" {
		t.Fatalf("GetDefaultPersonaName = %q, want default", got)
	}
}

func TestActiveAgentRuntimeCloneKeepsRequestParamsIsolated(t *testing.T) {
	temp := 0.2
	a := &App{
		ActiveAgentRuntime: &ActiveAgentRuntime{
			PersonaKey: "default",
			EmotionMain: ModelRuntime{
				Model:  "gpt-4o",
				Params: llm.RequestParams{Temperature: &temp},
			},
		},
	}

	cp := cloneActiveAgentRuntime(a.ActiveAgentRuntime)
	*cp.EmotionMain.Params.Temperature = 0.9

	if *a.ActiveAgentRuntime.EmotionMain.Params.Temperature != 0.2 {
		t.Fatalf("ActiveAgentRuntime params mutated through copy, got %v", *a.ActiveAgentRuntime.EmotionMain.Params.Temperature)
	}
}

func TestRegisterRoutesAgentConfigDispatch(t *testing.T) {
	adminApp := &routeTestAdminApp{
		providers:    []config.LLMProvider{{ID: "moonshot", Name: "Moonshot", Protocol: "openai_compatible", BaseURL: "https://api.moonshot.cn", APIKeyEnv: "MOONSHOT_API_KEY", ModelDiscovery: "manual", Enabled: true}},
		agentConfigs: []config.AgentConfig{{ID: "default", Name: "Default", PersonaKey: "default"}},
		activeAgent:  &config.AgentConfig{ID: "default", Name: "Default", PersonaKey: "default"},
	}
	api := web.NewAPIHandler(adminApp, slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := http.NewServeMux()

	registerRoutes(
		mux,
		api,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		http.NotFoundHandler(),
	)

	t.Run("list providers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/llm-providers", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		var resp struct {
			Providers []config.LLMProvider `json:"providers"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if len(resp.Providers) != 1 || resp.Providers[0].ID != "moonshot" {
			t.Fatalf("providers = %#v, want moonshot", resp.Providers)
		}
	})

	t.Run("provider env alias route dispatches", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/providers/moonshot/env-status", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("memory config route dispatches", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/memory/config", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("sidecar status route dispatches", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sidecar/status", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("list agent configs", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/agent-configs", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		var resp struct {
			ActiveID string `json:"active_id"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if resp.ActiveID != "default" {
			t.Fatalf("active_id = %q, want default", resp.ActiveID)
		}
	})

	t.Run("agent activate route dispatches", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/agent-configs/default/activate", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if adminApp.lastActive != "default" {
			t.Fatalf("lastActive = %q, want default", adminApp.lastActive)
		}
	})

	t.Run("agent activate path with wrong method does not hit activate handler", func(t *testing.T) {
		adminApp.lastActive = ""

		req := httptest.NewRequest(http.MethodGet, "/api/agent-configs/default/activate", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		if adminApp.lastActive != "" {
			t.Fatalf("lastActive changed on GET activate path, got %q", adminApp.lastActive)
		}
	})

	t.Run("old llm profile routes return 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/llm-profiles", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("persona activate route returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/personas/default/activate", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}

func TestUpdateChatSettingsPersistsRuntimeOverrideAndHotUpdatesEngine(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	engine := chat.NewEngine(chat.EngineConfig{
		DB:          db,
		Logger:      logger,
		Model:       "test-model",
		MaxTokens:   128,
		Temperature: 0.2,
	})
	a := &App{
		Config: &config.Config{Chat: config.ChatConfig{
			RealtimeStreaming: false,
			TurnPipeline:      config.TurnPipelineConfig{Shadow: true, Enabled: true, MemoryStages: true, ApprovalStages: true},
		}},
		DB:     db,
		Logger: logger,
		engine: engine,
	}

	if err := a.UpdateChatSettings(config.ChatConfig{RealtimeStreaming: true}); err != nil {
		t.Fatalf("UpdateChatSettings: %v", err)
	}

	value, ok, err := db.GetRuntimeConfig("chat.realtime_streaming")
	if err != nil {
		t.Fatalf("GetRuntimeConfig: %v", err)
	}
	if !ok || value != "true" {
		t.Fatalf("runtime chat.realtime_streaming = %q/%t, want true/true", value, ok)
	}
	if !a.Config.Chat.RealtimeStreaming {
		t.Fatal("Config.Chat.RealtimeStreaming = false, want true")
	}
	if !engine.RuntimeConfig().RealtimeStreaming {
		t.Fatal("engine realtime streaming = false, want true")
	}
	if got := a.Config.Chat.TurnPipeline; !got.Shadow || !got.Enabled || !got.MemoryStages || !got.ApprovalStages {
		t.Fatalf("turn pipeline config = %#v, want preserved", got)
	}
}

func TestConfigurePluginHostHonorsEnabledConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = true
	cfg.Plugins.BuiltinEnabled = []string{plugin.TurnAuditPluginID}
	cfg.Chat.TurnPipeline.Enabled = true
	cfg.Chat.TurnPipeline.RolloutPercent = 100
	a := &App{Config: cfg, Logger: logger, toolRegistry: tool.NewRegistry()}
	dispatcher := tool.NewDispatcher(a.toolRegistry, tool.MinimalSchemaValidator{}, logger)

	if err := a.configurePluginHost(context.Background(), dispatcher, nil); err != nil {
		t.Fatalf("configurePluginHost: %v", err)
	}
	if a.PluginHost == nil || !a.PluginHost.Enabled() {
		t.Fatalf("PluginHost = %#v, want enabled host", a.PluginHost)
	}

	disabled := &App{Config: config.DefaultConfig(), Logger: logger, toolRegistry: tool.NewRegistry()}
	dispatcher = tool.NewDispatcher(disabled.toolRegistry, tool.MinimalSchemaValidator{}, logger)
	if err := disabled.configurePluginHost(context.Background(), dispatcher, nil); err != nil {
		t.Fatalf("disabled configurePluginHost: %v", err)
	}
	if disabled.PluginHost != nil {
		t.Fatalf("disabled PluginHost = %#v, want nil", disabled.PluginHost)
	}
}

func TestQueueMemoryExtractionEnqueuesSessionSegmentsImmediately(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.CreateSession(ctx, "chat-memory", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	segment, err := db.CreateMemorySegment(ctx, storage.CreateMemorySegmentParams{
		ID:              "segment-memory",
		ChatSessionID:   "chat-memory",
		PersonaID:       "default",
		MemorySessionID: "memory-session",
	})
	if err != nil {
		t.Fatalf("CreateMemorySegment: %v", err)
	}
	if err := db.UpdateMemorySegmentEpisode(ctx, segment.ID, "user", "episode-user"); err != nil {
		t.Fatalf("UpdateMemorySegmentEpisode(user): %v", err)
	}
	cfg := config.DefaultConfig()
	cfg.Memory.Extraction.Enabled = true
	a := &App{Config: cfg, DB: db, Logger: logger}

	resp, err := a.QueueMemoryExtraction(ctx, web.MemoryExtractionRequest{SessionID: "chat-memory", Scope: "session", Mode: "apply"})
	if err != nil {
		t.Fatalf("QueueMemoryExtraction: %v", err)
	}
	if resp.EnqueuedCount != 1 || len(resp.Jobs) != 1 {
		t.Fatalf("resp = %#v, want one enqueued job", resp)
	}
	if resp.Jobs[0].Trigger != storage.MemoryExtractionTriggerManualScan || resp.Jobs[0].Status != storage.MemoryExtractionJobStatusPending {
		t.Fatalf("job = %#v, want pending manual_scan", resp.Jobs[0])
	}
}

func TestQueueMemoryExtractionSkipsSucceededSegmentUnlessForced(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := db.CreateSession(ctx, "chat-memory-force", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	segment, err := db.CreateMemorySegment(ctx, storage.CreateMemorySegmentParams{
		ID:              "segment-memory-force",
		ChatSessionID:   "chat-memory-force",
		PersonaID:       "default",
		MemorySessionID: "memory-session-force",
	})
	if err != nil {
		t.Fatalf("CreateMemorySegment: %v", err)
	}
	if err := db.UpdateMemorySegmentExtractionCompleted(ctx, segment.ID, storage.MemorySegmentExtractionCompleted{
		JobID:            "job-old",
		Status:           storage.MemorySegmentExtractionStatusSucceeded,
		ExtractedUntilAt: segment.LastActivityAt,
	}); err != nil {
		t.Fatalf("UpdateMemorySegmentExtractionCompleted: %v", err)
	}
	cfg := config.DefaultConfig()
	cfg.Memory.Extraction.Enabled = true
	a := &App{Config: cfg, DB: db, Logger: logger}

	resp, err := a.QueueMemoryExtraction(ctx, web.MemoryExtractionRequest{SessionID: "chat-memory-force", Scope: "session", Mode: "apply"})
	if err != nil {
		t.Fatalf("QueueMemoryExtraction(skip): %v", err)
	}
	if resp.EnqueuedCount != 0 || resp.SkippedCount != 1 {
		t.Fatalf("skip resp = %#v, want skipped succeeded segment", resp)
	}
	resp, err = a.QueueMemoryExtraction(ctx, web.MemoryExtractionRequest{SessionID: "chat-memory-force", Scope: "session", Mode: "apply", Force: true})
	if err != nil {
		t.Fatalf("QueueMemoryExtraction(force): %v", err)
	}
	if resp.EnqueuedCount != 1 || len(resp.Jobs) != 1 {
		t.Fatalf("force resp = %#v, want one job", resp)
	}
}

func TestQueueMemoryExtractionRespectsManualConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	cfg := config.DefaultConfig()
	cfg.Memory.Extraction.Enabled = true
	a := &App{Config: cfg, DB: db, Logger: logger}

	if _, err := a.QueueMemoryExtraction(ctx, web.MemoryExtractionRequest{Scope: "invalid"}); err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("QueueMemoryExtraction invalid scope error = %v", err)
	}
	cfg.Memory.Extraction.Enabled = false
	if _, err := a.QueueMemoryExtraction(ctx, web.MemoryExtractionRequest{Scope: "all"}); err == nil || !strings.Contains(err.Error(), "memory extraction is disabled") {
		t.Fatalf("QueueMemoryExtraction disabled extraction error = %v", err)
	}
	cfg.Memory.Extraction.Enabled = true
	cfg.Memory.Extraction.Async.Enabled = false
	if _, err := a.QueueMemoryExtraction(ctx, web.MemoryExtractionRequest{Scope: "all"}); err == nil || !strings.Contains(err.Error(), "async queue is disabled") {
		t.Fatalf("QueueMemoryExtraction disabled async error = %v", err)
	}
	cfg.Memory.Extraction.Async.Enabled = true
	cfg.Memory.Extraction.Async.WorkerEnabled = false
	if _, err := a.QueueMemoryExtraction(ctx, web.MemoryExtractionRequest{Scope: "all"}); err == nil || !strings.Contains(err.Error(), "worker is disabled") {
		t.Fatalf("QueueMemoryExtraction disabled worker error = %v", err)
	}
	cfg.Memory.Extraction.Async.WorkerEnabled = true
	cfg.Memory.Extraction.Manual.Enabled = false
	if _, err := a.QueueMemoryExtraction(ctx, web.MemoryExtractionRequest{Scope: "all"}); err == nil || !strings.Contains(err.Error(), "manual trigger is disabled") {
		t.Fatalf("QueueMemoryExtraction disabled manual error = %v", err)
	}
	cfg.Memory.Extraction.Manual.Enabled = true
	cfg.Memory.Extraction.Manual.AllowForce = false
	if _, err := a.QueueMemoryExtraction(ctx, web.MemoryExtractionRequest{Scope: "all", Force: true}); err == nil || !strings.Contains(err.Error(), "force is disabled") {
		t.Fatalf("QueueMemoryExtraction disabled force error = %v", err)
	}
	cfg.Memory.Extraction.Manual.AllowForce = true
	cfg.Memory.Extraction.Manual.AllowSegmentSelection = false
	if _, err := a.QueueMemoryExtraction(ctx, web.MemoryExtractionRequest{Scope: "segment", SegmentID: "segment-1"}); err == nil || !strings.Contains(err.Error(), "segment selection is disabled") {
		t.Fatalf("QueueMemoryExtraction disabled segment selection error = %v", err)
	}
}

func TestActivateAgentConfigBuildsRuntimeAndHotUpdatesEngine(t *testing.T) {
	t.Setenv("TEST_AGENT_API_KEY", "test-key")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	provider := config.LLMProvider{
		ID:             "moonshot",
		Name:           "Moonshot",
		Protocol:       "openai_compatible",
		BaseURL:        "https://api.moonshot.cn",
		APIKeyEnv:      "TEST_AGENT_API_KEY",
		ModelDiscovery: "manual",
		Enabled:        true,
	}
	if err := db.UpsertLLMProvider(provider); err != nil {
		t.Fatalf("UpsertLLMProvider: %v", err)
	}
	temp := 0.2
	summaryTemp := 0.05
	streamMain := true
	streamSummary := false
	agent := config.AgentConfig{
		ID:         "default",
		Name:       "Default",
		PersonaKey: "default",
		Emotion: config.AgentModelGroup{
			Main:    config.ModelBinding{ProviderID: "moonshot", Model: "emotion-main", Params: llm.RequestParams{MaxTokens: 111, Temperature: &temp, Stream: &streamMain}},
			Summary: config.ModelBinding{ProviderID: "moonshot", Model: "emotion-summary", Params: llm.RequestParams{MaxTokens: 222, Temperature: &summaryTemp, Stream: &streamSummary}},
		},
		Work: config.AgentModelGroup{
			Main:    config.ModelBinding{ProviderID: "moonshot", Model: "work-main", Params: llm.RequestParams{MaxTokens: 333, Temperature: &temp, Stream: &streamMain}},
			Summary: config.ModelBinding{ProviderID: "moonshot", Model: "work-summary", Params: llm.RequestParams{MaxTokens: 444, Temperature: &summaryTemp, Stream: &streamSummary}},
		},
		ContextOverrides: map[string]any{"input_budget_tokens": float64(9000)},
	}
	if err := db.UpsertAgentConfig(agent); err != nil {
		t.Fatalf("UpsertAgentConfig: %v", err)
	}

	engine := chat.NewEngine(chat.EngineConfig{DB: db, Logger: logger, Model: "old", MaxTokens: 1, Temperature: 1})
	a := &App{
		Config:   config.DefaultConfig(),
		DB:       db,
		Logger:   logger,
		engine:   engine,
		Personas: map[string]*config.Persona{"default": {Name: "Default"}},
	}

	if err := a.ActivateAgentConfig("default"); err != nil {
		t.Fatalf("ActivateAgentConfig: %v", err)
	}
	if got := a.GetDefaultPersonaName(); got != "default" {
		t.Fatalf("GetDefaultPersonaName = %q, want default", got)
	}
	if a.ActiveAgentRuntime == nil || a.ActiveAgentRuntime.WorkSummary.Model != "work-summary" {
		t.Fatalf("ActiveAgentRuntime = %#v", a.ActiveAgentRuntime)
	}
	runtimeCfg := engine.RuntimeConfig()
	if runtimeCfg.Model != "emotion-main" || runtimeCfg.SummaryModel != "emotion-summary" {
		t.Fatalf("engine runtime = %#v, want emotion main/summary", runtimeCfg)
	}
	if runtimeCfg.Params.MaxTokens != 111 {
		t.Fatalf("main max tokens = %d, want 111", runtimeCfg.Params.MaxTokens)
	}
	if runtimeCfg.SummaryParams.Temperature == nil || *runtimeCfg.SummaryParams.Temperature != 0.05 {
		t.Fatalf("summary temperature = %#v, want 0.05", runtimeCfg.SummaryParams.Temperature)
	}
	if runtimeCfg.ContextConfig.InputBudgetTokens != 9000 {
		t.Fatalf("context input budget = %d, want 9000", runtimeCfg.ContextConfig.InputBudgetTokens)
	}
}

func TestCreatePersonaStoresByKeyInDB(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	a := &App{
		Config: &config.Config{
			Personas: config.PersonasConfig{Dir: t.TempDir()},
		},
		DB:       db,
		Logger:   logger,
		Personas: map[string]*config.Persona{},
	}

	err = a.CreatePersona("neko", &config.Persona{
		Name:         "Tami",
		Description:  "cat roommate",
		SystemPrompt: "You are Tami.",
		Tone:         "snarky",
		Greeting:     "meow",
	})
	if err != nil {
		t.Fatalf("CreatePersona: %v", err)
	}

	record, err := db.GetPersona(context.Background(), "neko")
	if err != nil {
		t.Fatalf("GetPersona: %v", err)
	}
	if record == nil {
		t.Fatal("GetPersona returned nil")
	}
	if record.Key != "neko" {
		t.Fatalf("record.Key = %q, want neko", record.Key)
	}
	if record.Name != "Tami" {
		t.Fatalf("record.Name = %q, want Tami", record.Name)
	}
}

func TestUpdatePersonaKeepsStableDBKeyWhenDisplayNameChanges(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	personaDir := t.TempDir()
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	a := &App{
		Config: &config.Config{
			Personas: config.PersonasConfig{Dir: personaDir},
		},
		DB:     db,
		Logger: logger,
		Personas: map[string]*config.Persona{
			"neko": {Name: "Tami", Description: "cat roommate"},
		},
	}

	if err := config.SavePersona(personaDir, "neko", a.Personas["neko"]); err != nil {
		t.Fatalf("SavePersona: %v", err)
	}
	if err := db.UpsertPersona("neko", "Tami", "cat roommate", "prompt", "snarky", nil, "meow", nil); err != nil {
		t.Fatalf("UpsertPersona: %v", err)
	}

	err = a.UpdatePersona("neko", &config.Persona{
		Name:         "Mimi",
		Description:  "updated cat roommate",
		SystemPrompt: "You are Mimi.",
		Tone:         "cool",
		Greeting:     "hi",
	})
	if err != nil {
		t.Fatalf("UpdatePersona: %v", err)
	}

	record, err := db.GetPersona(context.Background(), "neko")
	if err != nil {
		t.Fatalf("GetPersona(updated): %v", err)
	}
	if record == nil {
		t.Fatal("updated record missing")
	}
	if record.Key != "neko" {
		t.Fatalf("record.Key = %q, want neko", record.Key)
	}
	if record.Name != "Mimi" {
		t.Fatalf("record.Name = %q, want Mimi", record.Name)
	}
}

func TestGetPersonaReturnsDeepCopyOfWorkProgressPhrases(t *testing.T) {
	a := &App{
		Personas: map[string]*config.Persona{
			"default": {
				Name: "default",
				WorkProgressPhrases: map[string][]string{
					"read_file": {"看看文件"},
				},
			},
		},
	}

	persona, ok := a.GetPersona("default")
	if !ok || persona == nil {
		t.Fatal("GetPersona returned nil")
	}
	persona.WorkProgressPhrases["read_file"][0] = "mutated"
	persona.WorkProgressPhrases["new_key"] = []string{"new"}

	original := a.Personas["default"].WorkProgressPhrases
	if original["read_file"][0] != "看看文件" {
		t.Fatalf("original read_file phrase = %q, want untouched", original["read_file"][0])
	}
	if _, exists := original["new_key"]; exists {
		t.Fatalf("original map unexpectedly mutated: %#v", original)
	}
}

func TestUpdateProgressPhrasesPersistsToFileDBAndMemory(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	personaDir := t.TempDir()
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	initial := &config.Persona{
		Name:        "default",
		Description: "desc",
	}

	a := &App{
		Config: &config.Config{
			Personas: config.PersonasConfig{Dir: personaDir},
		},
		DB:     db,
		Logger: logger,
		Personas: map[string]*config.Persona{
			"default": initial,
		},
	}

	if err := config.SavePersona(personaDir, "default", initial); err != nil {
		t.Fatalf("SavePersona: %v", err)
	}
	if err := db.UpsertPersona("default", initial.Name, initial.Description, initial.SystemPrompt, initial.Tone, initial.Quirks, initial.Greeting, initial.WorkProgressPhrases); err != nil {
		t.Fatalf("UpsertPersona: %v", err)
	}

	phrases := map[string][]string{
		"read_file": {"看看文件"},
		"_default":  {"处理中"},
	}
	if err := a.UpdateProgressPhrases("default", phrases); err != nil {
		t.Fatalf("UpdateProgressPhrases: %v", err)
	}

	if got := a.Personas["default"].WorkProgressPhrases["read_file"]; len(got) != 1 || got[0] != "看看文件" {
		t.Fatalf("memory phrases = %#v, want read_file phrase", a.Personas["default"].WorkProgressPhrases)
	}

	loaded, err := config.LoadPersona(filepath.Join(personaDir, "default.yaml"))
	if err != nil {
		t.Fatalf("LoadPersona: %v", err)
	}
	if got := loaded.WorkProgressPhrases["read_file"]; len(got) != 1 || got[0] != "看看文件" {
		t.Fatalf("file phrases = %#v, want read_file phrase", loaded.WorkProgressPhrases)
	}

	record, err := db.GetPersona(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetPersona: %v", err)
	}
	if record == nil {
		t.Fatal("GetPersona returned nil")
	}
	var decoded map[string][]string
	if err := json.Unmarshal([]byte(record.WorkProgressPhrases), &decoded); err != nil {
		t.Fatalf("Unmarshal WorkProgressPhrases: %v", err)
	}
	if got := decoded["read_file"]; len(got) != 1 || got[0] != "看看文件" {
		t.Fatalf("db phrases = %#v, want read_file phrase", decoded)
	}
}

func TestDeleteSessionReturnsNotFoundForMissingSession(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	a := &App{DB: db, Logger: logger}

	err = a.DeleteSession(context.Background(), "missing")
	if err == nil {
		t.Fatal("DeleteSession should fail for missing session")
	}
	if !errors.Is(err, apperrors.ErrSessionNotFound) {
		t.Fatalf("DeleteSession error = %v, want ErrSessionNotFound", err)
	}
}

func writeAppInitConfig(t *testing.T, dir string, memoryEnabled bool, memoryConfigPath string) string {
	t.Helper()
	return writeAppInitConfigWithManualRules(t, dir, memoryEnabled, memoryConfigPath, "")
}

func writeAppInitConfigWithManualRules(t *testing.T, dir string, memoryEnabled bool, memoryConfigPath string, manualRulesPath string) string {
	t.Helper()

	personaDir := filepath.Join(dir, "personas")
	if err := os.MkdirAll(personaDir, 0o755); err != nil {
		t.Fatalf("MkdirAll persona dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(personaDir, "default.yaml"), []byte("name: Default\nsystem_prompt: test\n"), 0o644); err != nil {
		t.Fatalf("WriteFile persona: %v", err)
	}
	if memoryEnabled && manualRulesPath == "" {
		manualRulesPath = writeAppManualRulesConfig(t, dir)
	}

	return writeAppInitConfigWithMemorySidecarAndRules(t, dir, memoryEnabled, memoryConfigPath, manualRulesPath, "")
}

func writeAppInitConfigWithMemorySidecar(t *testing.T, dir string, memoryEnabled bool, memoryConfigPath string, sidecarYAML string) string {
	t.Helper()
	return writeAppInitConfigWithMemorySidecarAndRules(t, dir, memoryEnabled, memoryConfigPath, "", sidecarYAML)
}

func writeAppInitConfigWithMemorySidecarAndRules(t *testing.T, dir string, memoryEnabled bool, memoryConfigPath string, manualRulesPath string, sidecarYAML string) string {
	t.Helper()

	personaDir := filepath.Join(dir, "personas")
	if err := os.MkdirAll(personaDir, 0o755); err != nil {
		t.Fatalf("MkdirAll persona dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(personaDir, "default.yaml"), []byte("name: Default\nsystem_prompt: test\n"), 0o644); err != nil {
		t.Fatalf("WriteFile persona: %v", err)
	}
	if memoryEnabled && manualRulesPath == "" {
		manualRulesPath = writeAppManualRulesConfig(t, dir)
	}
	if memoryEnabled {
		t.Setenv("MOONSHOT_API_KEY", "test-key")
	}

	configPath := filepath.Join(dir, "config.yaml")
	body := fmt.Sprintf(`
server:
  host: "127.0.0.1"
  port: 8081
llm_providers:
  - id: moonshot
    name: Moonshot
    protocol: openai_compatible
    base_url: https://api.moonshot.cn
    api_key_env: MOONSHOT_API_KEY
    model_discovery: manual
    enabled: true
agent_configs:
  - id: default
    name: Default
    persona_key: default
    emotion:
      main:
        provider_id: moonshot
        model: kimi-k2.6
      summary:
        provider_id: moonshot
        model: kimi-k2.6
    work:
      main:
        provider_id: moonshot
        model: kimi-k2.6
      summary:
        provider_id: moonshot
        model: kimi-k2.6
    context_overrides: {}
agent:
  active_config: default
memory:
  enabled: %t
  config_path: %q
%s
%s
db:
  path: %q
personas:
  dir: %q
`, memoryEnabled, filepath.ToSlash(memoryConfigPath), formatManualRulesPathYAML(manualRulesPath), sidecarYAML, filepath.ToSlash(filepath.Join(dir, "emo.db")), filepath.ToSlash(personaDir))
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile app config: %v", err)
	}
	return configPath
}

func writeAppManualRulesConfig(t *testing.T, dir string) string {
	t.Helper()

	path := filepath.Join(dir, "memory_manual_rules.yaml")
	body := `
pin_rules:
  - prefix: 请记住我喜欢
    predicate: likes
    fact_type: stable_preference
    content_summary: 用户喜欢{object}。
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile manual rules: %v", err)
	}
	return path
}

func formatManualRulesPathYAML(path string) string {
	if path == "" {
		return ""
	}
	return fmt.Sprintf("  manual_rules_path: %q", filepath.ToSlash(path))
}

func writeAppMemoryCoreConfig(t *testing.T, dir string, enabled bool, autoMigrate bool, dbPath string) string {
	t.Helper()

	configPath := filepath.Join(dir, "memorycore.yaml")
	body := fmt.Sprintf(`
schema_version: memorycore.config.v0.2
enabled: %t
core:
  db_path: %q
  persona_id: default
  auto_migrate: %t
  enable_fts: true
`, enabled, filepath.ToSlash(dbPath), autoMigrate)
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile memorycore config: %v", err)
	}
	return configPath
}

func writeAppMemoryCoreConfigWithPipeline(t *testing.T, dir string, dbPath string, providerID string) string {
	t.Helper()

	configPath := filepath.Join(dir, "memorycore.yaml")
	body := fmt.Sprintf(`
schema_version: memorycore.config.v0.2
enabled: true
core:
  db_path: %q
  persona_id: default
  auto_migrate: true
  enable_fts: true
pipelines:
  extraction:
    enabled: true
    provider_id: %s
    model: memory-model
`, filepath.ToSlash(dbPath), providerID)
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile memorycore config: %v", err)
	}
	return configPath
}

func writeAppMemoryCoreConfigWithSidecar(t *testing.T, dir string, dbPath string) string {
	t.Helper()

	configPath := filepath.Join(dir, "memorycore.yaml")
	body := fmt.Sprintf(`
schema_version: memorycore.config.v0.2
enabled: true
core:
  db_path: %q
  persona_id: default
  auto_migrate: true
  enable_fts: true
retrieval:
  use_fts: true
  use_mirror: true
sidecar:
  enabled: true
  adapter: trivium
mirror:
  enabled: true
`, filepath.ToSlash(dbPath))
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile memorycore config: %v", err)
	}
	return configPath
}

func TestGetSessionDetailReturnsMessages(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	a := &App{DB: db, Logger: logger}
	ctx := context.Background()
	if err := db.CreateSession(ctx, "session-1", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := db.AddMessage(ctx, "msg-1", "session-1", "user", "hello"); err != nil {
		t.Fatalf("AddMessage(user): %v", err)
	}
	if err := db.AddMessage(ctx, "msg-2", "session-1", "assistant", "hi"); err != nil {
		t.Fatalf("AddMessage(assistant): %v", err)
	}

	session, messages, err := a.GetSessionDetail(ctx, "session-1")
	if err != nil {
		t.Fatalf("GetSessionDetail: %v", err)
	}
	if session == nil || session.ID != "session-1" {
		t.Fatalf("session = %#v, want session-1", session)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}
	if messages[0].Content != "hello" || messages[1].Content != "hi" {
		t.Fatalf("messages = %#v, want [hello hi]", messages)
	}
}
