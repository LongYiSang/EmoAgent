package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/memoryhost"
	"github.com/longyisang/emoagent/internal/web"
)

const (
	userAddressMigrationPredicate     = "prefers_name"
	userAddressMigrationMergeStrategy = "append_legacy_after_existing"
)

type userAddressMigrationPreview struct {
	agent         *config.AgentConfig
	core          memoryhost.CoreClient
	forgetRequest memorycore.ForgetPreviewRequest
	forgetPreview *memorycore.ForgetPreviewResult
	response      web.UserAddressMigrationPreviewResponse
}

func (a *App) PreviewUserAddressMigration(ctx context.Context, id string) (web.UserAddressMigrationPreviewResponse, error) {
	preview, err := a.previewUserAddressMigration(ctx, id)
	if err != nil {
		return web.UserAddressMigrationPreviewResponse{}, err
	}
	return preview.response, nil
}

func (a *App) ExecuteUserAddressMigration(ctx context.Context, id string, req web.UserAddressMigrationExecuteRequest) (web.UserAddressMigrationExecuteResponse, error) {
	strategy := strings.TrimSpace(req.MergeStrategy)
	if strategy == "" {
		strategy = userAddressMigrationMergeStrategy
	}
	if strategy != userAddressMigrationMergeStrategy {
		return web.UserAddressMigrationExecuteResponse{}, fmt.Errorf("merge_strategy must be %s", userAddressMigrationMergeStrategy)
	}

	preview, err := a.previewUserAddressMigration(ctx, id)
	if err != nil {
		return web.UserAddressMigrationExecuteResponse{}, err
	}
	resp := web.UserAddressMigrationExecuteResponse{
		UserAddressMigrationPreviewResponse: preview.response,
		DryRun:                              req.DryRun,
		HideLegacy:                          req.HideLegacy,
	}
	if req.DryRun {
		return resp, nil
	}

	services, err := a.services()
	if err != nil {
		return resp, err
	}
	next := *preview.agent
	next.UserAddress = preview.response.Merged
	if err := services.AgentRuntime.UpdateAgentConfig(id, next); err != nil {
		return resp, err
	}
	resp.Updated = true

	if req.HideLegacy && len(preview.response.Legacy) > 0 {
		targetsByID := map[string]memorycore.ForgetResolvedTarget{}
		for _, target := range preview.forgetPreview.Targets {
			targetsByID[target.NodeID] = target
		}
		confirmed := make([]memorycore.ExactNodeRef, 0, len(preview.response.Legacy))
		for _, fact := range preview.response.Legacy {
			target, ok := targetsByID[fact.FactID]
			if !ok {
				continue
			}
			confirmed = append(confirmed, memorycore.ExactNodeRef{NodeType: target.NodeType, NodeID: target.NodeID})
		}
		if len(confirmed) == 0 {
			return resp, nil
		}
		forgetResp, err := preview.core.ExecuteForget(ctx, memorycore.ForgetExecuteRequest{
			PersonaID:        preview.forgetPreview.PersonaID,
			Actor:            memorycore.ForgetActorAdmin,
			ReasonCode:       memorycore.ForgetReasonAdminPolicy,
			Level:            memorycore.ForgetLevelSoft,
			PreviewRequest:   preview.forgetRequest,
			PreviewHash:      preview.forgetPreview.PreviewHash,
			ConfirmedTargets: confirmed,
			Confirmed:        true,
		})
		if err != nil {
			resp.HideErrors = append(resp.HideErrors, err.Error())
			return resp, nil
		}
		if forgetResp != nil {
			resp.HiddenCount = forgetResp.Executed
		}
	}
	return resp, nil
}

func (a *App) previewUserAddressMigration(ctx context.Context, id string) (*userAddressMigrationPreview, error) {
	services, err := a.services()
	if err != nil {
		return nil, err
	}
	agent, err := services.AgentRuntime.GetAgentConfig(id)
	if err != nil {
		return nil, err
	}
	core, err := userAddressMigrationCore(services.Memory)
	if err != nil {
		return nil, err
	}
	personaID := strings.TrimSpace(agent.PersonaKey)
	forgetReq := memorycore.ForgetPreviewRequest{
		PersonaID:           personaID,
		ScopeMode:           memorycore.ForgetScopePredicate,
		Predicate:           userAddressMigrationPredicate,
		RequestedLevel:      memorycore.ForgetLevelSoft,
		RequireConfirmation: true,
		Limit:               100,
	}
	forgetPreview, err := core.PreviewForget(ctx, forgetReq)
	if err != nil {
		return nil, err
	}

	existing, err := config.NormalizeAgentUserAddressConfig(agent.UserAddress)
	if err != nil {
		return nil, err
	}
	legacy, warnings := userAddressLegacyFacts(forgetPreview)
	merged, mergeWarnings := mergeLegacyUserAddress(existing, legacy)
	warnings = append(warnings, mergeWarnings...)

	return &userAddressMigrationPreview{
		agent:         agent,
		core:          core,
		forgetRequest: forgetReq,
		forgetPreview: forgetPreview,
		response: web.UserAddressMigrationPreviewResponse{
			AgentID:   agent.ID,
			PersonaID: personaID,
			Existing:  existing,
			Legacy:    legacy,
			Merged:    merged,
			Warnings:  warnings,
		},
	}, nil
}

func userAddressMigrationCore(memory *MemoryService) (memoryhost.CoreClient, error) {
	if memory == nil || memory.Host() == nil || memory.Host().Core == nil {
		return nil, fmt.Errorf("memorycore is not configured for user address migration")
	}
	return memory.Host().Core, nil
}

func userAddressLegacyFacts(preview *memorycore.ForgetPreviewResult) ([]web.UserAddressLegacyFact, []string) {
	if preview == nil {
		return nil, nil
	}
	legacy := make([]web.UserAddressLegacyFact, 0, len(preview.Targets))
	var warnings []string
	for _, target := range preview.Targets {
		value := strings.TrimSpace(target.ObjectLiteral)
		if value == "" {
			warnings = append(warnings, "skipped_empty_legacy_value")
			continue
		}
		if _, err := config.NormalizeAgentUserAddressConfig(config.AgentUserAddressConfig{Preferred: []string{value}, Usage: "natural"}); err != nil {
			warnings = append(warnings, "skipped_invalid_legacy_value:"+target.NodeID)
			continue
		}
		legacy = append(legacy, web.UserAddressLegacyFact{
			FactID:  target.NodeID,
			Value:   value,
			Summary: strings.TrimSpace(firstNonEmptyString(target.SafeSummary, target.Summary)),
		})
	}
	return legacy, warnings
}

func mergeLegacyUserAddress(existing config.AgentUserAddressConfig, legacy []web.UserAddressLegacyFact) (config.AgentUserAddressConfig, []string) {
	usage := strings.TrimSpace(existing.Usage)
	if usage == "" {
		usage = "natural"
	}
	merged := config.AgentUserAddressConfig{Usage: usage}
	seen := map[string]struct{}{}
	var warnings []string
	for _, value := range existing.Preferred {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		merged.Preferred = append(merged.Preferred, name)
	}
	for _, fact := range legacy {
		name := strings.TrimSpace(fact.Value)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		if len(merged.Preferred) >= 8 {
			warnings = append(warnings, "truncated_to_8")
			break
		}
		seen[name] = struct{}{}
		merged.Preferred = append(merged.Preferred, name)
	}
	normalized, err := config.NormalizeAgentUserAddressConfig(merged)
	if err != nil {
		warnings = append(warnings, err.Error())
		return config.AgentUserAddressConfig{Preferred: append([]string(nil), existing.Preferred...), Usage: usage}, warnings
	}
	return normalized, warnings
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
