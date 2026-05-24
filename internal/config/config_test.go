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
	if len(cfg.LLMProviders) != 0 {
		t.Errorf("default llm_providers length = %d, want 0", len(cfg.LLMProviders))
	}
	if len(cfg.AgentConfigs) != 0 {
		t.Errorf("default agent_configs length = %d, want 0", len(cfg.AgentConfigs))
	}
	if cfg.Chat.RealtimeStreaming {
		t.Error("default chat.realtime_streaming = true, want false")
	}
	if cfg.Memory.Enabled {
		t.Error("default memory.enabled = true, want false")
	}
	if cfg.Memory.ConfigPath != "./config/memorycore.yaml" {
		t.Errorf("default memory.config_path = %q, want ./config/memorycore.yaml", cfg.Memory.ConfigPath)
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

func TestLoadValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte(`
server:
  port: 9090
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
