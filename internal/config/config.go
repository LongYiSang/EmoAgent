package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	memconfig "github.com/longyisang/emoagent-memorycore/config"
	"github.com/longyisang/emoagent/internal/llm"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Time         TimeConfig         `yaml:"time"`
	Chat         ChatConfig         `yaml:"chat"`
	Context      ContextConfig      `yaml:"context"`
	Work         WorkConfig         `yaml:"work"`
	LLMProviders []LLMProvider      `yaml:"llm_providers"`
	AgentConfigs []AgentConfig      `yaml:"agent_configs"`
	Agent        AgentRuntimeConfig `yaml:"agent"`
	AgentAffect  AgentAffectConfig  `yaml:"agent_affect" json:"agent_affect"`
	Memory       MemoryConfig       `yaml:"memory"`
	Media        MediaConfig        `yaml:"media" json:"media"`
	DB           DBConfig           `yaml:"db"`
	Log          LogConfig          `yaml:"log"`
	Personas     PersonasConfig     `yaml:"personas"`
	WebSearch    WebSearchConfig    `yaml:"websearch"`
	WebFetch     WebFetchConfig     `yaml:"webfetch"`
	Bash         BashConfig         `yaml:"bash"`
	Plugins      PluginsConfig      `yaml:"plugins"`
}

type MediaConfig struct {
	StorageDir string `yaml:"storage_dir" json:"storage_dir"`
	MaxBytes   int64  `yaml:"max_bytes" json:"max_bytes"`
	MaxPixels  int    `yaml:"max_pixels" json:"max_pixels"`
}

type TimeConfig struct {
	Timezone string `yaml:"timezone" json:"timezone"`
}

type LLMProvider struct {
	ID             string   `yaml:"id" json:"id"`
	Name           string   `yaml:"name" json:"name"`
	PresetID       string   `yaml:"preset_id" json:"preset_id"`
	Protocol       string   `yaml:"protocol" json:"protocol"`
	BaseURL        string   `yaml:"base_url" json:"base_url"`
	APIKeyEnv      string   `yaml:"api_key_env" json:"api_key_env"`
	ModelDiscovery string   `yaml:"model_discovery" json:"model_discovery"`
	Enabled        bool     `yaml:"enabled" json:"enabled"`
	Capabilities   []string `yaml:"capabilities" json:"capabilities"`
}

type AgentRuntimeConfig struct {
	ActiveConfig string `yaml:"active_config" json:"active_config"`
}

type AgentAffectConfig struct {
	Enabled         bool                             `yaml:"enabled" json:"enabled"`
	UpdateMode      string                           `yaml:"update_mode" json:"update_mode"`
	StorageEnabled  bool                             `yaml:"storage_enabled" json:"storage_enabled"`
	State           AgentAffectStateConfig           `yaml:"state" json:"state"`
	Async           AgentAffectAsyncConfig           `yaml:"async" json:"async"`
	Evaluator       AgentAffectEvaluatorConfig       `yaml:"evaluator" json:"evaluator"`
	Context         AgentAffectContextConfig         `yaml:"context" json:"context"`
	Dimensions      AgentAffectDimensionsConfig      `yaml:"dimensions" json:"dimensions"`
	Externalization AgentAffectExternalizationConfig `yaml:"externalization" json:"externalization"`
	PluginAPI       AgentAffectPluginAPIConfig       `yaml:"plugin_api" json:"plugin_api"`
	Limits          AgentAffectLimitsConfig          `yaml:"limits" json:"limits"`
	Features        AgentAffectFeaturesConfig        `yaml:"features" json:"features"`
	Prompt          AgentAffectPromptConfig          `yaml:"prompt" json:"prompt"`
}

type AgentAffectStateConfig struct {
	Scope              string `yaml:"scope" json:"scope"`
	RecentContextScope string `yaml:"recent_context_scope" json:"recent_context_scope"`
}

type AgentAffectAsyncConfig struct {
	Enabled               bool                        `yaml:"enabled" json:"enabled"`
	QueueEnabled          bool                        `yaml:"queue_enabled" json:"queue_enabled"`
	WorkerEnabled         bool                        `yaml:"worker_enabled" json:"worker_enabled"`
	WorkerConcurrency     int                         `yaml:"worker_concurrency" json:"worker_concurrency"`
	PollIntervalMS        int                         `yaml:"poll_interval_ms" json:"poll_interval_ms"`
	QueueClaimTTLSeconds  int                         `yaml:"queue_claim_ttl_seconds" json:"queue_claim_ttl_seconds"`
	MaxAttempts           int                         `yaml:"max_attempts" json:"max_attempts"`
	RetryBaseDelaySeconds int                         `yaml:"retry_base_delay_seconds" json:"retry_base_delay_seconds"`
	RetryMaxDelaySeconds  int                         `yaml:"retry_max_delay_seconds" json:"retry_max_delay_seconds"`
	ClearRawAfterDone     bool                        `yaml:"clear_raw_after_done" json:"clear_raw_after_done"`
	Batch                 AgentAffectAsyncBatchConfig `yaml:"batch" json:"batch"`
}

type AgentAffectAsyncBatchConfig struct {
	Enabled                 bool `yaml:"enabled" json:"enabled"`
	MaxJobs                 int  `yaml:"max_jobs" json:"max_jobs"`
	MaxInputTokens          int  `yaml:"max_input_tokens" json:"max_input_tokens"`
	MaxAgeSeconds           int  `yaml:"max_age_seconds" json:"max_age_seconds"`
	MinWaitMS               int  `yaml:"min_wait_ms" json:"min_wait_ms"`
	MergeAcrossSessions     bool `yaml:"merge_across_sessions" json:"merge_across_sessions"`
	BreakOnManualBarrier    bool `yaml:"break_on_manual_barrier" json:"break_on_manual_barrier"`
	SummarizeTurnsBeforeLLM bool `yaml:"summarize_turns_before_llm" json:"summarize_turns_before_llm"`
}

type AgentAffectEvaluatorConfig struct {
	Mode                string  `yaml:"mode" json:"mode"`
	ProviderID          string  `yaml:"provider_id" json:"provider_id"`
	Model               string  `yaml:"model" json:"model"`
	ThinkingEnabled     bool    `yaml:"thinking_enabled" json:"thinking_enabled"`
	ReasoningEffort     string  `yaml:"reasoning_effort" json:"reasoning_effort"`
	TimeoutMS           int     `yaml:"timeout_ms" json:"timeout_ms"`
	MaxOutputTokens     int     `yaml:"max_output_tokens" json:"max_output_tokens"`
	Temperature         float64 `yaml:"temperature" json:"temperature"`
	StoreHiddenThinking bool    `yaml:"store_hidden_thinking" json:"store_hidden_thinking"`
}

type AgentAffectContextConfig struct {
	Mode                       string `yaml:"mode" json:"mode"`
	RawKeepLastRequests        int    `yaml:"raw_keep_last_requests" json:"raw_keep_last_requests"`
	RawKeepLastTokens          int    `yaml:"raw_keep_last_tokens" json:"raw_keep_last_tokens"`
	IncludePreviousEvaluations bool   `yaml:"include_previous_evaluations" json:"include_previous_evaluations"`
	PreviousEvaluationKeepLast int    `yaml:"previous_evaluation_keep_last" json:"previous_evaluation_keep_last"`
	SummaryEnabled             bool   `yaml:"summary_enabled" json:"summary_enabled"`
	StoreRawInputs             bool   `yaml:"store_raw_inputs" json:"store_raw_inputs"`
	StorePromptSnapshot        bool   `yaml:"store_prompt_snapshot" json:"store_prompt_snapshot"`
}

type AgentAffectDimensionsConfig struct {
}

type AgentAffectExternalizationConfig struct {
	Attachment  ExternalizedDimensionConfig `yaml:"attachment" json:"attachment"`
	Frustration ExternalizedDimensionConfig `yaml:"frustration" json:"frustration"`
}

type ExternalizedDimensionConfig struct {
	Enabled             bool    `yaml:"enabled" json:"enabled"`
	DefaultStyle        string  `yaml:"default_style" json:"default_style"`
	MaxVisibleIntensity float64 `yaml:"max_visible_intensity" json:"max_visible_intensity"`
}

type AgentAffectPluginAPIConfig struct {
	Enabled                      bool `yaml:"enabled" json:"enabled"`
	PluginSafeIncludeReason      bool `yaml:"plugin_safe_include_reason" json:"plugin_safe_include_reason"`
	PluginSafeIncludeRawText     bool `yaml:"plugin_safe_include_raw_text" json:"plugin_safe_include_raw_text"`
	OrdinaryPluginsCanCommit     bool `yaml:"ordinary_plugins_can_commit" json:"ordinary_plugins_can_commit"`
	OrdinaryPluginsCanWriteDelta bool `yaml:"ordinary_plugins_can_write_delta" json:"ordinary_plugins_can_write_delta"`
	TrustedPluginsCanWriteTarget bool `yaml:"trusted_plugins_can_write_target" json:"trusted_plugins_can_write_target"`
}

type AgentAffectLimitsConfig struct {
	PerRequestDelta       AgentAffectVectorLimitsConfig   `yaml:"per_request_delta" json:"per_request_delta"`
	Absolute              AgentAffectAbsoluteLimitsConfig `yaml:"absolute" json:"absolute"`
	PluginDeltaMultiplier float64                         `yaml:"plugin_delta_multiplier" json:"plugin_delta_multiplier"`
}

type AgentAffectVectorLimitsConfig struct {
	Valence     float64 `yaml:"valence" json:"valence"`
	Arousal     float64 `yaml:"arousal" json:"arousal"`
	Dominance   float64 `yaml:"dominance" json:"dominance"`
	Energy      float64 `yaml:"energy" json:"energy"`
	Warmth      float64 `yaml:"warmth" json:"warmth"`
	Concern     float64 `yaml:"concern" json:"concern"`
	Curiosity   float64 `yaml:"curiosity" json:"curiosity"`
	Playfulness float64 `yaml:"playfulness" json:"playfulness"`
	Attachment  float64 `yaml:"attachment" json:"attachment"`
	Frustration float64 `yaml:"frustration" json:"frustration"`
	Uncertainty float64 `yaml:"uncertainty" json:"uncertainty"`
}

type AgentAffectAbsoluteLimitsConfig struct {
	AttachmentMax  float64 `yaml:"attachment_max" json:"attachment_max"`
	FrustrationMax float64 `yaml:"frustration_max" json:"frustration_max"`
}

type AgentAffectFeaturesConfig struct {
}

type AgentAffectPromptConfig struct {
	Mode                      string `yaml:"mode" json:"mode"`
	IncludeMoodBlock          bool   `yaml:"include_mood_block" json:"include_mood_block"`
	IncludeReason             bool   `yaml:"include_reason" json:"include_reason"`
	IncludeExpressionGuidance bool   `yaml:"include_expression_guidance" json:"include_expression_guidance"`
	IncludeNumericValues      bool   `yaml:"include_numeric_values" json:"include_numeric_values"`
	MaxPromptChars            int    `yaml:"max_prompt_chars" json:"max_prompt_chars"`
}

type MemoryConfig struct {
	Enabled           bool                               `yaml:"enabled" json:"enabled"`
	ConfigPath        string                             `yaml:"config_path" json:"config_path"`
	ManualRulesPath   string                             `yaml:"manual_rules_path" json:"manual_rules_path"`
	Retrieval         MemoryRetrievalConfig              `yaml:"retrieval" json:"retrieval"`
	Extraction        MemoryExtractionConfig             `yaml:"extraction" json:"extraction"`
	Sidecar           MemorySidecarConfig                `yaml:"sidecar" json:"sidecar"`
	ProviderBindings  MemoryProviderBindingsConfig       `yaml:"provider_bindings" json:"provider_bindings"`
	NaturalMemory     MemoryNaturalMemoryConfig          `yaml:"natural_memory" json:"natural_memory"`
	Retention         *memconfig.RetentionConfig         `yaml:"retention,omitempty" json:"retention,omitempty"`
	ForgettingPrivacy *memconfig.ForgettingPrivacyConfig `yaml:"forgetting_privacy,omitempty" json:"forgetting_privacy,omitempty"`
	AgentAffect       *memconfig.AgentAffectConfig       `yaml:"agent_affect,omitempty" json:"agent_affect,omitempty"`
}

type MemoryRetrievalConfig struct {
	Enabled             bool `yaml:"enabled" json:"enabled"`
	InjectPrompt        bool `yaml:"inject_prompt" json:"inject_prompt"`
	UseFTS              bool `yaml:"use_fts" json:"use_fts"`
	UseMirror           bool `yaml:"use_mirror" json:"use_mirror"`
	FinalMemoryCount    int  `yaml:"final_memory_count" json:"final_memory_count"`
	ContextBudgetTokens int  `yaml:"context_budget_tokens" json:"context_budget_tokens"`
	FailOpen            bool `yaml:"fail_open" json:"fail_open"`
	PipelineDebug       bool `yaml:"pipeline_debug" json:"pipeline_debug"`
}

type MemoryExtractionConfig struct {
	Enabled                  bool                           `yaml:"enabled" json:"enabled"`
	Mode                     string                         `yaml:"mode" json:"mode"`
	TriggerOnFinalizeSegment bool                           `yaml:"trigger_on_finalize_segment" json:"trigger_on_finalize_segment"`
	TriggerOnManualPin       bool                           `yaml:"trigger_on_manual_pin" json:"trigger_on_manual_pin"`
	SessionEndMode           string                         `yaml:"session_end_mode" json:"session_end_mode"`
	ManualPinMode            string                         `yaml:"manual_pin_mode" json:"manual_pin_mode"`
	Limit                    int                            `yaml:"limit" json:"limit"`
	Timezone                 string                         `yaml:"timezone" json:"timezone"`
	AllowInference           bool                           `yaml:"allow_inference" json:"allow_inference"`
	AllowSensitiveExtraction bool                           `yaml:"allow_sensitive_extraction" json:"allow_sensitive_extraction"`
	MaxFacts                 int                            `yaml:"max_facts" json:"max_facts"`
	MaxLinks                 int                            `yaml:"max_links" json:"max_links"`
	Async                    MemoryExtractionAsyncConfig    `yaml:"async" json:"async"`
	Idle                     MemoryExtractionIdleConfig     `yaml:"idle" json:"idle"`
	Manual                   MemoryExtractionManualConfig   `yaml:"manual" json:"manual"`
	MirrorSync               MemoryExtractionMirrorConfig   `yaml:"mirror_sync" json:"mirror_sync"`
	RawLog                   MemoryExtractionRawLogConfig   `yaml:"raw_log" json:"raw_log"`
	Provider                 MemoryExtractionProviderConfig `yaml:"provider" json:"provider"`
	SemanticDedup            MemorySemanticDedupConfig      `yaml:"semantic_dedup" json:"semantic_dedup"`
	RepairEnabled            bool                           `yaml:"repair_enabled" json:"repair_enabled"`
	AuditEnabled             bool                           `yaml:"audit_enabled" json:"audit_enabled"`
}

type MemoryExtractionAsyncConfig struct {
	Enabled               bool `yaml:"enabled" json:"enabled"`
	WorkerEnabled         bool `yaml:"worker_enabled" json:"worker_enabled"`
	WorkerConcurrency     int  `yaml:"worker_concurrency" json:"worker_concurrency"`
	QueueClaimTTLSeconds  int  `yaml:"queue_claim_ttl_seconds" json:"queue_claim_ttl_seconds"`
	MaxAttempts           int  `yaml:"max_attempts" json:"max_attempts"`
	RetryBaseDelaySeconds int  `yaml:"retry_base_delay_seconds" json:"retry_base_delay_seconds"`
	RetryMaxDelaySeconds  int  `yaml:"retry_max_delay_seconds" json:"retry_max_delay_seconds"`
}

type MemoryExtractionIdleConfig struct {
	Enabled                  bool `yaml:"enabled" json:"enabled"`
	IdleAfterSeconds         int  `yaml:"idle_after_seconds" json:"idle_after_seconds"`
	SweepIntervalSeconds     int  `yaml:"sweep_interval_seconds" json:"sweep_interval_seconds"`
	MinEpisodeCount          int  `yaml:"min_episode_count" json:"min_episode_count"`
	MaxSegmentsPerSweep      int  `yaml:"max_segments_per_sweep" json:"max_segments_per_sweep"`
	IncludeFinalizedSegments bool `yaml:"include_finalized_segments" json:"include_finalized_segments"`
	IncludeActiveSegments    bool `yaml:"include_active_segments" json:"include_active_segments"`
}

type MemoryExtractionManualConfig struct {
	Enabled               bool   `yaml:"enabled" json:"enabled"`
	Mode                  string `yaml:"mode" json:"mode"`
	AllowForce            bool   `yaml:"allow_force" json:"allow_force"`
	AllowSegmentSelection bool   `yaml:"allow_segment_selection" json:"allow_segment_selection"`
}

type MemoryExtractionMirrorConfig struct {
	AfterApply                bool `yaml:"after_apply" json:"after_apply"`
	PeriodicEnabled           bool `yaml:"periodic_enabled" json:"periodic_enabled"`
	IntervalSeconds           int  `yaml:"interval_seconds" json:"interval_seconds"`
	Limit                     int  `yaml:"limit" json:"limit"`
	FailExtractionOnSyncError bool `yaml:"fail_extraction_on_sync_error" json:"fail_extraction_on_sync_error"`
}

type MemoryNaturalMemoryConfig struct {
	Enabled                 bool                            `yaml:"enabled" json:"enabled"`
	SchedulerEnabled        bool                            `yaml:"scheduler_enabled" json:"scheduler_enabled"`
	TickIntervalSeconds     int                             `yaml:"tick_interval_seconds" json:"tick_interval_seconds"`
	LocalTime               string                          `yaml:"local_time" json:"local_time"`
	Timezone                string                          `yaml:"timezone" json:"timezone"`
	RunMissedOnStart        bool                            `yaml:"run_missed_on_start" json:"run_missed_on_start"`
	MirrorSyncAfterRun      bool                            `yaml:"mirror_sync_after_run" json:"mirror_sync_after_run"`
	MirrorSyncLimit         int                             `yaml:"mirror_sync_limit" json:"mirror_sync_limit"`
	FailOnSyncError         bool                            `yaml:"fail_on_sync_error" json:"fail_on_sync_error"`
	Manual                  MemoryNaturalMemoryManualConfig `yaml:"manual" json:"manual"`
	MarkSleepCycleByDefault bool                            `yaml:"mark_sleep_cycle_by_default" json:"mark_sleep_cycle_by_default"`
}

type MemoryNaturalMemoryManualConfig struct {
	Enabled                 bool `yaml:"enabled" json:"enabled"`
	AllowDryRun             bool `yaml:"allow_dry_run" json:"allow_dry_run"`
	AllowForce              bool `yaml:"allow_force" json:"allow_force"`
	MarkSleepCycleByDefault bool `yaml:"mark_sleep_cycle_by_default" json:"mark_sleep_cycle_by_default"`
}

type MemorySemanticDedupConfig struct {
	Enabled          bool   `yaml:"enabled" json:"enabled"`
	Shadow           bool   `yaml:"shadow" json:"shadow"`
	Enforce          bool   `yaml:"enforce" json:"enforce"`
	CandidateLimit   int    `yaml:"candidate_limit" json:"candidate_limit"`
	ThresholdProfile string `yaml:"threshold_profile" json:"threshold_profile"`
}

type MemoryExtractionRawLogConfig struct {
	Enabled   bool   `yaml:"enabled" json:"enabled"`
	Directory string `yaml:"directory" json:"directory"`
}

type MemoryExtractionProviderConfig struct {
	Kind           string                         `yaml:"kind" json:"kind"`
	ID             string                         `yaml:"id" json:"id"`
	BaseURL        string                         `yaml:"base_url" json:"base_url"`
	APIKeyEnv      string                         `yaml:"api_key_env" json:"api_key_env"`
	Model          string                         `yaml:"model" json:"model"`
	TimeoutSeconds int                            `yaml:"timeout_seconds" json:"timeout_seconds"`
	MaxTokens      int                            `yaml:"max_tokens" json:"max_tokens"`
	Temperature    float64                        `yaml:"temperature" json:"temperature"`
	Thinking       MemoryExtractionThinkingConfig `yaml:"thinking" json:"thinking"`
}

type MemorySidecarConfig struct {
	Enabled            bool   `yaml:"enabled" json:"enabled"`
	Managed            bool   `yaml:"managed" json:"managed"`
	Adapter            string `yaml:"adapter" json:"adapter"`
	Host               string `yaml:"host" json:"host"`
	Port               int    `yaml:"port" json:"port"`
	URL                string `yaml:"url" json:"url"`
	WorkingDir         string `yaml:"working_dir" json:"working_dir"`
	ConfigPath         string `yaml:"config_path" json:"config_path"`
	StartupTimeoutMS   int    `yaml:"startup_timeout_ms" json:"startup_timeout_ms"`
	ShutdownTimeoutMS  int    `yaml:"shutdown_timeout_ms" json:"shutdown_timeout_ms"`
	FailOpen           bool   `yaml:"fail_open" json:"fail_open"`
	LogPath            string `yaml:"log_path" json:"log_path"`
	TriviumDir         string `yaml:"trivium_dir" json:"trivium_dir"`
	EmbeddingCachePath string `yaml:"embedding_cache_path" json:"embedding_cache_path"`
}

type MemoryProviderBindingsConfig struct {
	Prefilter        MemoryProviderBindingConfig `yaml:"prefilter" json:"prefilter"`
	Extraction       MemoryProviderBindingConfig `yaml:"extraction" json:"extraction"`
	ExtractionRepair MemoryProviderBindingConfig `yaml:"extraction_repair" json:"extraction_repair"`
	QueryAnalysis    MemoryProviderBindingConfig `yaml:"query_analysis" json:"query_analysis"`
	Curation         MemoryProviderBindingConfig `yaml:"curation" json:"curation"`
	Embedding        MemoryProviderBindingConfig `yaml:"embedding" json:"embedding"`
	Rerank           MemoryProviderBindingConfig `yaml:"rerank" json:"rerank"`
}

type MemoryProviderBindingConfig struct {
	Enabled    bool                           `yaml:"enabled" json:"enabled"`
	ProviderID string                         `yaml:"provider_id" json:"provider_id"`
	Model      string                         `yaml:"model" json:"model"`
	MaxTokens  int                            `yaml:"max_tokens" json:"max_tokens"`
	Thinking   MemoryExtractionThinkingConfig `yaml:"thinking" json:"thinking"`
	Dimensions int                            `yaml:"dimensions" json:"dimensions"`
	TopK       int                            `yaml:"top_k" json:"top_k"`
}

type MemoryExtractionThinkingConfig struct {
	Type string `yaml:"type" json:"type"`
}

type AgentConfig struct {
	ID               string          `yaml:"id" json:"id"`
	Name             string          `yaml:"name" json:"name"`
	PersonaKey       string          `yaml:"persona_key" json:"persona_key"`
	Emotion          AgentModelGroup `yaml:"emotion" json:"emotion"`
	Work             AgentModelGroup `yaml:"work" json:"work"`
	ContextOverrides map[string]any  `yaml:"context_overrides" json:"context_overrides"`
}

type AgentModelGroup struct {
	Main    ModelBinding `yaml:"main" json:"main"`
	Summary ModelBinding `yaml:"summary" json:"summary"`
}

type ModelBinding struct {
	ProviderID string            `yaml:"provider_id" json:"provider_id"`
	Model      string            `yaml:"model" json:"model"`
	Params     llm.RequestParams `yaml:"params" json:"params"`
}

type WebFetchConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Provider       string `yaml:"provider"`    // "tavily" | "direct"
	APIKeyEnv      string `yaml:"api_key_env"` // "TAVILY_API_KEY"
	BaseURL        string `yaml:"base_url"`
	TimeoutSec     int    `yaml:"timeout_sec"`
	MaxBytes       int    `yaml:"max_bytes"`
	MaxRedirects   int    `yaml:"max_redirects"`
	UserAgent      string `yaml:"user_agent"`
	ExtractDepth   string `yaml:"extract_depth"` // "basic" | "advanced"
	Format         string `yaml:"format"`        // "markdown" | "text"
	IncludeImages  bool   `yaml:"include_images"`
	IncludeFavicon bool   `yaml:"include_favicon"`
	IncludeUsage   bool   `yaml:"include_usage"`
}

func (c *WebFetchConfig) applyDefaults() {
	if c.Provider == "" {
		c.Provider = "tavily"
	}
	if c.APIKeyEnv == "" {
		c.APIKeyEnv = "TAVILY_API_KEY"
	}
	if c.BaseURL == "" {
		c.BaseURL = "https://api.tavily.com"
	}
	if c.TimeoutSec == 0 {
		c.TimeoutSec = 20
	}
	if c.MaxBytes == 0 {
		c.MaxBytes = 1 << 20
	}
	if c.MaxRedirects == 0 {
		c.MaxRedirects = 5
	}
	if c.UserAgent == "" {
		c.UserAgent = "EmoAgent/0.1"
	}
	if c.ExtractDepth == "" {
		c.ExtractDepth = "basic"
	}
	if c.Format == "" {
		c.Format = "markdown"
	}
}

type BashConfig struct {
	Enabled        bool   `yaml:"enabled"`
	TimeoutSec     int    `yaml:"timeout_sec"`
	MaxOutputBytes int    `yaml:"max_output_bytes"`
	Shell          string `yaml:"shell"`
}

type PluginsConfig struct {
	Enabled          bool                        `yaml:"enabled" json:"enabled"`
	Directories      []string                    `yaml:"directories" json:"directories"`
	BuiltinEnabled   []string                    `yaml:"builtin_enabled" json:"builtin_enabled"`
	RolloutPercent   int                         `yaml:"rollout_percent" json:"rollout_percent"`
	DefaultTimeoutMS int                         `yaml:"default_timeout_ms" json:"default_timeout_ms"`
	MaxTimeoutMS     int                         `yaml:"max_timeout_ms" json:"max_timeout_ms"`
	FailClosedHooks  []string                    `yaml:"fail_closed_hooks" json:"fail_closed_hooks"`
	Audit            PluginAuditConfig           `yaml:"audit" json:"audit"`
	Store            PluginStoreConfig           `yaml:"store" json:"store"`
	Runtime          PluginRuntimeConfig         `yaml:"runtime" json:"runtime"`
	Installer        PluginInstallerConfig       `yaml:"installer" json:"installer"`
	ProviderGateway  PluginProviderGatewayConfig `yaml:"provider_gateway" json:"provider_gateway"`
	Admin            PluginAdminConfig           `yaml:"admin" json:"admin"`
}

type PluginAuditConfig struct {
	Enabled           bool `yaml:"enabled" json:"enabled"`
	IncludePayload    bool `yaml:"include_payload" json:"include_payload"`
	enabledSet        bool
	includePayloadSet bool
}

type PluginStoreConfig struct {
	RootDir         string `yaml:"root_dir" json:"root_dir"`
	AllowDevDirs    bool   `yaml:"allow_dev_dirs" json:"allow_dev_dirs"`
	allowDevDirsSet bool
}

type PluginRuntimeConfig struct {
	ProcessEnabled             bool   `yaml:"process_enabled" json:"process_enabled"`
	PythonExecutable           string `yaml:"python_executable" json:"python_executable"`
	StartupTimeoutMS           int    `yaml:"startup_timeout_ms" json:"startup_timeout_ms"`
	ShutdownTimeoutMS          int    `yaml:"shutdown_timeout_ms" json:"shutdown_timeout_ms"`
	IdleTimeoutSeconds         int    `yaml:"idle_timeout_seconds" json:"idle_timeout_seconds"`
	CrashBackoffInitialSeconds int    `yaml:"crash_backoff_initial_seconds" json:"crash_backoff_initial_seconds"`
	CrashBackoffMaxSeconds     int    `yaml:"crash_backoff_max_seconds" json:"crash_backoff_max_seconds"`
	MaxStderrBytes             int    `yaml:"max_stderr_bytes" json:"max_stderr_bytes"`
	ContainerEnabled           bool   `yaml:"container_enabled" json:"container_enabled"`
	processEnabledSet          bool
}

type PluginInstallerConfig struct {
	GithubEnabled         bool   `yaml:"github_enabled" json:"github_enabled"`
	RequireSignature      bool   `yaml:"require_signature" json:"require_signature"`
	TrustedPublishersPath string `yaml:"trusted_publishers_path" json:"trusted_publishers_path"`
	AllowUnsignedDev      bool   `yaml:"allow_unsigned_dev" json:"allow_unsigned_dev"`
	githubEnabledSet      bool
	requireSignatureSet   bool
	allowUnsignedDevSet   bool
}

type PluginProviderGatewayConfig struct {
	Enabled           bool   `yaml:"enabled" json:"enabled"`
	DefaultProviderID string `yaml:"default_provider_id" json:"default_provider_id"`
	DefaultModel      string `yaml:"default_model" json:"default_model"`
	enabledSet        bool
}

type PluginAdminConfig struct {
	Enabled    bool `yaml:"enabled" json:"enabled"`
	enabledSet bool
}

func (c *PluginsConfig) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Kind == 0 {
		return nil
	}
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("plugins must be a mapping")
	}
	allowed := map[string]struct{}{
		"enabled":            {},
		"directories":        {},
		"builtin_enabled":    {},
		"rollout_percent":    {},
		"default_timeout_ms": {},
		"max_timeout_ms":     {},
		"fail_closed_hooks":  {},
		"audit":              {},
		"store":              {},
		"runtime":            {},
		"installer":          {},
		"provider_gateway":   {},
		"admin":              {},
	}
	for i := 0; i < len(value.Content); i += 2 {
		key := strings.TrimSpace(value.Content[i].Value)
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("plugins.%s is not supported", key)
		}
	}
	type rawPluginsConfig PluginsConfig
	var decoded rawPluginsConfig
	if err := value.Decode(&decoded); err != nil {
		return err
	}
	*c = PluginsConfig(decoded)
	return nil
}

func (c *PluginStoreConfig) UnmarshalYAML(value *yaml.Node) error {
	if err := decodeKnownPluginMapping(value, "plugins.store", map[string]struct{}{
		"root_dir":       {},
		"allow_dev_dirs": {},
	}, (*rawPluginStoreConfig)(c)); err != nil {
		return err
	}
	c.allowDevDirsSet = yamlMappingHasKey(value, "allow_dev_dirs")
	return nil
}

func (c *PluginRuntimeConfig) UnmarshalYAML(value *yaml.Node) error {
	if err := decodeKnownPluginMapping(value, "plugins.runtime", map[string]struct{}{
		"process_enabled":               {},
		"python_executable":             {},
		"startup_timeout_ms":            {},
		"shutdown_timeout_ms":           {},
		"idle_timeout_seconds":          {},
		"crash_backoff_initial_seconds": {},
		"crash_backoff_max_seconds":     {},
		"max_stderr_bytes":              {},
		"container_enabled":             {},
	}, (*rawPluginRuntimeConfig)(c)); err != nil {
		return err
	}
	c.processEnabledSet = yamlMappingHasKey(value, "process_enabled")
	return nil
}

func (c *PluginInstallerConfig) UnmarshalYAML(value *yaml.Node) error {
	if err := decodeKnownPluginMapping(value, "plugins.installer", map[string]struct{}{
		"github_enabled":          {},
		"require_signature":       {},
		"trusted_publishers_path": {},
		"allow_unsigned_dev":      {},
	}, (*rawPluginInstallerConfig)(c)); err != nil {
		return err
	}
	c.githubEnabledSet = yamlMappingHasKey(value, "github_enabled")
	c.requireSignatureSet = yamlMappingHasKey(value, "require_signature")
	c.allowUnsignedDevSet = yamlMappingHasKey(value, "allow_unsigned_dev")
	return nil
}

func (c *PluginProviderGatewayConfig) UnmarshalYAML(value *yaml.Node) error {
	if err := decodeKnownPluginMapping(value, "plugins.provider_gateway", map[string]struct{}{
		"enabled":             {},
		"default_provider_id": {},
		"default_model":       {},
	}, (*rawPluginProviderGatewayConfig)(c)); err != nil {
		return err
	}
	c.enabledSet = yamlMappingHasKey(value, "enabled")
	return nil
}

func (c *PluginAdminConfig) UnmarshalYAML(value *yaml.Node) error {
	if err := decodeKnownPluginMapping(value, "plugins.admin", map[string]struct{}{
		"enabled": {},
	}, (*rawPluginAdminConfig)(c)); err != nil {
		return err
	}
	c.enabledSet = yamlMappingHasKey(value, "enabled")
	return nil
}

type rawPluginStoreConfig PluginStoreConfig
type rawPluginRuntimeConfig PluginRuntimeConfig
type rawPluginInstallerConfig PluginInstallerConfig
type rawPluginProviderGatewayConfig PluginProviderGatewayConfig
type rawPluginAdminConfig PluginAdminConfig

func decodeKnownPluginMapping(value *yaml.Node, prefix string, allowed map[string]struct{}, target any) error {
	if value == nil || value.Kind == 0 {
		return nil
	}
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("%s must be a mapping", prefix)
	}
	for i := 0; i < len(value.Content); i += 2 {
		key := strings.TrimSpace(value.Content[i].Value)
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("%s.%s is not supported", prefix, key)
		}
	}
	return value.Decode(target)
}

func yamlMappingHasKey(value *yaml.Node, key string) bool {
	if value == nil || value.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i < len(value.Content); i += 2 {
		if strings.TrimSpace(value.Content[i].Value) == key {
			return true
		}
	}
	return false
}

func (c *PluginAuditConfig) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Kind == 0 {
		return nil
	}
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("plugins.audit must be a mapping")
	}
	allowed := map[string]struct{}{
		"enabled":         {},
		"include_payload": {},
	}
	for i := 0; i < len(value.Content); i += 2 {
		key := strings.TrimSpace(value.Content[i].Value)
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("plugins.audit.%s is not supported", key)
		}
	}
	type rawPluginAuditConfig PluginAuditConfig
	var decoded rawPluginAuditConfig
	if err := value.Decode(&decoded); err != nil {
		return err
	}
	*c = PluginAuditConfig(decoded)
	c.enabledSet = yamlMappingHasKey(value, "enabled")
	c.includePayloadSet = yamlMappingHasKey(value, "include_payload")
	return nil
}

func (c *PluginsConfig) applyDefaults() {
	if len(c.Directories) == 0 {
		c.Directories = []string{"data/plugins"}
	}
	if len(c.BuiltinEnabled) == 0 {
		c.BuiltinEnabled = defaultBuiltinPluginIDs()
	}
	if c.DefaultTimeoutMS == 0 {
		c.DefaultTimeoutMS = 80
	}
	if c.MaxTimeoutMS == 0 {
		c.MaxTimeoutMS = 1000
	}
	if len(c.FailClosedHooks) == 0 {
		c.FailClosedHooks = []string{"before_tool_call", "before_memory_commit"}
	}
	c.Audit.applyDefaults()
	c.Store.applyDefaults()
	c.Runtime.applyDefaults()
	c.Installer.applyDefaults(c.Store)
	c.ProviderGateway.applyDefaults()
	c.Admin.applyDefaults()
}

func (c *PluginAuditConfig) applyDefaults() {
	if !c.enabledSet && !c.IncludePayload {
		c.Enabled = true
	}
}

func (c *PluginStoreConfig) applyDefaults() {
	if c.RootDir == "" {
		c.RootDir = "data/plugins"
	}
	if !c.allowDevDirsSet {
		c.AllowDevDirs = true
	}
}

func (c *PluginRuntimeConfig) applyDefaults() {
	if !c.processEnabledSet {
		c.ProcessEnabled = true
	}
	if c.PythonExecutable == "" {
		c.PythonExecutable = "python3"
	}
	if c.StartupTimeoutMS == 0 {
		c.StartupTimeoutMS = 5000
	}
	if c.ShutdownTimeoutMS == 0 {
		c.ShutdownTimeoutMS = 3000
	}
	if c.IdleTimeoutSeconds == 0 {
		c.IdleTimeoutSeconds = 600
	}
	if c.CrashBackoffInitialSeconds == 0 {
		c.CrashBackoffInitialSeconds = 5
	}
	if c.CrashBackoffMaxSeconds == 0 {
		c.CrashBackoffMaxSeconds = 300
	}
	if c.MaxStderrBytes == 0 {
		c.MaxStderrBytes = 262144
	}
}

func (c *PluginInstallerConfig) applyDefaults(store PluginStoreConfig) {
	if !c.githubEnabledSet {
		c.GithubEnabled = true
	}
	if !c.requireSignatureSet {
		c.RequireSignature = true
	}
	if !c.allowUnsignedDevSet && store.AllowDevDirs {
		c.AllowUnsignedDev = true
	}
}

func (c *PluginProviderGatewayConfig) applyDefaults() {
	if !c.enabledSet {
		c.Enabled = true
	}
}

func (c *PluginAdminConfig) applyDefaults() {
	if !c.enabledSet {
		c.Enabled = true
	}
}

func (c PluginsConfig) Validate(turnPipeline TurnPipelineConfig) error {
	if c.RolloutPercent < 0 || c.RolloutPercent > 100 {
		return fmt.Errorf("rollout_percent must be between 0 and 100")
	}
	if c.DefaultTimeoutMS <= 0 {
		return fmt.Errorf("default_timeout_ms must be > 0")
	}
	if c.MaxTimeoutMS <= 0 {
		return fmt.Errorf("max_timeout_ms must be > 0")
	}
	if c.DefaultTimeoutMS > c.MaxTimeoutMS {
		return fmt.Errorf("default_timeout_ms must be <= max_timeout_ms")
	}
	if strings.TrimSpace(c.Store.RootDir) == "" {
		return fmt.Errorf("store.root_dir is required")
	}
	if c.Runtime.StartupTimeoutMS <= 0 {
		return fmt.Errorf("runtime.startup_timeout_ms must be > 0")
	}
	if c.Runtime.ShutdownTimeoutMS <= 0 {
		return fmt.Errorf("runtime.shutdown_timeout_ms must be > 0")
	}
	if c.Runtime.IdleTimeoutSeconds <= 0 {
		return fmt.Errorf("runtime.idle_timeout_seconds must be > 0")
	}
	if c.Runtime.CrashBackoffInitialSeconds <= 0 {
		return fmt.Errorf("runtime.crash_backoff_initial_seconds must be > 0")
	}
	if c.Runtime.CrashBackoffMaxSeconds < c.Runtime.CrashBackoffInitialSeconds {
		return fmt.Errorf("runtime.crash_backoff_max_seconds must be >= crash_backoff_initial_seconds")
	}
	if c.Runtime.MaxStderrBytes <= 0 {
		return fmt.Errorf("runtime.max_stderr_bytes must be > 0")
	}
	for _, hook := range c.FailClosedHooks {
		if !knownPluginHookName(hook) {
			return fmt.Errorf("fail_closed_hooks contains unknown hook %q", hook)
		}
	}
	if !c.Enabled {
		return nil
	}
	if !turnPipeline.Enabled {
		return fmt.Errorf("plugins.enabled requires chat.turn_pipeline.enabled=true")
	}
	if turnPipeline.RolloutPercent <= 0 && len(turnPipeline.AllowPersonas) == 0 && len(turnPipeline.AllowSessions) == 0 {
		return fmt.Errorf("plugins.enabled requires chat.turn_pipeline rollout or allow list")
	}
	return nil
}

func knownPluginHookName(hook string) bool {
	switch strings.TrimSpace(hook) {
	case "before_ingress_normalize",
		"after_ingress_normalize",
		"before_memory_prepare",
		"after_memory_prepare",
		"before_memory_retrieve",
		"after_memory_retrieve",
		"before_memory_commit",
		"after_memory_commit",
		"before_outbound",
		"after_outbound",
		"before_tool_call",
		"after_tool_call",
		"memory.candidate.submit",
		"memory.forget.request",
		"work.dispatch.annotate",
		"on_decision_packet",
		"on_approval_requested",
		"on_approval_resolved",
		"on_approval_consumed",
		"on_turn_error",
		"after_turn_end",
		"before_agent_affect_evaluate",
		"after_agent_affect_evaluate",
		"before_agent_affect_commit",
		"after_agent_affect_commit",
		"agent_affect_get_state":
		return true
	default:
		return false
	}
}

func (c *BashConfig) applyDefaults() {
	if c.TimeoutSec == 0 {
		c.TimeoutSec = 60
	}
	if c.MaxOutputBytes == 0 {
		c.MaxOutputBytes = 256 << 10 // 256 KiB
	}
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type ChatConfig struct {
	RealtimeStreaming bool               `yaml:"realtime_streaming" json:"realtime_streaming"`
	TurnPipeline      TurnPipelineConfig `yaml:"turn_pipeline" json:"turn_pipeline"`
}

type TurnPipelineConfig struct {
	Shadow         bool                      `yaml:"shadow" json:"shadow"`
	Enabled        bool                      `yaml:"enabled" json:"enabled"`
	MemoryStages   bool                      `yaml:"memory_stages" json:"memory_stages"`
	ApprovalStages bool                      `yaml:"approval_stages" json:"approval_stages"`
	RolloutPercent int                       `yaml:"rollout_percent" json:"rollout_percent"`
	AllowPersonas  []string                  `yaml:"allow_personas" json:"allow_personas"`
	AllowSessions  []string                  `yaml:"allow_sessions" json:"allow_sessions"`
	DenySessions   []string                  `yaml:"deny_sessions" json:"deny_sessions"`
	Journal        TurnPipelineJournalConfig `yaml:"journal" json:"journal"`
	Idempotency    TurnPipelineIdemConfig    `yaml:"idempotency" json:"idempotency"`
}

type TurnPipelineJournalConfig struct {
	Mode       string `yaml:"mode" json:"mode"`
	JSONLDir   string `yaml:"jsonl_dir" json:"jsonl_dir"`
	FailClosed bool   `yaml:"fail_closed" json:"fail_closed"`
}

type TurnPipelineIdemConfig struct {
	Mode             string `yaml:"mode" json:"mode"`
	DuplicateDone    string `yaml:"duplicate_done" json:"duplicate_done"`
	DuplicateRunning string `yaml:"duplicate_running" json:"duplicate_running"`
}

type DBConfig struct {
	Path string `yaml:"path"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type PersonasConfig struct {
	Dir string `yaml:"dir"`
}

type WebSearchConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Provider      string `yaml:"provider"`       // "tavily"
	APIKeyEnv     string `yaml:"api_key_env"`    // "TAVILY_API_KEY"
	MaxResults    int    `yaml:"max_results"`    // handler default cap, default 5
	TimeoutSec    int    `yaml:"timeout_sec"`    // HTTP timeout seconds, default 30
	IncludeAnswer bool   `yaml:"include_answer"` // default false
}

type ContextConfig struct {
	InputBudgetTokens    int     `yaml:"input_budget_tokens"`
	SoftCompactRatio     float64 `yaml:"soft_compact_ratio"`
	HardCompactRatio     float64 `yaml:"hard_compact_ratio"`
	ReserveOutputTokens  int     `yaml:"reserve_output_tokens"`
	KeepRecentUserTurns  int     `yaml:"keep_recent_user_turns"`
	ToolResultSoftTokens int     `yaml:"tool_result_soft_tokens"`
	ToolResultHardTokens int     `yaml:"tool_result_hard_tokens"`
}

type WorkConfig struct {
	MaxToolRounds            int           `yaml:"max_tool_rounds"`
	MaxInputTokens           int           `yaml:"max_input_tokens"`
	CompressSoftRatio        float64       `yaml:"compress_soft_ratio"`
	CompressKeepRounds       int           `yaml:"compress_keep_rounds"`
	ToolSnipSoftTokens       int           `yaml:"tool_snip_soft_tokens"`
	ToolSnipHardTokens       int           `yaml:"tool_snip_hard_tokens"`
	JournalDir               string        `yaml:"journal_dir"`
	MaxEscalationsPerTask    int           `yaml:"max_escalations_per_task"`
	PendingDecisionTTL       time.Duration `yaml:"pending_decision_ttl"`
	SoftTTL                  time.Duration `yaml:"soft_ttl"`
	HardTTL                  time.Duration `yaml:"hard_ttl"`
	ArchiveTTL               time.Duration `yaml:"archive_ttl"`
	ResumeClaimTTL           time.Duration `yaml:"resume_claim_ttl"`
	DeciderCleanupInterval   time.Duration `yaml:"decider_cleanup_interval"`
	PendingSnapshotMaxTokens int           `yaml:"pending_snapshot_max_tokens"`
}

func (w *WorkConfig) ApplyDefaults() {
	if w.MaxToolRounds == 0 {
		w.MaxToolRounds = 15
	}
	if w.MaxInputTokens == 0 {
		w.MaxInputTokens = 100000
	}
	if w.CompressSoftRatio == 0 {
		w.CompressSoftRatio = 0.7
	}
	if w.CompressKeepRounds == 0 {
		w.CompressKeepRounds = 2
	}
	if w.ToolSnipSoftTokens == 0 {
		w.ToolSnipSoftTokens = 500
	}
	if w.ToolSnipHardTokens == 0 {
		w.ToolSnipHardTokens = 2000
	}
	if w.JournalDir == "" {
		w.JournalDir = "./logs/work"
	}
	if w.MaxEscalationsPerTask == 0 {
		w.MaxEscalationsPerTask = 3
	}
	if w.PendingDecisionTTL == 0 {
		w.PendingDecisionTTL = 30 * time.Minute
	}
	if w.SoftTTL == 0 {
		if w.PendingDecisionTTL > 0 {
			w.SoftTTL = w.PendingDecisionTTL
		} else {
			w.SoftTTL = 30 * time.Minute
		}
	}
	if w.HardTTL == 0 {
		w.HardTTL = time.Hour
	}
	if w.ArchiveTTL == 0 {
		w.ArchiveTTL = 24 * time.Hour
	}
	if w.ResumeClaimTTL == 0 {
		w.ResumeClaimTTL = 10 * time.Minute
	}
	if w.DeciderCleanupInterval == 0 {
		w.DeciderCleanupInterval = 5 * time.Minute
	}
	if w.PendingSnapshotMaxTokens == 0 {
		w.PendingSnapshotMaxTokens = 60000
	}
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	cfg := &Config{
		Server: ServerConfig{
			Host: "127.0.0.1",
			Port: 8080,
		},
		Time: TimeConfig{
			Timezone: "Asia/Shanghai",
		},
		Chat: ChatConfig{
			RealtimeStreaming: false,
			TurnPipeline: TurnPipelineConfig{
				Journal: TurnPipelineJournalConfig{
					Mode:     "sqlite",
					JSONLDir: "./logs/turns",
				},
				Idempotency: TurnPipelineIdemConfig{
					Mode:             "sqlite",
					DuplicateDone:    "replay_summary",
					DuplicateRunning: "busy",
				},
			},
		},
		Context: ContextConfig{
			InputBudgetTokens:    24000,
			SoftCompactRatio:     0.75,
			HardCompactRatio:     0.92,
			ReserveOutputTokens:  4096,
			KeepRecentUserTurns:  6,
			ToolResultSoftTokens: 1000,
			ToolResultHardTokens: 3000,
		},
		DB: DBConfig{
			Path: "./data/emo.db",
		},
		Media: MediaConfig{
			StorageDir: "./data/media",
			MaxBytes:   10 * 1024 * 1024,
			MaxPixels:  20_000_000,
		},
		AgentAffect: AgentAffectConfig{
			Enabled:        false,
			UpdateMode:     "async_after_reply",
			StorageEnabled: true,
			State: AgentAffectStateConfig{
				Scope:              "persona",
				RecentContextScope: "persona",
			},
			Async: AgentAffectAsyncConfig{
				Enabled:               true,
				QueueEnabled:          true,
				WorkerEnabled:         true,
				WorkerConcurrency:     1,
				PollIntervalMS:        800,
				QueueClaimTTLSeconds:  300,
				MaxAttempts:           3,
				RetryBaseDelaySeconds: 30,
				RetryMaxDelaySeconds:  900,
				ClearRawAfterDone:     true,
				Batch: AgentAffectAsyncBatchConfig{
					Enabled:              true,
					MaxJobs:              6,
					MaxInputTokens:       12000,
					MaxAgeSeconds:        300,
					MergeAcrossSessions:  true,
					BreakOnManualBarrier: true,
				},
			},
			Evaluator: AgentAffectEvaluatorConfig{
				Mode:            "llm",
				TimeoutMS:       30000,
				MaxOutputTokens: 4096,
				Temperature:     0.2,
			},
			Context: AgentAffectContextConfig{
				Mode:                       "raw_window",
				RawKeepLastRequests:        20,
				RawKeepLastTokens:          12000,
				IncludePreviousEvaluations: true,
				PreviousEvaluationKeepLast: 30,
				SummaryEnabled:             false,
				StoreRawInputs:             true,
				StorePromptSnapshot:        false,
			},
			Externalization: AgentAffectExternalizationConfig{
				Attachment: ExternalizedDimensionConfig{
					Enabled:             true,
					DefaultStyle:        "gentle_explicit",
					MaxVisibleIntensity: 0.65,
				},
				Frustration: ExternalizedDimensionConfig{Enabled: false},
			},
			PluginAPI: AgentAffectPluginAPIConfig{
				Enabled:                      true,
				PluginSafeIncludeReason:      true,
				PluginSafeIncludeRawText:     false,
				OrdinaryPluginsCanCommit:     true,
				OrdinaryPluginsCanWriteDelta: true,
				TrustedPluginsCanWriteTarget: true,
			},
			Limits: AgentAffectLimitsConfig{
				PluginDeltaMultiplier: 1.0,
				PerRequestDelta: AgentAffectVectorLimitsConfig{
					Valence:     0.15,
					Arousal:     0.18,
					Dominance:   0.12,
					Energy:      0.12,
					Warmth:      0.15,
					Concern:     0.18,
					Curiosity:   0.18,
					Playfulness: 0.15,
					Attachment:  0.08,
					Frustration: 0.08,
					Uncertainty: 0.12,
				},
				Absolute: AgentAffectAbsoluteLimitsConfig{
					AttachmentMax:  0.75,
					FrustrationMax: 0.35,
				},
			},
			Prompt: AgentAffectPromptConfig{
				Mode:                      "natural_summary",
				IncludeMoodBlock:          true,
				IncludeReason:             true,
				IncludeExpressionGuidance: false,
				IncludeNumericValues:      false,
				MaxPromptChars:            240,
			},
		},
		Memory: MemoryConfig{
			Enabled:         false,
			ConfigPath:      "./config/memorycore.yaml",
			ManualRulesPath: "./config/memory_manual_rules.yaml",
			Sidecar: MemorySidecarConfig{
				Enabled:            false,
				Managed:            false,
				Adapter:            "trivium",
				Host:               "127.0.0.1",
				Port:               8765,
				URL:                "http://127.0.0.1:8765",
				WorkingDir:         "../EmoAgent-MemoryCore/sidecar",
				ConfigPath:         "./data/runtime/sidecar.generated.toml",
				StartupTimeoutMS:   15000,
				ShutdownTimeoutMS:  5000,
				FailOpen:           true,
				LogPath:            "./logs/sidecar.log",
				TriviumDir:         "./data/trivium",
				EmbeddingCachePath: "./data/embedding_cache.sqlite3",
			},
			Retrieval: MemoryRetrievalConfig{
				Enabled:             true,
				InjectPrompt:        false,
				UseFTS:              true,
				UseMirror:           false,
				FinalMemoryCount:    4,
				ContextBudgetTokens: 700,
				FailOpen:            true,
			},
			Extraction: MemoryExtractionConfig{
				Enabled:                  false,
				Mode:                     "dry_run",
				TriggerOnFinalizeSegment: true,
				TriggerOnManualPin:       true,
				ManualPinMode:            "apply",
				Limit:                    50,
				Timezone:                 "Asia/Shanghai",
				AllowInference:           true,
				AllowSensitiveExtraction: false,
				MaxFacts:                 12,
				MaxLinks:                 20,
				Async: MemoryExtractionAsyncConfig{
					Enabled:               true,
					WorkerEnabled:         true,
					WorkerConcurrency:     1,
					QueueClaimTTLSeconds:  300,
					MaxAttempts:           3,
					RetryBaseDelaySeconds: 30,
					RetryMaxDelaySeconds:  900,
				},
				Idle: MemoryExtractionIdleConfig{
					Enabled:                  true,
					IdleAfterSeconds:         900,
					SweepIntervalSeconds:     60,
					MinEpisodeCount:          2,
					MaxSegmentsPerSweep:      20,
					IncludeFinalizedSegments: true,
					IncludeActiveSegments:    true,
				},
				Manual: MemoryExtractionManualConfig{
					Enabled:               true,
					Mode:                  "apply",
					AllowForce:            true,
					AllowSegmentSelection: true,
				},
				MirrorSync: MemoryExtractionMirrorConfig{
					AfterApply:      true,
					PeriodicEnabled: true,
					IntervalSeconds: 60,
					Limit:           100,
				},
				Provider: MemoryExtractionProviderConfig{
					Kind:           "openai-compatible",
					ID:             "memory_extractor",
					APIKeyEnv:      "MEMORYCORE_LLM_API_KEY",
					TimeoutSeconds: 60,
					MaxTokens:      4096,
				},
				RepairEnabled: true,
				AuditEnabled:  true,
			},
			NaturalMemory: MemoryNaturalMemoryConfig{
				Enabled:             false,
				SchedulerEnabled:    true,
				TickIntervalSeconds: 60,
				LocalTime:           "03:30",
				Timezone:            "",
				MirrorSyncAfterRun:  true,
				MirrorSyncLimit:     100,
				Manual: MemoryNaturalMemoryManualConfig{
					Enabled:     true,
					AllowDryRun: true,
					AllowForce:  true,
				},
			},
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
		Personas: PersonasConfig{
			Dir: "./personas",
		},
		WebSearch: WebSearchConfig{
			Enabled:    false,
			Provider:   "tavily",
			APIKeyEnv:  "TAVILY_API_KEY",
			MaxResults: 5,
			TimeoutSec: 30,
		},
		WebFetch: WebFetchConfig{
			Enabled:      true,
			Provider:     "tavily",
			APIKeyEnv:    "TAVILY_API_KEY",
			BaseURL:      "https://api.tavily.com",
			TimeoutSec:   20,
			MaxBytes:     1 << 20,
			MaxRedirects: 5,
			UserAgent:    "EmoAgent/0.1",
			ExtractDepth: "basic",
			Format:       "markdown",
		},
		Bash: BashConfig{
			Enabled:        false,
			TimeoutSec:     60,
			MaxOutputBytes: 256 << 10,
		},
		Plugins: PluginsConfig{
			Enabled:          false,
			Directories:      []string{"data/plugins"},
			BuiltinEnabled:   defaultBuiltinPluginIDs(),
			RolloutPercent:   0,
			DefaultTimeoutMS: 80,
			MaxTimeoutMS:     1000,
			FailClosedHooks:  []string{"before_tool_call", "before_memory_commit"},
			Audit: PluginAuditConfig{
				Enabled:        true,
				IncludePayload: false,
			},
		},
	}
	cfg.Work.ApplyDefaults()
	cfg.Plugins.applyDefaults()
	return cfg
}

func defaultBuiltinPluginIDs() []string {
	return []string{
		"com.emoagent.plugins.turn-audit",
		"com.emoagent.plugins.memory-context-debug",
		"com.emoagent.plugins.outbound-guard",
	}
}

// Load reads a YAML config file and returns a Config.
// Missing fields retain their default values.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	explicitMemoryExtractionTimezone, memoryExtractionTimezone := memoryExtractionTimezoneValue(data)
	cfg.Chat.TurnPipeline.applyDefaults()
	cfg.Work.ApplyDefaults()
	cfg.WebFetch.applyDefaults()
	cfg.Bash.applyDefaults()
	cfg.Memory.Sidecar.applyDefaults()
	cfg.Memory.Extraction.applyDefaults()
	cfg.Memory.NaturalMemory.applyDefaults()
	cfg.applyTimezoneDefaults(explicitMemoryExtractionTimezone, memoryExtractionTimezone)
	cfg.Plugins.applyDefaults()
	for i := range cfg.LLMProviders {
		provider, err := cfg.LLMProviders[i].WithPresetDefaults()
		if err != nil {
			return nil, fmt.Errorf("llm_providers[%d]: %w", i, err)
		}
		cfg.LLMProviders[i] = provider
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

func (c *TurnPipelineConfig) applyDefaults() {
	if c.Journal.Mode == "" {
		c.Journal.Mode = "sqlite"
	}
	if c.Journal.JSONLDir == "" {
		c.Journal.JSONLDir = "./logs/turns"
	}
	if c.Idempotency.Mode == "" {
		c.Idempotency.Mode = "sqlite"
	}
	if c.Idempotency.DuplicateDone == "" {
		c.Idempotency.DuplicateDone = "replay_summary"
	}
	if c.Idempotency.DuplicateRunning == "" {
		c.Idempotency.DuplicateRunning = "busy"
	}
}

func memoryExtractionTimezoneValue(data []byte) (bool, string) {
	var raw struct {
		Memory struct {
			Extraction struct {
				Timezone *string `yaml:"timezone"`
			} `yaml:"extraction"`
		} `yaml:"memory"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return false, ""
	}
	if raw.Memory.Extraction.Timezone == nil {
		return false, ""
	}
	return true, *raw.Memory.Extraction.Timezone
}

func (c *Config) applyTimezoneDefaults(explicitMemoryExtractionTimezone bool, memoryExtractionTimezone string) {
	if strings.TrimSpace(c.Time.Timezone) == "" {
		c.Time.Timezone = "Asia/Shanghai"
	}
	if !explicitMemoryExtractionTimezone || strings.TrimSpace(memoryExtractionTimezone) == "" {
		c.Memory.Extraction.Timezone = c.Time.Timezone
	}
}

// Validate checks that required fields are set.
func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be 1-65535, got %d", c.Server.Port)
	}
	if _, err := time.LoadLocation(strings.TrimSpace(c.Time.Timezone)); err != nil {
		return fmt.Errorf("time.timezone must be a valid IANA timezone: %w", err)
	}
	if err := c.Chat.TurnPipeline.Validate(); err != nil {
		return fmt.Errorf("chat.turn_pipeline: %w", err)
	}
	if err := c.Plugins.Validate(c.Chat.TurnPipeline); err != nil {
		return fmt.Errorf("plugins: %w", err)
	}
	if c.Memory.Enabled && strings.TrimSpace(c.Memory.ConfigPath) == "" {
		return fmt.Errorf("memory.config_path is required when memory is enabled")
	}
	if c.Memory.Enabled && strings.TrimSpace(c.Memory.ManualRulesPath) == "" {
		return fmt.Errorf("memory.manual_rules_path is required when memory is enabled")
	}
	if c.Memory.Enabled && c.Memory.Retrieval.Enabled {
		if c.Memory.Retrieval.FinalMemoryCount <= 0 {
			return fmt.Errorf("memory.retrieval.final_memory_count must be > 0")
		}
		if c.Memory.Retrieval.ContextBudgetTokens <= 0 {
			return fmt.Errorf("memory.retrieval.context_budget_tokens must be > 0")
		}
	}
	if err := c.Memory.Extraction.Validate(); err != nil {
		return fmt.Errorf("memory.extraction: %w", err)
	}
	if err := c.Memory.NaturalMemory.Validate(); err != nil {
		return fmt.Errorf("memory.natural_memory.%w", err)
	}
	if err := c.Context.Validate(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	for i, provider := range c.LLMProviders {
		if err := provider.Validate(); err != nil {
			return fmt.Errorf("llm_providers[%d]: %w", i, err)
		}
	}
	for i, agent := range c.AgentConfigs {
		if err := agent.Validate(); err != nil {
			return fmt.Errorf("agent_configs[%d]: %w", i, err)
		}
		if _, err := agent.ResolveContextConfig(c.Context); err != nil {
			return fmt.Errorf("agent_configs[%d].context_overrides: %w", i, err)
		}
	}
	if c.WebSearch.Enabled {
		if c.WebSearch.Provider == "" {
			return fmt.Errorf("websearch.provider is required when websearch is enabled")
		}
		if c.WebSearch.APIKeyEnv == "" {
			return fmt.Errorf("websearch.api_key_env is required when websearch is enabled")
		}
	}
	if c.WebFetch.Enabled {
		switch c.WebFetch.Provider {
		case "direct", "tavily":
		default:
			return fmt.Errorf("webfetch.provider must be direct or tavily, got %q", c.WebFetch.Provider)
		}
		switch c.WebFetch.ExtractDepth {
		case "basic", "advanced":
		default:
			return fmt.Errorf("webfetch.extract_depth must be basic or advanced, got %q", c.WebFetch.ExtractDepth)
		}
		switch c.WebFetch.Format {
		case "markdown", "text":
		default:
			return fmt.Errorf("webfetch.format must be markdown or text, got %q", c.WebFetch.Format)
		}
	}
	if c.Work.SoftTTL <= 0 {
		return fmt.Errorf("work.soft_ttl must be > 0")
	}
	if c.Work.HardTTL <= c.Work.SoftTTL {
		return fmt.Errorf("work.hard_ttl must be > work.soft_ttl")
	}
	if c.Work.ArchiveTTL <= 0 {
		return fmt.Errorf("work.archive_ttl must be > 0")
	}
	if c.Work.ResumeClaimTTL <= 0 {
		return fmt.Errorf("work.resume_claim_ttl must be > 0")
	}
	if !(c.Work.CompressSoftRatio > 0 && c.Work.CompressSoftRatio < 1) {
		return fmt.Errorf("work.compress_soft_ratio must be between 0 and 1")
	}
	if c.Work.CompressKeepRounds <= 0 {
		return fmt.Errorf("work.compress_keep_rounds must be > 0")
	}
	if c.Work.ToolSnipSoftTokens <= 0 {
		return fmt.Errorf("work.tool_snip_soft_tokens must be > 0")
	}
	if c.Work.ToolSnipHardTokens <= 0 {
		return fmt.Errorf("work.tool_snip_hard_tokens must be > 0")
	}
	if c.Work.ToolSnipSoftTokens >= c.Work.ToolSnipHardTokens {
		return fmt.Errorf("work.tool_snip_soft_tokens must be < work.tool_snip_hard_tokens")
	}
	return nil
}

func (c TurnPipelineConfig) Validate() error {
	if c.RolloutPercent < 0 || c.RolloutPercent > 100 {
		return fmt.Errorf("rollout_percent must be between 0 and 100")
	}
	switch c.Journal.Mode {
	case "", "memory", "sqlite", "jsonl", "sqlite_jsonl":
	default:
		return fmt.Errorf("journal.mode must be memory, sqlite, jsonl, or sqlite_jsonl")
	}
	switch c.Idempotency.Mode {
	case "", "memory", "sqlite":
	default:
		return fmt.Errorf("idempotency.mode must be memory or sqlite")
	}
	switch c.Idempotency.DuplicateDone {
	case "", "replay_summary", "noop":
	default:
		return fmt.Errorf("idempotency.duplicate_done must be replay_summary or noop")
	}
	switch c.Idempotency.DuplicateRunning {
	case "", "busy", "status":
	default:
		return fmt.Errorf("idempotency.duplicate_running must be busy or status")
	}
	return nil
}

func (c *MemoryExtractionConfig) applyDefaults() {
	if c.Mode == "" {
		c.Mode = "dry_run"
	}
	if c.SessionEndMode == "" {
		c.SessionEndMode = c.Mode
	}
	if c.ManualPinMode == "" {
		c.ManualPinMode = "apply"
	}
	if c.Limit == 0 {
		c.Limit = 50
	}
	if c.Timezone == "" {
		c.Timezone = "Asia/Shanghai"
	}
	if c.MaxFacts == 0 {
		c.MaxFacts = 12
	}
	if c.MaxLinks == 0 {
		c.MaxLinks = 20
	}
	if c.Provider.Kind == "" {
		c.Provider.Kind = "openai-compatible"
	}
	if c.Provider.ID == "" {
		c.Provider.ID = "memory_extractor"
	}
	if c.Provider.APIKeyEnv == "" {
		c.Provider.APIKeyEnv = "MEMORYCORE_LLM_API_KEY"
	}
	if c.Provider.TimeoutSeconds == 0 {
		c.Provider.TimeoutSeconds = 60
	}
	if c.Provider.MaxTokens == 0 {
		c.Provider.MaxTokens = 4096
	}
	if c.Async.WorkerConcurrency == 0 {
		c.Async.WorkerConcurrency = 1
	}
	if c.Async.QueueClaimTTLSeconds == 0 {
		c.Async.QueueClaimTTLSeconds = 300
	}
	if c.Async.MaxAttempts == 0 {
		c.Async.MaxAttempts = 3
	}
	if c.Async.RetryBaseDelaySeconds == 0 {
		c.Async.RetryBaseDelaySeconds = 30
	}
	if c.Async.RetryMaxDelaySeconds == 0 {
		c.Async.RetryMaxDelaySeconds = 900
	}
	if c.Idle.IdleAfterSeconds == 0 {
		c.Idle.IdleAfterSeconds = 900
	}
	if c.Idle.SweepIntervalSeconds == 0 {
		c.Idle.SweepIntervalSeconds = 60
	}
	if c.Idle.MinEpisodeCount == 0 {
		c.Idle.MinEpisodeCount = 2
	}
	if c.Idle.MaxSegmentsPerSweep == 0 {
		c.Idle.MaxSegmentsPerSweep = 20
	}
	if c.Manual.Mode == "" {
		c.Manual.Mode = "apply"
	}
	if c.MirrorSync.IntervalSeconds == 0 {
		c.MirrorSync.IntervalSeconds = 60
	}
	if c.MirrorSync.Limit == 0 {
		c.MirrorSync.Limit = 100
	}
}

func (c *MemorySidecarConfig) applyDefaults() {
	if c.Adapter == "" {
		c.Adapter = "trivium"
	}
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	if c.Port == 0 {
		c.Port = 8765
	}
	if c.URL == "" {
		c.URL = "http://127.0.0.1:8765"
	}
	if c.WorkingDir == "" {
		c.WorkingDir = "../EmoAgent-MemoryCore/sidecar"
	}
	if c.ConfigPath == "" {
		c.ConfigPath = "./data/runtime/sidecar.generated.toml"
	}
	if c.StartupTimeoutMS == 0 {
		c.StartupTimeoutMS = 15000
	}
	if c.ShutdownTimeoutMS == 0 {
		c.ShutdownTimeoutMS = 5000
	}
	if c.LogPath == "" {
		c.LogPath = "./logs/sidecar.log"
	}
	if c.TriviumDir == "" {
		c.TriviumDir = "./data/trivium"
	}
	if c.EmbeddingCachePath == "" {
		c.EmbeddingCachePath = "./data/embedding_cache.sqlite3"
	}
}

func (c *MemoryNaturalMemoryConfig) applyDefaults() {
	if c.TickIntervalSeconds == 0 {
		c.TickIntervalSeconds = 60
	}
	if c.LocalTime == "" {
		c.LocalTime = "03:30"
	}
	if c.MirrorSyncLimit == 0 {
		c.MirrorSyncLimit = 100
	}
}

func (c MemoryExtractionConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	switch normalizeMemoryExtractionMode(c.Mode) {
	case "validate", "dry-run", "apply":
	default:
		return fmt.Errorf("mode must be validate, dry_run, or apply")
	}
	sessionEndMode := c.SessionEndMode
	if strings.TrimSpace(sessionEndMode) == "" {
		sessionEndMode = c.Mode
	}
	for name, mode := range map[string]string{
		"session_end_mode": sessionEndMode,
		"manual_pin_mode":  c.ManualPinMode,
	} {
		switch normalizeMemoryExtractionMode(mode) {
		case "validate", "dry-run", "apply":
		default:
			return fmt.Errorf("%s must be validate, dry_run, or apply", name)
		}
	}
	if c.Limit <= 0 {
		return fmt.Errorf("limit must be > 0")
	}
	if strings.TrimSpace(c.Timezone) == "" {
		return fmt.Errorf("timezone is required")
	}
	if _, err := time.LoadLocation(strings.TrimSpace(c.Timezone)); err != nil {
		return fmt.Errorf("timezone must be a valid IANA timezone: %w", err)
	}
	if c.Async.Enabled {
		if c.Async.WorkerConcurrency <= 0 {
			return fmt.Errorf("async.worker_concurrency must be > 0")
		}
		if c.Async.QueueClaimTTLSeconds <= 0 {
			return fmt.Errorf("async.queue_claim_ttl_seconds must be > 0")
		}
		if c.Async.MaxAttempts <= 0 {
			return fmt.Errorf("async.max_attempts must be > 0")
		}
		if c.Async.RetryBaseDelaySeconds <= 0 {
			return fmt.Errorf("async.retry_base_delay_seconds must be > 0")
		}
		if c.Async.RetryMaxDelaySeconds < c.Async.RetryBaseDelaySeconds {
			return fmt.Errorf("async.retry_max_delay_seconds must be >= retry_base_delay_seconds")
		}
	}
	if c.Idle.Enabled {
		if c.Idle.IdleAfterSeconds <= 0 {
			return fmt.Errorf("idle.idle_after_seconds must be > 0")
		}
		if c.Idle.SweepIntervalSeconds <= 0 {
			return fmt.Errorf("idle.sweep_interval_seconds must be > 0")
		}
		if c.Idle.MinEpisodeCount <= 0 {
			return fmt.Errorf("idle.min_episode_count must be > 0")
		}
		if c.Idle.MaxSegmentsPerSweep <= 0 {
			return fmt.Errorf("idle.max_segments_per_sweep must be > 0")
		}
		if !c.Idle.IncludeActiveSegments && !c.Idle.IncludeFinalizedSegments {
			return fmt.Errorf("idle must include active or finalized segments")
		}
	}
	if c.Manual.Enabled {
		switch normalizeMemoryExtractionMode(c.Manual.Mode) {
		case "validate", "dry-run", "apply":
		default:
			return fmt.Errorf("manual.mode must be validate, dry_run, or apply")
		}
	}
	if c.MirrorSync.AfterApply || c.MirrorSync.PeriodicEnabled {
		if c.MirrorSync.IntervalSeconds <= 0 {
			return fmt.Errorf("mirror_sync.interval_seconds must be > 0")
		}
		if c.MirrorSync.Limit <= 0 {
			return fmt.Errorf("mirror_sync.limit must be > 0")
		}
	}
	return nil
}

func (c MemoryNaturalMemoryConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.SchedulerEnabled && c.TickIntervalSeconds <= 0 {
		return fmt.Errorf("tick_interval_seconds must be > 0")
	}
	if strings.TrimSpace(c.LocalTime) != "" {
		if _, err := time.Parse("15:04", strings.TrimSpace(c.LocalTime)); err != nil {
			return fmt.Errorf("local_time must be HH:mm")
		}
	}
	if strings.TrimSpace(c.Timezone) != "" {
		if _, err := time.LoadLocation(strings.TrimSpace(c.Timezone)); err != nil {
			return fmt.Errorf("timezone must be a valid IANA timezone: %w", err)
		}
	}
	if c.MirrorSyncAfterRun && c.MirrorSyncLimit <= 0 {
		return fmt.Errorf("mirror_sync_limit must be > 0")
	}
	return nil
}

func normalizeMemoryExtractionMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "dry_run":
		return "dry-run"
	default:
		return strings.TrimSpace(mode)
	}
}

func (p LLMProvider) Validate() error {
	if p.ID == "" {
		return fmt.Errorf("id is required")
	}
	if p.Name == "" {
		return fmt.Errorf("name is required")
	}
	if p.PresetID != "" {
		if _, ok := llm.ProviderPresetByID(p.PresetID); !ok {
			return fmt.Errorf("unsupported preset_id: %s", p.PresetID)
		}
	}
	switch p.Protocol {
	case "openai_compatible", "anthropic", "dashscope_vl", "dashscope-vl":
	default:
		return fmt.Errorf("unsupported protocol: %s", p.Protocol)
	}
	if p.BaseURL == "" {
		return fmt.Errorf("base_url is required")
	}
	if p.APIKeyEnv == "" {
		return fmt.Errorf("api_key_env is required")
	}
	switch p.ModelDiscovery {
	case "", "manual", "openai_models", "anthropic_models":
	default:
		return fmt.Errorf("unsupported model_discovery: %s", p.ModelDiscovery)
	}
	for _, capability := range p.Capabilities {
		switch capability {
		case "chat", "embedding", "rerank", "query_analysis":
		default:
			return fmt.Errorf("unsupported capability: %s", capability)
		}
	}
	return nil
}

func (p LLMProvider) WithPresetDefaults() (LLMProvider, error) {
	p.ID = strings.TrimSpace(p.ID)
	p.Name = strings.TrimSpace(p.Name)
	p.PresetID = strings.TrimSpace(p.PresetID)
	p.Protocol = strings.TrimSpace(p.Protocol)
	p.BaseURL = strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	p.APIKeyEnv = strings.TrimSpace(p.APIKeyEnv)
	p.ModelDiscovery = strings.TrimSpace(p.ModelDiscovery)
	p.Capabilities = NormalizeProviderCapabilities(p.Capabilities)
	if p.PresetID == "" {
		return p, nil
	}
	preset, ok := llm.ProviderPresetByID(p.PresetID)
	if !ok {
		return LLMProvider{}, fmt.Errorf("unsupported preset_id: %s", p.PresetID)
	}
	if p.ID == "" {
		p.ID = preset.ID
	}
	if p.Name == "" {
		p.Name = preset.Name
	}
	if p.Protocol == "" {
		p.Protocol = preset.Protocol
	}
	if p.BaseURL == "" {
		p.BaseURL = preset.BaseURL
	}
	if p.APIKeyEnv == "" {
		p.APIKeyEnv = preset.APIKeyEnv
	}
	if p.ModelDiscovery == "" {
		p.ModelDiscovery = preset.ModelDiscovery
	}
	return p, nil
}

func NormalizeProviderCapabilities(capabilities []string) []string {
	if len(capabilities) == 0 {
		return []string{"chat"}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		capability = strings.ToLower(strings.TrimSpace(capability))
		if capability == "" {
			continue
		}
		if _, ok := seen[capability]; ok {
			continue
		}
		seen[capability] = struct{}{}
		out = append(out, capability)
	}
	if len(out) == 0 {
		return []string{"chat"}
	}
	return out
}

func (a AgentConfig) Validate() error {
	if a.ID == "" {
		return fmt.Errorf("id is required")
	}
	if a.Name == "" {
		return fmt.Errorf("name is required")
	}
	if a.PersonaKey == "" {
		return fmt.Errorf("persona_key is required")
	}
	if err := a.Emotion.Main.Validate(); err != nil {
		return fmt.Errorf("emotion.main: %w", err)
	}
	if err := a.Emotion.Summary.Validate(); err != nil {
		return fmt.Errorf("emotion.summary: %w", err)
	}
	if err := a.Work.Main.Validate(); err != nil {
		return fmt.Errorf("work.main: %w", err)
	}
	if err := a.Work.Summary.Validate(); err != nil {
		return fmt.Errorf("work.summary: %w", err)
	}
	return nil
}

func (b ModelBinding) Validate() error {
	if b.ProviderID == "" {
		return fmt.Errorf("provider_id is required")
	}
	if b.Model == "" {
		return fmt.Errorf("model is required")
	}
	if b.Params.MaxTokens < 0 {
		return fmt.Errorf("params.max_tokens must be >= 0")
	}
	if err := validateOptionalTemperature("params.temperature", b.Params.Temperature); err != nil {
		return err
	}
	return nil
}

func (a AgentConfig) ResolveContextConfig(base ContextConfig) (ContextConfig, error) {
	effective := base
	for key, raw := range a.ContextOverrides {
		switch key {
		case "input_budget_tokens":
			v, ok := numberAsInt(raw)
			if !ok {
				return ContextConfig{}, fmt.Errorf("%s must be a number", key)
			}
			effective.InputBudgetTokens = v
		case "soft_compact_ratio":
			v, ok := numberAsFloat(raw)
			if !ok {
				return ContextConfig{}, fmt.Errorf("%s must be a number", key)
			}
			effective.SoftCompactRatio = v
		case "hard_compact_ratio":
			v, ok := numberAsFloat(raw)
			if !ok {
				return ContextConfig{}, fmt.Errorf("%s must be a number", key)
			}
			effective.HardCompactRatio = v
		case "reserve_output_tokens":
			v, ok := numberAsInt(raw)
			if !ok {
				return ContextConfig{}, fmt.Errorf("%s must be a number", key)
			}
			effective.ReserveOutputTokens = v
		default:
			return ContextConfig{}, fmt.Errorf("unsupported key %q", key)
		}
	}
	if err := effective.Validate(); err != nil {
		return ContextConfig{}, err
	}
	return effective, nil
}

func validateOptionalTemperature(name string, value *float64) error {
	if value == nil {
		return nil
	}
	if *value < 0 || *value > 2 {
		return fmt.Errorf("%s must be between 0 and 2", name)
	}
	return nil
}

func numberAsInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		if typed != float64(int(typed)) {
			return 0, false
		}
		return int(typed), true
	case float32:
		f := float64(typed)
		if f != float64(int(f)) {
			return 0, false
		}
		return int(f), true
	default:
		return 0, false
	}
}

func numberAsFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	default:
		return 0, false
	}
}

func (c ContextConfig) Validate() error {
	if c.InputBudgetTokens <= 0 {
		return fmt.Errorf("input_budget_tokens must be > 0")
	}
	if c.ReserveOutputTokens <= 0 {
		return fmt.Errorf("reserve_output_tokens must be > 0")
	}
	if c.KeepRecentUserTurns <= 0 {
		return fmt.Errorf("keep_recent_user_turns must be > 0")
	}
	if c.ToolResultSoftTokens <= 0 {
		return fmt.Errorf("tool_result_soft_tokens must be > 0")
	}
	if c.ToolResultHardTokens <= 0 {
		return fmt.Errorf("tool_result_hard_tokens must be > 0")
	}
	if !(c.SoftCompactRatio > 0 && c.SoftCompactRatio < 1) {
		return fmt.Errorf("soft_compact_ratio must be between 0 and 1")
	}
	if !(c.HardCompactRatio > 0 && c.HardCompactRatio < 1) {
		return fmt.Errorf("hard_compact_ratio must be between 0 and 1")
	}
	if c.SoftCompactRatio >= c.HardCompactRatio {
		return fmt.Errorf("soft_compact_ratio must be < hard_compact_ratio")
	}
	return nil
}
