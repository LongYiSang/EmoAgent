package config

import (
	"os"
	"path/filepath"
	"testing"
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
	if cfg.Context.InputBudgetTokens <= 0 {
		t.Errorf("default context.input_budget_tokens = %d, want > 0", cfg.Context.InputBudgetTokens)
	}
	if cfg.Context.KeepRecentUserTurns <= 0 {
		t.Errorf("default context.keep_recent_user_turns = %d, want > 0", cfg.Context.KeepRecentUserTurns)
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
  api_key_env: ANTHROPIC_API_KEY
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
    max_tokens: 2048
    temperature: 0.3
    api_key_env: OPENAI_API_KEY
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
	// Default should still apply for unset fields.
	if cfg.DB.Path != "./data/emo.db" {
		t.Errorf("db.path = %q, want default", cfg.DB.Path)
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
