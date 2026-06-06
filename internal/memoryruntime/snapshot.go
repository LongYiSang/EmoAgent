package memoryruntime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/configcenter"
	sidecarruntime "github.com/longyisang/emoagent/internal/sidecar"
)

const (
	SchemaVersion       = "emoagent.memory_runtime_snapshot.v1"
	DefaultSnapshotPath = "./data/runtime/memory_runtime_snapshot.json"
)

type Input struct {
	Memory        config.MemoryConfig
	MemoryCore    *configcenter.MemoryCoreEffective
	SidecarStatus *sidecarruntime.Status
}

type Snapshot struct {
	SchemaVersion     string                    `json:"schema_version"`
	MemoryEnabled     bool                      `json:"memory_enabled"`
	MemoryCoreDB      string                    `json:"memorycore_db"`
	SQLiteFTSEnabled  bool                      `json:"sqlite_fts_enabled"`
	MirrorEnabled     bool                      `json:"mirror_enabled"`
	Sidecar           SidecarSnapshot           `json:"sidecar"`
	SidecarResilience SidecarResilienceSnapshot `json:"sidecar_resilience"`
	Retrieval         RetrievalSnapshot         `json:"retrieval"`
	QueryAnalysis     QueryAnalysisSnapshot     `json:"query_analysis"`
	Extraction        ExtractionSnapshot        `json:"extraction"`
	NaturalMemory     NaturalMemorySnapshot     `json:"natural_memory"`
}

type SidecarSnapshot struct {
	Mode    string `json:"mode"`
	Status  string `json:"status"`
	URL     string `json:"url,omitempty"`
	Adapter string `json:"adapter,omitempty"`
}

type SidecarResilienceSnapshot struct {
	TotalTimeoutMS      int                             `json:"total_timeout_ms"`
	MirrorTimeoutMS     int                             `json:"mirror_timeout_ms"`
	ActivationTimeoutMS int                             `json:"activation_timeout_ms"`
	RerankTimeoutMS     int                             `json:"rerank_timeout_ms"`
	CircuitBreaker      SidecarBreakerSnapshot          `json:"circuit_breaker"`
	ActivationBudget    SidecarActivationBudgetSnapshot `json:"activation_budget"`
}

type SidecarBreakerSnapshot struct {
	Enabled          bool `json:"enabled"`
	Window           int  `json:"window"`
	FailureThreshold int  `json:"failure_threshold"`
	OpenMS           int  `json:"open_ms"`
}

type SidecarActivationBudgetSnapshot struct {
	MaxEdgesScannedPerRequest int `json:"max_edges_scanned_per_request"`
	MaxNeighborsPerNode       int `json:"max_neighbors_per_node"`
	MaxWallMS                 int `json:"max_wall_ms"`
}

type RetrievalSnapshot struct {
	ChatPromptPolicy        PolicySnapshot `json:"chat_prompt_policy"`
	MemoryCoreDefaultPolicy PolicySnapshot `json:"memorycore_default_policy"`
}

type PolicySnapshot struct {
	Source                string `json:"source"`
	SensitivityPermission string `json:"sensitivity_permission"`
	AllowHistorical       bool   `json:"allow_historical"`
	AllowDeepArchive      bool   `json:"allow_deep_archive"`
	FinalMemoryCount      int    `json:"final_memory_count"`
	ContextBudgetTokens   int    `json:"context_budget_tokens"`
	UseFTS                bool   `json:"use_fts"`
	UseMirror             bool   `json:"use_mirror"`
}

type QueryAnalysisSnapshot struct {
	Enabled        bool                             `json:"enabled"`
	Mode           string                           `json:"mode"`
	RuntimeMode    string                           `json:"runtime_mode"`
	FallbackMode   string                           `json:"fallback_mode"`
	ProviderID     string                           `json:"provider_id,omitempty"`
	Model          string                           `json:"model,omitempty"`
	ScorerVersion  string                           `json:"scorer_version"`
	RouterVersion  string                           `json:"router_version"`
	SemanticBudget QueryAnalysisBudgetSnapshot      `json:"semantic_budget"`
	Diagnostics    QueryAnalysisDiagnosticsSnapshot `json:"diagnostics"`
}

type QueryAnalysisBudgetSnapshot struct {
	MaxSemanticCallsPerSession         int `json:"max_semantic_calls_per_session"`
	MaxSemanticCallsPerSessionWindowMS int `json:"max_semantic_calls_per_session_window_ms"`
	MaxSemanticCallsPer1000Queries     int `json:"max_semantic_calls_per_1000_queries"`
	MaxSemanticLatencyMS               int `json:"max_semantic_latency_ms"`
}

type QueryAnalysisDiagnosticsSnapshot struct {
	IncludeScoreBreakdown bool    `json:"include_score_breakdown"`
	IncludeReasonCodes    bool    `json:"include_reason_codes"`
	SampleRate            float64 `json:"sample_rate"`
}

type ExtractionSnapshot struct {
	Enabled       bool `json:"enabled"`
	AsyncEnabled  bool `json:"async_enabled"`
	WorkerEnabled bool `json:"worker_enabled"`
	ManualPin     bool `json:"manual_pin"`
	SessionEnd    bool `json:"session_end"`
}

type NaturalMemorySnapshot struct {
	Enabled          bool `json:"enabled"`
	SchedulerEnabled bool `json:"scheduler_enabled"`
	ManualTrigger    bool `json:"manual_trigger"`
}

func BuildSnapshot(input Input) Snapshot {
	memory := input.Memory
	snapshot := Snapshot{
		SchemaVersion: SchemaVersion,
		MemoryEnabled: memory.Enabled,
		Sidecar:       sidecarSnapshot(memory, input.SidecarStatus),
		Retrieval: RetrievalSnapshot{
			ChatPromptPolicy: policySnapshot("emoagent.chat_prompt_policy", ChatPromptRetrievalPolicy(memory.Retrieval)),
		},
		Extraction: ExtractionSnapshot{
			Enabled:       memory.Extraction.Enabled,
			AsyncEnabled:  memory.Extraction.Async.Enabled,
			WorkerEnabled: memory.Extraction.Async.WorkerEnabled,
			ManualPin:     memory.Extraction.TriggerOnManualPin,
			SessionEnd:    memory.Extraction.TriggerOnFinalizeSegment,
		},
		NaturalMemory: NaturalMemorySnapshot{
			Enabled:          memory.NaturalMemory.Enabled,
			SchedulerEnabled: memory.NaturalMemory.SchedulerEnabled,
			ManualTrigger:    memory.NaturalMemory.Manual.Enabled,
		},
	}
	if input.MemoryCore != nil {
		core := input.MemoryCore
		snapshot.MemoryCoreDB = core.Core.DBPath
		snapshot.SQLiteFTSEnabled = core.Core.EnableFTS
		snapshot.MirrorEnabled = core.Mirror.Enabled
		snapshot.SidecarResilience = SidecarResilienceSnapshot{
			TotalTimeoutMS:      core.SidecarResilience.TotalTimeoutMS,
			MirrorTimeoutMS:     core.SidecarResilience.MirrorTimeoutMS,
			ActivationTimeoutMS: core.SidecarResilience.ActivationTimeoutMS,
			RerankTimeoutMS:     core.SidecarResilience.RerankTimeoutMS,
			CircuitBreaker: SidecarBreakerSnapshot{
				Enabled:          core.SidecarResilience.CircuitBreaker.Enabled,
				Window:           core.SidecarResilience.CircuitBreaker.Window,
				FailureThreshold: core.SidecarResilience.CircuitBreaker.FailureThreshold,
				OpenMS:           core.SidecarResilience.CircuitBreaker.OpenMS,
			},
			ActivationBudget: SidecarActivationBudgetSnapshot{
				MaxEdgesScannedPerRequest: core.SidecarResilience.ActivationBudget.MaxEdgesScannedPerRequest,
				MaxNeighborsPerNode:       core.SidecarResilience.ActivationBudget.MaxNeighborsPerNode,
				MaxWallMS:                 core.SidecarResilience.ActivationBudget.MaxWallMS,
			},
		}
		snapshot.Retrieval.MemoryCoreDefaultPolicy = PolicySnapshot{
			Source:                "memorycore.effective_config",
			SensitivityPermission: core.Retrieval.SensitivityPermission,
			AllowHistorical:       core.Retrieval.AllowHistorical,
			AllowDeepArchive:      core.Retrieval.AllowDeepArchive,
			FinalMemoryCount:      core.Retrieval.FinalMemoryCount,
			ContextBudgetTokens:   core.Retrieval.ContextBudgetTokens,
			UseFTS:                core.Retrieval.UseFTS,
			UseMirror:             core.Retrieval.UseMirror,
		}
		qa := core.Pipelines.QueryAnalysis
		snapshot.QueryAnalysis = QueryAnalysisSnapshot{
			Enabled:       qa.Enabled,
			Mode:          qa.Mode,
			RuntimeMode:   qa.RuntimeMode,
			FallbackMode:  qa.FallbackMode,
			ProviderID:    qa.ProviderID,
			Model:         qa.Model,
			ScorerVersion: qa.ScorerVersion,
			RouterVersion: qa.RouterVersion,
			SemanticBudget: QueryAnalysisBudgetSnapshot{
				MaxSemanticCallsPerSession:         qa.Budget.MaxSemanticCallsPerSession,
				MaxSemanticCallsPerSessionWindowMS: qa.Budget.MaxSemanticCallsPerSessionWindowMS,
				MaxSemanticCallsPer1000Queries:     qa.Budget.MaxSemanticCallsPer1000Queries,
				MaxSemanticLatencyMS:               qa.Budget.MaxSemanticLatencyMS,
			},
			Diagnostics: QueryAnalysisDiagnosticsSnapshot{
				IncludeScoreBreakdown: qa.Diagnostics.IncludeScoreBreakdown,
				IncludeReasonCodes:    qa.Diagnostics.IncludeReasonCodes,
				SampleRate:            qa.Diagnostics.SampleRate,
			},
		}
	}
	if !memory.Enabled {
		snapshot.Sidecar = SidecarSnapshot{Mode: "disabled", Status: "disabled"}
	}
	return snapshot
}

func WriteSnapshot(path string, snapshot Snapshot) error {
	path = strings.TrimSpace(path)
	if path == "" {
		path = DefaultSnapshotPath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}

func ChatPromptRetrievalPolicy(cfg config.MemoryRetrievalConfig) memorycore.RetrievalPolicy {
	return memorycore.RetrievalPolicy{
		SensitivityPermission: memorycore.SensitivityNormal,
		FinalMemoryCount:      cfg.FinalMemoryCount,
		ContextBudgetTokens:   cfg.ContextBudgetTokens,
		UseFTS:                cfg.UseFTS,
		UseMirror:             cfg.UseMirror,
	}
}

func policySnapshot(source string, policy memorycore.RetrievalPolicy) PolicySnapshot {
	return PolicySnapshot{
		Source:                source,
		SensitivityPermission: policy.SensitivityPermission,
		AllowHistorical:       policy.AllowHistorical,
		AllowDeepArchive:      policy.AllowDeepArchive,
		FinalMemoryCount:      policy.FinalMemoryCount,
		ContextBudgetTokens:   policy.ContextBudgetTokens,
		UseFTS:                policy.UseFTS,
		UseMirror:             policy.UseMirror,
	}
}

func sidecarSnapshot(memory config.MemoryConfig, status *sidecarruntime.Status) SidecarSnapshot {
	if !memory.Sidecar.Enabled {
		return SidecarSnapshot{Mode: "disabled", Status: "disabled"}
	}
	mode := "external"
	if memory.Sidecar.Managed {
		mode = "managed"
	}
	out := SidecarSnapshot{
		Mode:    mode,
		Status:  "unknown",
		URL:     strings.TrimSpace(memory.Sidecar.URL),
		Adapter: strings.TrimSpace(memory.Sidecar.Adapter),
	}
	if status != nil {
		out.Status = string(status.State)
		if strings.TrimSpace(status.URL) != "" {
			out.URL = strings.TrimSpace(status.URL)
		}
		if strings.TrimSpace(status.Adapter) != "" {
			out.Adapter = strings.TrimSpace(status.Adapter)
		}
	}
	return out
}
