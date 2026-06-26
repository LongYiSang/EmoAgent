package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Server.Port != 8080 {
		t.Errorf("default port = %d, want 8080", cfg.Server.Port)
	}
	if len(cfg.LLMProviders) != 0 {
		t.Errorf("default llm_providers length = %d, want 0", len(cfg.LLMProviders))
	}
	if len(cfg.AgentConfigs) != 0 {
		t.Errorf("default agent_configs length = %d, want 0", len(cfg.AgentConfigs))
	}
	if cfg.Chat.RealtimeStreaming {
		t.Error("default chat.realtime_streaming = true, want false")
	}
	if cfg.Chat.TurnPipeline.Shadow {
		t.Error("default chat.turn_pipeline.shadow = true, want false")
	}
	if cfg.Chat.TurnPipeline.Enabled {
		t.Error("default chat.turn_pipeline.enabled = true, want false")
	}
	if cfg.Chat.TurnPipeline.MemoryStages {
		t.Error("default chat.turn_pipeline.memory_stages = true, want false")
	}
	if cfg.Chat.TurnPipeline.ApprovalStages {
		t.Error("default chat.turn_pipeline.approval_stages = true, want false")
	}
	if cfg.Chat.TurnPipeline.RolloutPercent != 0 {
		t.Errorf("default chat.turn_pipeline.rollout_percent = %d, want 0", cfg.Chat.TurnPipeline.RolloutPercent)
	}
	if cfg.Chat.TurnPipeline.Journal.Mode != "sqlite" {
		t.Errorf("default chat.turn_pipeline.journal.mode = %q, want sqlite", cfg.Chat.TurnPipeline.Journal.Mode)
	}
	if cfg.Chat.TurnPipeline.Journal.JSONLDir != "./logs/turns" {
		t.Errorf("default chat.turn_pipeline.journal.jsonl_dir = %q, want ./logs/turns", cfg.Chat.TurnPipeline.Journal.JSONLDir)
	}
	if cfg.Chat.TurnPipeline.Journal.FailClosed {
		t.Error("default chat.turn_pipeline.journal.fail_closed = true, want false")
	}
	if cfg.Chat.TurnPipeline.Idempotency.Mode != "sqlite" {
		t.Errorf("default chat.turn_pipeline.idempotency.mode = %q, want sqlite", cfg.Chat.TurnPipeline.Idempotency.Mode)
	}
	if cfg.Chat.TurnPipeline.Idempotency.DuplicateDone != "replay_summary" {
		t.Errorf("default duplicate_done = %q, want replay_summary", cfg.Chat.TurnPipeline.Idempotency.DuplicateDone)
	}
	if cfg.Chat.TurnPipeline.Idempotency.DuplicateRunning != "busy" {
		t.Errorf("default duplicate_running = %q, want busy", cfg.Chat.TurnPipeline.Idempotency.DuplicateRunning)
	}
	if !cfg.PromptCenter.Snapshots.Enabled {
		t.Error("default prompt_center.snapshots.enabled = false, want true")
	}
	if !cfg.PromptCenter.Snapshots.StoreRenderedText {
		t.Error("default prompt_center.snapshots.store_rendered_text = false, want true")
	}
	if cfg.PromptCenter.Snapshots.MaxRenderedTextChars != 50000 {
		t.Errorf("default prompt_center.snapshots.max_rendered_text_chars = %d, want 50000", cfg.PromptCenter.Snapshots.MaxRenderedTextChars)
	}
	if cfg.PromptCenter.Snapshots.RetentionDays != 30 {
		t.Errorf("default prompt_center.snapshots.retention_days = %d, want 30", cfg.PromptCenter.Snapshots.RetentionDays)
	}
	if cfg.PromptCenter.Snapshots.MaxRows != 1000 {
		t.Errorf("default prompt_center.snapshots.max_rows = %d, want 1000", cfg.PromptCenter.Snapshots.MaxRows)
	}
	if cfg.Plugins.Enabled {
		t.Error("default plugins.enabled = true, want false")
	}
	if cfg.Plugins.DefaultTimeoutMS != 80 {
		t.Errorf("default plugins.default_timeout_ms = %d, want 80", cfg.Plugins.DefaultTimeoutMS)
	}
	if cfg.Plugins.MaxTimeoutMS != 1000 {
		t.Errorf("default plugins.max_timeout_ms = %d, want 1000", cfg.Plugins.MaxTimeoutMS)
	}
	if len(cfg.Plugins.BuiltinEnabled) != 3 {
		t.Fatalf("default plugins.builtin_enabled = %#v, want 3 demo plugins", cfg.Plugins.BuiltinEnabled)
	}
	if !cfg.Plugins.Audit.Enabled {
		t.Error("default plugins.audit.enabled = false, want true")
	}
	if cfg.Plugins.Audit.IncludePayload {
		t.Error("default plugins.audit.include_payload = true, want false")
	}
	if cfg.Plugins.Store.RootDir != "data/plugins" || !cfg.Plugins.Store.AllowDevDirs {
		t.Fatalf("default plugins.store = %#v, want data/plugins with dev dirs", cfg.Plugins.Store)
	}
	if !cfg.Plugins.Runtime.ProcessEnabled ||
		cfg.Plugins.Runtime.PythonExecutable != "" ||
		cfg.Plugins.Runtime.PrivatePythonExecutable != "" ||
		cfg.Plugins.Runtime.StartupTimeoutMS != 5000 {
		t.Fatalf("default plugins.runtime = %#v", cfg.Plugins.Runtime)
	}
	if _, ok := reflect.TypeOf(*cfg).FieldByName("CapabilityRuntime"); ok {
		t.Fatal("default config still exposes legacy capability_runtime")
	}
	if cfg.HostResources.Enabled {
		t.Fatal("default host_resources.enabled = true, want false in phase 0")
	}
	if cfg.HostResources.DefaultProfile != "personal_read" || cfg.HostResources.StagingDir != "data/resource-staging" || cfg.HostResources.QuarantineDir != "data/resource-quarantine" {
		t.Fatalf("default host_resources = %#v", cfg.HostResources)
	}
	if cfg.Bash.ExecutionMode != "managed_host" || cfg.Bash.UnsafeHostExecEnabled || cfg.Bash.MaxProcesses != 64 || cfg.Bash.MemoryMB != 512 {
		t.Fatalf("default bash managed fields = %#v", cfg.Bash)
	}
	bashType := reflect.TypeOf(cfg.Bash)
	for _, field := range []string{"NetworkDefault", "LinuxDriver", "MacOSDriver", "WindowsDriver"} {
		if _, ok := bashType.FieldByName(field); ok {
			t.Fatalf("default BashConfig still exposes legacy %s", field)
		}
	}
	if cfg.Plugins.Runtime.DefaultKind != "managed_python_process" || cfg.Plugins.Runtime.ProcessDevEnabled ||
		cfg.Plugins.Runtime.MaxProcesses != 64 || cfg.Plugins.Runtime.MemoryMB != 1024 ||
		cfg.Plugins.Runtime.CrashQuarantineThreshold != 5 {
		t.Fatalf("default plugins.runtime kind/dev = %#v", cfg.Plugins.Runtime)
	}
	if cfg.Plugins.Policy.AllowActiveHooks {
		t.Fatalf("default plugins.policy.allow_active_hooks = true, want false")
	}
	runtimeType := reflect.TypeOf(cfg.Plugins.Runtime)
	for _, field := range []string{"ContainerEnabled", "SandboxEndpoint", "PreferRootless"} {
		if _, ok := runtimeType.FieldByName(field); ok {
			t.Fatalf("default PluginRuntimeConfig still exposes legacy %s", field)
		}
	}
	if !cfg.Plugins.Runtime.FailClosedIfUnavailable {
		t.Fatalf("default plugins.runtime should fail closed when runtime unavailable: %#v", cfg.Plugins.Runtime)
	}
	if cfg.PythonToolchain.Enabled ||
		cfg.PythonToolchain.PythonExecutable != "" ||
		cfg.PythonToolchain.UVExecutable != "" ||
		cfg.PythonToolchain.RequiredPython != "3.12" ||
		cfg.PythonToolchain.MinimumUVVersion != "0.11.0" ||
		cfg.PythonToolchain.EnvironmentRoot != "data/python-envs" ||
		cfg.PythonToolchain.CacheDir != "data/uv-cache" ||
		cfg.PythonToolchain.DefaultIndex != "https://pypi.org/simple" ||
		cfg.PythonToolchain.SyncTimeoutSeconds != 600 ||
		!cfg.PythonToolchain.UseSystemCertificates {
		t.Fatalf("default python_toolchain = %#v", cfg.PythonToolchain)
	}
	if !cfg.Plugins.Installer.GithubEnabled || !cfg.Plugins.Installer.RequireSignature || !cfg.Plugins.Installer.AllowUnsignedDev {
		t.Fatalf("default plugins.installer = %#v", cfg.Plugins.Installer)
	}
	if !cfg.Plugins.ProviderGateway.Enabled || !cfg.Plugins.Admin.Enabled {
		t.Fatalf("default plugins provider/admin = %#v / %#v", cfg.Plugins.ProviderGateway, cfg.Plugins.Admin)
	}
	if cfg.AgentAffect.Enabled {
		t.Error("default agent_affect.enabled = true, want false")
	}
	if !cfg.AgentAffect.StorageEnabled {
		t.Error("default agent_affect.storage_enabled = false, want true")
	}
	if cfg.AgentAffect.Evaluator.Mode != "llm" {
		t.Errorf("default agent_affect.evaluator.mode = %q, want llm", cfg.AgentAffect.Evaluator.Mode)
	}
	if cfg.AgentAffect.Evaluator.TimeoutMS != 30000 {
		t.Errorf("default agent_affect.evaluator.timeout_ms = %d, want 30000", cfg.AgentAffect.Evaluator.TimeoutMS)
	}
	if cfg.AgentAffect.Evaluator.StoreHiddenThinking {
		t.Error("default agent_affect.evaluator.store_hidden_thinking = true, want false")
	}
	if cfg.AgentAffect.UpdateMode != "async_after_reply" {
		t.Errorf("default agent_affect.update_mode = %q, want async_after_reply", cfg.AgentAffect.UpdateMode)
	}
	if cfg.AgentAffect.State.Scope != "persona" || cfg.AgentAffect.State.RecentContextScope != "persona" {
		t.Fatalf("default agent_affect.state = %#v, want persona scope", cfg.AgentAffect.State)
	}
	if !cfg.AgentAffect.Async.Enabled || !cfg.AgentAffect.Async.QueueEnabled || !cfg.AgentAffect.Async.WorkerEnabled {
		t.Fatalf("default agent_affect.async enabled flags = %#v, want enabled queue and worker", cfg.AgentAffect.Async)
	}
	if cfg.AgentAffect.Async.WorkerConcurrency != 1 || cfg.AgentAffect.Async.PollIntervalMS != 800 || cfg.AgentAffect.Async.QueueClaimTTLSeconds != 300 {
		t.Fatalf("default agent_affect.async worker settings = %#v", cfg.AgentAffect.Async)
	}
	if cfg.AgentAffect.Async.MaxAttempts != 3 || cfg.AgentAffect.Async.RetryBaseDelaySeconds != 30 || cfg.AgentAffect.Async.RetryMaxDelaySeconds != 900 {
		t.Fatalf("default agent_affect.async retry settings = %#v", cfg.AgentAffect.Async)
	}
	if !cfg.AgentAffect.Async.ClearRawAfterDone {
		t.Fatal("default agent_affect.async.clear_raw_after_done = false, want true")
	}
	if !cfg.AgentAffect.Async.Batch.Enabled || cfg.AgentAffect.Async.Batch.MaxJobs != 6 || cfg.AgentAffect.Async.Batch.MaxInputTokens != 12000 {
		t.Fatalf("default agent_affect.async.batch = %#v", cfg.AgentAffect.Async.Batch)
	}
	if cfg.AgentAffect.Async.Batch.MaxAgeSeconds != 300 || !cfg.AgentAffect.Async.Batch.MergeAcrossSessions || !cfg.AgentAffect.Async.Batch.BreakOnManualBarrier {
		t.Fatalf("default agent_affect.async.batch limits = %#v", cfg.AgentAffect.Async.Batch)
	}
	if cfg.AgentAffect.Context.Mode != "raw_window" {
		t.Errorf("default agent_affect.context.mode = %q, want raw_window", cfg.AgentAffect.Context.Mode)
	}
	if cfg.AgentAffect.Context.RawKeepLastRequests != 20 {
		t.Errorf("default agent_affect.context.raw_keep_last_requests = %d, want 20", cfg.AgentAffect.Context.RawKeepLastRequests)
	}
	if cfg.AgentAffect.Context.RawKeepLastTokens != 12000 {
		t.Errorf("default agent_affect.context.raw_keep_last_tokens = %d, want 12000", cfg.AgentAffect.Context.RawKeepLastTokens)
	}
	if cfg.AgentAffect.Context.IncludePreviousEvaluations {
		t.Error("default agent_affect.context.include_previous_evaluations = true, want false")
	}
	if cfg.AgentAffect.Context.SummaryEnabled {
		t.Error("default agent_affect.context.summary_enabled = true, want false")
	}
	if cfg.AgentAffect.Context.StoreRawInputs {
		t.Error("default agent_affect.context.store_raw_inputs = true, want false")
	}
	if cfg.AgentAffect.Context.StorePromptSnapshot {
		t.Error("default agent_affect.context.store_prompt_snapshot = true, want false")
	}
	if cfg.AgentAffect.Context.Strategy != "checkpoint_trace_v1" || cfg.AgentAffect.Context.MaxInputTokens != 2800 || cfg.AgentAffect.Context.MaxMemoryContextChars != 600 {
		t.Fatalf("default agent_affect.context budget = %#v", cfg.AgentAffect.Context)
	}
	if !cfg.AgentAffect.State.DecayEnabled || cfg.AgentAffect.State.DecayHalfLifeSeconds != 1800 || cfg.AgentAffect.State.CauseStackMaxItems != 5 {
		t.Fatalf("default agent_affect.state checkpoint settings = %#v", cfg.AgentAffect.State)
	}
	if cfg.AgentAffect.Evaluator.MaxOutputTokens != 512 || cfg.AgentAffect.Evaluator.Temperature != 0 || cfg.AgentAffect.Evaluator.ReasoningEffort != "minimal" {
		t.Fatalf("default agent_affect.evaluator cost settings = %#v", cfg.AgentAffect.Evaluator)
	}
	if !cfg.AgentAffect.Externalization.Attachment.Enabled {
		t.Error("default agent_affect.externalization.attachment.enabled = false, want true")
	}
	if cfg.AgentAffect.Externalization.Attachment.DefaultStyle != "gentle_explicit" {
		t.Errorf("default attachment style = %q, want gentle_explicit", cfg.AgentAffect.Externalization.Attachment.DefaultStyle)
	}
	if cfg.AgentAffect.Externalization.Attachment.MaxVisibleIntensity != 0.65 {
		t.Errorf("default attachment max visible intensity = %v, want 0.65", cfg.AgentAffect.Externalization.Attachment.MaxVisibleIntensity)
	}
	if cfg.AgentAffect.Externalization.Frustration.Enabled {
		t.Error("default agent_affect.externalization.frustration.enabled = true, want false")
	}
	if cfg.AgentAffect.Prompt.Mode != "natural_summary" {
		t.Fatalf("default agent_affect.prompt.mode = %q, want natural_summary", cfg.AgentAffect.Prompt.Mode)
	}
	if !cfg.AgentAffect.Prompt.IncludeMoodBlock || !cfg.AgentAffect.Prompt.IncludeReason {
		t.Fatalf("default agent_affect.prompt = %#v, want mood/reason enabled", cfg.AgentAffect.Prompt)
	}
	if cfg.AgentAffect.Prompt.IncludeNumericValues {
		t.Fatalf("default agent_affect.prompt.include_numeric_values = true, want false")
	}
	if cfg.AgentAffect.Prompt.MaxPromptChars != 240 {
		t.Fatalf("default agent_affect.prompt.max_prompt_chars = %d, want 240", cfg.AgentAffect.Prompt.MaxPromptChars)
	}
	if cfg.AgentAffect.Prompt.IncludeExpressionGuidance {
		t.Error("default agent_affect.prompt.include_expression_guidance = true, want false")
	}
	if cfg.Memory.Enabled {
		t.Error("default memory.enabled = true, want false")
	}
	if cfg.Memory.ConfigPath != "./config/memorycore.yaml" {
		t.Errorf("default memory.config_path = %q, want ./config/memorycore.yaml", cfg.Memory.ConfigPath)
	}
	if cfg.Memory.ManualRulesPath != "./config/memory_manual_rules.yaml" {
		t.Errorf("default memory.manual_rules_path = %q, want ./config/memory_manual_rules.yaml", cfg.Memory.ManualRulesPath)
	}
	if !cfg.Memory.Retrieval.Enabled {
		t.Error("default memory.retrieval.enabled = false, want true")
	}
	if cfg.Memory.Retrieval.InjectPrompt {
		t.Error("default memory.retrieval.inject_prompt = true, want false")
	}
	if !cfg.Memory.Retrieval.UseFTS {
		t.Error("default memory.retrieval.use_fts = false, want true")
	}
	if cfg.Memory.Retrieval.UseMirror {
		t.Error("default memory.retrieval.use_mirror = true, want false")
	}
	if cfg.Memory.Retrieval.FinalMemoryCount != 4 {
		t.Errorf("default memory.retrieval.final_memory_count = %d, want 4", cfg.Memory.Retrieval.FinalMemoryCount)
	}
	if cfg.Memory.Retrieval.ContextBudgetTokens != 700 {
		t.Errorf("default memory.retrieval.context_budget_tokens = %d, want 700", cfg.Memory.Retrieval.ContextBudgetTokens)
	}
	if !cfg.Memory.Retrieval.FailOpen {
		t.Error("default memory.retrieval.fail_open = false, want true")
	}
	if cfg.Memory.Retrieval.PipelineDebug {
		t.Error("default memory.retrieval.pipeline_debug = true, want false")
	}
	if cfg.Memory.Extraction.Enabled {
		t.Error("default memory.extraction.enabled = true, want false")
	}
	if cfg.Memory.Extraction.Mode != "dry_run" {
		t.Errorf("default memory.extraction.mode = %q, want dry_run", cfg.Memory.Extraction.Mode)
	}
	if !cfg.Memory.Extraction.TriggerOnFinalizeSegment {
		t.Error("default memory.extraction.trigger_on_finalize_segment = false, want true")
	}
	if !cfg.Memory.Extraction.TriggerOnManualPin {
		t.Error("default memory.extraction.trigger_on_manual_pin = false, want true")
	}
	if cfg.Memory.Extraction.ManualPinMode != "apply" {
		t.Errorf("default memory.extraction.manual_pin_mode = %q, want apply", cfg.Memory.Extraction.ManualPinMode)
	}
	if !cfg.Memory.Extraction.Async.Enabled {
		t.Error("default memory.extraction.async.enabled = false, want true")
	}
	if !cfg.Memory.Extraction.Async.WorkerEnabled {
		t.Error("default memory.extraction.async.worker_enabled = false, want true")
	}
	if cfg.Memory.Extraction.Async.WorkerConcurrency != 1 {
		t.Errorf("default memory.extraction.async.worker_concurrency = %d, want 1", cfg.Memory.Extraction.Async.WorkerConcurrency)
	}
	if cfg.Memory.Extraction.Async.QueueClaimTTLSeconds != 300 {
		t.Errorf("default memory.extraction.async.queue_claim_ttl_seconds = %d, want 300", cfg.Memory.Extraction.Async.QueueClaimTTLSeconds)
	}
	if cfg.Memory.Extraction.Async.MaxAttempts != 3 {
		t.Errorf("default memory.extraction.async.max_attempts = %d, want 3", cfg.Memory.Extraction.Async.MaxAttempts)
	}
	if !cfg.Memory.Extraction.Idle.Enabled {
		t.Error("default memory.extraction.idle.enabled = false, want true")
	}
	if cfg.Memory.Extraction.Idle.IdleAfterSeconds != 900 {
		t.Errorf("default memory.extraction.idle.idle_after_seconds = %d, want 900", cfg.Memory.Extraction.Idle.IdleAfterSeconds)
	}
	if cfg.Memory.Extraction.Idle.SweepIntervalSeconds != 60 {
		t.Errorf("default memory.extraction.idle.sweep_interval_seconds = %d, want 60", cfg.Memory.Extraction.Idle.SweepIntervalSeconds)
	}
	if cfg.Memory.Extraction.Idle.MinEpisodeCount != 2 {
		t.Errorf("default memory.extraction.idle.min_episode_count = %d, want 2", cfg.Memory.Extraction.Idle.MinEpisodeCount)
	}
	if !cfg.Memory.Extraction.Manual.Enabled {
		t.Error("default memory.extraction.manual.enabled = false, want true")
	}
	if cfg.Memory.Extraction.Manual.Mode != "apply" {
		t.Errorf("default memory.extraction.manual.mode = %q, want apply", cfg.Memory.Extraction.Manual.Mode)
	}
	if !cfg.Memory.Extraction.MirrorSync.AfterApply {
		t.Error("default memory.extraction.mirror_sync.after_apply = false, want true")
	}
	if !cfg.Memory.Extraction.MirrorSync.PeriodicEnabled {
		t.Error("default memory.extraction.mirror_sync.periodic_enabled = false, want true")
	}
	if cfg.Memory.Extraction.MirrorSync.Limit != 100 {
		t.Errorf("default memory.extraction.mirror_sync.limit = %d, want 100", cfg.Memory.Extraction.MirrorSync.Limit)
	}
	if cfg.Memory.Extraction.MirrorSync.FailExtractionOnSyncError {
		t.Error("default memory.extraction.mirror_sync.fail_extraction_on_sync_error = true, want false")
	}
	if cfg.Memory.NaturalMemory.Enabled {
		t.Error("default memory.natural_memory.enabled = true, want false")
	}
	if !cfg.Memory.NaturalMemory.Manual.Enabled {
		t.Error("default memory.natural_memory.manual.enabled = false, want true")
	}
	if !cfg.Memory.NaturalMemory.Manual.AllowDryRun || !cfg.Memory.NaturalMemory.Manual.AllowForce {
		t.Fatalf("default memory.natural_memory.manual = %#v, want dry-run and force allowed", cfg.Memory.NaturalMemory.Manual)
	}
	if cfg.Memory.NaturalMemory.TickIntervalSeconds != 60 {
		t.Errorf("default memory.natural_memory.tick_interval_seconds = %d, want 60", cfg.Memory.NaturalMemory.TickIntervalSeconds)
	}
	if cfg.Memory.Extraction.AllowSensitiveExtraction {
		t.Error("default memory.extraction.allow_sensitive_extraction = true, want false")
	}
	if cfg.Memory.Extraction.Provider.APIKeyEnv != "MEMORYCORE_LLM_API_KEY" {
		t.Errorf("default memory.extraction.provider.api_key_env = %q, want MEMORYCORE_LLM_API_KEY", cfg.Memory.Extraction.Provider.APIKeyEnv)
	}
	if cfg.Context.InputBudgetTokens <= 0 {
		t.Errorf("default context.input_budget_tokens = %d, want > 0", cfg.Context.InputBudgetTokens)
	}
	if cfg.Context.KeepRecentUserTurns <= 0 {
		t.Errorf("default context.keep_recent_user_turns = %d, want > 0", cfg.Context.KeepRecentUserTurns)
	}
	if cfg.Work.MaxToolRounds != 15 {
		t.Errorf("default work.max_tool_rounds = %d, want 15", cfg.Work.MaxToolRounds)
	}
	if cfg.Work.MaxInputTokens != 100000 {
		t.Errorf("default work.max_input_tokens = %d, want 100000", cfg.Work.MaxInputTokens)
	}
	if cfg.Work.JournalDir != "./logs/work" {
		t.Errorf("default work.journal_dir = %q, want ./logs/work", cfg.Work.JournalDir)
	}
	if cfg.Work.SoftTTL != 30*time.Minute {
		t.Errorf("default work.soft_ttl = %v, want 30m", cfg.Work.SoftTTL)
	}
	if cfg.Work.HardTTL != time.Hour {
		t.Errorf("default work.hard_ttl = %v, want 1h", cfg.Work.HardTTL)
	}
	if cfg.Work.ArchiveTTL != 24*time.Hour {
		t.Errorf("default work.archive_ttl = %v, want 24h", cfg.Work.ArchiveTTL)
	}
	if cfg.Work.ResumeClaimTTL != 10*time.Minute {
		t.Errorf("default work.resume_claim_ttl = %v, want 10m", cfg.Work.ResumeClaimTTL)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load("/nonexistent/config.yaml")
	if err != nil {
		t.Fatalf("Load missing file should return defaults, got error: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected defaults, got port %d", cfg.Server.Port)
	}
}

func TestLoadLegacySandboxAndContainerFieldsRemainParseable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
bash:
  enabled: true
  execution_mode: sandbox
  linux_driver: bubblewrap
  macos_driver: seatbelt
  windows_driver: wsl2
plugins:
  runtime:
    default_kind: container
    container_enabled: true
    sandbox_endpoint: npipe://emo-sandboxd
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load legacy config: %v", err)
	}
	if cfg.Bash.ExecutionMode != "sandbox" {
		t.Fatalf("legacy bash fields not preserved: %#v", cfg.Bash)
	}
	if cfg.Plugins.Runtime.DefaultKind != "container" {
		t.Fatalf("legacy plugin runtime fields not preserved: %#v", cfg.Plugins.Runtime)
	}
}

func TestLoadPromptCenterSnapshotConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
prompt_center:
  snapshots:
    enabled: false
    store_rendered_text: false
    max_rendered_text_chars: 123
    retention_days: 7
    max_rows: 42
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg.PromptCenter.Snapshots
	if got.Enabled || got.StoreRenderedText || got.MaxRenderedTextChars != 123 || got.RetentionDays != 7 || got.MaxRows != 42 {
		t.Fatalf("prompt center snapshots = %#v", got)
	}
}

func TestLoadEmptyMemoryExtractionTimezoneInheritsGlobalTimezone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
time:
  timezone: UTC
memory:
  extraction:
    timezone: ""
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Time.Timezone != "UTC" {
		t.Fatalf("time.timezone = %q, want UTC", cfg.Time.Timezone)
	}
	if cfg.Memory.Extraction.Timezone != "UTC" {
		t.Fatalf("memory.extraction.timezone = %q, want UTC", cfg.Memory.Extraction.Timezone)
	}
}

func TestLoadMemoryNaturalMemoryManualCanBeExplicitlyDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
memory:
  natural_memory:
    enabled: true
    manual:
      enabled: false
      allow_dry_run: false
      allow_force: false
      mark_sleep_cycle_by_default: false
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Memory.NaturalMemory.Manual.Enabled || cfg.Memory.NaturalMemory.Manual.AllowDryRun || cfg.Memory.NaturalMemory.Manual.AllowForce || cfg.Memory.NaturalMemory.Manual.MarkSleepCycleByDefault {
		t.Fatalf("memory.natural_memory.manual = %#v, want explicitly disabled", cfg.Memory.NaturalMemory.Manual)
	}
}

func TestLoadAgentAffectTopLevelConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
agent_affect:
  enabled: true
  update_mode: sync_before_reply
  storage_enabled: true
  state:
    scope: session
    recent_context_scope: session
  async:
    enabled: true
    queue_enabled: true
    worker_enabled: false
    worker_concurrency: 2
    poll_interval_ms: 900
    queue_claim_ttl_seconds: 111
    max_attempts: 4
    retry_base_delay_seconds: 12
    retry_max_delay_seconds: 120
    clear_raw_after_done: false
    batch:
      enabled: true
      max_jobs: 5
      max_input_tokens: 2345
      max_age_seconds: 67
      min_wait_ms: 10
      merge_across_sessions: false
      break_on_manual_barrier: false
      summarize_turns_before_llm: true
  evaluator:
    mode: disabled
    provider_id: moonshot
    model: affect-evaluator
    thinking_enabled: true
    reasoning_effort: medium
    timeout_ms: 1234
    max_output_tokens: 222
    temperature: 0.3
    store_hidden_thinking: false
  context:
    mode: raw_window
    raw_keep_last_requests: 7
    raw_keep_last_tokens: 700
    include_previous_evaluations: true
    previous_evaluation_keep_last: 9
    summary_enabled: false
    store_raw_inputs: true
    store_prompt_snapshot: false
  externalization:
    attachment:
      enabled: true
      default_style: gentle_explicit
      max_visible_intensity: 0.5
    frustration:
      enabled: false
  plugin_api:
    enabled: true
    plugin_safe_include_reason: true
    plugin_safe_include_raw_text: false
    ordinary_plugins_can_commit: true
    ordinary_plugins_can_write_delta: true
    trusted_plugins_can_write_target: true
  limits:
    per_request_delta:
      valence: 0.11
      arousal: 0.12
      dominance: 0.13
      energy: 0.14
      warmth: 0.15
      concern: 0.16
      curiosity: 0.17
      playfulness: 0.18
      attachment: 0.19
      frustration: 0.2
      uncertainty: 0.21
    absolute:
      attachment_max: 0.6
      frustration_max: 0.25
  prompt:
    mode: both
    include_mood_block: true
    include_reason: true
    include_expression_guidance: true
    include_numeric_values: true
    max_prompt_chars: 123
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.AgentAffect.Enabled {
		t.Fatal("agent_affect.enabled = false, want true")
	}
	if cfg.AgentAffect.UpdateMode != "sync_before_reply" {
		t.Fatalf("agent_affect.update_mode = %q, want sync_before_reply", cfg.AgentAffect.UpdateMode)
	}
	if cfg.AgentAffect.State.Scope != "session" || cfg.AgentAffect.State.RecentContextScope != "session" {
		t.Fatalf("agent_affect.state = %#v", cfg.AgentAffect.State)
	}
	if cfg.AgentAffect.Async.WorkerEnabled || cfg.AgentAffect.Async.WorkerConcurrency != 2 || cfg.AgentAffect.Async.PollIntervalMS != 900 {
		t.Fatalf("agent_affect.async worker = %#v", cfg.AgentAffect.Async)
	}
	if cfg.AgentAffect.Async.QueueClaimTTLSeconds != 111 || cfg.AgentAffect.Async.MaxAttempts != 4 || cfg.AgentAffect.Async.RetryBaseDelaySeconds != 12 || cfg.AgentAffect.Async.RetryMaxDelaySeconds != 120 {
		t.Fatalf("agent_affect.async retry = %#v", cfg.AgentAffect.Async)
	}
	if cfg.AgentAffect.Async.ClearRawAfterDone {
		t.Fatalf("agent_affect.async.clear_raw_after_done = true, want false")
	}
	if cfg.AgentAffect.Async.Batch.MaxJobs != 5 || cfg.AgentAffect.Async.Batch.MaxInputTokens != 2345 || cfg.AgentAffect.Async.Batch.MaxAgeSeconds != 67 {
		t.Fatalf("agent_affect.async.batch limits = %#v", cfg.AgentAffect.Async.Batch)
	}
	if cfg.AgentAffect.Async.Batch.MergeAcrossSessions || cfg.AgentAffect.Async.Batch.BreakOnManualBarrier || !cfg.AgentAffect.Async.Batch.SummarizeTurnsBeforeLLM {
		t.Fatalf("agent_affect.async.batch flags = %#v", cfg.AgentAffect.Async.Batch)
	}
	if cfg.AgentAffect.Evaluator.Mode != "disabled" || cfg.AgentAffect.Evaluator.ProviderID != "moonshot" || cfg.AgentAffect.Evaluator.Model != "affect-evaluator" {
		t.Fatalf("agent_affect.evaluator = %#v", cfg.AgentAffect.Evaluator)
	}
	if cfg.AgentAffect.Evaluator.TimeoutMS != 1234 || cfg.AgentAffect.Evaluator.MaxOutputTokens != 222 || cfg.AgentAffect.Evaluator.Temperature != 0.3 {
		t.Fatalf("agent_affect.evaluator numeric fields = %#v", cfg.AgentAffect.Evaluator)
	}
	if !cfg.AgentAffect.Evaluator.ThinkingEnabled || cfg.AgentAffect.Evaluator.ReasoningEffort != "medium" {
		t.Fatalf("agent_affect.evaluator thinking fields = %#v", cfg.AgentAffect.Evaluator)
	}
	if cfg.AgentAffect.Context.RawKeepLastRequests != 7 || cfg.AgentAffect.Context.RawKeepLastTokens != 700 || cfg.AgentAffect.Context.PreviousEvaluationKeepLast != 9 {
		t.Fatalf("agent_affect.context = %#v", cfg.AgentAffect.Context)
	}
	if cfg.AgentAffect.Externalization.Attachment.MaxVisibleIntensity != 0.5 {
		t.Fatalf("attachment max visible intensity = %v", cfg.AgentAffect.Externalization.Attachment.MaxVisibleIntensity)
	}
	if !cfg.AgentAffect.PluginAPI.Enabled || !cfg.AgentAffect.PluginAPI.OrdinaryPluginsCanCommit || !cfg.AgentAffect.PluginAPI.OrdinaryPluginsCanWriteDelta || !cfg.AgentAffect.PluginAPI.TrustedPluginsCanWriteTarget {
		t.Fatalf("agent_affect.plugin_api = %#v", cfg.AgentAffect.PluginAPI)
	}
	if cfg.AgentAffect.PluginAPI.PluginSafeIncludeRawText {
		t.Fatalf("plugin_safe_include_raw_text = true, want false")
	}
	if cfg.AgentAffect.Limits.PerRequestDelta.Valence != 0.11 || cfg.AgentAffect.Limits.PerRequestDelta.Uncertainty != 0.21 {
		t.Fatalf("agent_affect.limits.per_request_delta = %#v", cfg.AgentAffect.Limits.PerRequestDelta)
	}
	if cfg.AgentAffect.Limits.Absolute.AttachmentMax != 0.6 || cfg.AgentAffect.Limits.Absolute.FrustrationMax != 0.25 {
		t.Fatalf("agent_affect.limits.absolute = %#v", cfg.AgentAffect.Limits.Absolute)
	}
	if cfg.AgentAffect.Prompt.Mode != "both" || cfg.AgentAffect.Prompt.MaxPromptChars != 123 || !cfg.AgentAffect.Prompt.IncludeExpressionGuidance || !cfg.AgentAffect.Prompt.IncludeNumericValues {
		t.Fatalf("agent_affect.prompt = %#v", cfg.AgentAffect.Prompt)
	}
}

func TestPluginFailClosedHooksAcceptAgentAffectHooks(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Plugins.FailClosedHooks = []string{
		"before_agent_affect_evaluate",
		"after_agent_affect_evaluate",
		"before_agent_affect_commit",
		"after_agent_affect_commit",
		"agent_affect_get_state",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLoadValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
server:
  port: 9090
chat:
  realtime_streaming: true
  turn_pipeline:
    shadow: true
    enabled: true
    memory_stages: true
    approval_stages: true
    rollout_percent: 25
    allow_personas: ["default"]
    allow_sessions: ["session-allow"]
    deny_sessions: ["session-deny"]
    journal:
      mode: sqlite_jsonl
      jsonl_dir: ./tmp/turns
      fail_closed: true
    idempotency:
      mode: sqlite
      duplicate_done: replay_summary
      duplicate_running: busy
plugins:
  enabled: true
  directories: ["data/plugins"]
  builtin_enabled:
    - com.emoagent.plugins.turn-audit
  rollout_percent: 100
  default_timeout_ms: 120
  max_timeout_ms: 800
  fail_closed_hooks:
    - before_tool_call
    - before_memory_commit
  audit:
    enabled: true
    include_payload: false
context:
  input_budget_tokens: 12345
  soft_compact_ratio: 0.7
  hard_compact_ratio: 0.9
  reserve_output_tokens: 2048
  keep_recent_user_turns: 4
  tool_result_soft_tokens: 500
  tool_result_hard_tokens: 1500
llm_providers:
  - id: moonshot
    name: Moonshot
    preset_id: moonshot
    protocol: openai_compatible
    base_url: https://api.moonshot.cn
    api_key_env: MOONSHOT_API_KEY
    model_discovery: openai_models
    enabled: true
agent_configs:
  - id: default
    name: Default
    persona_key: default
    emotion:
      main:
        provider_id: moonshot
        model: kimi-k2.6
        params:
          max_tokens: 8192
          temperature: 1
          stream: true
      summary:
        provider_id: moonshot
        model: kimi-k2.6
        params:
          max_tokens: 4096
          temperature: 0.1
          stream: false
    work:
      main:
        provider_id: moonshot
        model: kimi-k2.6
        params:
          max_tokens: 4096
      summary:
        provider_id: moonshot
        model: kimi-k2.6
        params:
          max_tokens: 2048
    context_overrides:
      input_budget_tokens: 12000
      reserve_output_tokens: 1024
agent:
  active_config: default
memory:
  enabled: true
  config_path: ./custom-memorycore.yaml
  manual_rules_path: ./custom-memory-rules.yaml
  retrieval:
    enabled: true
    inject_prompt: true
    use_fts: false
    use_mirror: true
    final_memory_count: 3
    context_budget_tokens: 600
    fail_open: false
    pipeline_debug: true
  extraction:
    enabled: true
    mode: apply
    trigger_on_finalize_segment: true
    limit: 25
    timezone: Asia/Shanghai
    allow_inference: false
    allow_sensitive_extraction: false
    max_facts: 5
    max_links: 7
    raw_log:
      enabled: true
      directory: ./debug/memory_extraction_raw
    provider:
      kind: openai-compatible
      id: memory_extractor
      base_url: https://api.example.test
      api_key_env: MEMORY_EXTRACT_KEY
      model: memory-extractor
      timeout_seconds: 45
      max_tokens: 2048
      temperature: 0.2
      thinking:
        type: disabled
    semantic_dedup:
      enabled: true
      shadow: true
      candidate_limit: 9
      threshold_profile: default_v0
    repair_enabled: false
    audit_enabled: true
  natural_memory:
    enabled: true
    scheduler_enabled: true
    tick_interval_seconds: 90
    local_time: "03:30"
    timezone: Asia/Shanghai
    run_missed_on_start: true
    mirror_sync_after_run: true
    mirror_sync_limit: 25
    fail_on_sync_error: false
    manual:
      enabled: true
      allow_dry_run: true
      allow_force: false
      mark_sleep_cycle_by_default: true
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("port = %d, want 9090", cfg.Server.Port)
	}
	if !cfg.Chat.RealtimeStreaming {
		t.Fatal("chat.realtime_streaming = false, want true")
	}
	if !cfg.Chat.TurnPipeline.Shadow {
		t.Fatal("chat.turn_pipeline.shadow = false, want true")
	}
	if !cfg.Chat.TurnPipeline.Enabled {
		t.Fatal("chat.turn_pipeline.enabled = false, want true")
	}
	if !cfg.Chat.TurnPipeline.MemoryStages {
		t.Fatal("chat.turn_pipeline.memory_stages = false, want true")
	}
	if !cfg.Chat.TurnPipeline.ApprovalStages {
		t.Fatal("chat.turn_pipeline.approval_stages = false, want true")
	}
	if cfg.Chat.TurnPipeline.RolloutPercent != 25 {
		t.Fatalf("chat.turn_pipeline.rollout_percent = %d, want 25", cfg.Chat.TurnPipeline.RolloutPercent)
	}
	if len(cfg.Chat.TurnPipeline.AllowPersonas) != 1 || cfg.Chat.TurnPipeline.AllowPersonas[0] != "default" {
		t.Fatalf("chat.turn_pipeline.allow_personas = %#v", cfg.Chat.TurnPipeline.AllowPersonas)
	}
	if len(cfg.Chat.TurnPipeline.AllowSessions) != 1 || cfg.Chat.TurnPipeline.AllowSessions[0] != "session-allow" {
		t.Fatalf("chat.turn_pipeline.allow_sessions = %#v", cfg.Chat.TurnPipeline.AllowSessions)
	}
	if len(cfg.Chat.TurnPipeline.DenySessions) != 1 || cfg.Chat.TurnPipeline.DenySessions[0] != "session-deny" {
		t.Fatalf("chat.turn_pipeline.deny_sessions = %#v", cfg.Chat.TurnPipeline.DenySessions)
	}
	if cfg.Chat.TurnPipeline.Journal.Mode != "sqlite_jsonl" || cfg.Chat.TurnPipeline.Journal.JSONLDir != "./tmp/turns" || !cfg.Chat.TurnPipeline.Journal.FailClosed {
		t.Fatalf("chat.turn_pipeline.journal = %#v", cfg.Chat.TurnPipeline.Journal)
	}
	if cfg.Chat.TurnPipeline.Idempotency.Mode != "sqlite" || cfg.Chat.TurnPipeline.Idempotency.DuplicateDone != "replay_summary" || cfg.Chat.TurnPipeline.Idempotency.DuplicateRunning != "busy" {
		t.Fatalf("chat.turn_pipeline.idempotency = %#v", cfg.Chat.TurnPipeline.Idempotency)
	}
	if !cfg.Plugins.Enabled {
		t.Fatal("plugins.enabled = false, want true")
	}
	if len(cfg.Plugins.Directories) != 1 || cfg.Plugins.Directories[0] != "data/plugins" {
		t.Fatalf("plugins.directories = %#v", cfg.Plugins.Directories)
	}
	if len(cfg.Plugins.BuiltinEnabled) != 1 || cfg.Plugins.BuiltinEnabled[0] != "com.emoagent.plugins.turn-audit" {
		t.Fatalf("plugins.builtin_enabled = %#v", cfg.Plugins.BuiltinEnabled)
	}
	if cfg.Plugins.RolloutPercent != 100 || cfg.Plugins.DefaultTimeoutMS != 120 || cfg.Plugins.MaxTimeoutMS != 800 {
		t.Fatalf("plugins timing/rollout = %#v", cfg.Plugins)
	}
	if len(cfg.Plugins.FailClosedHooks) != 2 || cfg.Plugins.FailClosedHooks[0] != "before_tool_call" || cfg.Plugins.FailClosedHooks[1] != "before_memory_commit" {
		t.Fatalf("plugins.fail_closed_hooks = %#v", cfg.Plugins.FailClosedHooks)
	}
	if !cfg.Plugins.Audit.Enabled || cfg.Plugins.Audit.IncludePayload {
		t.Fatalf("plugins.audit = %#v", cfg.Plugins.Audit)
	}
	if cfg.Context.InputBudgetTokens != 12345 {
		t.Errorf("context.input_budget_tokens = %d, want 12345", cfg.Context.InputBudgetTokens)
	}
	if cfg.Context.KeepRecentUserTurns != 4 {
		t.Errorf("context.keep_recent_user_turns = %d, want 4", cfg.Context.KeepRecentUserTurns)
	}
	if len(cfg.LLMProviders) != 1 || cfg.LLMProviders[0].ID != "moonshot" {
		t.Fatalf("LLMProviders = %#v, want moonshot", cfg.LLMProviders)
	}
	if len(cfg.AgentConfigs) != 1 {
		t.Fatalf("len(AgentConfigs) = %d, want 1", len(cfg.AgentConfigs))
	}
	agent := cfg.AgentConfigs[0]
	if agent.Emotion.Main.Params.Temperature == nil || *agent.Emotion.Main.Params.Temperature != 1 {
		t.Fatalf("emotion.main.temperature = %#v, want 1", agent.Emotion.Main.Params.Temperature)
	}
	effective, err := agent.ResolveContextConfig(cfg.Context)
	if err != nil {
		t.Fatalf("ResolveContextConfig: %v", err)
	}
	if effective.InputBudgetTokens != 12000 {
		t.Fatalf("effective.input_budget_tokens = %d, want 12000", effective.InputBudgetTokens)
	}
	if effective.ReserveOutputTokens != 1024 {
		t.Fatalf("effective.reserve_output_tokens = %d, want 1024", effective.ReserveOutputTokens)
	}
	if effective.KeepRecentUserTurns != cfg.Context.KeepRecentUserTurns {
		t.Fatalf("effective.keep_recent_user_turns = %d, want global %d", effective.KeepRecentUserTurns, cfg.Context.KeepRecentUserTurns)
	}
	if cfg.Agent.ActiveConfig != "default" {
		t.Fatalf("agent.active_config = %q, want default", cfg.Agent.ActiveConfig)
	}
	if !cfg.Memory.Enabled {
		t.Fatal("memory.enabled = false, want true")
	}
	if cfg.Memory.ConfigPath != "./custom-memorycore.yaml" {
		t.Fatalf("memory.config_path = %q, want ./custom-memorycore.yaml", cfg.Memory.ConfigPath)
	}
	if cfg.Memory.ManualRulesPath != "./custom-memory-rules.yaml" {
		t.Fatalf("memory.manual_rules_path = %q, want ./custom-memory-rules.yaml", cfg.Memory.ManualRulesPath)
	}
	if !cfg.Memory.Retrieval.Enabled || !cfg.Memory.Retrieval.InjectPrompt || cfg.Memory.Retrieval.UseFTS || !cfg.Memory.Retrieval.UseMirror {
		t.Fatalf("memory.retrieval flags = %#v", cfg.Memory.Retrieval)
	}
	if cfg.Memory.Retrieval.FinalMemoryCount != 3 {
		t.Fatalf("memory.retrieval.final_memory_count = %d, want 3", cfg.Memory.Retrieval.FinalMemoryCount)
	}
	if cfg.Memory.Retrieval.ContextBudgetTokens != 600 {
		t.Fatalf("memory.retrieval.context_budget_tokens = %d, want 600", cfg.Memory.Retrieval.ContextBudgetTokens)
	}
	if cfg.Memory.Retrieval.FailOpen {
		t.Fatal("memory.retrieval.fail_open = true, want false")
	}
	if !cfg.Memory.Retrieval.PipelineDebug {
		t.Fatal("memory.retrieval.pipeline_debug = false, want true")
	}
	if !cfg.Memory.Extraction.Enabled || cfg.Memory.Extraction.Mode != "apply" {
		t.Fatalf("memory.extraction enabled/mode = %v/%q, want true/apply", cfg.Memory.Extraction.Enabled, cfg.Memory.Extraction.Mode)
	}
	if cfg.Memory.Extraction.SessionEndMode != "apply" {
		t.Fatalf("memory.extraction.session_end_mode = %q, want apply", cfg.Memory.Extraction.SessionEndMode)
	}
	if cfg.Memory.Extraction.Limit != 25 || cfg.Memory.Extraction.MaxFacts != 5 || cfg.Memory.Extraction.MaxLinks != 7 {
		t.Fatalf("memory.extraction limits = %#v", cfg.Memory.Extraction)
	}
	if cfg.Memory.Extraction.AllowInference {
		t.Fatal("memory.extraction.allow_inference = true, want false")
	}
	if cfg.Memory.Extraction.Provider.BaseURL != "https://api.example.test" || cfg.Memory.Extraction.Provider.APIKeyEnv != "MEMORY_EXTRACT_KEY" {
		t.Fatalf("memory.extraction.provider = %#v", cfg.Memory.Extraction.Provider)
	}
	if cfg.Memory.Extraction.Provider.TimeoutSeconds != 45 || cfg.Memory.Extraction.Provider.MaxTokens != 2048 {
		t.Fatalf("memory.extraction.provider timeout/max_tokens = %#v", cfg.Memory.Extraction.Provider)
	}
	if cfg.Memory.Extraction.Provider.Thinking.Type != "disabled" {
		t.Fatalf("memory.extraction.provider.thinking.type = %q, want disabled", cfg.Memory.Extraction.Provider.Thinking.Type)
	}
	if !cfg.Memory.Extraction.SemanticDedup.Enabled || !cfg.Memory.Extraction.SemanticDedup.Shadow || cfg.Memory.Extraction.SemanticDedup.CandidateLimit != 9 || cfg.Memory.Extraction.SemanticDedup.ThresholdProfile != "default_v0" {
		t.Fatalf("memory.extraction.semantic_dedup = %#v", cfg.Memory.Extraction.SemanticDedup)
	}
	if !cfg.Memory.Extraction.RawLog.Enabled || cfg.Memory.Extraction.RawLog.Directory != "./debug/memory_extraction_raw" {
		t.Fatalf("memory.extraction.raw_log = %#v", cfg.Memory.Extraction.RawLog)
	}
	if !cfg.Memory.NaturalMemory.Enabled || !cfg.Memory.NaturalMemory.SchedulerEnabled {
		t.Fatalf("memory.natural_memory enabled flags = %#v", cfg.Memory.NaturalMemory)
	}
	if cfg.Memory.NaturalMemory.TickIntervalSeconds != 90 || cfg.Memory.NaturalMemory.LocalTime != "03:30" || cfg.Memory.NaturalMemory.Timezone != "Asia/Shanghai" {
		t.Fatalf("memory.natural_memory schedule = %#v", cfg.Memory.NaturalMemory)
	}
	if !cfg.Memory.NaturalMemory.RunMissedOnStart || !cfg.Memory.NaturalMemory.MirrorSyncAfterRun || cfg.Memory.NaturalMemory.MirrorSyncLimit != 25 || cfg.Memory.NaturalMemory.FailOnSyncError {
		t.Fatalf("memory.natural_memory run/mirror = %#v", cfg.Memory.NaturalMemory)
	}
	if !cfg.Memory.NaturalMemory.Manual.Enabled || !cfg.Memory.NaturalMemory.Manual.AllowDryRun || cfg.Memory.NaturalMemory.Manual.AllowForce || !cfg.Memory.NaturalMemory.Manual.MarkSleepCycleByDefault {
		t.Fatalf("memory.natural_memory.manual = %#v", cfg.Memory.NaturalMemory.Manual)
	}
	// Default should still apply for unset fields.
	if cfg.DB.Path != "./data/emo.db" {
		t.Errorf("db.path = %q, want default", cfg.DB.Path)
	}
	if cfg.Work.MaxToolRounds != 15 {
		t.Errorf("work.max_tool_rounds = %d, want 15", cfg.Work.MaxToolRounds)
	}
	if cfg.Work.MaxInputTokens != 100000 {
		t.Errorf("work.max_input_tokens = %d, want 100000", cfg.Work.MaxInputTokens)
	}
	if cfg.Work.JournalDir != "./logs/work" {
		t.Errorf("work.journal_dir = %q, want ./logs/work", cfg.Work.JournalDir)
	}
}

func TestValidateMemoryRetrievalLimits(t *testing.T) {
	tests := []struct {
		name   string
		update func(*Config)
		want   string
	}{
		{
			name: "final memory count",
			update: func(cfg *Config) {
				cfg.Memory.Retrieval.FinalMemoryCount = 0
			},
			want: "memory.retrieval.final_memory_count must be > 0",
		},
		{
			name: "context budget tokens",
			update: func(cfg *Config) {
				cfg.Memory.Retrieval.ContextBudgetTokens = 0
			},
			want: "memory.retrieval.context_budget_tokens must be > 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Memory.Enabled = true
			tt.update(cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadRejectsUnknownPluginConfigKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
plugins:
  enabled: false
  raw_prompt_debug: true
`), 0o644)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `plugins.raw_prompt_debug is not supported`) {
		t.Fatalf("Load error = %v, want unsupported plugins key", err)
	}
}

func TestLoadRejectsUnknownTopLevelConfigKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
capability_runtim:
  enabled: true
`), 0o644)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `capability_runtim is not supported`) {
		t.Fatalf("Load error = %v, want unsupported top-level key", err)
	}
}

func TestLoadPluginRuntimeV02Config(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
plugins:
  enabled: false
  store:
    root_dir: ./tmp/plugins
    allow_dev_dirs: false
  runtime:
    process_enabled: false
    python_executable: python
    startup_timeout_ms: 7000
    shutdown_timeout_ms: 4000
    idle_timeout_seconds: 30
    crash_backoff_initial_seconds: 2
    crash_backoff_max_seconds: 10
    crash_quarantine_threshold: 7
    max_stderr_bytes: 1024
    container_enabled: false
  installer:
    github_enabled: false
    require_signature: false
    trusted_publishers_path: ./config/plugin_publishers.yaml
    allow_unsigned_dev: false
  provider_gateway:
    enabled: false
    default_provider_id: local
    default_model: fake
  admin:
    enabled: false
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Plugins.Store.AllowDevDirs {
		t.Fatal("plugins.store.allow_dev_dirs = true, want explicit false")
	}
	if cfg.Plugins.Runtime.ProcessEnabled {
		t.Fatal("plugins.runtime.process_enabled = true, want explicit false")
	}
	if cfg.Plugins.Installer.GithubEnabled || cfg.Plugins.Installer.RequireSignature || cfg.Plugins.Installer.AllowUnsignedDev {
		t.Fatalf("plugins.installer booleans = %#v, want explicit false", cfg.Plugins.Installer)
	}
	if cfg.Plugins.ProviderGateway.Enabled || cfg.Plugins.Admin.Enabled {
		t.Fatalf("plugins provider/admin enabled = %v/%v, want explicit false", cfg.Plugins.ProviderGateway.Enabled, cfg.Plugins.Admin.Enabled)
	}
	if cfg.Plugins.Runtime.PythonExecutable != "python" ||
		cfg.Plugins.Runtime.MaxStderrBytes != 1024 ||
		cfg.Plugins.Runtime.CrashQuarantineThreshold != 7 {
		t.Fatalf("plugins.runtime = %#v", cfg.Plugins.Runtime)
	}
	if cfg.Plugins.Runtime.DefaultKind != "managed_python_process" || cfg.Plugins.Runtime.ProcessDevEnabled {
		t.Fatalf("plugins.runtime v0.2 compatibility managed defaults = %#v", cfg.Plugins.Runtime)
	}
	if !cfg.Plugins.Runtime.FailClosedIfUnavailable {
		t.Fatalf("plugins.runtime v0.2 default managed fields = %#v", cfg.Plugins.Runtime)
	}
}

func TestLoadPluginRuntimePrivatePythonExecutable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
plugins:
  runtime:
    private_python_executable: C:/EmoAgent/runtime/python/python.exe
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Plugins.Runtime.PrivatePythonExecutable != "C:/EmoAgent/runtime/python/python.exe" {
		t.Fatalf("private_python_executable = %q", cfg.Plugins.Runtime.PrivatePythonExecutable)
	}
	if cfg.PythonToolchain.PythonExecutable != "C:/EmoAgent/runtime/python/python.exe" {
		t.Fatalf("python_toolchain.python_executable = %q, want migrated private executable", cfg.PythonToolchain.PythonExecutable)
	}
	if cfg.Plugins.Runtime.PythonExecutable != "" {
		t.Fatalf("python_executable default = %q, want empty", cfg.Plugins.Runtime.PythonExecutable)
	}
}

func TestLoadPluginRuntimePrivatePythonProvisioningConfigRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
plugins:
  runtime:
    private_python_artifact_path: ./runtime/python-embed.zip
    private_python_artifact_sha256: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "private_python_artifact_path is no longer supported") {
		t.Fatalf("Load error = %v, want unsupported private artifact", err)
	}
}

func TestLoadPythonToolchainConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
python_toolchain:
  enabled: true
  python_executable: C:/Python312/python.exe
  uv_executable: C:/Users/me/.local/bin/uv.exe
  required_python: "3.12"
  minimum_uv_version: "0.11.1"
  environment_root: data/custom-python-envs
  cache_dir: data/custom-uv-cache
  default_index: https://example.test/simple
  sync_timeout_seconds: 120
  use_system_certificates: false
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg.PythonToolchain
	if !got.Enabled ||
		got.PythonExecutable != "C:/Python312/python.exe" ||
		got.UVExecutable != "C:/Users/me/.local/bin/uv.exe" ||
		got.RequiredPython != "3.12" ||
		got.MinimumUVVersion != "0.11.1" ||
		got.EnvironmentRoot != "data/custom-python-envs" ||
		got.CacheDir != "data/custom-uv-cache" ||
		got.DefaultIndex != "https://example.test/simple" ||
		got.SyncTimeoutSeconds != 120 ||
		got.UseSystemCertificates {
		t.Fatalf("python_toolchain = %#v", got)
	}
}

func TestLoadPythonToolchainRejectsRelativeExecutablesWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
python_toolchain:
  enabled: true
  python_executable: python
  uv_executable: uv
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "python_toolchain.python_executable must be an absolute path") {
		t.Fatalf("Load error = %v, want absolute python path", err)
	}
}

func TestLoadPythonToolchainRejectsInvalidMinimumUVVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
python_toolchain:
  minimum_uv_version: latest
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "python_toolchain.minimum_uv_version must be a dotted numeric version") {
		t.Fatalf("Load error = %v, want invalid minimum_uv_version", err)
	}
}

func TestLoadPluginRuntimeProcessLimits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
plugins:
  runtime:
    max_processes: 12
    memory_mb: 256
    cpus: 0.5
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Plugins.Runtime.MaxProcesses != 12 || cfg.Plugins.Runtime.MemoryMB != 256 || cfg.Plugins.Runtime.CPUs != 0.5 {
		t.Fatalf("runtime limits = %#v", cfg.Plugins.Runtime)
	}
}

func TestLoadPluginRuntimePrivatePythonArtifactRequiresChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
plugins:
  runtime:
    private_python_artifact_path: ./runtime/python-embed.zip
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "private_python_artifact_path is no longer supported") {
		t.Fatalf("Load error = %v, want unsupported private artifact", err)
	}
}

func TestLoadPluginRuntimePrivatePythonArtifactRejectsInvalidChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
plugins:
  runtime:
    private_python_artifact_path: ./runtime/python-embed.zip
    private_python_artifact_sha256: sha256:not-a-real-digest
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "private_python_artifact_path is no longer supported") {
		t.Fatalf("Load error = %v, want unsupported private artifact", err)
	}
}

func TestLoadPluginRuntimePrivatePythonArtifactChecksumRequiresPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
plugins:
  runtime:
    private_python_artifact_sha256: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "private_python_artifact_path is no longer supported") {
		t.Fatalf("Load error = %v, want unsupported private artifact", err)
	}
}

func TestLoadPluginRuntimePrivatePythonArtifactConflictsWithExplicitExecutable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
plugins:
  runtime:
    private_python_executable: C:/EmoAgent/runtime/python/python.exe
    private_python_artifact_path: ./runtime/python-embed.zip
    private_python_artifact_sha256: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "private_python_artifact_path is no longer supported") {
		t.Fatalf("Load error = %v, want unsupported private artifact", err)
	}
}

func TestRepositoryConfigDoesNotDefaultPluginRuntimeToSystemPython(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "config.yaml"))
	if err != nil {
		t.Fatalf("Load repository config: %v", err)
	}
	if cfg.Plugins.Runtime.PythonExecutable == "python3" {
		t.Fatalf("repository config python_executable = %q, want no normal-path system Python default", cfg.Plugins.Runtime.PythonExecutable)
	}
}

func TestLoadCapabilityRuntimePhase0Config(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
capability_runtime:
  enabled: true
host_resources:
  enabled: true
  default_profile: personal_read
  staging_dir: ./tmp/staging
  quarantine_dir: ./tmp/quarantine
  max_read_bytes: 2048
  max_search_results: 50
  persistent_grants_enabled: true
  protected_policy: default
  roots:
    - id: documents
      path: ${HOME}/Documents
      access: read
      recursive: true
bash:
  enabled: false
  execution_mode: sandbox
  unsafe_host_exec_enabled: false
  network_default: deny
  linux_driver: bubblewrap
  macos_driver: seatbelt
  windows_driver: wsl2
  max_processes: 32
  memory_mb: 512
  cpus: 0.5
plugins:
  runtime:
    default_kind: container
    process_enabled: false
    process_dev_enabled: true
    container_enabled: true
    sandbox_endpoint: npipe://emo-sandboxd
    fail_closed_if_unavailable: true
    prefer_rootless: false
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.HostResources.Enabled || cfg.HostResources.StagingDir != "./tmp/staging" || len(cfg.HostResources.Roots) != 1 {
		t.Fatalf("host_resources = %#v", cfg.HostResources)
	}
	if cfg.HostResources.Roots[0].ID != "documents" || cfg.HostResources.Roots[0].Access != "read" || !cfg.HostResources.Roots[0].Recursive {
		t.Fatalf("host_resources.roots[0] = %#v", cfg.HostResources.Roots[0])
	}
	if cfg.Bash.Enabled || cfg.Bash.ExecutionMode != "sandbox" || cfg.Bash.UnsafeHostExecEnabled {
		t.Fatalf("bash = %#v", cfg.Bash)
	}
	if cfg.Plugins.Runtime.DefaultKind != "container" || !cfg.Plugins.Runtime.ProcessDevEnabled {
		t.Fatalf("plugins.runtime = %#v", cfg.Plugins.Runtime)
	}
}

func TestCapabilityRuntimeLegacyConfigIsParseOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
capability_runtime:
  enabled: true
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := reflect.TypeOf(*cfg).FieldByName("CapabilityRuntime"); ok {
		t.Fatal("Config still exposes legacy capability_runtime as runtime state")
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if strings.Contains(string(raw), "capability_runtime") {
		t.Fatalf("marshaled config exposed legacy capability_runtime: %s", raw)
	}
}

func TestPluginRuntimeLegacySandboxContainerFieldsAreParseOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
plugins:
  runtime:
    container_enabled: true
    sandbox_endpoint: npipe://emo-sandboxd
    prefer_rootless: true
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	runtimeType := reflect.TypeOf(cfg.Plugins.Runtime)
	for _, field := range []string{"ContainerEnabled", "SandboxEndpoint", "PreferRootless"} {
		if _, ok := runtimeType.FieldByName(field); ok {
			t.Fatalf("PluginRuntimeConfig still exposes legacy %s as runtime state", field)
		}
	}
	raw, err := json.Marshal(cfg.Plugins.Runtime)
	if err != nil {
		t.Fatalf("marshal plugin runtime config: %v", err)
	}
	for _, key := range []string{"container_enabled", "sandbox_endpoint", "prefer_rootless"} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("marshaled plugin runtime config exposed legacy %s: %s", key, raw)
		}
	}
}

func TestBashLegacySandboxResourceFieldsAreParseOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
bash:
  enabled: true
  execution_mode: sandbox
  unsafe_host_exec_enabled: false
  network_default: deny
  linux_driver: bubblewrap
  macos_driver: seatbelt
  windows_driver: wsl2
  max_processes: 32
  memory_mb: 512
  cpus: 0.5
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Bash.Enabled || cfg.Bash.ExecutionMode != "sandbox" || cfg.Bash.UnsafeHostExecEnabled ||
		cfg.Bash.MaxProcesses != 32 || cfg.Bash.MemoryMB != 512 || cfg.Bash.CPUs != 0.5 {
		t.Fatalf("bash executable compatibility fields = %#v", cfg.Bash)
	}
	bashType := reflect.TypeOf(cfg.Bash)
	for _, field := range []string{"NetworkDefault", "LinuxDriver", "MacOSDriver", "WindowsDriver"} {
		if _, ok := bashType.FieldByName(field); ok {
			t.Fatalf("BashConfig still exposes legacy %s as runtime state", field)
		}
	}
	raw, err := json.Marshal(cfg.Bash)
	if err != nil {
		t.Fatalf("marshal bash config: %v", err)
	}
	for _, key := range []string{"network_default", "linux_driver", "macos_driver", "windows_driver"} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("marshaled bash config exposed legacy %s: %s", key, raw)
		}
	}
}

func TestPluginRuntimeDocsAndRepositoryConfigDoNotPromoteLegacySandboxContainerKnobs(t *testing.T) {
	for _, path := range []string{
		filepath.Join("..", "..", "config.yaml"),
		filepath.Join("..", "..", "docs", "plugin_development_guide.md"),
		filepath.Join("..", "..", "docs", "specs", "plugin_runtime_v0.2_update_spec.md"),
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(raw)
		for _, legacyKey := range []string{"container_enabled:", "sandbox_endpoint:", "prefer_rootless:"} {
			if strings.Contains(text, legacyKey) {
				t.Fatalf("%s still promotes legacy runtime key %q", path, legacyKey)
			}
		}
	}

	guidePath := filepath.Join("..", "..", "docs", "plugin_development_guide.md")
	raw, err := os.ReadFile(guidePath)
	if err != nil {
		t.Fatalf("read %s: %v", guidePath, err)
	}
	guide := string(raw)
	if strings.Contains(guide, "第三方插件当前推荐 `python_process`") ||
		strings.Contains(guide, "`python_process` | 当前第三方插件主路径") {
		t.Fatalf("plugin guide still recommends legacy python_process")
	}
	if !strings.Contains(guide, "`managed_python_process` | 当前第三方插件主路径") {
		t.Fatalf("plugin guide does not present managed_python_process as the third-party main path")
	}
	if !strings.Contains(guide, "legacy/dev") {
		t.Fatalf("plugin guide does not label python_process/process as legacy/dev")
	}
}

func TestLoadPluginPolicyConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
plugins:
  enabled: false
  policy:
    allow_active_hooks: true
    allowed_capabilities:
      - plugin.kv
      - provider.generate
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Plugins.Policy.AllowActiveHooks {
		t.Fatalf("plugins.policy.allow_active_hooks = false, want true")
	}
	if got := strings.Join(cfg.Plugins.Policy.AllowedCapabilities, ","); got != "plugin.kv,provider.generate" {
		t.Fatalf("plugins.policy.allowed_capabilities = %q", got)
	}
}

func TestLoadPluginInstallerTrustPolicyConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
plugins:
  enabled: false
  installer:
    blocked_package_digests:
      - sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
    blocked_manifest_digests:
      - sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
    blocked_publishers:
      - revoked-publisher
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := strings.Join(cfg.Plugins.Installer.BlockedPackageDigests, ","); got != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("blocked package digests = %q", got)
	}
	if got := strings.Join(cfg.Plugins.Installer.BlockedManifestDigests, ","); got != "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("blocked manifest digests = %q", got)
	}
	if got := strings.Join(cfg.Plugins.Installer.BlockedPublishers, ","); got != "revoked-publisher" {
		t.Fatalf("blocked publishers = %q", got)
	}
}

func TestPluginInstallerTrustPolicyRejectsInvalidDigest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
plugins:
  enabled: false
  installer:
    blocked_package_digests:
      - sha256:not-real
`), 0o644)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `installer.blocked_package_digests contains invalid digest "sha256:not-real"`) {
		t.Fatalf("Load error = %v, want invalid blocked digest rejection", err)
	}
}

func TestPluginPolicyRejectsUnknownCapability(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
plugins:
  enabled: false
  policy:
    allowed_capabilities:
      - does.not.exist
`), 0o644)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `policy.allowed_capabilities contains unknown capability "does.not.exist"`) {
		t.Fatalf("Load error = %v, want unknown capability rejection", err)
	}
}

func TestLoadAllowsEnabledBashSandboxAfterBroker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
bash:
  enabled: true
  execution_mode: sandbox
  unsafe_host_exec_enabled: false
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Bash.Enabled || cfg.Bash.ExecutionMode != "sandbox" || cfg.Bash.UnsafeHostExecEnabled {
		t.Fatalf("bash = %#v", cfg.Bash)
	}
}

func TestLoadRejectsEnabledLegacyHostBashWithoutUnsafeFlag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
bash:
  enabled: true
  execution_mode: legacy_host
  unsafe_host_exec_enabled: false
`), 0o644)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `bash.execution_mode=legacy_host requires unsafe_host_exec_enabled=true`) {
		t.Fatalf("Load error = %v, want unsafe legacy rejection", err)
	}
}

func TestLoadAllowsExplicitUnsafeLegacyHostBash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
bash:
  enabled: true
  execution_mode: legacy_host
  unsafe_host_exec_enabled: true
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Bash.ExecutionMode != "legacy_host" || !cfg.Bash.UnsafeHostExecEnabled {
		t.Fatalf("bash = %#v", cfg.Bash)
	}
}

func TestLoadRejectsUnknownHostResourcesKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
host_resources:
  enabled: true
  raw_host_path: true
`), 0o644)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `host_resources.raw_host_path is not supported`) {
		t.Fatalf("Load error = %v, want unsupported host_resources key", err)
	}
}

func TestLoadRejectsUnknownHostResourceRootKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
host_resources:
  roots:
    - id: documents
      path: ${HOME}/Documents
      raw_host_path: true
`), 0o644)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `host_resources.roots[].raw_host_path is not supported`) {
		t.Fatalf("Load error = %v, want unsupported host_resources root key", err)
	}
}

func TestLoadRejectsHostResourceRootWithoutAccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
host_resources:
  roots:
    - id: documents
      path: ${HOME}/Documents
`), 0o644)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `host_resources.roots[0].access is required`) {
		t.Fatalf("Load error = %v, want missing root access", err)
	}
}

func TestLoadRejectsInvalidCapabilityRuntimeConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
capability_runtime:
  enabled: false
  shadow: true
`), 0o644)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `capability_runtime.shadow is not supported`) {
		t.Fatalf("Load error = %v, want unsupported capability_runtime key", err)
	}
}

func TestLoadRejectsNonMappingCapabilityRuntimeConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
capability_runtime: true
`), 0o644)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `capability_runtime must be a mapping`) {
		t.Fatalf("Load error = %v, want non-mapping capability_runtime rejection", err)
	}
}

func TestValidatePluginsEnabledRejectsContainerDefaultBeforeRuntime(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Plugins.Enabled = true
	cfg.Plugins.Runtime.DefaultKind = "container"
	cfg.Chat.TurnPipeline.Enabled = true
	cfg.Chat.TurnPipeline.AllowPersonas = []string{"default"}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `runtime.default_kind=container is unavailable`) {
		t.Fatalf("Validate error = %v, want container unavailable", err)
	}
}

func TestValidatePluginsEnabledRejectsProcessDevDefaultUnlessExplicit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Plugins.Enabled = true
	cfg.Plugins.Runtime.DefaultKind = "process_dev"
	cfg.Chat.TurnPipeline.Enabled = true
	cfg.Chat.TurnPipeline.AllowPersonas = []string{"default"}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `runtime.default_kind=process_dev requires process_dev_enabled=true`) {
		t.Fatalf("Validate error = %v, want process_dev explicit opt-in", err)
	}

	cfg.Plugins.Runtime.ProcessDevEnabled = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate with process_dev explicit opt-in: %v", err)
	}
}

func TestLoadPluginAuditCanBeExplicitlyDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
plugins:
  audit:
    enabled: false
    include_payload: false
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Plugins.Audit.Enabled {
		t.Fatal("plugins.audit.enabled = true, want explicit false")
	}
}

func TestLoadRejectsUnknownPluginRuntimeNestedKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
plugins:
  runtime:
    process_enabled: true
    tcp_port: 9999
`), 0o644)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), `plugins.runtime.tcp_port is not supported`) {
		t.Fatalf("Load error = %v, want unsupported nested plugin key", err)
	}
}

func TestValidatePluginsEnabledRequiresTurnPipelineTarget(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Config)
		want string
	}{
		{
			name: "turn pipeline disabled",
			mut: func(cfg *Config) {
				cfg.Plugins.Enabled = true
				cfg.Chat.TurnPipeline.Enabled = false
			},
			want: "plugins.enabled requires chat.turn_pipeline.enabled=true",
		},
		{
			name: "no target",
			mut: func(cfg *Config) {
				cfg.Plugins.Enabled = true
				cfg.Chat.TurnPipeline.Enabled = true
				cfg.Chat.TurnPipeline.RolloutPercent = 0
			},
			want: "plugins.enabled requires chat.turn_pipeline rollout or allow list",
		},
		{
			name: "targeted persona",
			mut: func(cfg *Config) {
				cfg.Plugins.Enabled = true
				cfg.Chat.TurnPipeline.Enabled = true
				cfg.Chat.TurnPipeline.AllowPersonas = []string{"default"}
			},
		},
		{
			name: "rollout target",
			mut: func(cfg *Config) {
				cfg.Plugins.Enabled = true
				cfg.Chat.TurnPipeline.Enabled = true
				cfg.Chat.TurnPipeline.RolloutPercent = 100
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.mut(cfg)
			err := cfg.Validate()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateMemoryExtractionHostModes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Memory.Enabled = true
	cfg.Memory.Extraction.Enabled = true
	cfg.Memory.Extraction.ManualPinMode = "invalid"

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "manual_pin_mode must be validate, dry_run, or apply") {
		t.Fatalf("Validate error = %v", err)
	}
}

func TestValidateMemoryNaturalMemoryConfig(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Config)
		want string
	}{
		{
			name: "invalid local time",
			mut: func(cfg *Config) {
				cfg.Memory.NaturalMemory.Enabled = true
				cfg.Memory.NaturalMemory.LocalTime = "25:00"
			},
			want: "memory.natural_memory.local_time must be HH:mm",
		},
		{
			name: "scheduler interval",
			mut: func(cfg *Config) {
				cfg.Memory.NaturalMemory.Enabled = true
				cfg.Memory.NaturalMemory.SchedulerEnabled = true
				cfg.Memory.NaturalMemory.TickIntervalSeconds = 0
			},
			want: "memory.natural_memory.tick_interval_seconds must be > 0",
		},
		{
			name: "invalid timezone",
			mut: func(cfg *Config) {
				cfg.Memory.NaturalMemory.Enabled = true
				cfg.Memory.NaturalMemory.Timezone = "Mars/Base"
			},
			want: "memory.natural_memory.timezone must be a valid IANA timezone",
		},
		{
			name: "mirror limit",
			mut: func(cfg *Config) {
				cfg.Memory.NaturalMemory.Enabled = true
				cfg.Memory.NaturalMemory.MirrorSyncAfterRun = true
				cfg.Memory.NaturalMemory.MirrorSyncLimit = 0
			},
			want: "memory.natural_memory.mirror_sync_limit must be > 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.mut(cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadAgentProviderConfigYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
server:
  port: 9090
llm_providers:
  - id: moonshot
    name: Moonshot
    preset_id: moonshot
    protocol: openai_compatible
    base_url: https://api.moonshot.cn
    api_key_env: MOONSHOT_API_KEY
    model_discovery: openai_models
    enabled: true
  - id: anthropic-main
    name: Anthropic
    protocol: anthropic
    base_url: https://api.anthropic.com
    api_key_env: ANTHROPIC_API_KEY
    model_discovery: anthropic_models
    enabled: false
agent_configs:
  - id: default
    name: Default
    persona_key: default
    emotion:
      main:
        provider_id: moonshot
        model: kimi-k2.6
        params:
          max_tokens: 8192
          temperature: 1
          stream: true
      summary:
        provider_id: moonshot
        model: kimi-k2.6
        params:
          max_tokens: 4096
          temperature: 0.1
          stream: false
    work:
      main:
        provider_id: anthropic-main
        model: claude-sonnet
        params:
          max_tokens: 4096
          thinking:
            mode: adaptive
            effort: medium
      summary:
        provider_id: moonshot
        model: kimi-k2.6
        params:
          max_tokens: 2048
          extra:
            response_format:
              type: json_object
    context_overrides:
      input_budget_tokens: 12000
agent:
  active_config: default
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.LLMProviders) != 2 {
		t.Fatalf("len(LLMProviders) = %d, want 2", len(cfg.LLMProviders))
	}
	if got := cfg.LLMProviders[0].Protocol; got != "openai_compatible" {
		t.Fatalf("provider protocol = %q, want openai_compatible", got)
	}
	if got := cfg.LLMProviders[0].PresetID; got != "moonshot" {
		t.Fatalf("provider preset_id = %q, want moonshot", got)
	}
	if len(cfg.AgentConfigs) != 1 {
		t.Fatalf("len(AgentConfigs) = %d, want 1", len(cfg.AgentConfigs))
	}
	agent := cfg.AgentConfigs[0]
	if agent.PersonaKey != "default" {
		t.Fatalf("persona_key = %q, want default", agent.PersonaKey)
	}
	if agent.Emotion.Main.Params.Temperature == nil || *agent.Emotion.Main.Params.Temperature != 1 {
		t.Fatalf("emotion main temperature = %#v, want 1", agent.Emotion.Main.Params.Temperature)
	}
	if agent.Work.Main.Params.Thinking == nil || agent.Work.Main.Params.Thinking.Mode != "adaptive" {
		t.Fatalf("work main thinking = %#v, want adaptive", agent.Work.Main.Params.Thinking)
	}
	if got := cfg.Agent.ActiveConfig; got != "default" {
		t.Fatalf("agent.active_config = %q, want default", got)
	}
}

func TestLLMProviderWithPresetDefaultsAppliesSiliconFlowProviderCapabilities(t *testing.T) {
	provider, err := (LLMProvider{PresetID: "siliconflow"}).WithPresetDefaults()
	if err != nil {
		t.Fatalf("WithPresetDefaults: %v", err)
	}
	if provider.ID != "siliconflow" || provider.Name != "SiliconFlow" {
		t.Fatalf("provider identity = %#v", provider)
	}
	if provider.Protocol != "openai_compatible" {
		t.Fatalf("Protocol = %q, want openai_compatible", provider.Protocol)
	}
	if provider.ModelDiscovery != "siliconflow_models" {
		t.Fatalf("ModelDiscovery = %q, want siliconflow_models", provider.ModelDiscovery)
	}
	for _, capability := range []string{"chat", "query_analysis", "embedding", "rerank"} {
		if !testContainsString(provider.Capabilities, capability) {
			t.Fatalf("Capabilities = %#v, want %q", provider.Capabilities, capability)
		}
	}
	if err := provider.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func testContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestWorkConfigApplyDefaults_PausedPersistence(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Work.SoftTTL != 30*time.Minute {
		t.Fatalf("SoftTTL = %v, want 30m", cfg.Work.SoftTTL)
	}
	if cfg.Work.HardTTL != time.Hour {
		t.Fatalf("HardTTL = %v, want 1h", cfg.Work.HardTTL)
	}
	if cfg.Work.ArchiveTTL != 24*time.Hour {
		t.Fatalf("ArchiveTTL = %v, want 24h", cfg.Work.ArchiveTTL)
	}
	if cfg.Work.ResumeClaimTTL != 10*time.Minute {
		t.Fatalf("ResumeClaimTTL = %v, want 10m", cfg.Work.ResumeClaimTTL)
	}

	cfg = DefaultConfig()
	cfg.Work.PendingDecisionTTL = 45 * time.Minute
	cfg.Work.SoftTTL = 0
	cfg.Work.HardTTL = 0
	cfg.Work.ArchiveTTL = 0
	cfg.Work.ResumeClaimTTL = 0
	cfg.Work.ApplyDefaults()

	if cfg.Work.SoftTTL != 45*time.Minute {
		t.Fatalf("SoftTTL fallback = %v, want 45m from pending_decision_ttl", cfg.Work.SoftTTL)
	}
	if cfg.Work.HardTTL != time.Hour {
		t.Fatalf("HardTTL after ApplyDefaults = %v, want 1h", cfg.Work.HardTTL)
	}
	if cfg.Work.ArchiveTTL != 24*time.Hour {
		t.Fatalf("ArchiveTTL after ApplyDefaults = %v, want 24h", cfg.Work.ArchiveTTL)
	}
	if cfg.Work.ResumeClaimTTL != 10*time.Minute {
		t.Fatalf("ResumeClaimTTL after ApplyDefaults = %v, want 10m", cfg.Work.ResumeClaimTTL)
	}
}

func TestValidateInvalidPort(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.Port = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for port 0")
	}
}

func TestValidateRejectsInvalidContextRatios(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Context.SoftCompactRatio = 0.95
	cfg.Context.HardCompactRatio = 0.9
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for soft >= hard")
	}
}

func TestValidateRejectsInvalidContextBudget(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Context.InputBudgetTokens = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid context budget")
	}
}

func TestValidateRejectsInvalidAgentContextOverrides(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LLMProviders = []LLMProvider{{
		ID:             "moonshot",
		Name:           "Moonshot",
		Protocol:       "openai_compatible",
		BaseURL:        "https://api.moonshot.cn",
		APIKeyEnv:      "MOONSHOT_API_KEY",
		ModelDiscovery: "manual",
		Enabled:        true,
	}}
	cfg.AgentConfigs = []AgentConfig{validAgentConfig()}
	cfg.AgentConfigs[0].ContextOverrides = map[string]any{"input_budget_tokens": 0}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid agent input_budget_tokens override")
	}
}

func TestValidateRejectsInvalidSummaryTemperature(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AgentConfigs = []AgentConfig{validAgentConfig()}
	cfg.AgentConfigs[0].Emotion.Summary.Params.Temperature = floatPtr(2.5)
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid agent summary temperature")
	}
}

func TestValidateRejectsInvalidSummaryMaxTokens(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AgentConfigs = []AgentConfig{validAgentConfig()}
	cfg.AgentConfigs[0].Emotion.Summary.Params.MaxTokens = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid agent summary max_tokens")
	}
}

func TestWorkConfig_CompressionDefaults(t *testing.T) {
	cfg := DefaultConfig()
	w := cfg.Work
	if w.CompressSoftRatio != 0.7 {
		t.Fatalf("CompressSoftRatio = %f, want 0.7", w.CompressSoftRatio)
	}
	if w.CompressKeepRounds != 2 {
		t.Fatalf("CompressKeepRounds = %d, want 2", w.CompressKeepRounds)
	}
	if w.ToolSnipSoftTokens != 500 {
		t.Fatalf("ToolSnipSoftTokens = %d, want 500", w.ToolSnipSoftTokens)
	}
	if w.ToolSnipHardTokens != 2000 {
		t.Fatalf("ToolSnipHardTokens = %d, want 2000", w.ToolSnipHardTokens)
	}
}

func TestConfigValidateRejectsInvalidWorkCompression(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Config)
		want string
	}{
		{
			name: "soft ratio <= 0",
			mut: func(cfg *Config) {
				cfg.Work.CompressSoftRatio = 0
			},
			want: "work.compress_soft_ratio must be between 0 and 1",
		},
		{
			name: "soft ratio >= 1",
			mut: func(cfg *Config) {
				cfg.Work.CompressSoftRatio = 1
			},
			want: "work.compress_soft_ratio must be between 0 and 1",
		},
		{
			name: "keep rounds <= 0",
			mut: func(cfg *Config) {
				cfg.Work.CompressKeepRounds = 0
			},
			want: "work.compress_keep_rounds must be > 0",
		},
		{
			name: "tool snip soft <= 0",
			mut: func(cfg *Config) {
				cfg.Work.ToolSnipSoftTokens = 0
			},
			want: "work.tool_snip_soft_tokens must be > 0",
		},
		{
			name: "tool snip hard <= 0",
			mut: func(cfg *Config) {
				cfg.Work.ToolSnipHardTokens = 0
			},
			want: "work.tool_snip_hard_tokens must be > 0",
		},
		{
			name: "tool snip soft >= hard",
			mut: func(cfg *Config) {
				cfg.Work.ToolSnipSoftTokens = 3000
				cfg.Work.ToolSnipHardTokens = 2000
			},
			want: "work.tool_snip_soft_tokens must be < work.tool_snip_hard_tokens",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.mut(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

func validAgentConfig() AgentConfig {
	return AgentConfig{
		ID:         "default",
		Name:       "Default",
		PersonaKey: "default",
		Emotion: AgentModelGroup{
			Main:    ModelBinding{ProviderID: "moonshot", Model: "kimi-k2.6"},
			Summary: ModelBinding{ProviderID: "moonshot", Model: "kimi-k2.6"},
		},
		Work: AgentModelGroup{
			Main:    ModelBinding{ProviderID: "moonshot", Model: "kimi-k2.6"},
			Summary: ModelBinding{ProviderID: "moonshot", Model: "kimi-k2.6"},
		},
	}
}

func floatPtr(v float64) *float64 { return &v }

func TestDefaultWebSearchConfig(t *testing.T) {
	cfg := DefaultConfig()
	ws := cfg.WebSearch
	if ws.Enabled != false {
		t.Errorf("default websearch.enabled = %v, want false", ws.Enabled)
	}
	if ws.Provider != "tavily" {
		t.Errorf("default websearch.provider = %q, want tavily", ws.Provider)
	}
	if ws.APIKeyEnv != "TAVILY_API_KEY" {
		t.Errorf("default websearch.api_key_env = %q, want TAVILY_API_KEY", ws.APIKeyEnv)
	}
	if ws.MaxResults != 5 {
		t.Errorf("default websearch.max_results = %d, want 5", ws.MaxResults)
	}
	if ws.TimeoutSec != 30 {
		t.Errorf("default websearch.timeout_sec = %d, want 30", ws.TimeoutSec)
	}
	if ws.IncludeAnswer != false {
		t.Errorf("default websearch.include_answer = %v, want false", ws.IncludeAnswer)
	}
	pipeline := reflect.ValueOf(ws).FieldByName("Pipeline")
	if !pipeline.IsValid() {
		t.Fatalf("websearch config missing Pipeline")
	}
	enabled := pipeline.FieldByName("Enabled")
	if !enabled.IsValid() || enabled.Kind() != reflect.Bool {
		t.Fatalf("websearch.pipeline.enabled missing or non-bool")
	}
	if enabled.Bool() {
		t.Fatalf("default websearch.pipeline.enabled = true, want false")
	}
	reader := pipeline.FieldByName("Reader")
	if !reader.IsValid() {
		t.Fatalf("websearch.pipeline missing Reader")
	}
	if enabled := reader.FieldByName("Enabled"); !enabled.IsValid() || !enabled.Bool() {
		t.Fatalf("default websearch.pipeline.reader.enabled = %#v, want true", enabled)
	}
	assertIntConfigField(t, reader, "TopN", 4)
	assertStringConfigField(t, reader, "ExtractDepth", "basic")
	assertStringConfigField(t, reader, "Format", "markdown")
	if maxChars := reader.FieldByName("MaxCharsPerDoc"); !maxChars.IsValid() || maxChars.Int() <= 0 {
		t.Fatalf("default websearch.pipeline.reader.max_chars_per_doc = %#v, want > 0", maxChars)
	}
	if chunkChars := reader.FieldByName("MaxChunkChars"); !chunkChars.IsValid() || chunkChars.Int() <= 0 {
		t.Fatalf("default websearch.pipeline.reader.max_chunk_chars = %#v, want > 0", chunkChars)
	}
	rerank := pipeline.FieldByName("Rerank")
	if !rerank.IsValid() {
		t.Fatalf("websearch.pipeline missing Rerank")
	}
	if enabled := rerank.FieldByName("Enabled"); !enabled.IsValid() || !enabled.Bool() {
		t.Fatalf("default websearch.pipeline.rerank.enabled = %#v, want true", enabled)
	}
	assertStringConfigField(t, rerank, "Provider", "siliconflow")
	assertStringConfigField(t, rerank, "Fallback", "heuristic")
	assertStringConfigField(t, rerank, "Model", "BAAI/bge-reranker-v2-m3")
	assertStringConfigField(t, rerank, "APIKeyEnv", "SILICONFLOW_API_KEY")
	assertIntConfigField(t, rerank, "TimeoutSec", 10)
	assertIntConfigField(t, rerank, "InputTopN", 8)
	assertIntConfigField(t, rerank, "TopK", 5)
	assertIntConfigField(t, rerank, "MaxDocChars", 4000)
}

func TestLoadWebSearchPipelineSearchConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
websearch:
  enabled: true
  provider: pipeline
  api_key_env: TAVILY_API_KEY
  pipeline:
    enabled: true
    search:
      default_profile: official_docs
      default_depth: advanced
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	pipeline := reflect.ValueOf(cfg.WebSearch).FieldByName("Pipeline")
	if !pipeline.IsValid() {
		t.Fatalf("websearch config missing Pipeline")
	}
	if enabled := pipeline.FieldByName("Enabled"); !enabled.IsValid() || !enabled.Bool() {
		t.Fatalf("websearch.pipeline.enabled = %#v, want true", enabled)
	}
	search := pipeline.FieldByName("Search")
	if !search.IsValid() {
		t.Fatalf("websearch.pipeline missing Search")
	}
	assertStringConfigField(t, search, "DefaultProfile", "official_docs")
	assertStringConfigField(t, search, "DefaultDepth", "advanced")
}

func TestLoadWebSearchPipelineReaderConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
websearch:
  enabled: true
  provider: pipeline
  api_key_env: TAVILY_API_KEY
  base_url: http://127.0.0.1:9999
  pipeline:
    enabled: true
    reader:
      enabled: false
      top_n: 2
      extract_depth: advanced
      format: text
      max_chars_per_doc: 1234
      max_chunk_chars: 321
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ws := reflect.ValueOf(cfg.WebSearch)
	assertStringConfigField(t, ws, "BaseURL", "http://127.0.0.1:9999")
	reader := ws.FieldByName("Pipeline").FieldByName("Reader")
	if !reader.IsValid() {
		t.Fatalf("websearch.pipeline missing Reader")
	}
	if enabled := reader.FieldByName("Enabled"); !enabled.IsValid() || enabled.Bool() {
		t.Fatalf("websearch.pipeline.reader.enabled = %#v, want false", enabled)
	}
	assertIntConfigField(t, reader, "TopN", 2)
	assertStringConfigField(t, reader, "ExtractDepth", "advanced")
	assertStringConfigField(t, reader, "Format", "text")
	assertIntConfigField(t, reader, "MaxCharsPerDoc", 1234)
	assertIntConfigField(t, reader, "MaxChunkChars", 321)
}

func TestLoadWebSearchPipelineReaderTopNZeroIsPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
websearch:
  enabled: true
  provider: pipeline
  api_key_env: TAVILY_API_KEY
  pipeline:
    enabled: true
    reader:
      enabled: true
      top_n: 0
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	reader := reflect.ValueOf(cfg.WebSearch).FieldByName("Pipeline").FieldByName("Reader")
	assertIntConfigField(t, reader, "TopN", 0)
}

func TestLoadWebSearchPipelineRerankConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
websearch:
  enabled: true
  provider: pipeline
  api_key_env: TAVILY_API_KEY
  pipeline:
    enabled: true
    rerank:
      enabled: true
      provider: siliconflow
      model: BAAI/bge-reranker-v2-m3
      base_url: https://api.siliconflow.cn
      path: /v1/rerank
      api_key_env: SILICONFLOW_API_KEY
      fallback: heuristic
      timeout_sec: 7
      input_top_n: 6
      top_k: 3
      max_doc_chars: 2048
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rerank := reflect.ValueOf(cfg.WebSearch).FieldByName("Pipeline").FieldByName("Rerank")
	if !rerank.IsValid() {
		t.Fatalf("websearch.pipeline missing Rerank")
	}
	if enabled := rerank.FieldByName("Enabled"); !enabled.IsValid() || !enabled.Bool() {
		t.Fatalf("websearch.pipeline.rerank.enabled = %#v, want true", enabled)
	}
	assertStringConfigField(t, rerank, "Provider", "siliconflow")
	assertStringConfigField(t, rerank, "Model", "BAAI/bge-reranker-v2-m3")
	assertStringConfigField(t, rerank, "BaseURL", "https://api.siliconflow.cn")
	assertStringConfigField(t, rerank, "Path", "/v1/rerank")
	assertStringConfigField(t, rerank, "APIKeyEnv", "SILICONFLOW_API_KEY")
	assertStringConfigField(t, rerank, "Fallback", "heuristic")
	assertIntConfigField(t, rerank, "TimeoutSec", 7)
	assertIntConfigField(t, rerank, "InputTopN", 6)
	assertIntConfigField(t, rerank, "TopK", 3)
	assertIntConfigField(t, rerank, "MaxDocChars", 2048)
}

func TestLoadWebSearchPipelineExampleConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
websearch:
  enabled: true
  provider: pipeline
  api_key_env: TAVILY_API_KEY
  base_url: "https://api.tavily.com"
  max_results: 5
  timeout_sec: 30
  include_answer: false
  pipeline:
    enabled: true
    search:
      default_profile: auto
      default_depth: advanced
      fast_depth: basic
      max_subqueries: 1
      candidate_cap: 10
      per_query_max_results: 5
    reader:
      enabled: true
      top_n: 4
      extract_depth: basic
      format: markdown
      timeout_sec: 20
      max_chars_per_doc: 12000
      max_chunk_chars: 2000
    rerank:
      enabled: true
      provider: siliconflow
      base_url: "https://api.siliconflow.cn"
      path: "/v1/rerank"
      api_key_env: SILICONFLOW_API_KEY
      model: "BAAI/bge-reranker-v2-m3"
      fallback: heuristic
      timeout_sec: 10
      input_top_n: 8
      top_k: 5
      max_doc_chars: 4000
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.WebSearch.Provider != "pipeline" || !cfg.WebSearch.Pipeline.Enabled {
		t.Fatalf("websearch provider/pipeline = %#v, want enabled pipeline", cfg.WebSearch)
	}
	if cfg.WebSearch.BaseURL != "https://api.tavily.com" {
		t.Fatalf("websearch.base_url = %q", cfg.WebSearch.BaseURL)
	}
	if !cfg.WebSearch.Pipeline.Reader.Enabled || cfg.WebSearch.Pipeline.Reader.TopN != 4 {
		t.Fatalf("reader config = %#v", cfg.WebSearch.Pipeline.Reader)
	}
	rerank := cfg.WebSearch.Pipeline.Rerank
	if !rerank.Enabled || rerank.Provider != "siliconflow" || rerank.APIKeyEnv != "SILICONFLOW_API_KEY" ||
		rerank.Model != "BAAI/bge-reranker-v2-m3" || rerank.Fallback != "heuristic" {
		t.Fatalf("rerank config = %#v", rerank)
	}
}

func TestWebSearchValidateProviderEnum(t *testing.T) {
	tests := []struct {
		provider string
		wantErr  bool
	}{
		{provider: "tavily"},
		{provider: "pipeline"},
		{provider: "unknown", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.WebSearch.Enabled = true
			cfg.WebSearch.Provider = tt.provider
			err := cfg.Validate()
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "websearch.provider") {
					t.Fatalf("Validate error = %v, want websearch.provider error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate error = %v, want nil", err)
			}
		})
	}
}

func TestWebSearchValidateLegacyTavilyIgnoresDisabledPipelineFields(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WebSearch.Enabled = true
	cfg.WebSearch.Provider = "tavily"
	cfg.WebSearch.Pipeline.Enabled = false
	cfg.WebSearch.Pipeline.Reader.ExtractDepth = "invalid"
	cfg.WebSearch.Pipeline.Reader.Format = "invalid"
	cfg.WebSearch.Pipeline.Rerank.Provider = "invalid"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate error = %v, want nil for legacy tavily rollback", err)
	}
}

func TestWebSearchValidateRerankProviderEnum(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		wantErr  bool
	}{
		{name: "heuristic", provider: "heuristic"},
		{name: "siliconflow", provider: "siliconflow"},
		{name: "unknown", provider: "other", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.WebSearch.Enabled = true
			cfg.WebSearch.Provider = "pipeline"
			cfg.WebSearch.Pipeline.Enabled = true
			rerank := reflect.ValueOf(&cfg.WebSearch.Pipeline).Elem().FieldByName("Rerank")
			if !rerank.IsValid() {
				t.Fatalf("websearch.pipeline missing Rerank")
			}
			rerank.FieldByName("Enabled").SetBool(true)
			rerank.FieldByName("Provider").SetString(tt.provider)

			err := cfg.Validate()
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "websearch.pipeline.rerank.provider") {
					t.Fatalf("Validate error = %v, want rerank provider error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate error = %v, want nil", err)
			}
		})
	}
}

func assertStringConfigField(t *testing.T, owner reflect.Value, name, want string) {
	t.Helper()
	field := owner.FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("config missing %s", name)
	}
	if field.Kind() != reflect.String {
		t.Fatalf("%s kind = %s, want string", name, field.Kind())
	}
	if got := field.String(); got != want {
		t.Fatalf("%s = %q, want %q", name, got, want)
	}
}

func assertIntConfigField(t *testing.T, owner reflect.Value, name string, want int) {
	t.Helper()
	field := owner.FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("config missing %s", name)
	}
	if field.Kind() != reflect.Int {
		t.Fatalf("%s kind = %s, want int", name, field.Kind())
	}
	if got := int(field.Int()); got != want {
		t.Fatalf("%s = %d, want %d", name, got, want)
	}
}

func TestDefaultWebFetchConfig(t *testing.T) {
	cfg := DefaultConfig()
	wf := cfg.WebFetch
	if wf.Enabled != true {
		t.Errorf("default webfetch.enabled = %v, want true", wf.Enabled)
	}
	if wf.Provider != "tavily" {
		t.Errorf("default webfetch.provider = %q, want tavily", wf.Provider)
	}
	if wf.APIKeyEnv != "TAVILY_API_KEY" {
		t.Errorf("default webfetch.api_key_env = %q, want TAVILY_API_KEY", wf.APIKeyEnv)
	}
	if wf.BaseURL != "https://api.tavily.com" {
		t.Errorf("default webfetch.base_url = %q, want https://api.tavily.com", wf.BaseURL)
	}
	if wf.ExtractDepth != "basic" {
		t.Errorf("default webfetch.extract_depth = %q, want basic", wf.ExtractDepth)
	}
	if wf.Format != "markdown" {
		t.Errorf("default webfetch.format = %q, want markdown", wf.Format)
	}
}

func TestLoadWebFetchDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
webfetch:
  enabled: true
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WebFetch.Provider != "tavily" {
		t.Errorf("webfetch.provider = %q, want tavily", cfg.WebFetch.Provider)
	}
	if cfg.WebFetch.APIKeyEnv != "TAVILY_API_KEY" {
		t.Errorf("webfetch.api_key_env = %q, want TAVILY_API_KEY", cfg.WebFetch.APIKeyEnv)
	}
	if cfg.WebFetch.BaseURL != "https://api.tavily.com" {
		t.Errorf("webfetch.base_url = %q, want https://api.tavily.com", cfg.WebFetch.BaseURL)
	}
	if cfg.WebFetch.ExtractDepth != "basic" {
		t.Errorf("webfetch.extract_depth = %q, want basic", cfg.WebFetch.ExtractDepth)
	}
	if cfg.WebFetch.Format != "markdown" {
		t.Errorf("webfetch.format = %q, want markdown", cfg.WebFetch.Format)
	}
}

func TestWebSearchValidateEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WebSearch.Enabled = true
	// valid: provider and api_key_env are both set from defaults
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no validation error with valid websearch config, got: %v", err)
	}
}

func TestWebSearchValidateEmptyProvider(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WebSearch.Enabled = true
	cfg.WebSearch.Provider = ""
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error when websearch.provider is empty")
	}
}

func TestWebSearchValidateEmptyAPIKeyEnv(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WebSearch.Enabled = true
	cfg.WebSearch.APIKeyEnv = ""
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error when websearch.api_key_env is empty")
	}
}
