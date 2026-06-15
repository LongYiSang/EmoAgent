package configcenter

import (
	"fmt"
	"os"
	"strings"

	"github.com/longyisang/emoagent/internal/config"
)

type WebSearchRuntimeStatus struct {
	Registered     bool              `json:"registered"`
	EffectiveMode  string            `json:"effective_mode"`
	PipelineActive bool              `json:"pipeline_active"`
	Provider       string            `json:"provider"`
	ActiveProvider string            `json:"active_provider"`
	TavilyEnv      ProviderEnvStatus `json:"tavily_env"`
	RerankEnv      ProviderEnvStatus `json:"rerank_env"`
	LastError      string            `json:"last_error,omitempty"`
	Warnings       []string          `json:"warnings,omitempty"`
}

func BuildWebSearchRuntimeStatus(cfg config.WebSearchConfig, registered bool, lastError string, envLookup func(string) (string, bool)) WebSearchRuntimeStatus {
	if envLookup == nil {
		envLookup = os.LookupEnv
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = "tavily"
	}
	tavilyEnv := envStatus(cfg.APIKeyEnv, envLookup)
	rerankEnv := envStatus(cfg.Pipeline.Rerank.APIKeyEnv, envLookup)
	lastError = strings.TrimSpace(lastError)
	if cfg.Enabled && !tavilyEnv.Present && strings.TrimSpace(cfg.APIKeyEnv) != "" && lastError == "" {
		lastError = fmt.Sprintf("%s not set", cfg.APIKeyEnv)
	}
	registered = registered && cfg.Enabled && lastError == ""
	pipelineActive := registered && provider == "pipeline" && cfg.Pipeline.Enabled

	status := WebSearchRuntimeStatus{
		Registered:     registered,
		EffectiveMode:  "disabled",
		PipelineActive: pipelineActive,
		Provider:       provider,
		TavilyEnv:      tavilyEnv,
		RerankEnv:      rerankEnv,
	}
	if !cfg.Enabled {
		return status
	}
	if lastError != "" {
		status.LastError = lastError
	}
	if provider == "tavily" && cfg.Pipeline.Enabled {
		status.Warnings = append(status.Warnings, "pipeline config is saved but inactive because running mode is pure simple search")
	}
	if provider == "pipeline" && !cfg.Pipeline.Enabled {
		status.Warnings = append(status.Warnings, "pipeline mode is selected but pipeline.enabled is false; simple search is active")
	}
	if pipelineActive && cfg.Pipeline.Rerank.Enabled && strings.EqualFold(cfg.Pipeline.Rerank.Provider, "siliconflow") && !rerankEnv.Present {
		status.Warnings = append(status.Warnings, "siliconflow rerank env is not set; fallback may be used")
	}
	if !status.Registered {
		status.EffectiveMode = "unavailable"
		return status
	}
	if pipelineActive {
		status.EffectiveMode = "pipeline"
		status.ActiveProvider = "pipeline"
	} else {
		status.EffectiveMode = "tavily"
		status.ActiveProvider = "tavily"
	}
	return status
}

func envStatus(name string, envLookup func(string) (string, bool)) ProviderEnvStatus {
	name = strings.TrimSpace(name)
	if name == "" {
		return ProviderEnvStatus{}
	}
	value, ok := envLookup(name)
	return ProviderEnvStatus{APIKeyEnv: name, Present: ok && strings.TrimSpace(value) != ""}
}
