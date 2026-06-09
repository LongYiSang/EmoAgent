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

func TestApplyRuntimeSettingsSupportsRootAgentAffect(t *testing.T) {
	seed := config.DefaultConfig()

	effective, issues := ApplyRuntimeSettings(seed, []storage.RuntimeSetting{
		{Namespace: "agent_affect", Key: "config", ValueJSON: `{"enabled":true,"evaluator":{"mode":"disabled"},"context":{"store_raw_inputs":false},"limits":{"per_request_delta":{"valence":0.05}}}`},
	})
	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
	if !effective.AgentAffect.Enabled {
		t.Fatalf("agent_affect.enabled = false, want true")
	}
	if effective.AgentAffect.Evaluator.Mode != "disabled" {
		t.Fatalf("agent_affect evaluator mode = %q, want disabled", effective.AgentAffect.Evaluator.Mode)
	}
	if effective.AgentAffect.Context.StoreRawInputs {
		t.Fatalf("agent_affect context store_raw_inputs = true, want false")
	}
	if effective.AgentAffect.Limits.PerRequestDelta.Valence != 0.05 {
		t.Fatalf("agent_affect valence delta = %v, want 0.05", effective.AgentAffect.Limits.PerRequestDelta.Valence)
	}
	if effective.Memory.AgentAffect != nil {
		t.Fatalf("root agent_affect runtime setting mutated memory.agent_affect: %#v", effective.Memory.AgentAffect)
	}
}
