package configcenter

import (
	"testing"

	memconfig "github.com/longyisang/emoagent-memorycore/config"
	"github.com/longyisang/emoagent/internal/config"
)

func TestBuildIssuesCoversMemoryCoreFeatureDependencies(t *testing.T) {
	seed := config.DefaultConfig()
	seed.Memory.Enabled = true
	seed.Memory.Retrieval.UseMirror = true
	memoryCore := &MemoryCoreEffective{
		Core: MemoryCoreCoreEffective{EnableFTS: true},
		Retrieval: MemoryCoreRetrievalEffective{
			UseFTS:    true,
			UseMirror: true,
		},
		Pipelines: memconfig.PipelinesConfig{
			QueryAnalysis: memconfig.QueryAnalysisPipeline{
				LLMPipelineConfig: memconfig.LLMPipelineConfig{Enabled: true},
				Mode:              "sidecar",
			},
		},
		Sidecar: MemoryCoreSidecarEffective{Enabled: false},
		Mirror: MemoryCoreMirrorEffective{
			Enabled:        false,
			RebuildOnStart: true,
		},
		Retention: memconfig.RetentionConfig{
			Jobs: memconfig.RetentionJobsConfig{MirrorCompaction: true},
		},
		SemanticOps: memconfig.SemanticOpsConfig{
			Forget: memconfig.SemanticForgetConfig{ExecuteEnabled: true},
		},
		ForgettingPrivacy: memconfig.ForgettingPrivacyConfig{
			Cleanup: memconfig.ForgettingCleanupConfig{
				DeleteTriviumNodes:   true,
				CleanAgentAffectRefs: true,
			},
		},
		AgentAffect: memconfig.AgentAffectConfig{
			Enabled:        true,
			StorageEnabled: false,
			Retrieval:      memconfig.AgentAffectRetrievalConfig{WeightCap: 0.05},
		},
	}

	issues := BuildIssues(seed, nil, memoryCore)

	for _, path := range []string{
		"memory.retrieval.use_mirror",
		"memory.mirror.rebuild_on_start",
		"memory.query_analysis.sidecar",
		"memory.retention.jobs.mirror_compaction",
		"memory.forgetting.execute_enabled",
		"memory.forgetting.cleanup.delete_trivium_nodes",
		"memory.forgetting.cleanup.clean_agent_affect_refs",
		"memory.agent_affect.storage_enabled",
		"memory.agent_affect.neutral_fallback",
		"memory.agent_affect.retrieval.weight_cap",
	} {
		requireConfigIssue(t, issues, path)
	}
}

func TestBuildIssuesRejectsNegativeMemoryPipelineMaxTokens(t *testing.T) {
	seed := config.DefaultConfig()
	seed.Memory.ProviderBindings.Extraction.MaxTokens = -1

	issues := BuildIssues(seed, nil, nil)

	requireConfigIssue(t, issues, "memory.provider_bindings.extraction.max_tokens")
}
