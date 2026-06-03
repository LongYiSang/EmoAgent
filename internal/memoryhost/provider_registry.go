package memoryhost

import (
	"fmt"
	"strings"

	memconfig "github.com/longyisang/emoagent-memorycore/config"
	"github.com/longyisang/emoagent/internal/config"
)

const memoryCoreSupportedLLMProtocol = "openai_compatible"

// BuildProviderRegistry adapts EmoAgent Provider Center entries into MemoryCore's
// host-injected registry. MemoryCore remains standalone and does not read EmoAgent DB.
func BuildProviderRegistry(providers []config.LLMProvider) memconfig.ProviderRegistry {
	registry := memconfig.ProviderRegistry{
		LLM: make([]memconfig.ProviderMapping, 0, len(providers)),
	}
	for _, provider := range providers {
		protocol := normalizeProviderProtocol(provider.Protocol)
		registry.LLM = append(registry.LLM, memconfig.ProviderMapping{
			ID:        strings.TrimSpace(provider.ID),
			Provider:  memoryCoreProviderName(protocol),
			Protocol:  protocol,
			BaseURL:   strings.TrimSpace(provider.BaseURL),
			APIKeyEnv: strings.TrimSpace(provider.APIKeyEnv),
			Enabled:   provider.Enabled,
		})
	}
	return registry
}

func ValidateLLMProviderBindings(cfg memconfig.Config) error {
	check := func(path string, enabled bool, providerID string) error {
		if !enabled {
			return nil
		}
		providerID = strings.TrimSpace(providerID)
		if providerID == "" {
			return nil
		}
		provider := cfg.ProviderByID(providerID)
		if provider == nil {
			return nil
		}
		protocol := normalizeProviderProtocol(provider.Protocol)
		if protocol != memoryCoreSupportedLLMProtocol {
			return fmt.Errorf("%s.provider_id %q uses unsupported protocol %q; MemoryCore LLM pipelines currently support only openai_compatible providers from EmoAgent Provider Center", path, providerID, provider.Protocol)
		}
		return nil
	}

	if err := check("pipelines.prefilter", cfg.Pipelines.Prefilter.Enabled, cfg.Pipelines.Prefilter.ProviderID); err != nil {
		return err
	}
	if err := check("pipelines.extraction", cfg.Pipelines.Extraction.Enabled, cfg.Pipelines.Extraction.ProviderID); err != nil {
		return err
	}
	if err := check("pipelines.extraction_repair", cfg.Pipelines.ExtractionRepair.Enabled, cfg.Pipelines.ExtractionRepair.ProviderID); err != nil {
		return err
	}
	queryAnalysisUsesProvider := cfg.Pipelines.QueryAnalysis.Enabled || (cfg.Pipelines.QueryAnalysis.Mode != "" && cfg.Pipelines.QueryAnalysis.Mode != "rule_only")
	if err := check("pipelines.query_analysis", queryAnalysisUsesProvider, cfg.Pipelines.QueryAnalysis.ProviderID); err != nil {
		return err
	}
	if err := check("pipelines.narrative_insight", cfg.Pipelines.NarrativeInsight.Enabled, cfg.Pipelines.NarrativeInsight.ProviderID); err != nil {
		return err
	}
	curationUsesProvider := cfg.SemanticOps.Curation.Enabled && cfg.SemanticOps.Curation.LLM.ProviderKind != "mock" && cfg.SemanticOps.Curation.LLM.ProviderKind != "disabled"
	if err := check("semantic_ops.curation.llm", curationUsesProvider, cfg.SemanticOps.Curation.LLM.ProviderID); err != nil {
		return err
	}
	return nil
}

func normalizeProviderProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "openai-compatible":
		return memoryCoreSupportedLLMProtocol
	default:
		return strings.ToLower(strings.TrimSpace(protocol))
	}
}

func memoryCoreProviderName(protocol string) string {
	if protocol == memoryCoreSupportedLLMProtocol {
		return "openai-compatible"
	}
	return protocol
}
