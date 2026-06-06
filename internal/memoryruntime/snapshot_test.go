package memoryruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	memconfig "github.com/longyisang/emoagent-memorycore/config"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/configcenter"
	sidecarruntime "github.com/longyisang/emoagent/internal/sidecar"
)

func TestBuildSnapshotMatchesGolden(t *testing.T) {
	memoryCfg := config.DefaultConfig().Memory
	memoryCfg.Enabled = true
	memoryCfg.Retrieval.InjectPrompt = true
	memoryCfg.Retrieval.UseFTS = true
	memoryCfg.Retrieval.UseMirror = true
	memoryCfg.Retrieval.FinalMemoryCount = 8
	memoryCfg.Retrieval.ContextBudgetTokens = 1500
	memoryCfg.Sidecar.Enabled = true
	memoryCfg.Sidecar.Managed = true
	memoryCfg.Sidecar.URL = "http://127.0.0.1:8765"
	memoryCfg.Sidecar.Adapter = "trivium"
	memoryCfg.Extraction.Enabled = true
	memoryCfg.Extraction.TriggerOnManualPin = true
	memoryCfg.Extraction.TriggerOnFinalizeSegment = true
	memoryCfg.Extraction.Async.Enabled = true
	memoryCfg.Extraction.Async.WorkerEnabled = true
	memoryCfg.NaturalMemory.Enabled = true
	memoryCfg.NaturalMemory.SchedulerEnabled = true
	memoryCfg.NaturalMemory.Manual.Enabled = true

	snapshot := BuildSnapshot(Input{
		Memory: memoryCfg,
		MemoryCore: &configcenter.MemoryCoreEffective{
			Enabled: true,
			Core: configcenter.MemoryCoreCoreEffective{
				DBPath:    "D:/Dev/Project/Agent/EmoAgent/data/memory.db",
				EnableFTS: true,
			},
			Retrieval: configcenter.MemoryCoreRetrievalEffective{
				UseFTS:                true,
				UseMirror:             true,
				AllowHistorical:       false,
				AllowDeepArchive:      false,
				SensitivityPermission: "normal",
				FinalMemoryCount:      8,
				ContextBudgetTokens:   1500,
			},
			Pipelines: memconfig.PipelinesConfig{
				QueryAnalysis: memconfig.QueryAnalysisPipeline{
					LLMPipelineConfig: memconfig.LLMPipelineConfig{Enabled: true, ProviderID: "deepseek", Model: "deepseek-v4-flash"},
					Mode:              "sidecar",
					RuntimeMode:       "adaptive_safe",
					FallbackMode:      "rule_only",
					ScorerVersion:     "query_analysis_scorer_v1",
					RouterVersion:     "semantic_router_v1",
					Budget: memconfig.QueryAnalysisBudgetConfig{
						MaxSemanticCallsPerSession:         8,
						MaxSemanticCallsPerSessionWindowMS: 1800000,
						MaxSemanticCallsPer1000Queries:     250,
						MaxSemanticLatencyMS:               1500,
					},
					Diagnostics: memconfig.QueryAnalysisDiagnosticsConfig{
						IncludeScoreBreakdown: true,
						IncludeReasonCodes:    true,
						SampleRate:            1,
					},
				},
			},
			Sidecar: configcenter.MemoryCoreSidecarEffective{
				Enabled: true,
				URL:     "http://127.0.0.1:8765",
				Adapter: "trivium",
			},
			SidecarResilience: configcenter.MemoryCoreSidecarResilienceEffective{
				TotalTimeoutMS:      45000,
				MirrorTimeoutMS:     18000,
				ActivationTimeoutMS: 15000,
				RerankTimeoutMS:     18000,
				CircuitBreaker: configcenter.MemoryCoreSidecarBreakerEffective{
					Enabled:          true,
					Window:           20,
					FailureThreshold: 3,
					OpenMS:           60000,
				},
				ActivationBudget: configcenter.MemoryCoreSidecarActivationBudgetEffective{
					MaxEdgesScannedPerRequest: 10000,
					MaxNeighborsPerNode:       100,
					MaxWallMS:                 120,
				},
			},
			Mirror: configcenter.MemoryCoreMirrorEffective{Enabled: true},
		},
		SidecarStatus: &sidecarruntime.Status{
			State:   sidecarruntime.StateHealthy,
			Managed: true,
			URL:     "http://127.0.0.1:8765",
			Adapter: "trivium",
			PID:     12345,
		},
	})

	got := mustMarshalSnapshot(t, snapshot)
	want, err := os.ReadFile(filepath.Join("testdata", "memory_runtime_snapshot.golden.json"))
	if err != nil {
		t.Fatalf("ReadFile golden: %v", err)
	}
	if got != string(want) {
		t.Fatalf("snapshot mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
	if gotHasSecret := strings.Contains(got, "secret-value") || strings.Contains(got, "api_key"); gotHasSecret {
		t.Fatalf("snapshot leaked secret material: %s", got)
	}
}

func TestBuildDisabledSnapshotRecordsDisabledStatus(t *testing.T) {
	memoryCfg := config.DefaultConfig().Memory
	memoryCfg.Enabled = false
	memoryCfg.Retrieval.UseMirror = true
	memoryCfg.Sidecar.Enabled = true
	memoryCfg.Sidecar.Managed = true

	snapshot := BuildSnapshot(Input{Memory: memoryCfg})

	if snapshot.MemoryEnabled {
		t.Fatal("MemoryEnabled = true, want false")
	}
	if snapshot.MemoryCoreDB != "" {
		t.Fatalf("MemoryCoreDB = %q, want empty", snapshot.MemoryCoreDB)
	}
	if snapshot.Sidecar.Status != "disabled" || snapshot.Sidecar.Mode != "disabled" {
		t.Fatalf("sidecar = %#v, want disabled mode/status", snapshot.Sidecar)
	}
	if snapshot.Sidecar.URL != "" {
		t.Fatalf("sidecar URL = %q, want empty for disabled snapshot", snapshot.Sidecar.URL)
	}
}

func TestChatPromptRetrievalPolicyLocksArchiveDefaults(t *testing.T) {
	cfg := config.MemoryRetrievalConfig{
		UseFTS:              true,
		UseMirror:           true,
		FinalMemoryCount:    6,
		ContextBudgetTokens: 2048,
	}

	policy := ChatPromptRetrievalPolicy(cfg)

	if policy.SensitivityPermission != memorycore.SensitivityNormal {
		t.Fatalf("SensitivityPermission = %q, want normal", policy.SensitivityPermission)
	}
	if policy.AllowHistorical || policy.AllowDeepArchive {
		t.Fatalf("archive policy = historical:%v deep:%v, want both false", policy.AllowHistorical, policy.AllowDeepArchive)
	}
	if !policy.UseFTS || !policy.UseMirror || policy.FinalMemoryCount != 6 || policy.ContextBudgetTokens != 2048 {
		t.Fatalf("policy = %#v", policy)
	}
}

func mustMarshalSnapshot(t *testing.T, snapshot Snapshot) string {
	t.Helper()
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent snapshot: %v", err)
	}
	return string(raw) + "\n"
}
