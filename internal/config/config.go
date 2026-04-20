package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server      ServerConfig    `yaml:"server"`
	LLM         LLMConfig       `yaml:"llm"`
	Context     ContextConfig   `yaml:"context"`
	Work        WorkConfig      `yaml:"work"`
	LLMProfiles []LLMProfile    `yaml:"llm_profiles"`
	DB          DBConfig        `yaml:"db"`
	Log         LogConfig       `yaml:"log"`
	Personas    PersonasConfig  `yaml:"personas"`
	WebSearch   WebSearchConfig `yaml:"websearch"`
	WebFetch    WebFetchConfig  `yaml:"webfetch"`
	Bash        BashConfig      `yaml:"bash"`
}

type WebFetchConfig struct {
	Enabled      bool   `yaml:"enabled"`
	TimeoutSec   int    `yaml:"timeout_sec"`
	MaxBytes     int    `yaml:"max_bytes"`
	MaxRedirects int    `yaml:"max_redirects"`
	UserAgent    string `yaml:"user_agent"`
}

func (c *WebFetchConfig) applyDefaults() {
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

type LLMConfig struct {
	Provider           string   `yaml:"provider"`
	BaseURL            string   `yaml:"base_url"`
	Model              string   `yaml:"model"`
	SummaryModel       string   `yaml:"summary_model"`
	SummaryTemperature *float64 `yaml:"summary_temperature"`
	MaxTokens          int      `yaml:"max_tokens"`
	Temperature        float64  `yaml:"temperature"`
	APIKeyEnv          string   `yaml:"api_key_env"`
}

type LLMProfile struct {
	Name                string   `yaml:"name" json:"name"`
	Provider            string   `yaml:"provider" json:"provider"`
	BaseURL             string   `yaml:"base_url" json:"base_url"`
	Model               string   `yaml:"model" json:"model"`
	SummaryModel        string   `yaml:"summary_model" json:"summary_model"`
	SummaryTemperature  *float64 `yaml:"summary_temperature,omitempty" json:"summary_temperature,omitempty"`
	MaxTokens           int      `yaml:"max_tokens" json:"max_tokens"`
	Temperature         float64  `yaml:"temperature" json:"temperature"`
	APIKeyEnv           string   `yaml:"api_key_env" json:"api_key_env"`
	InputBudgetTokens   *int     `yaml:"input_budget_tokens,omitempty" json:"input_budget_tokens,omitempty"`
	SoftCompactRatio    *float64 `yaml:"soft_compact_ratio,omitempty" json:"soft_compact_ratio,omitempty"`
	HardCompactRatio    *float64 `yaml:"hard_compact_ratio,omitempty" json:"hard_compact_ratio,omitempty"`
	ReserveOutputTokens *int     `yaml:"reserve_output_tokens,omitempty" json:"reserve_output_tokens,omitempty"`
}

type DBConfig struct {
	Path string `yaml:"path"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type PersonasConfig struct {
	Dir     string `yaml:"dir"`
	Default string `yaml:"default"`
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
	Profile                  string        `yaml:"profile"`
	MaxToolRounds            int           `yaml:"max_tool_rounds"`
	MaxInputTokens           int           `yaml:"max_input_tokens"`
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
	if w.Profile == "" {
		w.Profile = "default"
	}
	if w.MaxToolRounds == 0 {
		w.MaxToolRounds = 15
	}
	if w.MaxInputTokens == 0 {
		w.MaxInputTokens = 100000
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
	return &Config{
		Server: ServerConfig{
			Host: "127.0.0.1",
			Port: 8080,
		},
		LLM: LLMConfig{
			Provider:    "openai",
			BaseURL:     "https://api.openai.com",
			Model:       "gpt-4o",
			MaxTokens:   4096,
			Temperature: 0.7,
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
		Work: WorkConfig{
			Profile:                  "default",
			MaxToolRounds:            15,
			MaxInputTokens:           100000,
			JournalDir:               "./logs/work",
			MaxEscalationsPerTask:    3,
			PendingDecisionTTL:       30 * time.Minute,
			SoftTTL:                  30 * time.Minute,
			HardTTL:                  time.Hour,
			ArchiveTTL:               24 * time.Hour,
			ResumeClaimTTL:           10 * time.Minute,
			DeciderCleanupInterval:   5 * time.Minute,
			PendingSnapshotMaxTokens: 60000,
		},
		LLMProfiles: []LLMProfile{},
		DB: DBConfig{
			Path: "./data/emo.db",
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
		Personas: PersonasConfig{
			Dir:     "./personas",
			Default: "default",
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
			TimeoutSec:   20,
			MaxBytes:     1 << 20,
			MaxRedirects: 5,
			UserAgent:    "EmoAgent/0.1",
		},
		Bash: BashConfig{
			Enabled:        false,
			TimeoutSec:     60,
			MaxOutputBytes: 256 << 10,
		},
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
	cfg.Work.ApplyDefaults()
	cfg.WebFetch.applyDefaults()
	cfg.Bash.applyDefaults()

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
	if c.LLM.Provider == "" {
		return fmt.Errorf("llm.provider is required")
	}
	if c.LLM.Model == "" {
		return fmt.Errorf("llm.model is required")
	}
	if err := validateOptionalTemperature("llm.summary_temperature", c.LLM.SummaryTemperature); err != nil {
		return err
	}
	if err := c.Context.Validate(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	for i, profile := range c.LLMProfiles {
		if err := profile.ValidateAgainst(c.Context); err != nil {
			return fmt.Errorf("llm_profiles[%d]: %w", i, err)
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
	return nil
}

// Validate checks that required fields are set.
func (p LLMProfile) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("name is required")
	}
	switch p.Provider {
	case "openai", "anthropic":
	default:
		return fmt.Errorf("unsupported provider: %s", p.Provider)
	}
	if p.BaseURL == "" {
		return fmt.Errorf("base_url is required")
	}
	if p.Model == "" {
		return fmt.Errorf("model is required")
	}
	if p.MaxTokens <= 0 {
		return fmt.Errorf("max_tokens must be greater than 0")
	}
	if p.Temperature < 0 || p.Temperature > 2 {
		return fmt.Errorf("temperature must be between 0 and 2")
	}
	if err := validateOptionalTemperature("summary_temperature", p.SummaryTemperature); err != nil {
		return err
	}
	return nil
}

// ValidateAgainst checks that the profile is valid and resolves cleanly against the provided base context.
func (p LLMProfile) ValidateAgainst(base ContextConfig) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if _, err := p.ResolveContextConfig(base); err != nil {
		return err
	}
	return nil
}

// ResolveContextConfig applies the profile's optional context budget overrides to the provided base config.
func (p LLMProfile) ResolveContextConfig(base ContextConfig) (ContextConfig, error) {
	effective := base
	if p.InputBudgetTokens != nil {
		effective.InputBudgetTokens = *p.InputBudgetTokens
	}
	if p.SoftCompactRatio != nil {
		effective.SoftCompactRatio = *p.SoftCompactRatio
	}
	if p.HardCompactRatio != nil {
		effective.HardCompactRatio = *p.HardCompactRatio
	}
	if p.ReserveOutputTokens != nil {
		effective.ReserveOutputTokens = *p.ReserveOutputTokens
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
