package agentaffect

import "time"

const (
	EvaluationStatusPreview   = "preview"
	EvaluationStatusCommitted = "committed"
	EvaluationStatusRejected  = "rejected"
	EvaluationStatusFailed    = "failed"

	CommitModePreview         = "preview"
	CommitModeCommitIfAllowed = "commit_if_allowed"
)

type MoodVector struct {
	Valence     float64 `json:"valence"`
	Arousal     float64 `json:"arousal"`
	Dominance   float64 `json:"dominance"`
	Energy      float64 `json:"energy"`
	Warmth      float64 `json:"warmth"`
	Concern     float64 `json:"concern"`
	Curiosity   float64 `json:"curiosity"`
	Playfulness float64 `json:"playfulness"`
	Attachment  float64 `json:"attachment"`
	Frustration float64 `json:"frustration"`
	Uncertainty float64 `json:"uncertainty"`
}

func (v MoodVector) IsZero() bool {
	return v == MoodVector{}
}

type CauseContributor struct {
	Kind    string  `json:"kind"`
	Summary string  `json:"summary"`
	Weight  float64 `json:"weight"`
}

type MoodSnapshot struct {
	StateID             string             `json:"state_id"`
	PersonaID           string             `json:"persona_id"`
	SessionID           string             `json:"session_id,omitempty"`
	Vector              MoodVector         `json:"vector"`
	Label               string             `json:"label"`
	Confidence          float64            `json:"confidence"`
	CauseSummary        string             `json:"cause_summary,omitempty"`
	VisibleCauseSummary string             `json:"visible_cause_summary,omitempty"`
	CauseStack          []CauseContributor `json:"cause_stack,omitempty"`
	UpdatedAt           time.Time          `json:"updated_at"`
}

type TriggerDescriptor struct {
	TriggerType    string `json:"trigger_type"`
	CustomType     string `json:"custom_type,omitempty"`
	CustomTypeDesc string `json:"custom_type_desc,omitempty"`
	SourceKind     string `json:"source_kind,omitempty"`
	SourceRefType  string `json:"source_ref_type,omitempty"`
	SourceRefID    string `json:"source_ref_id,omitempty"`
	SourceRefHash  string `json:"source_ref_hash,omitempty"`
	PluginID       string `json:"plugin_id,omitempty"`
}

type MoodImpactInput struct {
	Mode    string `json:"mode"`
	Text    string `json:"text,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type GetCurrentMoodRequest struct {
	PersonaID string `json:"persona_id"`
	SessionID string `json:"session_id,omitempty"`
	View      string `json:"view,omitempty"`
}

type GetCurrentMoodResponse struct {
	Enabled bool         `json:"enabled"`
	Mood    MoodSnapshot `json:"mood"`
}

type EvaluateMoodImpactRequest struct {
	PersonaID         string            `json:"persona_id"`
	SessionID         string            `json:"session_id,omitempty"`
	TurnID            string            `json:"turn_id,omitempty"`
	Trigger           TriggerDescriptor `json:"trigger"`
	Input             MoodImpactInput   `json:"input"`
	MemoryPromptBlock string            `json:"memory_prompt_block,omitempty"`
	CommitMode        string            `json:"commit_mode,omitempty"`
}

type EvaluateMoodImpactResponse struct {
	Enabled       bool         `json:"enabled"`
	EvaluationID  string       `json:"evaluation_id,omitempty"`
	Mood          MoodSnapshot `json:"mood"`
	ProposedDelta MoodVector   `json:"proposed_delta"`
	ClampedDelta  MoodVector   `json:"clamped_delta"`
	PredictedMood MoodSnapshot `json:"predicted_mood"`
	ClampNotes    []string     `json:"clamp_notes,omitempty"`
	NoChange      bool         `json:"no_change"`
	Status        string       `json:"status"`
}

type SubmitMoodImpactRequest = EvaluateMoodImpactRequest

type SubmitMoodImpactResponse struct {
	Enabled       bool         `json:"enabled"`
	EvaluationID  string       `json:"evaluation_id,omitempty"`
	EventID       string       `json:"event_id,omitempty"`
	Mood          MoodSnapshot `json:"mood"`
	ProposedDelta MoodVector   `json:"proposed_delta"`
	ClampedDelta  MoodVector   `json:"clamped_delta"`
	ClampNotes    []string     `json:"clamp_notes,omitempty"`
	NoChange      bool         `json:"no_change"`
	Status        string       `json:"status"`
}

type ApplyMoodDeltaRequest struct {
	PersonaID   string            `json:"persona_id"`
	SessionID   string            `json:"session_id,omitempty"`
	TurnID      string            `json:"turn_id,omitempty"`
	Trigger     TriggerDescriptor `json:"trigger"`
	Delta       MoodVector        `json:"delta"`
	CommittedBy string            `json:"committed_by,omitempty"`
}

type ApplyMoodDeltaResponse struct {
	EventID      string       `json:"event_id,omitempty"`
	Mood         MoodSnapshot `json:"mood"`
	ClampedDelta MoodVector   `json:"clamped_delta"`
	ClampNotes   []string     `json:"clamp_notes,omitempty"`
}

type BuildPromptAffectBlockRequest struct {
	PersonaID string       `json:"persona_id"`
	SessionID string       `json:"session_id,omitempty"`
	Mood      MoodSnapshot `json:"mood,omitempty"`
}

type AffectProfile struct {
	ID          string     `json:"id"`
	PersonaID   string     `json:"persona_id"`
	ProfileName string     `json:"profile_name"`
	Baseline    MoodVector `json:"baseline"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
}

type LLMEvaluationRequest struct {
	PersonaID            string
	SessionID            string
	TurnID               string
	PersonaAffectProfile AffectProfile
	CurrentMood          MoodSnapshot
	Trigger              TriggerDescriptor
	Input                MoodImpactInput
	MemoryPromptBlock    string
	Recent               []AffectEvaluationRecord
	PromptPolicy         any
}

type LLMEvaluationResult struct {
	Delta               MoodVector
	Label               string
	CauseSummary        string
	VisibleCauseSummary string
	Confidence          float64
	RawResponseJSON     string
	Fallback            bool
	Status              string
}

type AffectEvaluationRecord struct {
	ID                        string
	PersonaID                 string
	SessionID                 string
	TurnID                    string
	Trigger                   TriggerDescriptor
	Input                     MoodImpactInput
	ContextWindowPolicyJSON   string
	ContextWindowSnapshotJSON string
	BeforeStateID             string
	BeforeStateJSON           string
	LLMProvider               string
	LLMModel                  string
	LLMThinkingEnabled        bool
	PromptVersion             string
	PromptHash                string
	PromptSnapshot            string
	ResponseJSON              string
	ProposedDelta             MoodVector
	ClampedDelta              MoodVector
	PredictedState            MoodVector
	CauseSummary              string
	VisibleCauseSummary       string
	Confidence                float64
	ClampNotes                []string
	Status                    string
	CreatedAt                 time.Time
}

type AffectEventRecord struct {
	ID             string
	PersonaID      string
	SessionID      string
	TurnID         string
	EvaluationID   string
	Trigger        TriggerDescriptor
	BeforeStateID  string
	AfterStateID   string
	ProposedDelta  MoodVector
	ClampedDelta   MoodVector
	CommittedDelta MoodVector
	LabelBefore    string
	LabelAfter     string
	CauseSummary   string
	Significance   float64
	Confidence     float64
	CommittedBy    string
	CreatedAt      time.Time
}

type PluginWriteRecord struct {
	ID              string
	PersonaID       string
	SessionID       string
	TurnID          string
	PluginID        string
	Capability      string
	RequestKind     string
	RequestJSON     string
	Accepted        bool
	RejectionReason string
	ClampNotes      []string
	EvaluationID    string
	AffectEventID   string
	CreatedAt       time.Time
}

type RecentEvaluationsQuery struct {
	PersonaID string
	SessionID string
	Limit     int
}
