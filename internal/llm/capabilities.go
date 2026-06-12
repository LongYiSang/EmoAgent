package llm

import (
	"encoding/json"
	"strings"
)

type CapabilitySource string

const (
	CapabilitySourceUnknown            CapabilitySource = "unknown"
	CapabilitySourceProviderMetadata   CapabilitySource = "provider_metadata"
	CapabilitySourceProviderDocsPreset CapabilitySource = "provider_docs_preset"
	CapabilitySourceManualOverride     CapabilitySource = "manual_override"
	CapabilitySourceProbePassed        CapabilitySource = "probe_passed"
	CapabilitySourceProbeFailed        CapabilitySource = "probe_failed"
	CapabilitySourceMerged             CapabilitySource = "merged"
)

type ModelCapabilities struct {
	ProviderID string `json:"provider_id,omitempty"`
	ModelID    string `json:"model_id,omitempty"`

	InputModalities  []string `json:"input_modalities,omitempty"`
	OutputModalities []string `json:"output_modalities,omitempty"`
	ImageTransports  []string `json:"image_transports,omitempty"`
	ImageFormats     []string `json:"image_formats,omitempty"`

	MaxImagesPerRequest int   `json:"max_images_per_request,omitempty"`
	MaxImageBytes       int64 `json:"max_image_bytes,omitempty"`
	MaxRequestBytes     int64 `json:"max_request_bytes,omitempty"`
	MaxLongEdgePixels   int64 `json:"max_long_edge_pixels,omitempty"`

	SupportsVisionTools     bool `json:"supports_vision_tools,omitempty"`
	SupportsVisionStreaming bool `json:"supports_vision_streaming,omitempty"`
	SupportsVisionJSONMode  bool `json:"supports_vision_json_mode,omitempty"`

	ParamPolicyJSON  string  `json:"param_policy_json,omitempty"`
	CapabilitySource string  `json:"capability_source,omitempty"`
	Confidence       float64 `json:"confidence,omitempty"`
	LastRefreshedAt  string  `json:"last_refreshed_at,omitempty"`
	LastVerifiedAt   string  `json:"last_verified_at,omitempty"`
	RawProviderJSON  string  `json:"raw_provider_json,omitempty"`
}

func EnrichModelCapabilities(providerID, presetID string, models []ModelInfo, refreshedAt string, manual map[string]ModelCapabilities) []ModelCapabilities {
	caps := make([]ModelCapabilities, 0, len(models))
	for _, model := range models {
		capability := presetCapability(providerID, presetID, model.ID)
		if metadata := metadataCapability(providerID, model, refreshedAt); metadata != nil {
			capability = mergeCapability(capability, *metadata)
		}
		capability.ProviderID = providerID
		capability.ModelID = model.ID
		capability.LastRefreshedAt = refreshedAt
		if override, ok := manual[model.ID]; ok && sourceRank(CapabilitySource(override.CapabilitySource)) >= sourceRank(CapabilitySourceManualOverride) {
			override.ProviderID = providerID
			override.ModelID = model.ID
			if override.LastRefreshedAt == "" {
				override.LastRefreshedAt = refreshedAt
			}
			capability = override
		}
		caps = append(caps, normalizeCapability(capability))
	}
	return caps
}

func metadataCapability(providerID string, model ModelInfo, refreshedAt string) *ModelCapabilities {
	if len(model.InputModalities) == 0 && len(model.ImageTransports) == 0 && len(model.ImageFormats) == 0 {
		return nil
	}
	raw := model.RawJSON
	return &ModelCapabilities{
		ProviderID:       providerID,
		ModelID:          model.ID,
		InputModalities:  model.InputModalities,
		OutputModalities: firstNonEmpty(model.OutputModalities, []string{"text"}),
		ImageTransports:  model.ImageTransports,
		ImageFormats:     model.ImageFormats,
		CapabilitySource: string(CapabilitySourceProviderMetadata),
		Confidence:       0.8,
		LastRefreshedAt:  refreshedAt,
		RawProviderJSON:  raw,
	}
}

func presetCapability(providerID, presetID, modelID string) ModelCapabilities {
	provider := strings.ToLower(strings.TrimSpace(firstNonEmptyString(presetID, providerID)))
	model := strings.ToLower(strings.TrimSpace(modelID))
	capability := ModelCapabilities{
		ProviderID:       providerID,
		ModelID:          modelID,
		InputModalities:  []string{"text"},
		OutputModalities: []string{"text"},
		ImageTransports:  []string{},
		ImageFormats:     []string{},
		CapabilitySource: string(CapabilitySourceUnknown),
		Confidence:       0.2,
	}
	switch {
	case provider == "moonshot" && (strings.HasPrefix(model, "kimi-k2.6") || strings.Contains(model, "vision")):
		capability.InputModalities = []string{"text", "image", "video"}
		capability.ImageTransports = []string{"data_url", "kimi_ms"}
		capability.ImageFormats = []string{"image/png", "image/jpeg"}
		capability.MaxRequestBytes = 104857600
		capability.CapabilitySource = string(CapabilitySourceProviderDocsPreset)
		capability.Confidence = 0.7
	case provider == "anthropic" && strings.HasPrefix(model, "claude-"):
		capability.InputModalities = []string{"text", "image"}
		capability.ImageTransports = []string{"base64", "remote_url", "anthropic_file_id"}
		capability.ImageFormats = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}
		capability.CapabilitySource = string(CapabilitySourceProviderDocsPreset)
		capability.Confidence = 0.7
	case provider == "openai" && (strings.HasPrefix(model, "gpt-4.1") || strings.HasPrefix(model, "gpt-4o") || strings.HasPrefix(model, "gpt-5")):
		capability.InputModalities = []string{"text", "image"}
		capability.ImageTransports = []string{"data_url", "remote_url", "openai_file_id"}
		capability.ImageFormats = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}
		capability.CapabilitySource = string(CapabilitySourceProviderDocsPreset)
		capability.Confidence = 0.7
	}
	return capability
}

func mergeCapability(base, incoming ModelCapabilities) ModelCapabilities {
	if sourceRank(CapabilitySource(incoming.CapabilitySource)) < sourceRank(CapabilitySource(base.CapabilitySource)) {
		return base
	}
	if len(incoming.InputModalities) > 0 {
		base.InputModalities = incoming.InputModalities
	}
	if len(incoming.OutputModalities) > 0 {
		base.OutputModalities = incoming.OutputModalities
	}
	if len(incoming.ImageTransports) > 0 {
		base.ImageTransports = incoming.ImageTransports
	}
	if len(incoming.ImageFormats) > 0 {
		base.ImageFormats = incoming.ImageFormats
	}
	if incoming.MaxImagesPerRequest > 0 {
		base.MaxImagesPerRequest = incoming.MaxImagesPerRequest
	}
	if incoming.MaxImageBytes > 0 {
		base.MaxImageBytes = incoming.MaxImageBytes
	}
	if incoming.MaxRequestBytes > 0 {
		base.MaxRequestBytes = incoming.MaxRequestBytes
	}
	if incoming.MaxLongEdgePixels > 0 {
		base.MaxLongEdgePixels = incoming.MaxLongEdgePixels
	}
	base.SupportsVisionTools = incoming.SupportsVisionTools
	base.SupportsVisionStreaming = incoming.SupportsVisionStreaming
	base.SupportsVisionJSONMode = incoming.SupportsVisionJSONMode
	base.CapabilitySource = incoming.CapabilitySource
	base.Confidence = incoming.Confidence
	base.LastVerifiedAt = incoming.LastVerifiedAt
	base.RawProviderJSON = incoming.RawProviderJSON
	return base
}

func normalizeCapability(cap ModelCapabilities) ModelCapabilities {
	if len(cap.InputModalities) == 0 {
		cap.InputModalities = []string{"text"}
	}
	if len(cap.OutputModalities) == 0 {
		cap.OutputModalities = []string{"text"}
	}
	if cap.CapabilitySource == "" {
		cap.CapabilitySource = string(CapabilitySourceUnknown)
	}
	if cap.Confidence < 0 {
		cap.Confidence = 0
	}
	if cap.Confidence > 1 {
		cap.Confidence = 1
	}
	return cap
}

func sourceRank(source CapabilitySource) int {
	switch source {
	case CapabilitySourceManualOverride:
		return 5
	case CapabilitySourceProbePassed, CapabilitySourceProbeFailed:
		return 4
	case CapabilitySourceProviderMetadata:
		return 3
	case CapabilitySourceProviderDocsPreset:
		return 2
	case CapabilitySourceMerged:
		return 1
	default:
		return 0
	}
}

func firstNonEmpty[T any](values []T, fallback []T) []T {
	if len(values) > 0 {
		return values
	}
	return fallback
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func rawJSONOf(v any) string {
	payload, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(payload)
}
