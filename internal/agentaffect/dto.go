package agentaffect

import (
	"encoding/json"
	"time"
)

const (
	EvaluationStatusPreview   = "preview"
	EvaluationStatusCommitted = "committed"
	EvaluationStatusRejected  = "rejected"
	EvaluationStatusFailed    = "failed"

	CommitModePreview         = "preview"
	CommitModeCommitIfAllowed = "commit_if_allowed"

	AffectJobTypeTurnEvaluate   = "turn_evaluate"
	AffectJobTypePluginEvaluate = "plugin_evaluate"
	AffectJobTypeManualEvaluate = "manual_evaluate"
	AffectJobTypeBarrier        = "barrier"

	AffectJobStatusPending    = "pending"
	AffectJobStatusRunning    = "running"
	AffectJobStatusDone       = "done"
	AffectJobStatusFailed     = "failed"
	AffectJobStatusSuperseded = "superseded"

	AffectBatchStatusRunning    = "running"
	AffectBatchStatusDone       = "done"
	AffectBatchStatusFailed     = "failed"
	AffectBatchStatusSuperseded = "superseded"
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
	MoodOwnerScope      string             `json:"mood_owner_scope,omitempty"`
	MoodOwnerID         string             `json:"mood_owner_id,omitempty"`
	Vector              MoodVector         `json:"vector"`
	Label               string             `json:"label"`
	Confidence          float64            `json:"confidence"`
	MoodDescription     string             `json:"mood_description,omitempty"`
	MoodReason          string             `json:"mood_reason,omitempty"`
	PromptMoodText      string             `json:"prompt_mood_text,omitempty"`
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
	BatchID           string            `json:"batch_id,omitempty"`
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

type PromptPreviewResponse struct {
	PromptBlock string `json:"prompt_block"`
}

type AffectProfile struct {
	ID                        string     `json:"id"`
	PersonaID                 string     `json:"persona_id"`
	ProfileName               string     `json:"profile_name"`
	Baseline                  MoodVector `json:"baseline"`
	DimensionConfigJSON       string     `json:"dimension_config_json,omitempty"`
	ExternalizationConfigJSON string     `json:"externalization_config_json,omitempty"`
	LLMConfigJSON             string     `json:"llm_config_json,omitempty"`
	ContextPolicyJSON         string     `json:"context_policy_json,omitempty"`
	ClampPolicyJSON           string     `json:"clamp_policy_json,omitempty"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 *time.Time `json:"updated_at,omitempty"`
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
	MoodDescription     string
	MoodReason          string
	PromptMoodText      string
	CauseSummary        string
	VisibleCauseSummary string
	Confidence          float64
	RawResponseJSON     string
	Fallback            bool
	Status              string
}

type AffectEvaluationRecord struct {
	ID                        string            `json:"id"`
	PersonaID                 string            `json:"persona_id"`
	SessionID                 string            `json:"session_id,omitempty"`
	TurnID                    string            `json:"turn_id,omitempty"`
	BatchID                   string            `json:"batch_id,omitempty"`
	MoodOwnerScope            string            `json:"mood_owner_scope,omitempty"`
	MoodOwnerID               string            `json:"mood_owner_id,omitempty"`
	Trigger                   TriggerDescriptor `json:"trigger"`
	Input                     MoodImpactInput   `json:"input"`
	ContextWindowPolicyJSON   string            `json:"context_window_policy_json,omitempty"`
	ContextWindowSnapshotJSON string            `json:"context_window_snapshot_json,omitempty"`
	BeforeStateID             string            `json:"before_state_id,omitempty"`
	BeforeStateJSON           string            `json:"before_state_json,omitempty"`
	LLMProvider               string            `json:"llm_provider,omitempty"`
	LLMModel                  string            `json:"llm_model,omitempty"`
	LLMThinkingEnabled        bool              `json:"llm_thinking_enabled"`
	PromptVersion             string            `json:"prompt_version,omitempty"`
	PromptHash                string            `json:"prompt_hash,omitempty"`
	PromptSnapshot            string            `json:"prompt_snapshot,omitempty"`
	ResponseJSON              string            `json:"response_json,omitempty"`
	ProposedDelta             MoodVector        `json:"proposed_delta"`
	ClampedDelta              MoodVector        `json:"clamped_delta"`
	PredictedState            MoodVector        `json:"predicted_state"`
	MoodDescription           string            `json:"mood_description,omitempty"`
	MoodReason                string            `json:"mood_reason,omitempty"`
	PromptMoodText            string            `json:"prompt_mood_text,omitempty"`
	CauseSummary              string            `json:"cause_summary,omitempty"`
	VisibleCauseSummary       string            `json:"visible_cause_summary,omitempty"`
	Confidence                float64           `json:"confidence"`
	ClampNotes                []string          `json:"clamp_notes,omitempty"`
	Status                    string            `json:"status"`
	CreatedAt                 time.Time         `json:"created_at"`
}

type AffectEventRecord struct {
	ID              string            `json:"id"`
	PersonaID       string            `json:"persona_id"`
	SessionID       string            `json:"session_id,omitempty"`
	TurnID          string            `json:"turn_id,omitempty"`
	BatchID         string            `json:"batch_id,omitempty"`
	MoodOwnerScope  string            `json:"mood_owner_scope,omitempty"`
	MoodOwnerID     string            `json:"mood_owner_id,omitempty"`
	EvaluationID    string            `json:"evaluation_id,omitempty"`
	Trigger         TriggerDescriptor `json:"trigger"`
	BeforeStateID   string            `json:"before_state_id,omitempty"`
	AfterStateID    string            `json:"after_state_id,omitempty"`
	ProposedDelta   MoodVector        `json:"proposed_delta"`
	ClampedDelta    MoodVector        `json:"clamped_delta"`
	CommittedDelta  MoodVector        `json:"committed_delta"`
	LabelBefore     string            `json:"label_before,omitempty"`
	LabelAfter      string            `json:"label_after,omitempty"`
	MoodDescription string            `json:"mood_description,omitempty"`
	MoodReason      string            `json:"mood_reason,omitempty"`
	PromptMoodText  string            `json:"prompt_mood_text,omitempty"`
	CauseSummary    string            `json:"cause_summary,omitempty"`
	Significance    float64           `json:"significance"`
	Confidence      float64           `json:"confidence"`
	CommittedBy     string            `json:"committed_by"`
	CreatedAt       time.Time         `json:"created_at"`
}

type PluginWriteRecord struct {
	ID              string    `json:"id"`
	PersonaID       string    `json:"persona_id"`
	SessionID       string    `json:"session_id,omitempty"`
	TurnID          string    `json:"turn_id,omitempty"`
	PluginID        string    `json:"plugin_id"`
	Capability      string    `json:"capability"`
	RequestKind     string    `json:"request_kind"`
	RequestJSON     string    `json:"request_json,omitempty"`
	Accepted        bool      `json:"accepted"`
	RejectionReason string    `json:"rejection_reason,omitempty"`
	ClampNotes      []string  `json:"clamp_notes,omitempty"`
	EvaluationID    string    `json:"evaluation_id,omitempty"`
	AffectEventID   string    `json:"affect_event_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type RecentEvaluationsQuery struct {
	PersonaID string
	SessionID string
	Limit     int
}

type RecentEventsQuery struct {
	PersonaID string
	SessionID string
	Limit     int
}

type PluginWritesQuery struct {
	PersonaID string
	SessionID string
	PluginID  string
	Limit     int
}

type HistoryQuery struct {
	PersonaID string `json:"persona_id"`
	SessionID string `json:"session_id,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Kind      string `json:"kind,omitempty"`
}

type HistoryResponse struct {
	Evaluations []AffectEvaluationRecord `json:"evaluations"`
	Events      []AffectEventRecord      `json:"events"`
}

type JobQueueQuery struct {
	PersonaID      string `json:"persona_id,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	MoodOwnerScope string `json:"mood_owner_scope,omitempty"`
	MoodOwnerID    string `json:"mood_owner_id,omitempty"`
	Status         string `json:"status,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

type BatchQuery struct {
	PersonaID      string `json:"persona_id,omitempty"`
	MoodOwnerScope string `json:"mood_owner_scope,omitempty"`
	MoodOwnerID    string `json:"mood_owner_id,omitempty"`
	Status         string `json:"status,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

type QueueStatusResponse struct {
	PendingJobs int                    `json:"pending_jobs"`
	RunningJobs int                    `json:"running_jobs"`
	FailedJobs  int                    `json:"failed_jobs"`
	LatestBatch *AffectJobBatchRecord  `json:"latest_batch,omitempty"`
	Jobs        []AffectJobRecord      `json:"jobs"`
	Batches     []AffectJobBatchRecord `json:"batches"`
}

type ProcessBatchOnceResponse struct {
	Processed bool `json:"processed"`
}

type ClearFailedJobsResponse struct {
	Cleared int `json:"cleared"`
}

type SupersedePendingJobsResponse struct {
	Superseded int `json:"superseded"`
}

type ResetMoodRequest struct {
	PersonaID   string     `json:"persona_id"`
	SessionID   string     `json:"session_id,omitempty"`
	Baseline    MoodVector `json:"baseline,omitempty"`
	Reason      string     `json:"reason,omitempty"`
	CommittedBy string     `json:"committed_by,omitempty"`
}

type ResetMoodResponse struct {
	EventID string       `json:"event_id,omitempty"`
	Mood    MoodSnapshot `json:"mood"`
}

type EnqueueTurnEvaluationJobRequest struct {
	PersonaID          string            `json:"persona_id"`
	SessionID          string            `json:"session_id,omitempty"`
	TurnID             string            `json:"turn_id,omitempty"`
	MoodOwner          MoodOwner         `json:"mood_owner,omitempty"`
	UserText           string            `json:"user_text,omitempty"`
	AssistantText      string            `json:"assistant_text,omitempty"`
	InputSummary       string            `json:"input_summary,omitempty"`
	MemoryPromptBlock  string            `json:"memory_prompt_block,omitempty"`
	Trigger            TriggerDescriptor `json:"trigger"`
	BaseStateID        string            `json:"base_state_id,omitempty"`
	BaseStateUpdatedAt time.Time         `json:"base_state_updated_at,omitempty"`
	RunAfter           time.Time         `json:"run_after,omitempty"`
	MaxAttempts        int               `json:"max_attempts,omitempty"`
}

type EnqueueAffectJobRequest struct {
	ID                 string            `json:"id,omitempty"`
	PersonaID          string            `json:"persona_id"`
	SessionID          string            `json:"session_id,omitempty"`
	TurnID             string            `json:"turn_id,omitempty"`
	MoodOwner          MoodOwner         `json:"mood_owner"`
	JobType            string            `json:"job_type"`
	Batchable          bool              `json:"batchable"`
	BarrierKind        string            `json:"barrier_kind,omitempty"`
	Status             string            `json:"status,omitempty"`
	Priority           int               `json:"priority,omitempty"`
	RunAfter           time.Time         `json:"run_after,omitempty"`
	MaxAttempts        int               `json:"max_attempts,omitempty"`
	Trigger            TriggerDescriptor `json:"trigger"`
	TriggerJSONRaw     json.RawMessage   `json:"trigger_json,omitempty"`
	InputMode          string            `json:"input_mode,omitempty"`
	UserText           string            `json:"user_text,omitempty"`
	AssistantText      string            `json:"assistant_text,omitempty"`
	InputSummary       string            `json:"input_summary,omitempty"`
	MemoryPromptBlock  string            `json:"memory_prompt_block,omitempty"`
	BaseStateID        string            `json:"base_state_id,omitempty"`
	BaseStateUpdatedAt time.Time         `json:"base_state_updated_at,omitempty"`
}

type AffectJobRecord struct {
	Seq                int64             `json:"seq"`
	ID                 string            `json:"id"`
	PersonaID          string            `json:"persona_id"`
	SessionID          string            `json:"session_id,omitempty"`
	TurnID             string            `json:"turn_id,omitempty"`
	MoodOwnerScope     string            `json:"mood_owner_scope"`
	MoodOwnerID        string            `json:"mood_owner_id"`
	JobType            string            `json:"job_type"`
	Batchable          bool              `json:"batchable"`
	BarrierKind        string            `json:"barrier_kind,omitempty"`
	Status             string            `json:"status"`
	Priority           int               `json:"priority"`
	RunAfter           time.Time         `json:"run_after"`
	Attempts           int               `json:"attempts"`
	MaxAttempts        int               `json:"max_attempts"`
	ClaimedBy          string            `json:"claimed_by,omitempty"`
	ClaimedUntil       time.Time         `json:"claimed_until,omitempty"`
	Trigger            TriggerDescriptor `json:"trigger"`
	InputMode          string            `json:"input_mode"`
	UserText           string            `json:"user_text,omitempty"`
	AssistantText      string            `json:"assistant_text,omitempty"`
	InputSummary       string            `json:"input_summary,omitempty"`
	MemoryPromptBlock  string            `json:"memory_prompt_block,omitempty"`
	BaseStateID        string            `json:"base_state_id,omitempty"`
	BaseStateUpdatedAt time.Time         `json:"base_state_updated_at,omitempty"`
	BatchID            string            `json:"batch_id,omitempty"`
	ResultEvaluationID string            `json:"result_evaluation_id,omitempty"`
	ResultEventID      string            `json:"result_event_id,omitempty"`
	ErrorMessage       string            `json:"error_message,omitempty"`
	CreatedAt          time.Time         `json:"created_at"`
	StartedAt          time.Time         `json:"started_at,omitempty"`
	FinishedAt         time.Time         `json:"finished_at,omitempty"`
}

type AffectJobBatchRecord struct {
	ID                        string    `json:"id"`
	PersonaID                 string    `json:"persona_id"`
	MoodOwnerScope            string    `json:"mood_owner_scope"`
	MoodOwnerID               string    `json:"mood_owner_id"`
	JobType                   string    `json:"job_type"`
	Status                    string    `json:"status"`
	JobCount                  int       `json:"job_count"`
	FirstJobSeq               int64     `json:"first_job_seq"`
	LastJobSeq                int64     `json:"last_job_seq"`
	JobIDs                    []string  `json:"job_ids"`
	SessionIDs                []string  `json:"session_ids,omitempty"`
	TurnIDs                   []string  `json:"turn_ids,omitempty"`
	BatchInputSummary         string    `json:"batch_input_summary,omitempty"`
	ContextWindowSnapshotJSON string    `json:"context_window_snapshot_json,omitempty"`
	EvaluationID              string    `json:"evaluation_id,omitempty"`
	AffectEventID             string    `json:"affect_event_id,omitempty"`
	ErrorMessage              string    `json:"error_message,omitempty"`
	ClaimedBy                 string    `json:"claimed_by,omitempty"`
	StartedAt                 time.Time `json:"started_at"`
	FinishedAt                time.Time `json:"finished_at,omitempty"`
}

type ClaimBatchOptions struct {
	MaxJobs        int
	ClaimTTL       time.Duration
	MaxAge         time.Duration
	MinWait        time.Duration
	MaxInputTokens int
	SplitSessions  bool
}

type MarkBatchDoneRequest struct {
	BatchID      string
	EvaluationID string
	EventID      string
	FinishedAt   time.Time
	ClearRaw     bool
}

type CommitBatchEvaluationRequest struct {
	BatchID    string
	Evaluation AffectEvaluationRecord
	State      MoodSnapshot
	Event      AffectEventRecord
	FinishedAt time.Time
	ClearRaw   bool
}

type MarkBatchFailedRequest struct {
	BatchID      string
	ErrorMessage string
	FinishedAt   time.Time
	Retry        bool
	RetryAt      time.Time
}

type SupersedePendingJobsRequest struct {
	MoodOwner    MoodOwner
	PersonaID    string
	All          bool
	Reason       string
	SupersededAt time.Time
}
