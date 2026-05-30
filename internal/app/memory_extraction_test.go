package app

import (
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
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
		SemanticDedup: config.MemorySemanticDedupConfig{
			Enabled:          true,
			Shadow:           true,
			CandidateLimit:   9,
			ThresholdProfile: "default_v0",
		},
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
	if hostCfg.SemanticDedup != (memorycore.SemanticDedupOptions{
		Enabled:          true,
		Shadow:           true,
		CandidateLimit:   9,
		ThresholdProfile: "default_v0",
	}) {
		t.Fatalf("semantic dedup = %#v", hostCfg.SemanticDedup)
	}
}

func TestMemoryExtractionBackgroundConfigsMapDurations(t *testing.T) {
	cfg := config.MemoryExtractionConfig{
		Async: config.MemoryExtractionAsyncConfig{
			QueueClaimTTLSeconds:  120,
			RetryBaseDelaySeconds: 5,
			RetryMaxDelaySeconds:  60,
		},
		Idle: config.MemoryExtractionIdleConfig{
			IdleAfterSeconds:         30,
			SweepIntervalSeconds:     7,
			MinEpisodeCount:          1,
			MaxSegmentsPerSweep:      3,
			IncludeActiveSegments:    true,
			IncludeFinalizedSegments: true,
		},
		MirrorSync: config.MemoryExtractionMirrorConfig{
			AfterApply:                true,
			Limit:                     25,
			FailExtractionOnSyncError: true,
		},
	}

	workerCfg := memoryExtractionWorkerConfig(cfg, 2)
	if workerCfg.ClaimLimit != 1 || workerCfg.ClaimTTL != 120*time.Second {
		t.Fatalf("worker claim config = %#v", workerCfg)
	}
	if workerCfg.RetryBaseDelay != 5*time.Second || workerCfg.RetryMaxDelay != 60*time.Second {
		t.Fatalf("worker retry config = %#v", workerCfg)
	}
	if !workerCfg.MirrorSyncAfterApply || workerCfg.MirrorSyncLimit != 25 || !workerCfg.FailExtractionOnSyncError {
		t.Fatalf("worker mirror config = %#v", workerCfg)
	}

	idleCfg := memoryExtractionIdleSchedulerConfig(cfg)
	if idleCfg.IdleAfter != 30*time.Second || idleCfg.SweepInterval != 7*time.Second {
		t.Fatalf("idle durations = %#v", idleCfg)
	}
	if idleCfg.MinEpisodeCount != 1 || idleCfg.MaxSegmentsPerSweep != 3 || !idleCfg.IncludeActiveSegments || !idleCfg.IncludeFinalizedSegments {
		t.Fatalf("idle scan config = %#v", idleCfg)
	}
}
