package promptcenter

type RenderComponent struct {
	ComponentID   string `json:"component_id"`
	Name          string `json:"name,omitempty"`
	Source        string `json:"source"`
	ScopeType     string `json:"scope_type,omitempty"`
	ScopeID       string `json:"scope_id,omitempty"`
	DefaultHash   string `json:"default_hash,omitempty"`
	EffectiveHash string `json:"effective_hash"`
	SectionName   string `json:"section_name,omitempty"`
	Kind          string `json:"kind,omitempty"`
	Editable      bool   `json:"editable"`
	Dynamic       bool   `json:"dynamic"`
	TextLength    int    `json:"text_length,omitempty"`
	Truncated     bool   `json:"truncated,omitempty"`
	MetadataJSON  string `json:"metadata_json,omitempty"`
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

type SnapshotRenderOptions struct {
	StoreRenderedText    bool
	MaxRenderedTextChars int
}

type CleanupResult struct {
	DeletedByRetention int `json:"deleted_by_retention"`
	DeletedByMaxRows   int `json:"deleted_by_max_rows"`
}
