package app

import (
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/memoryhost"
)

func memoryExtractionHostConfig(cfg config.MemoryExtractionConfig) memoryhost.ExtractionHostPolicy {
	return memoryhost.ExtractionHostPolicy{
		Enabled:                  cfg.Enabled,
		TriggerOnFinalizeSegment: cfg.TriggerOnFinalizeSegment,
		TriggerOnManualPin:       cfg.TriggerOnManualPin,
		TriggerOnManualForget:    cfg.TriggerOnManualForget,
		SessionEndMode:           memoryExtractionMode(firstMemoryExtractionMode(cfg.SessionEndMode, cfg.Mode)),
		ManualPinMode:            memoryExtractionMode(firstMemoryExtractionMode(cfg.ManualPinMode, "apply")),
		ManualForgetMode:         memoryExtractionMode(firstMemoryExtractionMode(cfg.ManualForgetMode, "dry-run")),
		Limit:                    cfg.Limit,
		Timezone:                 cfg.Timezone,
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

func firstMemoryExtractionMode(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
