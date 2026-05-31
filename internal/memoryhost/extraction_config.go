package memoryhost

import (
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

type ExtractionHostPolicy struct {
	Enabled                  bool
	AsyncEnabled             bool
	TriggerOnFinalizeSegment bool
	TriggerOnManualPin       bool
	SessionEndMode           memorycore.ExtractionRunMode
	ManualPinMode            memorycore.ExtractionRunMode
	Timezone                 string
	Limit                    int
	MaxAttempts              int
	AllowInference           bool
	AllowSensitiveExtraction bool
	MaxFacts                 int
	MaxLinks                 int
	SemanticDedup            memorycore.SemanticDedupOptions
}

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
	SemanticDedup            memorycore.SemanticDedupOptions
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

func (p ExtractionHostPolicy) normalized() ExtractionHostPolicy {
	if p.SessionEndMode == "" {
		p.SessionEndMode = memorycore.ExtractionRunModeApply
	}
	if p.SessionEndMode == "dry_run" {
		p.SessionEndMode = memorycore.ExtractionRunModeDryRun
	}
	if p.ManualPinMode == "" {
		p.ManualPinMode = memorycore.ExtractionRunModeApply
	}
	if p.ManualPinMode == "dry_run" {
		p.ManualPinMode = memorycore.ExtractionRunModeDryRun
	}
	if strings.TrimSpace(p.Timezone) == "" {
		p.Timezone = "Asia/Shanghai"
	}
	if p.Limit == 0 {
		p.Limit = 50
	}
	if p.MaxAttempts == 0 {
		p.MaxAttempts = 3
	}
	if p.MaxFacts == 0 {
		p.MaxFacts = 12
	}
	if p.MaxLinks == 0 {
		p.MaxLinks = 20
	}
	return p
}

func extractionHostPolicyFromConfig(c ExtractionConfig) ExtractionHostPolicy {
	c = c.normalized()
	return ExtractionHostPolicy{
		Enabled:                  c.Enabled,
		AsyncEnabled:             true,
		TriggerOnFinalizeSegment: c.TriggerOnFinalizeSegment,
		TriggerOnManualPin:       true,
		SessionEndMode:           c.Mode,
		ManualPinMode:            memorycore.ExtractionRunModeApply,
		Timezone:                 c.Timezone,
		Limit:                    c.Limit,
		MaxAttempts:              3,
		AllowInference:           c.AllowInference,
		AllowSensitiveExtraction: c.AllowSensitiveExtraction,
		MaxFacts:                 c.MaxFacts,
		MaxLinks:                 c.MaxLinks,
		SemanticDedup:            c.SemanticDedup,
	}.normalized()
}

func extractionHostPolicyFromOptions(opts memorycore.Options) ExtractionHostPolicy {
	mode := opts.Extraction.Defaults.Mode
	if mode == "" {
		mode = memorycore.ExtractionRunModeApply
	}
	return ExtractionHostPolicy{
		Enabled:                  opts.Extraction.Enabled,
		AsyncEnabled:             true,
		TriggerOnFinalizeSegment: true,
		TriggerOnManualPin:       true,
		SessionEndMode:           mode,
		ManualPinMode:            memorycore.ExtractionRunModeApply,
		Timezone:                 opts.Extraction.Defaults.Timezone,
		Limit:                    50,
		MaxAttempts:              3,
		AllowInference:           opts.Extraction.Defaults.AllowInference,
		AllowSensitiveExtraction: opts.Extraction.Defaults.AllowSensitiveExtraction,
		MaxFacts:                 opts.Extraction.Defaults.MaxFacts,
		MaxLinks:                 opts.Extraction.Defaults.MaxLinks,
		SemanticDedup:            opts.SemanticOps.Dedup,
	}.normalized()
}
