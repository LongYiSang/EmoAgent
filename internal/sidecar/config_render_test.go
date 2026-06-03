package sidecar

import (
	"strings"
	"testing"
)

func TestRenderGeneratedConfigUsesEnvNamesWithoutSecretValues(t *testing.T) {
	t.Setenv("DASHSCOPE_API_KEY", "secret-value")
	spec := DefaultSpec()
	spec.TriviumDir = `D:\Dev\Project\Agent\EmoAgent\data\trivium`
	spec.EmbeddingCacheDBPath = `D:\Dev\Project\Agent\EmoAgent\data\embedding_cache.sqlite3`
	spec.Embedding = ProviderBinding{
		Provider:   "openai-compatible",
		BaseURL:    "https://dashscope.aliyuncs.com/compatible-mode/v1",
		APIKeyEnv:  "DASHSCOPE_API_KEY",
		Model:      "text-embedding-v4",
		Dimensions: 1024,
	}
	spec.QueryAnalysis = ProviderBinding{
		Provider:  "openai-compatible",
		BaseURL:   "https://api.deepseek.com",
		APIKeyEnv: "MEMORYCORE_LLM_API_KEY",
		Model:     "deepseek-v4-flash",
	}
	spec.Rerank = ProviderBinding{
		Provider:    "dashscope-vl",
		EndpointURL: "https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank",
		APIKeyEnv:   "DASHSCOPE_API_KEY",
		Model:       "qwen3-vl-rerank",
		TopK:        12,
	}

	body, err := RenderConfig(spec)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	text := string(body)
	for _, want := range []string{
		`[trivium]`,
		`dir = "D:\\Dev\\Project\\Agent\\EmoAgent\\data\\trivium"`,
		`api_key_env = "DASHSCOPE_API_KEY"`,
		`model = "text-embedding-v4"`,
		`endpoint_url = "https://dashscope.aliyuncs.com/api/v1/services/rerank/text-rerank/text-rerank"`,
		`top_n = 12`,
		`[query_analysis]`,
		`api_key_env = "MEMORYCORE_LLM_API_KEY"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("generated TOML missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "secret-value") {
		t.Fatalf("generated TOML leaked API key value:\n%s", text)
	}
	if strings.Contains(text, `instruct = ""`) {
		t.Fatalf("generated TOML should not write empty rerank instruct:\n%s", text)
	}
}

func TestSpecRejectsNonLoopbackHost(t *testing.T) {
	spec := DefaultSpec()
	spec.Host = "0.0.0.0"

	err := spec.Validate()
	if err == nil {
		t.Fatal("Validate succeeded, want loopback error")
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("error = %q, want loopback", err.Error())
	}
}
