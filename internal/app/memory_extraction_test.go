package app

import (
	"testing"

	"github.com/longyisang/emoagent/internal/config"
)

func TestMemoryExtractionHostConfigMapsProviderThinking(t *testing.T) {
	hostCfg := memoryExtractionHostConfig(config.MemoryExtractionConfig{
		Enabled: true,
		RawLog: config.MemoryExtractionRawLogConfig{
			Enabled:   true,
			Directory: "./debug/memory_extraction_raw",
		},
		Provider: config.MemoryExtractionProviderConfig{
			Kind:  "openai-compatible",
			Model: "memory-extractor",
			Thinking: config.MemoryExtractionThinkingConfig{
				Type: "disabled",
			},
		},
	})

	if hostCfg.Provider.Thinking.Type != "disabled" {
		t.Fatalf("thinking.type = %q, want disabled", hostCfg.Provider.Thinking.Type)
	}
	if !hostCfg.RawLog.Enabled || hostCfg.RawLog.Directory != "./debug/memory_extraction_raw" {
		t.Fatalf("raw_log = %#v", hostCfg.RawLog)
	}
}
