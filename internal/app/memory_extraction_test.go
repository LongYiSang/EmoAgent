package app

import (
	"testing"

	"github.com/longyisang/emoagent/internal/config"
)

func TestMemoryExtractionHostConfigMapsTriggerPolicy(t *testing.T) {
	hostCfg := memoryExtractionHostConfig(config.MemoryExtractionConfig{
		Enabled:                  true,
		TriggerOnFinalizeSegment: true,
		TriggerOnManualPin:       true,
		Mode:                     "apply",
		Limit:                    25,
		Timezone:                 "Asia/Shanghai",
	})

	if !hostCfg.Enabled || !hostCfg.TriggerOnFinalizeSegment || !hostCfg.TriggerOnManualPin {
		t.Fatalf("trigger policy = %#v", hostCfg)
	}
	if hostCfg.SessionEndMode != "apply" || hostCfg.ManualPinMode != "apply" {
		t.Fatalf("modes = %#v", hostCfg)
	}
	if hostCfg.Limit != 25 || hostCfg.Timezone != "Asia/Shanghai" {
		t.Fatalf("limit/timezone = %#v", hostCfg)
	}
}
