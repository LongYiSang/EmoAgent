package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/llm"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server       ServerConfig       `yaml:"server"`
	Chat         ChatConfig         `yaml:"chat"`
	Context      ContextConfig      `yaml:"context"`
	Work         WorkConfig         `yaml:"work"`
	LLMProviders []LLMProvider      `yaml:"llm_providers"`
	AgentConfigs []AgentConfig      `yaml:"agent_configs"`
	Agent        AgentRuntimeConfig `yaml:"agent"`
	Memory       MemoryConfig       `yaml:"memory"`
	DB           DBConfig           `yaml:"db"`
	Log          LogConfig          `yaml:"log"`
	Personas     PersonasConfig     `yaml:"personas"`
	WebSearch    WebSearchConfig    `yaml:"websearch"`
	WebFetch     WebFetchConfig     `yaml:"webfetch"`
	Bash         BashConfig         `yaml:"bash"`
}

type LLMProvider struct {
	ID             string `yaml:"id" json:"id"`
	Name           string `yaml:"name" json:"name"`
	PresetID       string `yaml:"preset_id" json:"preset_id"`
	Protocol       string `yaml:"protocol" json:"protocol"`
	BaseURL        string `yaml:"base_url" json:"base_url"`
	APIKeyEnv      string `yaml:"api_key_env" json:"api_key_env"`
	ModelDiscovery string `yaml:"model_discovery" json:"model_discovery"`
	Enabled        bool   `yaml:"enabled" json:"enabled"`
}

type AgentRuntimeConfig struct {
	ActiveConfig string `yaml:"active_config" json:"active_config"`
}

type MemoryConfig struct {
	Enabled         bool                   `yaml:"enabled" json:"enabled"`
	ConfigPath      string                 `yaml:"config_path" json:"config_path"`
	ManualRulesPath string                 `yaml:"manual_rules_path" json:"manual_rules_path"`
	Retrieval       MemoryRetrievalConfig  `yaml:"retrieval" json:"retrieval"`
	Extraction      MemoryExtractionConfig `yaml:"extraction" json:"extraction"`
}

type MemoryRetrievalConfig struct {
	Enabled             bool `yaml:"enabled" json:"enabled"`
	InjectPrompt        bool `yaml:"inject_prompt" json:"inject_prompt"`
	UseFTS              bool `yaml:"use_fts" json:"use_fts"`
	UseMirror           bool `yaml:"use_mirror" json:"use_mirror"`
	FinalMemoryCount    int  `yaml:"final_memory_count" json:"final_memory_count"`
	ContextBudgetTokens int  `yaml:"context_budget_tokens" json:"context_budget_tokens"`
	FailOpen            bool `yaml:"fail_open" json:"fail_open"`
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
	RealtimeStreaming bool `yaml:"realtime_streaming" json:"realtime_streaming"`
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
		Chat: ChatConfig{
			RealtimeStreaming: false,
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
		Memory: MemoryConfig{
			Enabled:         false,
			ConfigPath:      "./config/memorycore.yaml",
			ManualRulesPath: "./config/memory_manual_rules.yaml",
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
	}
	cfg.Work.ApplyDefaults()
	return cfg
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
	cfg.Work.ApplyDefaults()
	cfg.WebFetch.applyDefaults()
	cfg.Bash.applyDefaults()
	cfg.Memory.Extraction.applyDefaults()
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

// Validate checks that required fields are set.
func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be 1-65535, got %d", c.Server.Port)
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
	case "openai_compatible", "anthropic":
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
