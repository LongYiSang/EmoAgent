package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

type ModelInfo struct {
	ID               string             `json:"id"`
	InputModalities  []string           `json:"input_modalities,omitempty"`
	OutputModalities []string           `json:"output_modalities,omitempty"`
	ImageTransports  []string           `json:"image_transports,omitempty"`
	ImageFormats     []string           `json:"image_formats,omitempty"`
	Capabilities     *ModelCapabilities `json:"capabilities,omitempty"`
	RawJSON          string             `json:"-"`
}

func (m *ModelInfo) UnmarshalJSON(data []byte) error {
	type alias ModelInfo
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	if len(a.InputModalities) == 0 {
		a.InputModalities = readStringSlice(raw, "modalities", "input_modalities", "input")
	}
	if len(a.OutputModalities) == 0 {
		a.OutputModalities = readStringSlice(raw, "output_modalities", "output")
	}
	if len(a.ImageTransports) == 0 {
		a.ImageTransports = readStringSlice(raw, "image_transports", "image_transport")
	}
	if len(a.ImageFormats) == 0 {
		a.ImageFormats = readStringSlice(raw, "image_formats", "image_format")
	}
	a.RawJSON = compactJSON(data)
	*m = ModelInfo(a)
	return nil
}

func readStringSlice(raw map[string]json.RawMessage, keys ...string) []string {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok || len(value) == 0 {
			continue
		}
		var values []string
		if err := json.Unmarshal(value, &values); err == nil {
			return values
		}
		var one string
		if err := json.Unmarshal(value, &one); err == nil && one != "" {
			return []string{one}
		}
	}
	return nil
}

func compactJSON(data []byte) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, data); err != nil {
		return string(data)
	}
	return buf.String()
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
