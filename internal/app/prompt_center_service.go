package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/longyisang/emoagent/internal/promptcenter"
	"github.com/longyisang/emoagent/internal/storage"
)

type PromptCenterService struct {
	infra        *Infra
	agentRuntime *AgentRuntimeService
}

func (s *PromptCenterService) ListComponents(ctx context.Context, agentID string) (promptcenter.PromptComponentsResponse, error) {
	agentID, err := s.requireAgent(ctx, agentID)
	if err != nil {
		return promptcenter.PromptComponentsResponse{}, err
	}
	catalog, err := promptcenter.DefaultCatalog()
	if err != nil {
		return promptcenter.PromptComponentsResponse{}, err
	}
	components := catalog.List()
	details := make([]promptcenter.PromptComponentDetail, 0, len(components))
	for _, component := range components {
		detail, err := s.componentDetail(ctx, catalog, component.ID, agentID)
		if err != nil {
			return promptcenter.PromptComponentsResponse{}, err
		}
		details = append(details, detail)
	}
	return promptcenter.PromptComponentsResponse{AgentID: agentID, Components: details}, nil
}

func (s *PromptCenterService) GetComponent(ctx context.Context, id, agentID string) (promptcenter.PromptComponentDetail, error) {
	agentID, err := s.requireAgent(ctx, agentID)
	if err != nil {
		return promptcenter.PromptComponentDetail{}, err
	}
	catalog, err := promptcenter.DefaultCatalog()
	if err != nil {
		return promptcenter.PromptComponentDetail{}, err
	}
	return s.componentDetail(ctx, catalog, id, agentID)
}

func (s *PromptCenterService) UpsertOverride(ctx context.Context, req promptcenter.UpsertOverrideRequest) error {
	catalog, err := promptcenter.DefaultCatalog()
	if err != nil {
		return err
	}
	if err := promptcenter.ValidateUpsertOverride(ctx, catalog, s.agentExists, req); err != nil {
		return err
	}
	component := catalog.MustGet(req.ComponentID)
	req.DefaultHashAtEdit = component.DefaultHash
	req.TrustDefaultHashAtEdit = true
	if req.Mode == promptcenter.OverrideModeUseDefault {
		req.OverrideText = ""
	}
	return s.store().UpsertOverride(ctx, req)
}

func (s *PromptCenterService) DeleteOverride(ctx context.Context, req promptcenter.DeleteOverrideRequest) error {
	catalog, err := promptcenter.DefaultCatalog()
	if err != nil {
		return err
	}
	if _, ok := catalog.Get(req.ComponentID); !ok {
		return fmt.Errorf("unknown prompt component: %s", req.ComponentID)
	}
	switch req.ScopeType {
	case promptcenter.ScopeGlobal:
		if req.ScopeID != "" {
			return fmt.Errorf("global scope_id must be empty")
		}
	case promptcenter.ScopeAgent:
		agentID, err := s.requireAgent(ctx, req.ScopeID)
		if err != nil {
			return err
		}
		if agentID == "" {
			return fmt.Errorf("agent scope_id is required")
		}
		req.ScopeID = agentID
	default:
		return fmt.Errorf("scope_type must be global or agent")
	}
	return s.store().DeleteOverride(ctx, req.ComponentID, req.ScopeType, req.ScopeID)
}

func (s *PromptCenterService) Preview(ctx context.Context, req promptcenter.PromptPreviewRequest) (promptcenter.PromptPreviewResponse, error) {
	agentID, err := s.requireAgent(ctx, req.AgentID)
	if err != nil {
		return promptcenter.PromptPreviewResponse{}, err
	}
	catalog, err := promptcenter.DefaultCatalog()
	if err != nil {
		return promptcenter.PromptPreviewResponse{}, err
	}
	componentIDs := normalizePreviewComponentIDs(req, catalog)
	resolver := promptcenter.NewResolver(catalog, s.store())
	scope := promptcenter.PromptScope{AgentID: agentID, PersonaKey: strings.TrimSpace(req.PersonaKey)}
	resolved := make([]promptcenter.ResolvedPrompt, 0, len(componentIDs))
	parts := make([]string, 0, len(componentIDs))
	for _, id := range componentIDs {
		item, err := resolver.Resolve(ctx, id, scope)
		if err != nil {
			return promptcenter.PromptPreviewResponse{}, err
		}
		resolved = append(resolved, item)
		parts = append(parts, item.Text)
	}
	rendered := strings.Join(parts, "\n\n")
	return promptcenter.PromptPreviewResponse{
		AgentID:      scope.AgentID,
		PersonaKey:   scope.PersonaKey,
		Purpose:      strings.TrimSpace(req.Purpose),
		RenderedText: rendered,
		FinalHash:    promptcenter.HashText(rendered),
		Components:   resolved,
	}, nil
}

func (s *PromptCenterService) ListSnapshots(ctx context.Context, req promptcenter.PromptSnapshotListRequest) (promptcenter.PromptSnapshotListResponse, error) {
	items, err := s.store().ListRenderSnapshots(ctx, promptcenter.SnapshotFilter{
		AgentID:   strings.TrimSpace(req.AgentID),
		SessionID: strings.TrimSpace(req.SessionID),
		Purpose:   strings.TrimSpace(req.Purpose),
		Limit:     req.Limit,
	})
	if err != nil {
		return promptcenter.PromptSnapshotListResponse{}, err
	}
	return promptcenter.PromptSnapshotListResponse{Snapshots: items}, nil
}

func (s *PromptCenterService) GetSnapshot(ctx context.Context, id string) (promptcenter.PromptSnapshotDetail, error) {
	snapshot, err := s.store().GetRenderSnapshot(ctx, strings.TrimSpace(id))
	if err != nil {
		return promptcenter.PromptSnapshotDetail{}, err
	}
	if snapshot == nil {
		return promptcenter.PromptSnapshotDetail{}, fmt.Errorf("prompt snapshot not found")
	}
	return promptcenter.PromptSnapshotDetail{RenderSnapshot: *snapshot}, nil
}

func (s *PromptCenterService) componentDetail(ctx context.Context, catalog *promptcenter.Catalog, componentID, agentID string) (promptcenter.PromptComponentDetail, error) {
	component, ok := catalog.Get(componentID)
	if !ok {
		return promptcenter.PromptComponentDetail{}, fmt.Errorf("unknown prompt component: %s", componentID)
	}
	store := s.store()
	globalOverride, err := store.GetOverride(ctx, componentID, promptcenter.ScopeGlobal, "")
	if err != nil {
		return promptcenter.PromptComponentDetail{}, err
	}
	var agentOverride *promptcenter.OverrideRecord
	if strings.TrimSpace(agentID) != "" {
		agentOverride, err = store.GetOverride(ctx, componentID, promptcenter.ScopeAgent, strings.TrimSpace(agentID))
		if err != nil {
			return promptcenter.PromptComponentDetail{}, err
		}
	}
	resolved, err := promptcenter.NewResolver(catalog, store).Resolve(ctx, componentID, promptcenter.PromptScope{AgentID: strings.TrimSpace(agentID)})
	if err != nil {
		return promptcenter.PromptComponentDetail{}, err
	}
	return promptcenter.DetailFromComponent(component, globalOverride, agentOverride, resolved), nil
}

func (s *PromptCenterService) agentExists(ctx context.Context, id string) (bool, error) {
	if strings.TrimSpace(id) == "" {
		return false, nil
	}
	agent, err := s.store().GetAgentConfig(ctx, id)
	if err != nil {
		return false, err
	}
	return agent != nil, nil
}

func (s *PromptCenterService) requireAgent(ctx context.Context, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", nil
	}
	exists, err := s.agentExists(ctx, id)
	if err != nil {
		return "", fmt.Errorf("validate agent_id: %w", err)
	}
	if !exists {
		return "", fmt.Errorf("agent_id does not exist: %s", id)
	}
	return id, nil
}

func (s *PromptCenterService) store() *storage.DB {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		panic("prompt center requires database")
	}
	return s.infra.DB
}

func normalizePreviewComponentIDs(req promptcenter.PromptPreviewRequest, catalog *promptcenter.Catalog) []string {
	seen := map[string]struct{}{}
	var ids []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	add(req.ComponentID)
	for _, id := range req.ComponentIDs {
		add(id)
	}
	if len(ids) > 0 {
		return ids
	}
	for _, component := range catalog.List() {
		add(component.ID)
	}
	return ids
}
