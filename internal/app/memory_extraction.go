package app

import (
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/memoryhost"
)

func memoryExtractionHostConfig(cfg config.MemoryExtractionConfig) memoryhost.ExtractionConfig {
	return memoryhost.ExtractionConfig{
		Enabled:                  cfg.Enabled,
		Mode:                     memoryExtractionMode(cfg.Mode),
		TriggerOnFinalizeSegment: cfg.TriggerOnFinalizeSegment,
		Limit:                    cfg.Limit,
		Timezone:                 cfg.Timezone,
		AllowInference:           cfg.AllowInference,
		AllowSensitiveExtraction: cfg.AllowSensitiveExtraction,
		MaxFacts:                 cfg.MaxFacts,
		MaxLinks:                 cfg.MaxLinks,
		RawLog: memoryhost.ExtractionRawLogConfig{
			Enabled:   cfg.RawLog.Enabled,
			Directory: cfg.RawLog.Directory,
		},
		Provider: memoryhost.ExtractionProviderConfig{
			Kind:        cfg.Provider.Kind,
			ID:          cfg.Provider.ID,
			BaseURL:     cfg.Provider.BaseURL,
			APIKeyEnv:   cfg.Provider.APIKeyEnv,
			Model:       cfg.Provider.Model,
			Timeout:     time.Duration(cfg.Provider.TimeoutSeconds) * time.Second,
			MaxTokens:   cfg.Provider.MaxTokens,
			Temperature: cfg.Provider.Temperature,
			Thinking: memoryhost.ExtractionThinkingConfig{
				Type: cfg.Provider.Thinking.Type,
			},
		},
		RepairEnabled: cfg.RepairEnabled,
		AuditEnabled:  cfg.AuditEnabled,
	}
}

func memoryExtractionMode(mode string) memorycore.ExtractionRunMode {
	switch mode {
	case "validate":
		return memorycore.ExtractionRunModeValidate
	case "apply":
		return memorycore.ExtractionRunModeApply
	case "dry-run", "dry_run":
		return memorycore.ExtractionRunModeDryRun
	default:
		return memorycore.ExtractionRunMode(mode)
	}
}
