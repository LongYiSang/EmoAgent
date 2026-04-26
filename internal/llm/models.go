package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

type ModelInfo struct {
	ID string `json:"id"`
}

func DiscoverModels(ctx context.Context, cfg ProviderConfig) ([]ModelInfo, error) {
	resolved, err := ResolveProviderConfig(cfg)
	if err != nil {
		return nil, err
	}
	cfg = resolved
	apiKeyEnv := cfg.APIKeyEnv
	if apiKeyEnv == "" {
		apiKeyEnv = defaultAPIKeyEnv(cfg.Protocol)
	}
	apiKey := os.Getenv(apiKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("%s environment variable not set", apiKeyEnv)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL(cfg.BaseURL, cfg.ModelsPath), nil)
	if err != nil {
		return nil, fmt.Errorf("create models request: %w", err)
	}
	switch cfg.Protocol {
	case "openai", "openai_compatible":
		req.Header.Set("Authorization", "Bearer "+apiKey)
	case "anthropic":
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", cfg.Protocol)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, wrapRequestError(cfg.Protocol, "models", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, wrapStatusError(cfg.Protocol, "models", resp.StatusCode, "")
	}

	var envelope struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, wrapDecodeError(cfg.Protocol, "models", err)
	}
	return envelope.Data, nil
}
