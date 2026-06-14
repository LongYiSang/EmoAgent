package promptcenter

type RenderComponent struct {
	ComponentID   string `json:"component_id"`
	Source        string `json:"source"`
	ScopeType     string `json:"scope_type"`
	ScopeID       string `json:"scope_id"`
	DefaultHash   string `json:"default_hash"`
	EffectiveHash string `json:"effective_hash"`
}

type RenderSnapshot struct {
	ID             string            `json:"id"`
	RequestID      string            `json:"request_id"`
	TurnID         string            `json:"turn_id"`
	SessionID      string            `json:"session_id"`
	AgentID        string            `json:"agent_id"`
	PersonaKey     string            `json:"persona_key"`
	Purpose        string            `json:"purpose"`
	Model          string            `json:"model"`
	FinalHash      string            `json:"final_hash"`
	Components     []RenderComponent `json:"components"`
	ComponentsJSON string            `json:"components_json,omitempty"`
	RenderedText   string            `json:"rendered_text"`
	Truncated      bool              `json:"truncated"`
	CreatedAt      string            `json:"created_at"`
}

type RenderSnapshotSummary struct {
	ID         string `json:"id"`
	SessionID  string `json:"session_id"`
	AgentID    string `json:"agent_id"`
	PersonaKey string `json:"persona_key"`
	Purpose    string `json:"purpose"`
	Model      string `json:"model"`
	FinalHash  string `json:"final_hash"`
	Truncated  bool   `json:"truncated"`
	CreatedAt  string `json:"created_at"`
}

type SnapshotFilter struct {
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
	Purpose   string `json:"purpose"`
	Limit     int    `json:"limit"`
}
