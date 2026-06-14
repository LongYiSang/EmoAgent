package promptcenter

const (
	ScopeGlobal = "global"
	ScopeAgent  = "agent"

	OverrideModeCustom     OverrideMode = "custom"
	OverrideModeUseDefault OverrideMode = "use_default"

	SourceEmbeddedDefault = "embedded_default"
	SourceGlobalOverride  = "global_override"
	SourceAgentOverride   = "agent_override"
	SourceAgentDefault    = "agent_default"

	SourcePersona            = "persona"
	SourceRuntimeDynamic     = "runtime_dynamic"
	SourcePendingWorkDynamic = "pending_work_dynamic"
	SourceMemoryDynamic      = "memory_dynamic"
	SourceAgentAffectDynamic = "agent_affect_dynamic"
	SourceExtraSystemDynamic = "extra_system_dynamic"
)

const (
	ComponentEmotionOperatingContract         = "emotion.operating_contract"
	ComponentEmotionInternalContextDataPolicy = "emotion.internal_context_data_policy"
	ComponentEmotionPersona                   = "emotion.persona"
	ComponentEmotionRuntimeContext            = "emotion.runtime_context"
	ComponentEmotionPendingWork               = "emotion.pending_work"
	ComponentMemoryPromptBlock                = "memory.prompt_block"
	ComponentAgentAffectPromptBlock           = "agent_affect.prompt_block"
	ComponentTurnExtraSystem                  = "turn.extra_system"
	ComponentRunningSummarySystem             = "context.running_summary.system"
	ComponentRunningSummaryRepair             = "context.running_summary.repair"
	ComponentWorkRuntimeDeciderSystem         = "work.runtime_decider.system"
	ComponentWorkProgressSummarySystem        = "work.progress_summary.system"
	ComponentWorkProgressSummaryRepair        = "work.progress_summary.repair"
	ComponentToolDelegateDescription          = "tool.delegate_to_work.description"
	ComponentToolResumeDescription            = "tool.resume_work.description"
	ComponentToolFinishTaskDescription        = "tool.finish_task.description"
	ComponentToolRequestDecisionDescription   = "tool.request_decision.description"
)

type OverrideMode string

type PromptComponent struct {
	ID           string   `json:"id" yaml:"id"`
	Group        string   `json:"group" yaml:"group"`
	Name         string   `json:"name" yaml:"name"`
	Description  string   `json:"description" yaml:"description"`
	Kind         string   `json:"kind" yaml:"kind"`
	DefaultText  string   `json:"default_text" yaml:"-"`
	DefaultHash  string   `json:"default_hash" yaml:"-"`
	Editable     bool     `json:"editable" yaml:"editable"`
	RiskLevel    string   `json:"risk_level" yaml:"risk_level"`
	ScopeSupport []string `json:"scope_support" yaml:"scope_support"`
	MaxChars     int      `json:"max_chars" yaml:"max_chars"`
	Order        int      `json:"order" yaml:"order"`
}

func (c PromptComponent) SupportsScope(scopeType string) bool {
	for _, item := range c.ScopeSupport {
		if item == scopeType {
			return true
		}
	}
	return false
}

type PromptScope struct {
	AgentID    string `json:"agent_id"`
	PersonaKey string `json:"persona_key"`
}

type OverrideRecord struct {
	ID                string       `json:"id"`
	ComponentID       string       `json:"component_id"`
	ScopeType         string       `json:"scope_type"`
	ScopeID           string       `json:"scope_id"`
	Mode              OverrideMode `json:"mode"`
	OverrideText      string       `json:"override_text"`
	Enabled           bool         `json:"enabled"`
	DefaultHashAtEdit string       `json:"default_hash_at_edit"`
	Note              string       `json:"note"`
	CreatedAt         string       `json:"created_at"`
	UpdatedAt         string       `json:"updated_at"`
}

type UpsertOverrideRequest struct {
	ComponentID            string       `json:"component_id"`
	ScopeType              string       `json:"scope_type"`
	ScopeID                string       `json:"scope_id"`
	Mode                   OverrideMode `json:"mode"`
	OverrideText           string       `json:"override_text"`
	Enabled                *bool        `json:"enabled,omitempty"`
	Note                   string       `json:"note"`
	DefaultHashAtEdit      string       `json:"-"`
	TrustDefaultHashAtEdit bool         `json:"-"`
}

func (r UpsertOverrideRequest) EnabledOrDefault() bool {
	return r.Enabled == nil || *r.Enabled
}

type DeleteOverrideRequest struct {
	ComponentID string `json:"component_id"`
	ScopeType   string `json:"scope_type"`
	ScopeID     string `json:"scope_id"`
}

type ResolvedPrompt struct {
	ComponentID       string `json:"component_id"`
	Name              string `json:"name,omitempty"`
	Text              string `json:"text"`
	Source            string `json:"source"`
	ScopeType         string `json:"scope_type"`
	ScopeID           string `json:"scope_id"`
	DefaultHash       string `json:"default_hash"`
	EffectiveHash     string `json:"effective_hash"`
	Kind              string `json:"kind,omitempty"`
	Editable          bool   `json:"editable"`
	TextLength        int    `json:"text_length,omitempty"`
	DefaultHashAtEdit string `json:"default_hash_at_edit,omitempty"`
	StaleOverride     bool   `json:"stale_override"`
}
