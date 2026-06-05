package configcenter

import (
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/storage"
)

func TestApplyRuntimeSettingsSupportsMemoryDetailNamespaces(t *testing.T) {
	seed := config.DefaultConfig()

	effective, issues := ApplyRuntimeSettings(seed, []storage.RuntimeSetting{
		{Namespace: "memory.retention", Key: "config", ValueJSON: `{"thresholds":{"deep_archive_after_days":77}}`},
		{Namespace: "memory.forgetting_privacy", Key: "config", ValueJSON: `{"cleanup":{"verify_after_delete":false}}`},
		{Namespace: "memory.agent_affect", Key: "config", ValueJSON: `{"retrieval":{"weight_cap":0.02}}`},
		{Namespace: "memory.natural_memory", Key: "config", ValueJSON: `{"enabled":true,"local_time":"04:10","manual":{"allow_force":false}}`},
	})
	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
	if effective.Memory.Retention == nil || effective.Memory.Retention.Thresholds.DeepArchiveAfterDays != 77 {
		t.Fatalf("retention = %#v", effective.Memory.Retention)
	}
	if effective.Memory.ForgettingPrivacy == nil || effective.Memory.ForgettingPrivacy.Cleanup.VerifyAfterDelete {
		t.Fatalf("forgetting_privacy = %#v", effective.Memory.ForgettingPrivacy)
	}
	if effective.Memory.AgentAffect == nil || effective.Memory.AgentAffect.Retrieval.WeightCap != 0.02 {
		t.Fatalf("agent_affect = %#v", effective.Memory.AgentAffect)
	}
	if !effective.Memory.NaturalMemory.Enabled || effective.Memory.NaturalMemory.LocalTime != "04:10" || effective.Memory.NaturalMemory.Manual.AllowForce {
		t.Fatalf("natural_memory = %#v", effective.Memory.NaturalMemory)
	}
}
