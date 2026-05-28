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
		TriggerOnManualForget:    false,
		Mode:                     "apply",
		ManualForgetMode:         "dry_run",
		Limit:                    25,
		Timezone:                 "Asia/Shanghai",
	})

	if !hostCfg.Enabled || !hostCfg.TriggerOnFinalizeSegment || !hostCfg.TriggerOnManualPin || hostCfg.TriggerOnManualForget {
		t.Fatalf("trigger policy = %#v", hostCfg)
	}
	if hostCfg.SessionEndMode != "apply" || hostCfg.ManualPinMode != "apply" || hostCfg.ManualForgetMode != "dry-run" {
		t.Fatalf("modes = %#v", hostCfg)
	}
	if hostCfg.Limit != 25 || hostCfg.Timezone != "Asia/Shanghai" {
		t.Fatalf("limit/timezone = %#v", hostCfg)
	}
}
