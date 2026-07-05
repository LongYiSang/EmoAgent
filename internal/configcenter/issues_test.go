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

func TestBuildIssuesRejectsInvalidPythonToolchain(t *testing.T) {
	seed := config.DefaultConfig()
	seed.PythonToolchain.MinimumUVVersion = "latest"

	issues := BuildIssues(seed, nil, nil)

	requireConfigIssue(t, issues, "python_toolchain")
}

func TestBuildIssuesWarnsDeprecatedAgentAffectPreviousEvaluations(t *testing.T) {
	seed := config.DefaultConfig()
	seed.AgentAffect.Context.IncludePreviousEvaluations = true

	issues := BuildIssues(seed, nil, nil)

	requireConfigIssue(t, issues, "agent_affect.context.include_previous_evaluations")
}

func TestBuildIssuesWarnsPlatformsEnabledWithoutAdapters(t *testing.T) {
	seed := config.DefaultConfig()
	seed.Platforms.Enabled = true
	seed.Platforms.Adapters = map[string]config.PlatformAdapterConfig{}

	issues := BuildIssues(seed, nil, nil)

	requireConfigIssueSeverity(t, issues, "platforms.adapters", "warning")
}

func TestBuildIssuesReportsInvalidOneBotAdapterConfig(t *testing.T) {
	seed := config.DefaultConfig()
	seed.Platforms.Enabled = true
	seed.Platforms.Adapters = map[string]config.PlatformAdapterConfig{
		"qq-main": {
			Enabled: true,
			Kind:    "onebot_v11",
			ConfigJSON: map[string]any{
				"implementation": "unknown",
				"transport": map[string]any{
					"mode": "ws_reverse",
				},
			},
		},
	}

	issues := BuildIssues(seed, nil, nil)

	requireConfigIssueSeverity(t, issues, "platforms.adapters.qq-main.config", "error")
}

func TestBuildIssuesWarnsIgnoredOneBotTransportFields(t *testing.T) {
	seed := config.DefaultConfig()
	seed.Platforms.Enabled = true
	seed.Platforms.Adapters = map[string]config.PlatformAdapterConfig{
		"reverse-url": {
			Enabled: true,
			Kind:    "onebot_v11",
			ConfigJSON: map[string]any{
				"transport": map[string]any{
					"mode": "ws_reverse",
					"url":  "ws://127.0.0.1:3001",
				},
			},
		},
		"client-reverse-path": {
			Enabled: true,
			Kind:    "onebot_v11",
			ConfigJSON: map[string]any{
				"transport": map[string]any{
					"mode":         "ws_client",
					"url":          "ws://127.0.0.1:3001",
					"reverse_path": "/api/platforms/onebot/v11/qq-main/ws",
				},
			},
		},
	}

	issues := BuildIssues(seed, nil, nil)

	requireConfigIssueSeverity(t, issues, "platforms.adapters.reverse-url.config.transport.url", "warning")
	requireConfigIssueSeverity(t, issues, "platforms.adapters.client-reverse-path.config.transport.reverse_path", "warning")
}

func requireConfigIssueSeverity(t *testing.T, issues []ConfigIssue, path string, severity string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Path == path {
			if issue.Severity != severity {
				t.Fatalf("issue %s severity = %q, want %q", path, issue.Severity, severity)
			}
			if issue.Message == "" {
				t.Fatalf("issue %s message is empty", path)
			}
			return
		}
	}
	t.Fatalf("issue %s not found in %#v", path, issues)
}
