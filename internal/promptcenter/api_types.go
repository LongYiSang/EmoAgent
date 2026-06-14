package promptcenter

type PromptComponentDetail struct {
	ID                 string          `json:"id"`
	Group              string          `json:"group"`
	Name               string          `json:"name"`
	Description        string          `json:"description"`
	Kind               string          `json:"kind"`
	DefaultText        string          `json:"default_text"`
	Editable           bool            `json:"editable"`
	RiskLevel          string          `json:"risk_level"`
	ScopeSupport       []string        `json:"scope_support"`
	MaxChars           int             `json:"max_chars"`
	Order              int             `json:"order"`
	GlobalOverride     *OverrideRecord `json:"global_override,omitempty"`
	AgentOverride      *OverrideRecord `json:"agent_override,omitempty"`
	EffectiveText      string          `json:"effective_text"`
	EffectiveSource    string          `json:"effective_source"`
	EffectiveScopeType string          `json:"effective_scope_type"`
	EffectiveScopeID   string          `json:"effective_scope_id"`
	DefaultHash        string          `json:"default_hash"`
	EffectiveHash      string          `json:"effective_hash"`
	DefaultHashAtEdit  string          `json:"default_hash_at_edit,omitempty"`
	StaleOverride      bool            `json:"stale_override"`
}

type PromptComponentsResponse struct {
	AgentID    string                  `json:"agent_id"`
	Components []PromptComponentDetail `json:"components"`
}

type PromptPreviewRequest struct {
	AgentID      string   `json:"agent_id"`
	PersonaKey   string   `json:"persona_key"`
	Purpose      string   `json:"purpose"`
	ComponentID  string   `json:"component_id"`
	ComponentIDs []string `json:"component_ids"`
}

type PromptPreviewResponse struct {
	AgentID      string           `json:"agent_id"`
	PersonaKey   string           `json:"persona_key"`
	Purpose      string           `json:"purpose"`
	RenderedText string           `json:"rendered_text"`
	FinalHash    string           `json:"final_hash"`
	Components   []ResolvedPrompt `json:"components"`
}

type PromptSnapshotListRequest struct {
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
	Purpose   string `json:"purpose"`
	Limit     int    `json:"limit"`
}

type PromptSnapshotListResponse struct {
	Snapshots []RenderSnapshotSummary `json:"snapshots"`
}

type PromptSnapshotDetail struct {
	RenderSnapshot
}

func DetailFromComponent(component PromptComponent, globalOverride, agentOverride *OverrideRecord, resolved ResolvedPrompt) PromptComponentDetail {
	return PromptComponentDetail{
		ID:                 component.ID,
		Group:              component.Group,
		Name:               component.Name,
		Description:        component.Description,
		Kind:               component.Kind,
		DefaultText:        component.DefaultText,
		Editable:           component.Editable,
		RiskLevel:          component.RiskLevel,
		ScopeSupport:       append([]string(nil), component.ScopeSupport...),
		MaxChars:           component.MaxChars,
		Order:              component.Order,
		GlobalOverride:     cloneOverride(globalOverride),
		AgentOverride:      cloneOverride(agentOverride),
		EffectiveText:      resolved.Text,
		EffectiveSource:    resolved.Source,
		EffectiveScopeType: resolved.ScopeType,
		EffectiveScopeID:   resolved.ScopeID,
		DefaultHash:        resolved.DefaultHash,
		EffectiveHash:      resolved.EffectiveHash,
		DefaultHashAtEdit:  resolved.DefaultHashAtEdit,
		StaleOverride:      resolved.StaleOverride,
	}
}

func cloneOverride(record *OverrideRecord) *OverrideRecord {
	if record == nil {
		return nil
	}
	copy := *record
	return &copy
}
