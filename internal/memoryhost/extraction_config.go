package memoryhost

import (
	"fmt"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

type ExtractionConfig struct {
	Enabled                  bool
	Mode                     memorycore.ExtractionRunMode
	TriggerOnFinalizeSegment bool
	Limit                    int
	Timezone                 string
	AllowInference           bool
	AllowSensitiveExtraction bool
	MaxFacts                 int
	MaxLinks                 int
	RawLog                   ExtractionRawLogConfig
	Provider                 ExtractionProviderConfig
	RepairEnabled            bool
	AuditEnabled             bool
}

type ExtractionRawLogConfig struct {
	Enabled   bool
	Directory string
}

type ExtractionProviderConfig struct {
	Kind        string
	ID          string
	BaseURL     string
	APIKeyEnv   string
	Model       string
	Timeout     time.Duration
	MaxTokens   int
	Temperature float64
	Thinking    ExtractionThinkingConfig
}

type ExtractionThinkingConfig struct {
	Type string
}

func (c ExtractionConfig) normalized() ExtractionConfig {
	if c.Mode == "" {
		c.Mode = memorycore.ExtractionRunModeDryRun
	}
	if c.Mode == "dry_run" {
		c.Mode = memorycore.ExtractionRunModeDryRun
	}
	if c.Limit == 0 {
		c.Limit = 50
	}
	if strings.TrimSpace(c.Timezone) == "" {
		c.Timezone = "Asia/Shanghai"
	}
	if c.MaxFacts == 0 {
		c.MaxFacts = 12
	}
	if c.MaxLinks == 0 {
		c.MaxLinks = 20
	}
	if strings.TrimSpace(c.Provider.Kind) == "" {
		c.Provider.Kind = "openai-compatible"
	}
	if strings.TrimSpace(c.Provider.ID) == "" {
		c.Provider.ID = "memory_extractor"
	}
	if strings.TrimSpace(c.Provider.APIKeyEnv) == "" {
		c.Provider.APIKeyEnv = "MEMORYCORE_LLM_API_KEY"
	}
	if c.Provider.Timeout == 0 {
		c.Provider.Timeout = 60 * time.Second
	}
	if c.Provider.MaxTokens == 0 {
		c.Provider.MaxTokens = 4096
	}
	return c
}

func (c ExtractionConfig) validateForRunner(llm memorycore.ExtractionLLM) error {
	if !c.Enabled {
		return nil
	}
	switch c.Mode {
	case memorycore.ExtractionRunModeValidate, memorycore.ExtractionRunModeDryRun, memorycore.ExtractionRunModeApply:
	default:
		return fmt.Errorf("mode must be validate, dry_run, or apply")
	}
	if c.Limit <= 0 {
		return fmt.Errorf("limit must be > 0")
	}
	if strings.TrimSpace(c.Timezone) == "" {
		return fmt.Errorf("timezone is required")
	}
	if c.MaxFacts <= 0 {
		return fmt.Errorf("max_facts must be > 0")
	}
	if c.MaxLinks <= 0 {
		return fmt.Errorf("max_links must be > 0")
	}
	if c.RawLog.Enabled && strings.TrimSpace(c.RawLog.Directory) == "" {
		return fmt.Errorf("raw_log.directory is required when raw_log.enabled is true")
	}
	if llm != nil {
		return nil
	}
	switch strings.TrimSpace(c.Provider.Kind) {
	case "openai-compatible", "openai_compatible":
	default:
		return fmt.Errorf("provider.kind must be openai-compatible")
	}
	if strings.TrimSpace(c.Provider.BaseURL) == "" {
		return fmt.Errorf("provider.base_url is required")
	}
	if strings.TrimSpace(c.Provider.Model) == "" {
		return fmt.Errorf("provider.model is required")
	}
	switch strings.TrimSpace(c.Provider.Thinking.Type) {
	case "", "enabled", "disabled":
	default:
		return fmt.Errorf("provider.thinking.type must be enabled or disabled")
	}
	return nil
}

func (c ExtractionConfig) auditMode() string {
	if c.AuditEnabled {
		return memorycore.ExtractionAuditOn
	}
	return memorycore.ExtractionAuditOff
}
