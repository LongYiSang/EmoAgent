package app

import (
	"bytes"
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
	"reflect"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/agentaffect"
	"github.com/longyisang/emoagent/internal/apperrors"
	"github.com/longyisang/emoagent/internal/chat"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/configcenter"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/media"
	"github.com/longyisang/emoagent/internal/memoryhost"
	"github.com/longyisang/emoagent/internal/memoryruntime"
	"github.com/longyisang/emoagent/internal/plugin"
	"github.com/longyisang/emoagent/internal/protocol"
	sidecarruntime "github.com/longyisang/emoagent/internal/sidecar"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/turn"
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
func (a *routeTestAdminApp) UploadMedia(ctx context.Context, r io.Reader, meta media.UploadMeta) (*media.MediaAsset, error) {
	return &media.MediaAsset{ID: "med_test", Kind: "image", MimeType: "image/png", ByteSize: 1}, nil
}
func (a *routeTestAdminApp) GetMediaAsset(ctx context.Context, mediaAssetID string) (*media.MediaAsset, error) {
	return nil, ErrMediaNotFound
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
func (a *routeTestAdminApp) GetSessionMessageParts(ctx context.Context, sessionID string) (map[string][]storage.MessagePartRecord, error) {
	return map[string][]storage.MessagePartRecord{}, nil
}
func (a *routeTestAdminApp) OpenSessionMedia(ctx context.Context, sessionID, mediaAssetID string) (io.ReadCloser, *media.MediaAsset, error) {
	return nil, nil, ErrMediaNotFound
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
func (a *routeTestAdminApp) RunNaturalMemory(ctx context.Context, req web.NaturalMemoryRunRequest) (memoryhost.NaturalMemoryRunResponse, error) {
	return memoryhost.NaturalMemoryRunResponse{}, nil
}
func (a *routeTestAdminApp) LatestNaturalMemoryRun(ctx context.Context) (*memoryhost.NaturalMemoryRunResponse, error) {
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
func (a *routeTestAdminApp) GetAgentAffectConfig(ctx context.Context) (configcenter.AgentAffectConfigResponse, error) {
	return configcenter.AgentAffectConfigResponse{}, nil
}
func (a *routeTestAdminApp) UpdateAgentAffectConfig(ctx context.Context, cfg config.AgentAffectConfig) (configcenter.EffectiveConfig, error) {
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
func (a *routeTestAdminApp) GetAgentAffectCurrent(ctx context.Context, req web.AgentAffectCurrentRequest) (web.AgentAffectCurrentResponse, error) {
	return web.AgentAffectCurrentResponse{}, nil
}
func (a *routeTestAdminApp) GetAgentAffectProfile(ctx context.Context, personaID string) (web.AgentAffectProfileResponse, error) {
	return web.AgentAffectProfileResponse{}, nil
}
func (a *routeTestAdminApp) UpdateAgentAffectProfile(ctx context.Context, profile web.AgentAffectProfileResponse) (web.AgentAffectProfileResponse, error) {
	return web.AgentAffectProfileResponse{}, nil
}
func (a *routeTestAdminApp) ListAgentAffectHistory(ctx context.Context, req web.AgentAffectHistoryRequest) (web.AgentAffectHistoryResponse, error) {
	return web.AgentAffectHistoryResponse{}, nil
}
func (a *routeTestAdminApp) ListAgentAffectPluginWrites(ctx context.Context, req web.AgentAffectPluginWritesRequest) (web.AgentAffectPluginWritesResponse, error) {
	return nil, nil
}
func (a *routeTestAdminApp) EvaluateAgentAffect(ctx context.Context, req web.AgentAffectEvaluateRequest) (web.AgentAffectEvaluateResponse, error) {
	return web.AgentAffectEvaluateResponse{}, nil
}
func (a *routeTestAdminApp) SubmitAgentAffect(ctx context.Context, req web.AgentAffectSubmitRequest) (web.AgentAffectSubmitResponse, error) {
	return web.AgentAffectSubmitResponse{}, nil
}
func (a *routeTestAdminApp) ApplyAgentAffectDelta(ctx context.Context, req web.AgentAffectDeltaRequest) (web.AgentAffectDeltaResponse, error) {
	return web.AgentAffectDeltaResponse{}, nil
}
func (a *routeTestAdminApp) ResetAgentAffect(ctx context.Context, req web.AgentAffectResetRequest) (web.AgentAffectResetResponse, error) {
	return web.AgentAffectResetResponse{}, nil
}
func (a *routeTestAdminApp) PreviewAgentAffectPrompt(ctx context.Context, req web.AgentAffectPromptPreviewRequest) (web.AgentAffectPromptPreviewResponse, error) {
	return web.AgentAffectPromptPreviewResponse{}, nil
}
func (a *routeTestAdminApp) GetAgentAffectQueue(ctx context.Context, req web.AgentAffectQueueRequest) (web.AgentAffectQueueResponse, error) {
	return web.AgentAffectQueueResponse{}, nil
}
func (a *routeTestAdminApp) ProcessAgentAffectBatchOnce(ctx context.Context) (web.AgentAffectProcessOnceResponse, error) {
	return web.AgentAffectProcessOnceResponse{}, nil
}
func (a *routeTestAdminApp) ClearAgentAffectFailedJobs(ctx context.Context, req web.AgentAffectQueueRequest) (web.AgentAffectClearFailedResponse, error) {
	return web.AgentAffectClearFailedResponse{}, nil
}
func (a *routeTestAdminApp) SupersedeAgentAffectPendingJobs(ctx context.Context, req web.AgentAffectQueueRequest) (web.AgentAffectSupersedePendingResponse, error) {
	return web.AgentAffectSupersedePendingResponse{}, nil
}

func TestRunAllowsStartupWithoutLLM(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := newTestApp(&config.Config{
		Server: config.ServerConfig{Host: "127.0.0.1", Port: 0},
	}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

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
	t.Chdir(dir)
	missingMemoryConfig := filepath.Join(dir, "missing-memorycore.yaml")
	memoryDBPath := filepath.Join(dir, "memory.db")
	configPath := writeAppInitConfig(t, dir, false, missingMemoryConfig)

	a := New()
	t.Cleanup(func() { _ = a.Shutdown() })
	if err := a.Init(context.Background(), configPath); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if testMemoryHost(a) != nil {
		t.Fatalf("Memory host = %#v, want nil when memory.enabled=false", testMemoryHost(a))
	}
	if _, err := os.Stat(memoryDBPath); !os.IsNotExist(err) {
		t.Fatalf("memory db stat error = %v, want not exist", err)
	}
	raw, err := os.ReadFile(memoryruntime.DefaultSnapshotPath)
	if err != nil {
		t.Fatalf("ReadFile memory runtime snapshot: %v", err)
	}
	var snapshot memoryruntime.Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("Unmarshal snapshot: %v", err)
	}
	if snapshot.MemoryEnabled || snapshot.Sidecar.Status != "disabled" || snapshot.MemoryCoreDB != "" {
		t.Fatalf("disabled snapshot = %#v", snapshot)
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

	if testMemoryHost(a) == nil {
		t.Fatal("Memory host = nil, want initialized host")
	}
	if _, err := os.Stat(memoryDBPath); err != nil {
		t.Fatalf("memory db was not created: %v", err)
	}
}

func TestMemoryRetrievalPolicyMatchesRuntimeSnapshotPolicy(t *testing.T) {
	cfg := config.DefaultConfig().Memory
	cfg.Enabled = true
	cfg.Retrieval.UseFTS = true
	cfg.Retrieval.UseMirror = true
	cfg.Retrieval.FinalMemoryCount = 6
	cfg.Retrieval.ContextBudgetTokens = 2048

	policy := memoryRetrievalPolicy(cfg.Retrieval)
	snapshot := memoryruntime.BuildSnapshot(memoryruntime.Input{Memory: cfg})
	snapshotPolicy := snapshot.Retrieval.ChatPromptPolicy

	if policy.SensitivityPermission != snapshotPolicy.SensitivityPermission ||
		policy.AllowHistorical != snapshotPolicy.AllowHistorical ||
		policy.AllowDeepArchive != snapshotPolicy.AllowDeepArchive ||
		policy.FinalMemoryCount != snapshotPolicy.FinalMemoryCount ||
		policy.ContextBudgetTokens != snapshotPolicy.ContextBudgetTokens ||
		policy.UseFTS != snapshotPolicy.UseFTS ||
		policy.UseMirror != snapshotPolicy.UseMirror {
		t.Fatalf("app policy = %#v, snapshot policy = %#v", policy, snapshotPolicy)
	}
	if snapshotPolicy.Source != "emoagent.chat_prompt_policy" {
		t.Fatalf("snapshot policy source = %q", snapshotPolicy.Source)
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

	if testMemoryHost(a) == nil {
		t.Fatal("Memory host = nil, want initialized host")
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
	if testMemoryHost(a) == nil {
		t.Fatal("Memory host = nil, want initialized host")
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
	if testMemoryHost(a) == nil {
		t.Fatal("Memory host = nil, want initialized host")
	}
	if testMemoryHost(a).Source == "" {
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

	a := newTestApp(config.DefaultConfig(), db, logger)
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
	a := newTestApp(nil, nil, nil)
	setTestActiveRuntime(a, &ActiveAgentRuntime{PersonaKey: "default"})

	if got := a.GetDefaultPersonaName(); got != "default" {
		t.Fatalf("GetDefaultPersonaName = %q, want default", got)
	}
}

func TestActiveAgentRuntimeCloneKeepsRequestParamsIsolated(t *testing.T) {
	temp := 0.2
	runtime := &ActiveAgentRuntime{
		PersonaKey: "default",
		EmotionMain: ModelRuntime{
			Model:  "gpt-4o",
			Params: llm.RequestParams{Temperature: &temp},
		},
	}

	cp := cloneActiveAgentRuntime(runtime)
	*cp.EmotionMain.Params.Temperature = 0.9

	if *runtime.EmotionMain.Params.Temperature != 0.2 {
		t.Fatalf("ActiveAgentRuntime params mutated through copy, got %v", *runtime.EmotionMain.Params.Temperature)
	}
}

type fakeAgentAffectService struct{}

func (fakeAgentAffectService) UpdateMode() string {
	return "async_after_reply"
}
func (fakeAgentAffectService) GetCurrentMood(context.Context, agentaffect.GetCurrentMoodRequest) (agentaffect.GetCurrentMoodResponse, error) {
	return agentaffect.GetCurrentMoodResponse{}, nil
}
func (fakeAgentAffectService) GetProfile(context.Context, string) (agentaffect.AffectProfile, error) {
	return agentaffect.AffectProfile{}, nil
}
func (fakeAgentAffectService) UpdateProfile(context.Context, agentaffect.AffectProfile) (agentaffect.AffectProfile, error) {
	return agentaffect.AffectProfile{}, nil
}
func (fakeAgentAffectService) ListHistory(context.Context, agentaffect.HistoryQuery) (agentaffect.HistoryResponse, error) {
	return agentaffect.HistoryResponse{}, nil
}
func (fakeAgentAffectService) ListPluginWrites(context.Context, agentaffect.PluginWritesQuery) ([]agentaffect.PluginWriteRecord, error) {
	return nil, nil
}
func (fakeAgentAffectService) EvaluateMoodImpact(context.Context, agentaffect.EvaluateMoodImpactRequest) (agentaffect.EvaluateMoodImpactResponse, error) {
	return agentaffect.EvaluateMoodImpactResponse{}, nil
}
func (fakeAgentAffectService) SubmitMoodImpact(context.Context, agentaffect.SubmitMoodImpactRequest) (agentaffect.SubmitMoodImpactResponse, error) {
	return agentaffect.SubmitMoodImpactResponse{EventID: "event-1"}, nil
}
func (fakeAgentAffectService) EnqueueTurnEvaluationJob(context.Context, agentaffect.EnqueueTurnEvaluationJobRequest) (agentaffect.AffectJobRecord, error) {
	return agentaffect.AffectJobRecord{ID: "job-1"}, nil
}
func (fakeAgentAffectService) ApplyMoodDelta(context.Context, agentaffect.ApplyMoodDeltaRequest) (agentaffect.ApplyMoodDeltaResponse, error) {
	return agentaffect.ApplyMoodDeltaResponse{EventID: "event-1"}, nil
}
func (fakeAgentAffectService) ResetMood(context.Context, agentaffect.ResetMoodRequest) (agentaffect.ResetMoodResponse, error) {
	return agentaffect.ResetMoodResponse{EventID: "event-1"}, nil
}
func (fakeAgentAffectService) BuildPromptAffectBlock(context.Context, agentaffect.BuildPromptAffectBlockRequest) (string, error) {
	return "", nil
}
func (fakeAgentAffectService) PreviewPrompt(context.Context, agentaffect.BuildPromptAffectBlockRequest) (agentaffect.PromptPreviewResponse, error) {
	return agentaffect.PromptPreviewResponse{}, nil
}

func TestHookedAgentAffectRuntimeDispatchesPluginHooks(t *testing.T) {
	host := plugin.NewPluginHost(config.PluginsConfig{Enabled: true, DefaultTimeoutMS: 80, MaxTimeoutMS: 1000}, turn.NewMemoryJournal(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	counts := map[plugin.HookName]int{}
	for _, hook := range []plugin.HookName{
		plugin.HookBeforeAgentAffectEvaluate,
		plugin.HookAfterAgentAffectEvaluate,
		plugin.HookBeforeAgentAffectCommit,
		plugin.HookAfterAgentAffectCommit,
	} {
		hook := hook
		if err := host.HookBus().Register(plugin.RegisteredHook{
			PluginID:      "com.example.affect",
			Authorizer:    plugin.NewAuthorizer(plugin.Manifest{Capabilities: []plugin.Capability{plugin.CapabilityAgentAffectObserve}}),
			Hook:          hook,
			Mode:          plugin.HookModeObserve,
			FailurePolicy: plugin.FailurePolicyFailOpen,
			TimeoutMS:     50,
			Handler: func(context.Context, plugin.HookContext) (plugin.HookResult, error) {
				counts[hook]++
				return plugin.HookResult{}, nil
			},
		}); err != nil {
			t.Fatalf("Register %s: %v", hook, err)
		}
	}

	runtime := hookedAgentAffectRuntime{inner: fakeAgentAffectService{}, plugins: &PluginService{host: host}}
	_, err := runtime.SubmitMoodImpact(context.Background(), agentaffect.SubmitMoodImpactRequest{
		PersonaID:  "default",
		SessionID:  "session-1",
		TurnID:     "turn-1",
		Trigger:    agentaffect.TriggerDescriptor{TriggerType: "user_message"},
		CommitMode: agentaffect.CommitModeCommitIfAllowed,
	})
	if err != nil {
		t.Fatalf("SubmitMoodImpact: %v", err)
	}
	for _, hook := range []plugin.HookName{
		plugin.HookBeforeAgentAffectEvaluate,
		plugin.HookAfterAgentAffectEvaluate,
		plugin.HookBeforeAgentAffectCommit,
		plugin.HookAfterAgentAffectCommit,
	} {
		if counts[hook] != 1 {
			t.Fatalf("%s count = %d, want 1 (all=%#v)", hook, counts[hook], counts)
		}
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

	t.Run("natural memory route dispatches", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/memory/natural-runs", strings.NewReader(`{"mode":"manual","dry_run":true}`))
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("natural memory latest route dispatches", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/memory/natural-runs/latest", nil)
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

	t.Run("agent affect debug routes dispatch", func(t *testing.T) {
		cases := []struct {
			method string
			path   string
			body   string
		}{
			{http.MethodGet, "/api/agent-affect/config", ""},
			{http.MethodPut, "/api/agent-affect/config", `{"agent_affect":{"storage_enabled":true}}`},
			{http.MethodGet, "/api/agent-affect/profile?persona_id=default", ""},
			{http.MethodPut, "/api/agent-affect/profile", `{"persona_id":"default","profile_name":"default","baseline":{"warmth":0.7}}`},
			{http.MethodGet, "/api/agent-affect/history?persona_id=default&session_id=s1", ""},
			{http.MethodGet, "/api/agent-affect/plugin-writes?plugin_id=demo", ""},
			{http.MethodGet, "/api/agent-affect/current?persona_id=default", ""},
			{http.MethodPost, "/api/agent-affect/evaluate", `{"persona_id":"default","trigger":{"trigger_type":"debug"},"input":{"mode":"summary","summary":"x"}}`},
			{http.MethodPost, "/api/agent-affect/submit", `{"persona_id":"default","trigger":{"trigger_type":"debug"},"input":{"mode":"summary","summary":"x"}}`},
			{http.MethodPost, "/api/agent-affect/delta", `{"persona_id":"default","trigger":{"trigger_type":"debug"},"delta":{"warmth":0.1}}`},
			{http.MethodPost, "/api/agent-affect/reset", `{"persona_id":"default","reason":"smoke"}`},
			{http.MethodPost, "/api/agent-affect/prompt-preview", `{"persona_id":"default"}`},
		}
		for _, tc := range cases {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("%s %s status = %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
			}
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
	a := newTestApp(&config.Config{Chat: config.ChatConfig{
		RealtimeStreaming: false,
		TurnPipeline:      config.TurnPipelineConfig{Shadow: true, Enabled: true, MemoryStages: true, ApprovalStages: true},
	}}, db, logger)
	a.kernel.Services.Chat.engine = engine

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
	if !testConfig(a).Chat.RealtimeStreaming {
		t.Fatal("Config.Chat.RealtimeStreaming = false, want true")
	}
	if !engine.RuntimeConfig().RealtimeStreaming {
		t.Fatal("engine realtime streaming = false, want true")
	}
	if got := testConfig(a).Chat.TurnPipeline; !got.Shadow || !got.Enabled || !got.MemoryStages || !got.ApprovalStages {
		t.Fatalf("turn pipeline config = %#v, want preserved", got)
	}
}

func TestUpdateAgentAffectConfigPersistsRuntimeSettingAndHotUpdatesEngine(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.AgentAffect.Enabled = false
	a := newTestApp(cfg, db, logger)
	engine := chat.NewEngine(chat.EngineConfig{
		DB:        db,
		Logger:    logger,
		Model:     "test-model",
		MaxTokens: 128,
	})
	a.kernel.Services.Chat.engine = engine
	if engineHasAgentAffectRuntime(engine) {
		t.Fatal("engine agent affect runtime exists before enabling config")
	}

	next := cfg.AgentAffect
	next.Enabled = true
	next.Evaluator.Mode = "disabled"
	next.Context.StoreRawInputs = false
	effective, err := a.UpdateAgentAffectConfig(context.Background(), next)
	if err != nil {
		t.Fatalf("UpdateAgentAffectConfig: %v", err)
	}

	if !effective.AgentAffect.Enabled || effective.AgentAffect.Context.StoreRawInputs {
		t.Fatalf("effective agent_affect = %#v", effective.AgentAffect)
	}
	settings, err := db.ListRuntimeSettings()
	if err != nil {
		t.Fatalf("ListRuntimeSettings: %v", err)
	}
	if len(settings) != 1 || settings[0].Namespace != "agent_affect" || settings[0].Key != "config" {
		t.Fatalf("runtime settings = %#v", settings)
	}
	if !testConfig(a).AgentAffect.Enabled || testConfig(a).AgentAffect.Context.StoreRawInputs {
		t.Fatalf("app config agent_affect = %#v", testConfig(a).AgentAffect)
	}
	if !engineHasAgentAffectRuntime(engine) {
		t.Fatal("engine agent affect runtime was not hot-updated")
	}
}

func engineHasAgentAffectRuntime(engine *chat.Engine) bool {
	field := reflect.ValueOf(engine).Elem().FieldByName("agentAffect")
	return field.IsValid() && !field.IsNil()
}

func TestConfigurePluginHostHonorsEnabledConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.DefaultConfig()
	cfg.Plugins.Enabled = true
	cfg.Plugins.BuiltinEnabled = []string{plugin.TurnAuditPluginID}
	cfg.Chat.TurnPipeline.Enabled = true
	cfg.Chat.TurnPipeline.RolloutPercent = 100
	a := newTestApp(cfg, nil, logger)
	setTestToolRegistry(a, tool.NewRegistry())
	dispatcher := tool.NewDispatcher(a.kernel.Services.Tools.Registry(), tool.MinimalSchemaValidator{}, logger)

	if err := a.kernel.Services.Plugins.Configure(context.Background(), dispatcher, nil); err != nil {
		t.Fatalf("configurePluginHost: %v", err)
	}
	if testPluginHost(a) == nil || !testPluginHost(a).Enabled() {
		t.Fatalf("PluginHost = %#v, want enabled host", testPluginHost(a))
	}

	disabled := newTestApp(config.DefaultConfig(), nil, logger)
	setTestToolRegistry(disabled, tool.NewRegistry())
	dispatcher = tool.NewDispatcher(disabled.kernel.Services.Tools.Registry(), tool.MinimalSchemaValidator{}, logger)
	if err := disabled.kernel.Services.Plugins.Configure(context.Background(), dispatcher, nil); err != nil {
		t.Fatalf("disabled configurePluginHost: %v", err)
	}
	if testPluginHost(disabled) != nil {
		t.Fatalf("disabled PluginHost = %#v, want nil", testPluginHost(disabled))
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
	a := newTestApp(cfg, db, logger)

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
	a := newTestApp(cfg, db, logger)

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
	a := newTestApp(cfg, db, logger)

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
	a := newTestApp(config.DefaultConfig(), db, logger)
	a.kernel.Services.Chat.engine = engine
	setTestPersonas(a, map[string]*config.Persona{"default": {Name: "Default"}})

	if err := a.ActivateAgentConfig("default"); err != nil {
		t.Fatalf("ActivateAgentConfig: %v", err)
	}
	if got := a.GetDefaultPersonaName(); got != "default" {
		t.Fatalf("GetDefaultPersonaName = %q, want default", got)
	}
	if runtime := testActiveRuntime(a); runtime == nil || runtime.WorkSummary.Model != "work-summary" {
		t.Fatalf("ActiveAgentRuntime = %#v", runtime)
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

	a := newTestApp(&config.Config{
		Personas: config.PersonasConfig{Dir: t.TempDir()},
	}, db, logger)
	setTestPersonas(a, map[string]*config.Persona{})

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

	a := newTestApp(&config.Config{
		Personas: config.PersonasConfig{Dir: personaDir},
	}, db, logger)
	setTestPersonas(a, map[string]*config.Persona{
		"neko": {Name: "Tami", Description: "cat roommate"},
	})

	personas := a.ListPersonas()
	if err := config.SavePersona(personaDir, "neko", personas["neko"]); err != nil {
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
	a := newTestApp(nil, nil, nil)
	setTestPersonas(a, map[string]*config.Persona{
		"default": {
			Name: "default",
			WorkProgressPhrases: map[string][]string{
				"read_file": {"看看文件"},
			},
		},
	})

	persona, ok := a.GetPersona("default")
	if !ok || persona == nil {
		t.Fatal("GetPersona returned nil")
	}
	persona.WorkProgressPhrases["read_file"][0] = "mutated"
	persona.WorkProgressPhrases["new_key"] = []string{"new"}

	original := a.ListPersonas()["default"].WorkProgressPhrases
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

	a := newTestApp(&config.Config{
		Personas: config.PersonasConfig{Dir: personaDir},
	}, db, logger)
	setTestPersonas(a, map[string]*config.Persona{
		"default": initial,
	})

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

	if got := a.ListPersonas()["default"].WorkProgressPhrases["read_file"]; len(got) != 1 || got[0] != "看看文件" {
		t.Fatalf("memory phrases = %#v, want read_file phrase", a.ListPersonas()["default"].WorkProgressPhrases)
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

	a := newTestApp(nil, db, logger)

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

	a := newTestApp(nil, db, logger)
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

func TestOpenSessionMediaRequiresLinkedVisibleImage(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.Open(filepath.Join(t.TempDir(), "app.db"), logger)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := config.DefaultConfig()
	cfg.Media.StorageDir = filepath.Join(t.TempDir(), "media")
	a := newTestApp(cfg, db, logger)
	ctx := context.Background()
	asset, err := a.UploadMedia(ctx, bytes.NewReader(appTinyPNG()), media.UploadMeta{OriginalFilename: "tiny.png", CreatedByRole: "user"})
	if err != nil {
		t.Fatalf("UploadMedia: %v", err)
	}
	if err := db.CreateSession(ctx, "session-1", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := db.AddMessage(ctx, "msg-1", "session-1", "user", "look\n[used image]"); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if err := db.AddMessageParts(ctx, []storage.MessagePartRecord{
		{ID: "part-1", SessionID: "session-1", MessageID: "msg-1", Role: "user", Ordinal: 0, PartType: "text", TextContent: "look"},
		{ID: "part-2", SessionID: "session-1", MessageID: "msg-1", Role: "user", Ordinal: 1, PartType: "image", MediaAssetID: asset.ID},
	}); err != nil {
		t.Fatalf("AddMessageParts: %v", err)
	}

	rc, opened, err := a.OpenSessionMedia(ctx, "session-1", asset.ID)
	if err != nil {
		t.Fatalf("OpenSessionMedia(linked): %v", err)
	}
	body, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if opened == nil || opened.ID != asset.ID || opened.MimeType != "image/png" || len(body) == 0 {
		t.Fatalf("opened asset = %#v bytes=%d, want linked PNG", opened, len(body))
	}

	if _, _, err := a.OpenSessionMedia(ctx, "session-2", asset.ID); !errors.Is(err, ErrMediaNotFound) {
		t.Fatalf("OpenSessionMedia(unlinked) error = %v, want ErrMediaNotFound", err)
	}
	if err := a.kernel.Services.Media.Store().MarkPurged(ctx, asset.ID, "test"); err != nil {
		t.Fatalf("MarkPurged: %v", err)
	}
	if _, _, err := a.OpenSessionMedia(ctx, "session-1", asset.ID); !errors.Is(err, ErrMediaNotFound) {
		t.Fatalf("OpenSessionMedia(purged) error = %v, want ErrMediaNotFound", err)
	}
}

func appTinyPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}
}
