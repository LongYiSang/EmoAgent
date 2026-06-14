package llm

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const (
	ReasoningRequestReasoningEffort     = "reasoning_effort"
	ReasoningRequestThinkingType        = "thinking_type"
	ReasoningRequestSiliconFlowThinking = "siliconflow_enable_thinking"
	ReasoningRequestAnthropic           = "anthropic_thinking"

	ReasoningResponseReasoningContent = "reasoning_content"
	ReasoningResponseMessageReasoning = "message_reasoning"
	ReasoningResponseAnthropicBlocks  = "anthropic_thinking_blocks"

	ToolReasoningContinuationPreserve = "preserve_during_tool_loop"
)

type ProviderPreset struct {
	ID                   string               `yaml:"id" json:"id"`
	Name                 string               `yaml:"name" json:"name"`
	Protocol             string               `yaml:"protocol" json:"protocol"`
	BaseURL              string               `yaml:"base_url" json:"base_url"`
	APIKeyEnv            string               `yaml:"api_key_env" json:"api_key_env"`
	ModelDiscovery       string               `yaml:"model_discovery" json:"model_discovery"`
	ChatCompletionsPath  string               `yaml:"chat_completions_path" json:"chat_completions_path"`
	ModelsPath           string               `yaml:"models_path" json:"models_path"`
	RerankPath           string               `yaml:"rerank_path" json:"rerank_path,omitempty"`
	DocumentationURL     string               `yaml:"documentation_url" json:"documentation_url,omitempty"`
	ProviderCapabilities []string             `yaml:"provider_capabilities" json:"provider_capabilities,omitempty"`
	Capabilities         ProviderCapabilities `yaml:"capabilities" json:"capabilities"`
	Admin                ProviderAdmin        `yaml:"admin" json:"admin"`
}

type ProviderCapabilities struct {
	ReasoningRequestStyle          string `yaml:"reasoning_request_style" json:"reasoning_request_style,omitempty"`
	ReasoningResponseStyle         string `yaml:"reasoning_response_style" json:"reasoning_response_style,omitempty"`
	ToolReasoningContinuation      string `yaml:"tool_reasoning_continuation" json:"tool_reasoning_continuation,omitempty"`
	ThinkingEffortFallbackToReason bool   `yaml:"thinking_effort_fallback_to_reasoning" json:"thinking_effort_fallback_to_reasoning,omitempty"`
}

type ProviderAdmin struct {
	VisibleParams   []string      `yaml:"visible_params" json:"visible_params"`
	MainDefaults    RequestParams `yaml:"main_defaults" json:"main_defaults"`
	SummaryDefaults RequestParams `yaml:"summary_defaults" json:"summary_defaults"`
}

type providerPresetFile struct {
	Presets []ProviderPreset `yaml:"presets"`
}

//go:embed provider_presets.yaml
var providerPresetsYAML []byte

var (
	providerPresetOnce sync.Once
	providerPresets    []ProviderPreset
	providerPresetErr  error
)

func ListProviderPresets() []ProviderPreset {
	presets, err := loadProviderPresets()
	if err != nil {
		return []ProviderPreset{}
	}
	cp := make([]ProviderPreset, len(presets))
	copy(cp, presets)
	return cp
}

func ProviderPresetByID(id string) (ProviderPreset, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return ProviderPreset{}, false
	}
	presets, err := loadProviderPresets()
	if err != nil {
		return ProviderPreset{}, false
	}
	for _, preset := range presets {
		if preset.ID == id {
			return preset, true
		}
	}
	return ProviderPreset{}, false
}

func ResolveProviderConfig(cfg ProviderConfig) (ProviderConfig, error) {
	cfg.ID = strings.TrimSpace(cfg.ID)
	cfg.PresetID = strings.TrimSpace(cfg.PresetID)
	cfg.Protocol = strings.TrimSpace(cfg.Protocol)
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.APIKeyEnv = strings.TrimSpace(cfg.APIKeyEnv)
	cfg.ChatCompletionsPath = normalizePath(cfg.ChatCompletionsPath)
	cfg.ModelsPath = normalizePath(cfg.ModelsPath)
	cfg.RerankPath = normalizePath(cfg.RerankPath)
	cfg.ModelDiscovery = strings.TrimSpace(cfg.ModelDiscovery)

	if cfg.PresetID != "" {
		preset, ok := ProviderPresetByID(cfg.PresetID)
		if !ok {
			return ProviderConfig{}, fmt.Errorf("unknown provider preset %q", cfg.PresetID)
		}
		cfg = applyPresetToProviderConfig(cfg, preset)
	}
	applyProtocolDefaults(&cfg)
	return cfg, nil
}

func loadProviderPresets() ([]ProviderPreset, error) {
	providerPresetOnce.Do(func() {
		var file providerPresetFile
		if err := yaml.Unmarshal(providerPresetsYAML, &file); err != nil {
			providerPresetErr = fmt.Errorf("parse provider presets: %w", err)
			return
		}
		seen := map[string]struct{}{}
		for i := range file.Presets {
			preset := &file.Presets[i]
			preset.ID = strings.TrimSpace(preset.ID)
			preset.Name = strings.TrimSpace(preset.Name)
			preset.Protocol = strings.TrimSpace(preset.Protocol)
			preset.BaseURL = strings.TrimRight(strings.TrimSpace(preset.BaseURL), "/")
			preset.APIKeyEnv = strings.TrimSpace(preset.APIKeyEnv)
			preset.ModelDiscovery = strings.TrimSpace(preset.ModelDiscovery)
			preset.ChatCompletionsPath = normalizePath(preset.ChatCompletionsPath)
			preset.ModelsPath = normalizePath(preset.ModelsPath)
			preset.RerankPath = normalizePath(preset.RerankPath)
			preset.ProviderCapabilities = normalizeStringList(preset.ProviderCapabilities)
			if preset.ID == "" {
				providerPresetErr = fmt.Errorf("provider preset at index %d has empty id", i)
				return
			}
			if _, ok := seen[preset.ID]; ok {
				providerPresetErr = fmt.Errorf("duplicate provider preset id %q", preset.ID)
				return
			}
			seen[preset.ID] = struct{}{}
			if preset.Name == "" || preset.Protocol == "" || preset.BaseURL == "" || preset.APIKeyEnv == "" {
				providerPresetErr = fmt.Errorf("provider preset %q missing required defaults", preset.ID)
				return
			}
			applyProtocolDefaultsToPreset(preset)
		}
		providerPresets = file.Presets
		sort.SliceStable(providerPresets, func(i, j int) bool {
			return providerPresets[i].ID < providerPresets[j].ID
		})
	})
	return providerPresets, providerPresetErr
}

func applyPresetToProviderConfig(cfg ProviderConfig, preset ProviderPreset) ProviderConfig {
	if cfg.Protocol == "" {
		cfg.Protocol = preset.Protocol
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = preset.BaseURL
	}
	if cfg.APIKeyEnv == "" {
		cfg.APIKeyEnv = preset.APIKeyEnv
	}
	if cfg.ChatCompletionsPath == "" {
		cfg.ChatCompletionsPath = preset.ChatCompletionsPath
	}
	if cfg.ModelsPath == "" {
		cfg.ModelsPath = preset.ModelsPath
	}
	if cfg.RerankPath == "" {
		cfg.RerankPath = preset.RerankPath
	}
	if cfg.ModelDiscovery == "" {
		cfg.ModelDiscovery = preset.ModelDiscovery
	}
	if cfg.ReasoningRequestStyle == "" {
		cfg.ReasoningRequestStyle = preset.Capabilities.ReasoningRequestStyle
	}
	if cfg.ReasoningResponseStyle == "" {
		cfg.ReasoningResponseStyle = preset.Capabilities.ReasoningResponseStyle
	}
	if cfg.ToolReasoningContinuation == "" {
		cfg.ToolReasoningContinuation = preset.Capabilities.ToolReasoningContinuation
	}
	if !cfg.ThinkingEffortFallbackToReason {
		cfg.ThinkingEffortFallbackToReason = preset.Capabilities.ThinkingEffortFallbackToReason
	}
	return cfg
}

func applyProtocolDefaultsToPreset(preset *ProviderPreset) {
	cfg := ProviderConfig{
		Protocol:            preset.Protocol,
		ChatCompletionsPath: preset.ChatCompletionsPath,
		ModelsPath:          preset.ModelsPath,
	}
	applyProtocolDefaults(&cfg)
	preset.ChatCompletionsPath = cfg.ChatCompletionsPath
	preset.ModelsPath = cfg.ModelsPath
	preset.RerankPath = normalizePath(preset.RerankPath)
	if preset.ModelDiscovery == "" {
		preset.ModelDiscovery = "manual"
	}
}

func applyProtocolDefaults(cfg *ProviderConfig) {
	if cfg.ChatCompletionsPath == "" {
		switch cfg.Protocol {
		case "anthropic":
			cfg.ChatCompletionsPath = "/v1/messages"
		default:
			cfg.ChatCompletionsPath = "/v1/chat/completions"
		}
	}
	if cfg.ModelsPath == "" {
		cfg.ModelsPath = "/v1/models"
	}
}

func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return "/" + strings.Trim(path, "/")
}

func endpointURL(baseURL, path string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	path = normalizePath(path)
	if path == "" {
		return baseURL
	}
	return baseURL + path
}

func normalizeStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
