package configcenter

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	sidecarruntime "github.com/longyisang/emoagent/internal/sidecar"
	"github.com/longyisang/emoagent/internal/storage"
)

func TestBuildEffectiveIncludesProviderEnvStatusAndIssues(t *testing.T) {
	db := openConfigCenterDB(t)
	if err := db.UpsertLLMProvider(config.LLMProvider{
		ID:        "moonshot",
		Name:      "Moonshot",
		Protocol:  "openai_compatible",
		BaseURL:   "https://api.moonshot.cn/v1",
		APIKeyEnv: "MOONSHOT_API_KEY",
		Enabled:   true,
	}); err != nil {
		t.Fatalf("UpsertLLMProvider: %v", err)
	}
	if err := db.UpsertRuntimeSetting("memory.sidecar", "managed", `{"enabled":true}`, "ui"); err != nil {
		t.Fatalf("UpsertRuntimeSetting: %v", err)
	}

	seed := config.DefaultConfig()
	seed.Memory.Enabled = false
	seed.Memory.Retrieval.Enabled = true
	seed.Memory.Retrieval.UseMirror = true

	svc := NewService(seed, db)
	svc.EnvLookup = func(name string) (string, bool) {
		if name == "MOONSHOT_API_KEY" {
			return "secret-value", true
		}
		return "", false
	}

	effective, err := svc.BuildEffective(context.Background())
	if err != nil {
		t.Fatalf("BuildEffective: %v", err)
	}

	if len(effective.Providers) != 1 {
		t.Fatalf("len(Providers) = %d, want 1", len(effective.Providers))
	}
	provider := effective.Providers[0]
	if provider.ID != "moonshot" || provider.Env.APIKeyEnv != "MOONSHOT_API_KEY" || !provider.Env.Present {
		t.Fatalf("provider = %#v", provider)
	}
	if len(effective.RuntimeSettings) != 1 || effective.RuntimeSettings[0].Namespace != "memory.sidecar" {
		t.Fatalf("runtime settings = %#v", effective.RuntimeSettings)
	}
	requireConfigIssue(t, effective.Issues, "memory.retrieval.enabled")
	requireConfigIssue(t, effective.Issues, "memory.retrieval.use_mirror")

	payload, err := json.Marshal(effective)
	if err != nil {
		t.Fatalf("Marshal effective: %v", err)
	}
	if strings.Contains(string(payload), "secret-value") {
		t.Fatalf("effective config leaked API key value: %s", payload)
	}
}

func TestBuildEffectiveIncludesRootAgentAffect(t *testing.T) {
	db := openConfigCenterDB(t)
	if err := db.UpsertRuntimeSetting("agent_affect", "config", `{"enabled":true,"evaluator":{"mode":"disabled"},"context":{"store_raw_inputs":false}}`, "ui"); err != nil {
		t.Fatalf("UpsertRuntimeSetting: %v", err)
	}

	svc := NewService(config.DefaultConfig(), db)
	effective, err := svc.BuildEffective(context.Background())
	if err != nil {
		t.Fatalf("BuildEffective: %v", err)
	}

	if !effective.AgentAffect.Enabled {
		t.Fatalf("effective agent_affect.enabled = false, want true")
	}
	if effective.AgentAffect.Evaluator.Mode != "disabled" {
		t.Fatalf("effective agent_affect evaluator = %#v", effective.AgentAffect.Evaluator)
	}
	payload, err := json.Marshal(effective)
	if err != nil {
		t.Fatalf("Marshal effective: %v", err)
	}
	if !strings.Contains(string(payload), `"agent_affect"`) {
		t.Fatalf("effective config missing root agent_affect: %s", payload)
	}
}

func TestBuildEffectiveReportsMissingProviderEnv(t *testing.T) {
	db := openConfigCenterDB(t)
	if err := db.UpsertLLMProvider(config.LLMProvider{
		ID:        "deepseek",
		Name:      "DeepSeek",
		Protocol:  "openai_compatible",
		BaseURL:   "https://api.deepseek.com",
		APIKeyEnv: "DEEPSEEK_API_KEY",
		Enabled:   true,
	}); err != nil {
		t.Fatalf("UpsertLLMProvider: %v", err)
	}

	svc := NewService(config.DefaultConfig(), db)
	svc.EnvLookup = func(string) (string, bool) { return "", false }

	effective, err := svc.BuildEffective(context.Background())
	if err != nil {
		t.Fatalf("BuildEffective: %v", err)
	}

	requireConfigIssue(t, effective.Issues, "providers.deepseek.api_key_env")
}

func TestBuildEffectiveMergesRuntimeSettingsIntoMemoryCoreAndSidecar(t *testing.T) {
	db := openConfigCenterDB(t)
	for _, provider := range []config.LLMProvider{
		{
			ID:           "moonshot",
			Name:         "Moonshot",
			Protocol:     "openai_compatible",
			BaseURL:      "https://api.moonshot.cn/v1",
			APIKeyEnv:    "MOONSHOT_API_KEY",
			Enabled:      true,
			Capabilities: []string{"chat"},
		},
		{
			ID:           "dashscope_embedding",
			Name:         "DashScope Embedding",
			Protocol:     "openai_compatible",
			BaseURL:      "https://dashscope.aliyuncs.com/compatible-mode/v1",
			APIKeyEnv:    "DASHSCOPE_API_KEY",
			Enabled:      true,
			Capabilities: []string{"embedding"},
		},
	} {
		if err := db.UpsertLLMProvider(provider); err != nil {
			t.Fatalf("UpsertLLMProvider(%s): %v", provider.ID, err)
		}
	}
	memoryCorePath := writeConfigCenterMemoryCoreConfig(t)
	if err := db.UpsertRuntimeSetting("memory.retrieval", "final_memory_count", `9`, "ui"); err != nil {
		t.Fatalf("UpsertRuntimeSetting retrieval: %v", err)
	}
	if err := db.UpsertRuntimeSetting("memory.sidecar", "config", `{"enabled":true,"managed":true,"adapter":"trivium","host":"127.0.0.1","port":8765,"url":"http://127.0.0.1:8765","config_path":"./data/runtime/sidecar.generated.toml"}`, "ui"); err != nil {
		t.Fatalf("UpsertRuntimeSetting sidecar: %v", err)
	}
	if err := db.UpsertRuntimeSetting("memory.provider_bindings", "config", `{"prefilter":{"provider_id":"moonshot","model":"prefilter-model","max_tokens":1201},"extraction":{"provider_id":"moonshot","model":"memory-model","max_tokens":1202,"thinking":{"type":"enabled"}},"extraction_repair":{"provider_id":"moonshot","model":"repair-model","max_tokens":1203},"query_analysis":{"provider_id":"moonshot","model":"analysis-model","max_tokens":1204},"curation":{"provider_id":"moonshot","model":"curation-model","max_tokens":1205,"thinking":{"type":"enabled"}},"embedding":{"enabled":true,"provider_id":"dashscope_embedding","model":"text-embedding-v4","dimensions":1536}}`, "ui"); err != nil {
		t.Fatalf("UpsertRuntimeSetting bindings: %v", err)
	}

	seed := config.DefaultConfig()
	seed.Memory.Enabled = true
	seed.Memory.ConfigPath = memoryCorePath
	svc := NewService(seed, db)
	svc.EnvLookup = func(name string) (string, bool) {
		return "present", true
	}

	effective, err := svc.BuildEffective(context.Background())
	if err != nil {
		t.Fatalf("BuildEffective: %v", err)
	}

	if effective.Memory.Retrieval.FinalMemoryCount != 9 {
		t.Fatalf("effective memory final_memory_count = %d, want 9", effective.Memory.Retrieval.FinalMemoryCount)
	}
	if effective.MemoryCore == nil {
		t.Fatal("MemoryCore effective is nil")
	}
	if effective.MemoryCore.Retrieval.FinalMemoryCount != 9 {
		t.Fatalf("memorycore final_memory_count = %d, want 9", effective.MemoryCore.Retrieval.FinalMemoryCount)
	}
	if effective.MemoryCore.Retrieval.SensitivityPermission != "normal" ||
		effective.MemoryCore.Retrieval.AllowHistorical ||
		effective.MemoryCore.Retrieval.AllowDeepArchive {
		t.Fatalf("memorycore retrieval archive/sensitivity policy = %#v", effective.MemoryCore.Retrieval)
	}
	if effective.MemoryCore.Pipelines.Extraction.ProviderID != "moonshot" || effective.MemoryCore.Pipelines.Extraction.Model != "memory-model" {
		t.Fatalf("extraction pipeline = %#v", effective.MemoryCore.Pipelines.Extraction)
	}
	if effective.MemoryCore.Pipelines.Extraction.Thinking.Type != "enabled" {
		t.Fatalf("extraction thinking = %#v", effective.MemoryCore.Pipelines.Extraction.Thinking)
	}
	if effective.MemoryCore.Pipelines.Extraction.Params.MaxOutputTokens != 1202 {
		t.Fatalf("extraction max output tokens = %d, want 1202", effective.MemoryCore.Pipelines.Extraction.Params.MaxOutputTokens)
	}
	if effective.MemoryCore.Pipelines.Prefilter.Model != "prefilter-model" {
		t.Fatalf("prefilter pipeline = %#v", effective.MemoryCore.Pipelines.Prefilter)
	}
	if effective.MemoryCore.Pipelines.Prefilter.Params.MaxOutputTokens != 1201 {
		t.Fatalf("prefilter max output tokens = %d, want 1201", effective.MemoryCore.Pipelines.Prefilter.Params.MaxOutputTokens)
	}
	if effective.MemoryCore.Pipelines.ExtractionRepair.Model != "repair-model" {
		t.Fatalf("extraction repair pipeline = %#v", effective.MemoryCore.Pipelines.ExtractionRepair)
	}
	if effective.MemoryCore.Pipelines.ExtractionRepair.Params.MaxOutputTokens != 1203 {
		t.Fatalf("extraction repair max output tokens = %d, want 1203", effective.MemoryCore.Pipelines.ExtractionRepair.Params.MaxOutputTokens)
	}
	if effective.MemoryCore.Pipelines.QueryAnalysis.Model != "analysis-model" {
		t.Fatalf("query analysis pipeline = %#v", effective.MemoryCore.Pipelines.QueryAnalysis)
	}
	if effective.MemoryCore.Pipelines.QueryAnalysis.Params.MaxOutputTokens != 1204 {
		t.Fatalf("query analysis max output tokens = %d, want 1204", effective.MemoryCore.Pipelines.QueryAnalysis.Params.MaxOutputTokens)
	}
	if effective.MemoryCore.SemanticOps.Curation.LLM.Model != "curation-model" {
		t.Fatalf("curation llm = %#v", effective.MemoryCore.SemanticOps.Curation.LLM)
	}
	if effective.MemoryCore.SemanticOps.Curation.LLM.MaxTokens != 1205 {
		t.Fatalf("curation max tokens = %d, want 1205", effective.MemoryCore.SemanticOps.Curation.LLM.MaxTokens)
	}
	if effective.MemoryCore.SemanticOps.Curation.LLM.Thinking.Type != "enabled" {
		t.Fatalf("curation thinking = %#v", effective.MemoryCore.SemanticOps.Curation.LLM.Thinking)
	}
	if effective.MemoryCore.NaturalMemory.SleepCycle.LocalTime != "03:30" {
		t.Fatalf("natural memory local time = %q, want 03:30", effective.MemoryCore.NaturalMemory.SleepCycle.LocalTime)
	}
	for _, want := range []string{
		`provider = "openai-compatible"`,
		`base_url = "https://dashscope.aliyuncs.com/compatible-mode/v1"`,
		`api_key_env = "DASHSCOPE_API_KEY"`,
		`model = "text-embedding-v4"`,
		`dimensions = 1536`,
		`max_tokens = 1204`,
	} {
		if !strings.Contains(effective.SidecarGeneratedConfig, want) {
			t.Fatalf("generated sidecar TOML missing %q:\n%s", want, effective.SidecarGeneratedConfig)
		}
	}
}

func TestBuildMemoryCoreOpenConfigDegradedSidecarOnlyDisablesMirror(t *testing.T) {
	db := openConfigCenterDB(t)
	if err := db.UpsertLLMProvider(config.LLMProvider{
		ID:        "llm_main",
		Name:      "LLM Main",
		Protocol:  "openai_compatible",
		BaseURL:   "https://api.example.test/v1",
		APIKeyEnv: "LLM_MAIN_API_KEY",
		Enabled:   true,
	}); err != nil {
		t.Fatalf("UpsertLLMProvider: %v", err)
	}
	seed := config.DefaultConfig()
	seed.Memory.Enabled = true
	seed.Memory.ConfigPath = writeConfigCenterMemoryCoreConfig(t)
	seed.Memory.Retrieval.FinalMemoryCount = 11
	seed.Memory.Retrieval.ContextBudgetTokens = 2222
	seed.Memory.Retrieval.UseFTS = true
	seed.Memory.Retrieval.UseMirror = true
	seed.Memory.Sidecar.Enabled = true

	svc := NewService(seed, db)
	status := sidecarruntime.Status{
		State:   sidecarruntime.StateDegraded,
		Managed: false,
		URL:     "http://127.0.0.1:8765",
		Adapter: "trivium",
		Error:   "health check failed",
	}

	openCfg, err := svc.BuildMemoryCoreOpenConfig(context.Background(), &status)
	if err != nil {
		t.Fatalf("BuildMemoryCoreOpenConfig: %v", err)
	}
	if openCfg.MemoryCore == nil {
		t.Fatalf("MemoryCore effective is nil; issues = %#v", openCfg.Issues)
	}
	retrieval := openCfg.MemoryCore.Retrieval
	if retrieval.FinalMemoryCount != 11 || retrieval.ContextBudgetTokens != 2222 || !retrieval.UseFTS {
		t.Fatalf("retrieval policy lost non-mirror overrides after degradation: %#v", retrieval)
	}
	if retrieval.UseMirror {
		t.Fatalf("retrieval use_mirror = true, want false after degraded sidecar")
	}
	if openCfg.MemoryCore.Sidecar.Enabled || openCfg.MemoryCore.Mirror.Enabled {
		t.Fatalf("sidecar/mirror effective = %#v/%#v, want both disabled", openCfg.MemoryCore.Sidecar, openCfg.MemoryCore.Mirror)
	}
}

func TestBuildMemoryCoreOpenConfigCarriesNaturalMemoryRuntimeOverrides(t *testing.T) {
	db := openConfigCenterDB(t)
	seed := config.DefaultConfig()
	seed.Memory.Enabled = true
	seed.Memory.ConfigPath = writeConfigCenterMemoryCoreConfig(t)
	if err := db.UpsertRuntimeSetting("memory.natural_memory", "config", `{"enabled":true,"local_time":"04:10","timezone":"UTC","run_missed_on_start":true,"manual":{"enabled":true,"allow_dry_run":true,"allow_force":false,"mark_sleep_cycle_by_default":true}}`, "ui"); err != nil {
		t.Fatalf("UpsertRuntimeSetting natural_memory: %v", err)
	}

	svc := NewService(seed, db)
	openCfg, err := svc.BuildMemoryCoreOpenConfig(context.Background(), nil)
	if err != nil {
		t.Fatalf("BuildMemoryCoreOpenConfig: %v", err)
	}
	if !openCfg.NaturalMemory.Configured || !openCfg.NaturalMemory.Enabled {
		t.Fatalf("natural memory overrides = %#v, want configured enabled", openCfg.NaturalMemory)
	}
	if openCfg.NaturalMemory.LocalTime != "04:10" || openCfg.NaturalMemory.Timezone != "UTC" || !openCfg.NaturalMemory.RunMissedOnStart {
		t.Fatalf("natural memory schedule overrides = %#v", openCfg.NaturalMemory)
	}
	if !openCfg.NaturalMemory.ManualEnabled || !openCfg.NaturalMemory.AllowDryRun || openCfg.NaturalMemory.AllowForce || !openCfg.NaturalMemory.MarkSleepCycleByDefault {
		t.Fatalf("natural memory manual overrides = %#v", openCfg.NaturalMemory)
	}
}

func TestUpdateMemoryConfigRejectsInvalidActiveSidecar(t *testing.T) {
	db := openConfigCenterDB(t)
	seed := config.DefaultConfig()
	seed.Memory.ConfigPath = writeConfigCenterMemoryCoreConfig(t)
	svc := NewService(seed, db)

	next := seed.Memory
	next.Sidecar.Enabled = true
	next.Sidecar.Managed = true
	_, err := svc.UpdateMemoryConfig(context.Background(), next)
	if err == nil {
		t.Fatal("UpdateMemoryConfig succeeded, want validation error")
	}
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %T %v, want ValidationError", err, err)
	}
	requireConfigIssue(t, validation.Issues, "memory.provider_bindings.embedding.provider_id")
}

func TestUpdateMemoryConfigRejectsAutoFixDependencyIssue(t *testing.T) {
	db := openConfigCenterDB(t)
	seed := config.DefaultConfig()
	svc := NewService(seed, db)

	next := seed.Memory
	next.Enabled = false
	next.Retrieval.Enabled = true
	_, err := svc.UpdateMemoryConfig(context.Background(), next)
	if err == nil {
		t.Fatal("UpdateMemoryConfig succeeded, want validation error")
	}
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %T %v, want ValidationError", err, err)
	}
	requireConfigIssue(t, validation.Issues, "memory.retrieval.enabled")
}

func TestUpdateAgentAffectConfigPersistsRuntimeSetting(t *testing.T) {
	db := openConfigCenterDB(t)
	svc := NewService(config.DefaultConfig(), db)
	next := config.DefaultConfig().AgentAffect
	next.Enabled = true
	next.Evaluator.Mode = "disabled"
	next.Context.StoreRawInputs = false

	effective, err := svc.UpdateAgentAffectConfig(context.Background(), next)
	if err != nil {
		t.Fatalf("UpdateAgentAffectConfig: %v", err)
	}
	if !effective.AgentAffect.Enabled || effective.AgentAffect.Evaluator.Mode != "disabled" || effective.AgentAffect.Context.StoreRawInputs {
		t.Fatalf("effective agent_affect = %#v", effective.AgentAffect)
	}
	settings, err := db.ListRuntimeSettings()
	if err != nil {
		t.Fatalf("ListRuntimeSettings: %v", err)
	}
	if len(settings) != 1 || settings[0].Namespace != "agent_affect" || settings[0].Key != "config" {
		t.Fatalf("runtime settings = %#v", settings)
	}
}

func TestUpdateAgentAffectConfigRejectsInvalidConfig(t *testing.T) {
	db := openConfigCenterDB(t)
	svc := NewService(config.DefaultConfig(), db)
	next := config.DefaultConfig().AgentAffect
	next.Enabled = true
	next.StorageEnabled = false

	_, err := svc.UpdateAgentAffectConfig(context.Background(), next)
	if err == nil {
		t.Fatal("UpdateAgentAffectConfig succeeded, want validation error")
	}
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %T %v, want ValidationError", err, err)
	}
	requireConfigIssue(t, validation.Issues, "agent_affect.storage_enabled")
}

func requireConfigIssue(t *testing.T, issues []ConfigIssue, path string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Path == path {
			if issue.Severity == "" || issue.Message == "" {
				t.Fatalf("issue %s missing severity/message: %#v", path, issue)
			}
			return
		}
	}
	t.Fatalf("issue %q not found in %#v", path, issues)
}

func openConfigCenterDB(t *testing.T) *storage.DB {
	t.Helper()

	db, err := storage.Open(filepath.Join(t.TempDir(), "emo.db"), slog.Default())
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func writeConfigCenterMemoryCoreConfig(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "memorycore.yaml")
	body := `
schema_version: memorycore.config.v0.2
enabled: true
core:
  db_path: ./data/memory.db
  persona_id: default
  auto_migrate: true
  enable_fts: true
pipelines:
  extraction:
    enabled: true
    provider_id: llm_main
    model: old-model
retrieval:
  final_memory_count: 4
  context_budget_tokens: 700
  use_fts: true
  use_mirror: false
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile memorycore config: %v", err)
	}
	return path
}
