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
