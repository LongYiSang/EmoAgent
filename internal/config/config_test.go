package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Server.Port != 8080 {
		t.Errorf("default port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.LLM.Provider != "openai" {
		t.Errorf("default provider = %q, want openai", cfg.LLM.Provider)
	}
	if cfg.LLM.APIKeyEnv != "" {
		t.Errorf("default llm.api_key_env = %q, want empty", cfg.LLM.APIKeyEnv)
	}
	if len(cfg.LLMProfiles) != 0 {
		t.Errorf("default llm_profiles length = %d, want 0", len(cfg.LLMProfiles))
	}
	if cfg.Chat.RealtimeStreaming {
		t.Error("default chat.realtime_streaming = true, want false")
	}
	if cfg.Context.InputBudgetTokens <= 0 {
		t.Errorf("default context.input_budget_tokens = %d, want > 0", cfg.Context.InputBudgetTokens)
	}
	if cfg.Context.KeepRecentUserTurns <= 0 {
		t.Errorf("default context.keep_recent_user_turns = %d, want > 0", cfg.Context.KeepRecentUserTurns)
	}
	if cfg.Work.Profile != "default" {
		t.Errorf("default work.profile = %q, want default", cfg.Work.Profile)
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

func TestLoadValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
server:
  port: 9090
llm:
  provider: anthropic
  model: claude-sonnet-4-20250514
  summary_temperature: 0.2
  api_key_env: ANTHROPIC_API_KEY
chat:
  realtime_streaming: true
context:
  input_budget_tokens: 12345
  soft_compact_ratio: 0.7
  hard_compact_ratio: 0.9
  reserve_output_tokens: 2048
  keep_recent_user_turns: 4
  tool_result_soft_tokens: 500
  tool_result_hard_tokens: 1500
llm_profiles:
  - name: default
    provider: openai
    base_url: https://api.openai.com
    model: gpt-4o
    summary_model: gpt-4o-mini
    summary_temperature: 0.1
    max_tokens: 2048
    temperature: 0.3
    api_key_env: OPENAI_API_KEY
    input_budget_tokens: 12000
    reserve_output_tokens: 1024
`), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("port = %d, want 9090", cfg.Server.Port)
	}
	if cfg.LLM.Provider != "anthropic" {
		t.Errorf("provider = %q, want anthropic", cfg.LLM.Provider)
	}
	if cfg.LLM.APIKeyEnv != "ANTHROPIC_API_KEY" {
		t.Errorf("llm.api_key_env = %q, want ANTHROPIC_API_KEY", cfg.LLM.APIKeyEnv)
	}
	if cfg.LLM.SummaryTemperature == nil || *cfg.LLM.SummaryTemperature != 0.2 {
		t.Fatalf("llm.summary_temperature = %#v, want 0.2", cfg.LLM.SummaryTemperature)
	}
	if !cfg.Chat.RealtimeStreaming {
		t.Fatal("chat.realtime_streaming = false, want true")
	}
	if cfg.Context.InputBudgetTokens != 12345 {
		t.Errorf("context.input_budget_tokens = %d, want 12345", cfg.Context.InputBudgetTokens)
	}
	if cfg.Context.KeepRecentUserTurns != 4 {
		t.Errorf("context.keep_recent_user_turns = %d, want 4", cfg.Context.KeepRecentUserTurns)
	}
	if len(cfg.LLMProfiles) != 1 {
		t.Fatalf("llm_profiles length = %d, want 1", len(cfg.LLMProfiles))
	}
	profile := cfg.LLMProfiles[0]
	if profile.Name != "default" || profile.APIKeyEnv != "OPENAI_API_KEY" || profile.MaxTokens != 2048 {
		t.Fatalf("llm_profiles[0] = %#v", profile)
	}
	if profile.SummaryTemperature == nil || *profile.SummaryTemperature != 0.1 {
		t.Fatalf("llm_profiles[0].summary_temperature = %#v, want 0.1", profile.SummaryTemperature)
	}
	if profile.InputBudgetTokens == nil || *profile.InputBudgetTokens != 12000 {
		t.Fatalf("llm_profiles[0].input_budget_tokens = %#v, want 12000", profile.InputBudgetTokens)
	}
	if profile.ReserveOutputTokens == nil || *profile.ReserveOutputTokens != 1024 {
		t.Fatalf("llm_profiles[0].reserve_output_tokens = %#v, want 1024", profile.ReserveOutputTokens)
	}
	effective, err := profile.ResolveContextConfig(cfg.Context)
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
	// Default should still apply for unset fields.
	if cfg.DB.Path != "./data/emo.db" {
		t.Errorf("db.path = %q, want default", cfg.DB.Path)
	}
	if cfg.Work.Profile != "default" {
		t.Errorf("work.profile = %q, want default", cfg.Work.Profile)
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

func TestValidateRejectsInvalidProfileBudgetOverrides(t *testing.T) {
	cfg := DefaultConfig()
	zero := 0
	cfg.LLMProfiles = []LLMProfile{{
		Name:              "default",
		Provider:          "openai",
		BaseURL:           "https://api.openai.com",
		Model:             "gpt-4o-mini",
		MaxTokens:         1024,
		Temperature:       0.7,
		InputBudgetTokens: &zero,
	}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid profile input_budget_tokens override")
	}
}

func TestValidateRejectsInvalidSummaryTemperature(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LLM.SummaryTemperature = floatPtr(2.5)
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid llm.summary_temperature")
	}

	cfg = DefaultConfig()
	cfg.LLMProfiles = []LLMProfile{{
		Name:               "default",
		Provider:           "openai",
		BaseURL:            "https://api.openai.com",
		Model:              "gpt-4o-mini",
		MaxTokens:          1024,
		Temperature:        0.7,
		SummaryTemperature: floatPtr(-0.1),
	}}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid profile summary_temperature")
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
