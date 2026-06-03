package memoryhost

import (
	"testing"

	memconfig "github.com/longyisang/emoagent-memorycore/config"
	"github.com/longyisang/emoagent/internal/config"
)

func TestBuildProviderRegistryMapsOpenAICompatibleProviders(t *testing.T) {
	registry := BuildProviderRegistry([]config.LLMProvider{
		{
			ID:        "moonshot",
			Name:      "Moonshot",
			Protocol:  "openai_compatible",
			BaseURL:   "https://api.moonshot.cn/v1",
			APIKeyEnv: "MOONSHOT_API_KEY",
			Enabled:   true,
		},
		{
			ID:        "disabled",
			Name:      "Disabled",
			Protocol:  "openai-compatible",
			BaseURL:   "https://disabled.invalid/v1",
			APIKeyEnv: "DISABLED_API_KEY",
			Enabled:   false,
		},
	})

	if len(registry.LLM) != 2 {
		t.Fatalf("len(registry.LLM) = %d, want 2", len(registry.LLM))
	}
	got := registry.LLM[0]
	if got.ID != "moonshot" ||
		got.Provider != "openai-compatible" ||
		got.Protocol != "openai_compatible" ||
		got.BaseURL != "https://api.moonshot.cn/v1" ||
		got.APIKeyEnv != "MOONSHOT_API_KEY" ||
		!got.Enabled {
		t.Fatalf("registry.LLM[0] = %#v", got)
	}
	if registry.LLM[1].Enabled {
		t.Fatalf("registry.LLM[1].Enabled = true, want false")
	}
}

func TestValidateLLMProviderBindingsRejectsUnsupportedProtocol(t *testing.T) {
	cfg := memconfig.DefaultConfig()
	cfg.Enabled = true
	cfg.Pipelines.Extraction.Enabled = true
	cfg.Pipelines.Extraction.ProviderID = "anthropic"
	cfg.Pipelines.Extraction.Model = "claude-test"
	cfg.ApplyProviderRegistry(memconfig.ProviderRegistry{
		LLM: []memconfig.ProviderMapping{{
			ID:        "anthropic",
			Provider:  "anthropic",
			Protocol:  "anthropic",
			BaseURL:   "https://api.anthropic.com",
			APIKeyEnv: "ANTHROPIC_API_KEY",
			Enabled:   true,
		}},
	})

	err := ValidateLLMProviderBindings(cfg)
	if err == nil {
		t.Fatal("ValidateLLMProviderBindings succeeded, want unsupported protocol error")
	}
	if got := err.Error(); got != `pipelines.extraction.provider_id "anthropic" uses unsupported protocol "anthropic"; MemoryCore LLM pipelines currently support only openai_compatible providers from EmoAgent Provider Center` {
		t.Fatalf("error = %q", got)
	}
}
