package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/agentaffect"
	"github.com/longyisang/emoagent/internal/config"
	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/promptcenter"
	"github.com/longyisang/emoagent/internal/storage"
)

type PromptCenterService struct {
	infra        *Infra
	agentRuntime *AgentRuntimeService
	personas     *PersonaService
	memory       *MemoryService
	agentAffect  *AgentAffectService
}

func (s *PromptCenterService) StartBackground(ctx context.Context) {
	if s == nil || s.infra == nil || s.infra.DB == nil {
		return
	}
	cfg := config.DefaultConfig().PromptCenter.Snapshots
	if s.infra.Config != nil {
		cfg = s.infra.Config.PromptCenter.Snapshots
	}
	if !cfg.Enabled {
		return
	}
	cleaner, ok := any(s.infra.DB).(promptcenter.SnapshotCleaner)
	if !ok {
		return
	}
	go s.runSnapshotCleanupLoop(ctx, cleaner, cfg.RetentionDays, cfg.MaxRows)
}

func (s *PromptCenterService) runSnapshotCleanupLoop(ctx context.Context, cleaner promptcenter.SnapshotCleaner, retentionDays int, maxRows int) {
	s.cleanupSnapshots(ctx, cleaner, retentionDays, maxRows)
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupSnapshots(ctx, cleaner, retentionDays, maxRows)
		}
	}
}

func (s *PromptCenterService) cleanupSnapshots(ctx context.Context, cleaner promptcenter.SnapshotCleaner, retentionDays int, maxRows int) {
	if _, err := cleaner.CleanupRenderSnapshots(ctx, retentionDays, maxRows); err != nil && s.infra != nil && s.infra.Logger != nil {
		s.infra.Logger.Warn("failed to cleanup prompt render snapshots", "error", err)
	}
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

func (s *PromptCenterService) UpsertOverride(ctx context.Context, req promptcenter.UpsertOverrideRequest) (promptcenter.UpsertOverrideResponse, error) {
	catalog, err := promptcenter.DefaultCatalog()
	if err != nil {
		return promptcenter.UpsertOverrideResponse{}, err
	}
	if err := promptcenter.ValidateUpsertOverride(ctx, catalog, s.agentExists, req); err != nil {
		return promptcenter.UpsertOverrideResponse{}, err
	}
	component := catalog.MustGet(req.ComponentID)
	warnings := promptcenter.LintOverride(component, req)
	req.DefaultHashAtEdit = component.DefaultHash
	req.TrustDefaultHashAtEdit = true
	if req.Mode == promptcenter.OverrideModeUseDefault {
		req.OverrideText = ""
	}
	if err := s.store().UpsertOverride(ctx, req); err != nil {
		return promptcenter.UpsertOverrideResponse{}, err
	}
	return promptcenter.UpsertOverrideResponse{OK: true, Warnings: warnings}, nil
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
	if isFullPromptPreview(req) {
		return s.previewFullEmotionPrompt(ctx, req, catalog, agentID)
	}
	componentIDs := normalizePreviewComponentIDs(req, catalog)
	resolver := promptcenter.NewResolver(catalog, s.store())
	scope := promptcenter.PromptScope{AgentID: agentID, PersonaKey: strings.TrimSpace(req.PersonaKey)}
	components := make([]promptcenter.RenderComponent, 0, len(componentIDs))
	parts := make([]string, 0, len(componentIDs))
	for _, id := range componentIDs {
		item, err := resolver.Resolve(ctx, id, scope)
		if err != nil {
			return promptcenter.PromptPreviewResponse{}, err
		}
		components = append(components, promptcenter.RenderComponentFromResolved(item))
		parts = append(parts, item.Text)
	}
	rendered := strings.Join(parts, "\n\n")
	return promptcenter.PromptPreviewResponse{
		AgentID:      scope.AgentID,
		PersonaKey:   scope.PersonaKey,
		Purpose:      strings.TrimSpace(req.Purpose),
		RenderedText: rendered,
		FinalHash:    promptcenter.HashText(rendered),
		Components:   components,
	}, nil
}

func (s *PromptCenterService) previewFullEmotionPrompt(ctx context.Context, req promptcenter.PromptPreviewRequest, catalog *promptcenter.Catalog, agentID string) (promptcenter.PromptPreviewResponse, error) {
	if strings.TrimSpace(agentID) == "" {
		return promptcenter.PromptPreviewResponse{}, fmt.Errorf("agent_id is required for full prompt preview")
	}
	agent, err := s.store().GetAgentConfig(ctx, agentID)
	if err != nil {
		return promptcenter.PromptPreviewResponse{}, err
	}
	if agent == nil {
		return promptcenter.PromptPreviewResponse{}, fmt.Errorf("agent_id does not exist: %s", agentID)
	}
	personaKey := strings.TrimSpace(req.PersonaKey)
	if personaKey == "" {
		personaKey = strings.TrimSpace(agent.PersonaKey)
	}
	persona, err := s.previewPersona(ctx, personaKey)
	if err != nil {
		return promptcenter.PromptPreviewResponse{}, err
	}
	baseContext := config.DefaultConfig().Context
	if s.infra != nil && s.infra.Config != nil {
		baseContext = s.infra.Config.Context
	}
	contextCfg, err := agent.ResolveContextConfig(baseContext)
	if err != nil {
		return promptcenter.PromptPreviewResponse{}, err
	}
	var history []storage.MessageRecord
	var state *contextutil.ContextState
	warnings := []promptcenter.PromptPreviewWarning{}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		warnings = append(warnings, promptPreviewWarning("no_session", "info", "未提供 session_id，preview 不含 running summary 或历史状态。"))
	} else {
		history, err = s.store().GetAllMessages(ctx, sessionID)
		if err != nil {
			return promptcenter.PromptPreviewResponse{}, err
		}
		state, err = contextutil.LoadSessionState(ctx, s.store(), sessionID, contextCfg)
		if err != nil {
			return promptcenter.PromptPreviewResponse{}, err
		}
	}
	if !req.IncludeMemory {
		warnings = append(warnings, promptPreviewWarning("memory_preview_disabled", "info", "include_memory=false，preview 不含 memory block。"))
	} else if strings.TrimSpace(req.UserMessage) == "" {
		warnings = append(warnings, promptPreviewWarning("no_user_message", "info", "未提供 user_message，preview 不含 memory retrieval block。"))
	}
	if !req.IncludeAgentAffect {
		warnings = append(warnings, promptPreviewWarning("agent_affect_preview_disabled", "info", "include_agent_affect=false，preview 不含 agent affect block。"))
	}

	resolver := promptcenter.NewResolver(catalog, s.store())
	scope := promptcenter.PromptScope{AgentID: agentID, PersonaKey: personaKey}
	env := s.infra.Environment
	assembled, err := contextutil.BuildEmotionContextWithStateAndPromptResolver(ctx, persona, history, state, contextCfg, env, resolver, scope)
	if err != nil {
		return promptcenter.PromptPreviewResponse{}, err
	}
	if req.IncludeMemory && sessionID != "" && strings.TrimSpace(req.UserMessage) != "" {
		memoryBlock, warning := s.previewMemoryPromptBlock(ctx, sessionID, req.UserMessage)
		if warning != nil {
			warnings = append(warnings, *warning)
		}
		if memoryBlock != "" {
			assembled.System += "\n\n" + memoryBlock
			assembled.PromptComponents = append(assembled.PromptComponents, promptcenter.DynamicComponent(promptcenter.ComponentMemoryPromptBlock, "memory_context", promptcenter.SourceMemoryDynamic, memoryBlock, map[string]any{
				"preview":      true,
				"prompt_chars": len([]rune(memoryBlock)),
			}))
		}
	}
	if req.IncludeAgentAffect {
		affectBlock, warning := s.previewAgentAffectPromptBlock(ctx, personaKey, sessionID)
		if warning != nil {
			warnings = append(warnings, *warning)
		}
		if affectBlock != "" {
			assembled.System += "\n\n" + affectBlock
			assembled.PromptComponents = append(assembled.PromptComponents, promptcenter.DynamicComponent(promptcenter.ComponentAgentAffectPromptBlock, "agent_affect", promptcenter.SourceAgentAffectDynamic, affectBlock, map[string]any{
				"preview":      true,
				"prompt_chars": len([]rune(affectBlock)),
			}))
		}
	}
	purpose := strings.TrimSpace(req.Purpose)
	if purpose == "" {
		purpose = "emotion_chat_full"
	}
	return promptcenter.PromptPreviewResponse{
		AgentID:      agentID,
		PersonaKey:   personaKey,
		Purpose:      purpose,
		RenderedText: assembled.System,
		FinalHash:    promptcenter.HashText(assembled.System),
		Components:   assembled.PromptComponents,
		Warnings:     warnings,
	}, nil
}

func (s *PromptCenterService) previewMemoryPromptBlock(ctx context.Context, sessionID, userMessage string) (string, *promptcenter.PromptPreviewWarning) {
	if s == nil || s.memory == nil {
		warning := promptPreviewWarning("memory_preview_unavailable", "warning", "Memory preview 不可用：memory service 未配置。")
		return "", &warning
	}
	block, err := s.memory.Bridge().RetrievePromptBlock(ctx, sessionID, userMessage)
	if err != nil {
		if s.infra != nil && s.infra.Logger != nil {
			s.infra.Logger.Warn("prompt center memory preview failed", "session_id", sessionID, "error", err)
		}
		warning := promptPreviewWarning("memory_preview_unavailable", "warning", "Memory preview 读取失败，未追加 memory block。")
		return "", &warning
	}
	return strings.TrimSpace(block), nil
}

func (s *PromptCenterService) previewAgentAffectPromptBlock(ctx context.Context, personaKey, sessionID string) (string, *promptcenter.PromptPreviewWarning) {
	if s == nil || s.agentAffect == nil {
		warning := promptPreviewWarning("agent_affect_preview_unavailable", "warning", "Agent Affect preview 不可用：agent affect service 未配置。")
		return "", &warning
	}
	resp, err := s.agentAffect.PreviewPrompt(ctx, agentaffect.BuildPromptAffectBlockRequest{
		PersonaID: personaKey,
		SessionID: sessionID,
	})
	if err != nil {
		if s.infra != nil && s.infra.Logger != nil {
			s.infra.Logger.Warn("prompt center agent affect preview failed", "persona_key", personaKey, "session_id", sessionID, "error", err)
		}
		warning := promptPreviewWarning("agent_affect_preview_unavailable", "warning", "Agent Affect preview 读取失败，未追加 agent affect block。")
		return "", &warning
	}
	return strings.TrimSpace(resp.PromptBlock), nil
}

func (s *PromptCenterService) previewPersona(ctx context.Context, personaKey string) (*config.Persona, error) {
	if strings.TrimSpace(personaKey) == "" {
		return nil, fmt.Errorf("persona_key is required for full prompt preview")
	}
	if s.personas != nil {
		if persona, ok := s.personas.Get(personaKey); ok && persona != nil {
			return persona, nil
		}
	}
	record, err := s.store().GetPersona(ctx, personaKey)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, fmt.Errorf("persona not found: %s", personaKey)
	}
	persona := &config.Persona{
		Name:         record.Name,
		Description:  record.Description,
		SystemPrompt: record.SystemPrompt,
		Tone:         record.Tone,
		Greeting:     record.Greeting,
	}
	if strings.TrimSpace(persona.Name) == "" {
		persona.Name = personaKey
	}
	_ = json.Unmarshal([]byte(record.Quirks), &persona.Quirks)
	return persona, nil
}

func promptPreviewWarning(code, severity, message string) promptcenter.PromptPreviewWarning {
	return promptcenter.PromptPreviewWarning{Code: code, Severity: severity, Message: message}
}

func isFullPromptPreview(req promptcenter.PromptPreviewRequest) bool {
	return strings.EqualFold(strings.TrimSpace(req.Mode), "full") || strings.EqualFold(strings.TrimSpace(req.Purpose), "emotion_chat_full")
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
