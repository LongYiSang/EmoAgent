package configcenter

import (
	"fmt"
	"strings"

	"github.com/longyisang/emoagent/internal/config"
)

type ConfigIssue struct {
	Path            string   `json:"path"`
	Severity        string   `json:"severity"`
	Message         string   `json:"message"`
	DisabledReasons []string `json:"disabled_reasons,omitempty"`
	AutoFix         *AutoFix `json:"auto_fix,omitempty"`
}

type AutoFix struct {
	Value any `json:"value"`
}

func BuildIssues(seed *config.Config, providers []ProviderEffective, memoryCore *MemoryCoreEffective) []ConfigIssue {
	if seed == nil {
		seed = config.DefaultConfig()
	}
	issues := make([]ConfigIssue, 0)
	for _, provider := range providers {
		if provider.Enabled && provider.Env.APIKeyEnv != "" && !provider.Env.Present {
			issues = append(issues, ConfigIssue{
				Path:     fmt.Sprintf("providers.%s.api_key_env", provider.ID),
				Severity: "error",
				Message:  fmt.Sprintf("provider %q requires env %s", provider.ID, provider.Env.APIKeyEnv),
			})
		}
	}
	issues = append(issues, buildAgentAffectIssues(seed.AgentAffect)...)

	if !seed.Memory.Enabled {
		if seed.Memory.Retrieval.Enabled {
			issues = append(issues, disabledIssue(
				"memory.retrieval.enabled",
				"memory retrieval requires memory.enabled",
				"memory.enabled is false",
				false,
			))
		}
		if seed.Memory.Extraction.Enabled {
			issues = append(issues, disabledIssue(
				"memory.extraction.enabled",
				"memory extraction requires memory.enabled",
				"memory.enabled is false",
				false,
			))
		}
	}

	if seed.Memory.Enabled && strings.TrimSpace(seed.Memory.ConfigPath) == "" {
		issues = append(issues, ConfigIssue{
			Path:     "memory.config_path",
			Severity: "error",
			Message:  "memory.config_path is required when memory.enabled is true",
		})
	}
	if seed.Memory.Retrieval.Enabled {
		if seed.Memory.Retrieval.FinalMemoryCount <= 0 {
			issues = append(issues, ConfigIssue{Path: "memory.retrieval.final_memory_count", Severity: "error", Message: "final_memory_count must be > 0"})
		}
		if seed.Memory.Retrieval.ContextBudgetTokens <= 0 {
			issues = append(issues, ConfigIssue{Path: "memory.retrieval.context_budget_tokens", Severity: "error", Message: "context_budget_tokens must be > 0"})
		}
	}
	if seed.Memory.Retrieval.UseMirror {
		if !seed.Memory.Enabled {
			issues = append(issues, disabledIssue(
				"memory.retrieval.use_mirror",
				"use_mirror requires memory.enabled",
				"memory.enabled is false",
				false,
			))
		}
		if memoryCore != nil {
			if !memoryCore.Mirror.Enabled {
				issues = append(issues, disabledIssue(
					"memory.retrieval.use_mirror",
					"use_mirror requires memory.mirror.enabled",
					"memory.mirror.enabled is false",
					false,
				))
			}
			if !memoryCore.Sidecar.Enabled {
				issues = append(issues, disabledIssue(
					"memory.retrieval.use_mirror",
					"use_mirror requires memory.sidecar.enabled",
					"memory.sidecar.enabled is false",
					false,
				))
			}
		}
	}
	if seed.Memory.Sidecar.Enabled && seed.Memory.Sidecar.Managed && !seed.Memory.Enabled {
		issues = append(issues, disabledIssue(
			"memory.sidecar.managed",
			"managed sidecar requires memory.enabled",
			"memory.enabled is false",
			false,
		))
	}

	extraction := seed.Memory.Extraction
	if extraction.Async.WorkerEnabled && !extraction.Async.Enabled {
		issues = append(issues, disabledIssue(
			"memory.extraction.async.worker_enabled",
			"async worker requires memory.extraction.async.enabled",
			"memory.extraction.async.enabled is false",
			false,
		))
	}
	if extraction.Idle.Enabled && (!extraction.Enabled || !extraction.Async.WorkerEnabled) {
		issues = append(issues, disabledIssue(
			"memory.extraction.idle.enabled",
			"idle extraction requires extraction.enabled and async.worker_enabled",
			"memory.extraction.enabled or async.worker_enabled is false",
			false,
		))
	}
	if extraction.Manual.Enabled && !extraction.Enabled {
		issues = append(issues, disabledIssue(
			"memory.extraction.manual.enabled",
			"manual extraction requires memory.extraction.enabled",
			"memory.extraction.enabled is false",
			false,
		))
	}
	if extraction.SemanticDedup.Enforce {
		if !extraction.SemanticDedup.Enabled {
			issues = append(issues, disabledIssue(
				"memory.extraction.semantic_dedup.enforce",
				"semantic_dedup.enforce requires semantic_dedup.enabled",
				"memory.extraction.semantic_dedup.enabled is false",
				false,
			))
		}
		if extraction.SemanticDedup.Shadow {
			issues = append(issues, disabledIssue(
				"memory.extraction.semantic_dedup.enforce",
				"semantic_dedup.enforce cannot be active while shadow is true",
				"memory.extraction.semantic_dedup.shadow is true",
				false,
			))
		}
	}

	if memoryCore != nil {
		if memoryCore.Retrieval.UseFTS && !memoryCore.Core.EnableFTS {
			issues = append(issues, disabledIssue(
				"memory.retrieval.use_fts",
				"use_fts requires memorycore.core.enable_fts",
				"memorycore.core.enable_fts is false",
				false,
			))
		}
		if memoryCore.Mirror.RebuildOnStart && !memoryCore.Mirror.Enabled {
			issues = append(issues, disabledIssue(
				"memory.mirror.rebuild_on_start",
				"mirror rebuild_on_start requires memory.mirror.enabled",
				"memory.mirror.enabled is false",
				false,
			))
		}
		if memoryCore.Pipelines.QueryAnalysis.Enabled && memoryCore.Pipelines.QueryAnalysis.Mode == "sidecar" && !memoryCore.Sidecar.Enabled {
			issues = append(issues, disabledIssue(
				"memory.query_analysis.sidecar",
				"query_analysis sidecar mode requires memory.sidecar.enabled",
				"memory.sidecar.enabled is false",
				"rule_only",
			))
		}
		if memoryCore.Retention.Jobs.MirrorCompaction && !memoryCore.Mirror.Enabled {
			issues = append(issues, disabledIssue(
				"memory.retention.jobs.mirror_compaction",
				"mirror compaction requires memory.mirror.enabled",
				"memory.mirror.enabled is false",
				false,
			))
		}
		if memoryCore.SemanticOps.Forget.ExecuteEnabled && !memoryCore.SemanticOps.Forget.PreviewEnabled {
			issues = append(issues, disabledIssue(
				"memory.forgetting.execute_enabled",
				"forget execution requires forget preview",
				"memory.semantic_ops.forget.preview_enabled is false",
				false,
			))
		}
		if memoryCore.ForgettingPrivacy.Cleanup.DeleteTriviumNodes && !memoryCore.Mirror.Enabled {
			issues = append(issues, disabledIssue(
				"memory.forgetting.cleanup.delete_trivium_nodes",
				"delete_trivium_nodes requires memory.mirror.enabled",
				"memory.mirror.enabled is false",
				false,
			))
		}
		if memoryCore.ForgettingPrivacy.Cleanup.CleanAgentAffectRefs && !memoryCore.AgentAffect.StorageEnabled {
			issues = append(issues, disabledIssue(
				"memory.forgetting.cleanup.clean_agent_affect_refs",
				"clean_agent_affect_refs requires agent_affect.storage_enabled",
				"memory.agent_affect.storage_enabled is false",
				false,
			))
		}
		if memoryCore.AgentAffect.Enabled && !memoryCore.AgentAffect.StorageEnabled {
			issues = append(issues, ConfigIssue{
				Path:     "memory.agent_affect.storage_enabled",
				Severity: "error",
				Message:  "agent_affect.enabled requires agent_affect.storage_enabled",
			})
		}
		if memoryCore.AgentAffect.Enabled && !memoryCore.AgentAffect.NeutralFallback {
			issues = append(issues, ConfigIssue{
				Path:     "memory.agent_affect.neutral_fallback",
				Severity: "error",
				Message:  "agent_affect.enabled requires neutral fallback",
			})
		}
		if memoryCore.AgentAffect.Retrieval.WeightCap > 0.03 {
			issues = append(issues, ConfigIssue{
				Path:     "memory.agent_affect.retrieval.weight_cap",
				Severity: "error",
				Message:  "agent affect retrieval weight_cap must be <= 0.03",
			})
		}
	}
	return issues
}

func buildAgentAffectIssues(cfg config.AgentAffectConfig) []ConfigIssue {
	issues := make([]ConfigIssue, 0)
	if cfg.Enabled && !cfg.StorageEnabled {
		issues = append(issues, ConfigIssue{
			Path:     "agent_affect.storage_enabled",
			Severity: "error",
			Message:  "agent_affect.enabled requires agent_affect.storage_enabled",
		})
	}
	switch strings.TrimSpace(cfg.Evaluator.Mode) {
	case "", "llm", "disabled":
	default:
		issues = append(issues, ConfigIssue{
			Path:     "agent_affect.evaluator.mode",
			Severity: "error",
			Message:  "agent_affect.evaluator.mode must be llm or disabled",
		})
	}
	if cfg.Evaluator.StoreHiddenThinking {
		issues = append(issues, ConfigIssue{
			Path:     "agent_affect.evaluator.store_hidden_thinking",
			Severity: "error",
			Message:  "agent_affect must not store hidden thinking",
		})
	}
	switch strings.TrimSpace(cfg.Context.Mode) {
	case "", "none", "raw_window", "summary_window", "mixed":
	default:
		issues = append(issues, ConfigIssue{
			Path:     "agent_affect.context.mode",
			Severity: "error",
			Message:  "agent_affect.context.mode must be none, raw_window, summary_window, or mixed",
		})
	}
	if cfg.Limits.PluginDeltaMultiplier < 0 {
		issues = append(issues, ConfigIssue{Path: "agent_affect.limits.plugin_delta_multiplier", Severity: "error", Message: "plugin_delta_multiplier must be >= 0"})
	}
	for path, value := range map[string]float64{
		"agent_affect.limits.per_request_delta.valence":     cfg.Limits.PerRequestDelta.Valence,
		"agent_affect.limits.per_request_delta.arousal":     cfg.Limits.PerRequestDelta.Arousal,
		"agent_affect.limits.per_request_delta.dominance":   cfg.Limits.PerRequestDelta.Dominance,
		"agent_affect.limits.per_request_delta.energy":      cfg.Limits.PerRequestDelta.Energy,
		"agent_affect.limits.per_request_delta.warmth":      cfg.Limits.PerRequestDelta.Warmth,
		"agent_affect.limits.per_request_delta.concern":     cfg.Limits.PerRequestDelta.Concern,
		"agent_affect.limits.per_request_delta.curiosity":   cfg.Limits.PerRequestDelta.Curiosity,
		"agent_affect.limits.per_request_delta.playfulness": cfg.Limits.PerRequestDelta.Playfulness,
		"agent_affect.limits.per_request_delta.attachment":  cfg.Limits.PerRequestDelta.Attachment,
		"agent_affect.limits.per_request_delta.frustration": cfg.Limits.PerRequestDelta.Frustration,
		"agent_affect.limits.per_request_delta.uncertainty": cfg.Limits.PerRequestDelta.Uncertainty,
	} {
		if value < 0 {
			issues = append(issues, ConfigIssue{Path: path, Severity: "error", Message: "per_request_delta values must be >= 0"})
		}
	}
	if cfg.Limits.Absolute.AttachmentMax < 0 {
		issues = append(issues, ConfigIssue{Path: "agent_affect.limits.absolute.attachment_max", Severity: "error", Message: "attachment_max must be >= 0"})
	}
	if cfg.Limits.Absolute.FrustrationMax < 0 {
		issues = append(issues, ConfigIssue{Path: "agent_affect.limits.absolute.frustration_max", Severity: "error", Message: "frustration_max must be >= 0"})
	}
	return issues
}

func disabledIssue(path string, message string, reason string, value any) ConfigIssue {
	return ConfigIssue{
		Path:            path,
		Severity:        "warning",
		Message:         message,
		DisabledReasons: []string{reason},
		AutoFix:         &AutoFix{Value: value},
	}
}

func dedupeIssues(issues []ConfigIssue) []ConfigIssue {
	out := make([]ConfigIssue, 0, len(issues))
	seen := map[string]struct{}{}
	for _, issue := range issues {
		key := issue.Path + "\x00" + issue.Message
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, issue)
	}
	return out
}
