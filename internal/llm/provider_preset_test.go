package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProviderPresetsCoverCommonProviders(t *testing.T) {
	presets := ListProviderPresets()
	ids := make(map[string]bool, len(presets))
	for _, preset := range presets {
		ids[preset.ID] = true
	}
	for _, id := range []string{
		"openai", "moonshot", "deepseek", "anthropic", "gemini",
		"qwen_dashscope_cn", "qwen_dashscope_intl", "xai", "groq",
		"mistral", "openrouter", "custom_openai_compatible",
	} {
		if !ids[id] {
			t.Fatalf("preset %q missing from %#v", id, ids)
		}
	}

	moonshot, ok := ProviderPresetByID("moonshot")
	if !ok {
		t.Fatal("moonshot preset not found")
	}
	if moonshot.BaseURL != "https://api.moonshot.cn" {
		t.Fatalf("moonshot base_url = %q, want https://api.moonshot.cn", moonshot.BaseURL)
	}
	if moonshot.ChatCompletionsPath != "/v1/chat/completions" || moonshot.ModelsPath != "/v1/models" {
		t.Fatalf("moonshot paths = %q/%q", moonshot.ChatCompletionsPath, moonshot.ModelsPath)
	}
	if moonshot.Capabilities.ReasoningRequestStyle != ReasoningRequestThinkingType ||
		moonshot.Capabilities.ReasoningResponseStyle != ReasoningResponseReasoningContent {
		t.Fatalf("moonshot capabilities = %#v", moonshot.Capabilities)
	}
	if !containsString(moonshot.Admin.VisibleParams, "thinking_mode") {
		t.Fatalf("moonshot visible params = %#v, want thinking_mode", moonshot.Admin.VisibleParams)
	}

	groq, ok := ProviderPresetByID("groq")
	if !ok {
		t.Fatal("groq preset not found")
	}
	if groq.Capabilities.ReasoningResponseStyle != ReasoningResponseMessageReasoning {
		t.Fatalf("groq reasoning response style = %q, want %q", groq.Capabilities.ReasoningResponseStyle, ReasoningResponseMessageReasoning)
	}
}

func TestResolveProviderConfigAppliesPresetDefaultsAndKeepsOverrides(t *testing.T) {
	resolved, err := ResolveProviderConfig(ProviderConfig{
		ID:        "kimi-main",
		PresetID:  "moonshot",
		APIKeyEnv: "CUSTOM_KIMI_KEY",
	})
	if err != nil {
		t.Fatalf("ResolveProviderConfig: %v", err)
	}
	if resolved.Protocol != "openai_compatible" {
		t.Fatalf("Protocol = %q, want openai_compatible", resolved.Protocol)
	}
	if resolved.BaseURL != "https://api.moonshot.cn" {
		t.Fatalf("BaseURL = %q, want preset default", resolved.BaseURL)
	}
	if resolved.APIKeyEnv != "CUSTOM_KIMI_KEY" {
		t.Fatalf("APIKeyEnv = %q, want explicit override", resolved.APIKeyEnv)
	}
	if resolved.ChatCompletionsPath != "/v1/chat/completions" || resolved.ModelsPath != "/v1/models" {
		t.Fatalf("paths = %q/%q", resolved.ChatCompletionsPath, resolved.ModelsPath)
	}

	if _, err := ResolveProviderConfig(ProviderConfig{PresetID: "missing"}); err == nil {
		t.Fatal("ResolveProviderConfig missing preset error = nil")
	}
}

func TestEndpointURLJoinsBaseAndPath(t *testing.T) {
	if got := endpointURL("https://api.example.com/", "/compatible-mode/v1/models"); got != "https://api.example.com/compatible-mode/v1/models" {
		t.Fatalf("endpointURL = %q", got)
	}
}

func TestDiscoverModelsUsesResolvedModelsPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/compatible-mode/v1/models" {
			t.Fatalf("path = %s, want /compatible-mode/v1/models", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"qwen-a"}]}`)
	}))
	defer server.Close()
	t.Setenv("TEST_MODELS_KEY", "test-key")

	models, err := DiscoverModels(context.Background(), ProviderConfig{
		Protocol:   "openai_compatible",
		BaseURL:    server.URL,
		APIKeyEnv:  "TEST_MODELS_KEY",
		ModelsPath: "/compatible-mode/v1/models",
	})
	if err != nil {
		t.Fatalf("DiscoverModels: %v", err)
	}
	if len(models) != 1 || models[0].ID != "qwen-a" {
		t.Fatalf("models = %#v, want qwen-a", models)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
