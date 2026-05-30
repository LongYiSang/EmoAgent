package protocol

import "time"

// TaskBrief is the task contract from Emotion to Work.
type TaskBrief struct {
	TaskID             string    `json:"task_id"`
	Goal               string    `json:"goal"`
	Background         string    `json:"background,omitempty"`
	Constraints        []string  `json:"constraints,omitempty"`
	AcceptanceCriteria []string  `json:"acceptance_criteria,omitempty"`
	PermissionScope    string    `json:"permission_scope"`
	ReadScope          string    `json:"read_scope,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
}

// TaskReport is the result from Work to Emotion.
type TaskReport struct {
	TaskID        string    `json:"task_id"`
	Status        string    `json:"status"` // "completed", "failed", "partial"
	Goal          string    `json:"goal"`
	Summary       string    `json:"summary"`
	Findings      []string  `json:"findings,omitempty"`
	OpenQuestions []string  `json:"open_questions,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// EscalationCategory classifies the nature of a decision escalation.
type EscalationCategory string

const (
	CatAuto                         EscalationCategory = "auto"
	CatEmotionJudgment              EscalationCategory = "emotion_judgment"
	CatHumanConfirmation            EscalationCategory = "human_confirmation"
	CatPermissionEscalationRequired EscalationCategory = "permission_escalation_required"
	CatToolApproval                 EscalationCategory = "tool_approval"
)

// DecisionOption describes one possible course of action.
type DecisionOption struct {
	ID          string   `json:"id"`
	Summary     string   `json:"summary"`
	Pros        []string `json:"pros,omitempty"`
	Cons        []string `json:"cons,omitempty"`
	SideEffects []string `json:"side_effects,omitempty"`
}

// DecisionEvidence is a single fact Work has verified.
type DecisionEvidence struct {
	Finding string `json:"finding"`
	Source  string `json:"source,omitempty"`
}

// DecisionTradeoff describes one dimension of tension in the decision.
type DecisionTradeoff struct {
	Dimension string `json:"dimension"`
	Note      string `json:"note"`
}

type ToolApprovalBinding struct {
	ApprovalKind        string `json:"approval_kind"`
	ToolName            string `json:"tool_name"`
	NormalizedInputHash string `json:"normalized_input_hash"`
	PathDigest          string `json:"path_digest,omitempty"`
	InputPreview        string `json:"input_preview,omitempty"`
}

// DecisionPacket is the structured escalation payload from Work to Emotion.
// It carries summarized context without leaking raw Work traces.
type DecisionPacket struct {
	TaskID               string               `json:"task_id"`
	Category             EscalationCategory   `json:"category"`
	RiskLevel            string               `json:"-"`
	GoalSummary          string               `json:"goal_summary"`
	Question             string               `json:"question"`
	WhyBlocked           string               `json:"why_blocked"`
	Options              []DecisionOption     `json:"options"`
	RelevantFindings     []DecisionEvidence   `json:"relevant_findings,omitempty"`
	KeyTradeoffs         []DecisionTradeoff   `json:"key_tradeoffs,omitempty"`
	RecommendedOption    string               `json:"recommended_option,omitempty"`
	RecommendationReason string               `json:"recommendation_reason,omitempty"`
	RejectOptionID       string               `json:"reject_option_id,omitempty"`
	SuggestsUserInput    bool                 `json:"suggests_user_input"`
	ToolApprovalBinding  *ToolApprovalBinding `json:"tool_approval_binding,omitempty"`
	CreatedAt            time.Time            `json:"created_at"`
}

type ApprovalStatus string

const (
	ApprovalStatusPending  ApprovalStatus = "pending"
	ApprovalStatusApproved ApprovalStatus = "approved"
	ApprovalStatusRejected ApprovalStatus = "rejected"
	ApprovalStatusExpired  ApprovalStatus = "expired"
	ApprovalStatusConsumed ApprovalStatus = "consumed"
)

type ApprovalSummary struct {
	Required         bool   `json:"required"`
	RequestID        string `json:"request_id,omitempty"`
	Status           string `json:"status,omitempty"`
	SelectedOptionID string `json:"selected_option_id,omitempty"`
	ExpiresAt        string `json:"expires_at,omitempty"`
}

type ApprovalRequest struct {
	ID                   string               `json:"id"`
	SessionID            string               `json:"session_id"`
	TaskID               string               `json:"task_id"`
	Category             string               `json:"category"`
	RiskLevel            string               `json:"risk_level"`
	GoalSummary          string               `json:"goal_summary"`
	Question             string               `json:"question"`
	Options              []DecisionOption     `json:"options"`
	RecommendedOption    string               `json:"recommended_option,omitempty"`
	RecommendationReason string               `json:"recommendation_reason,omitempty"`
	RejectOptionID       string               `json:"reject_option_id"`
	Status               string               `json:"status"`
	SelectedOptionID     string               `json:"selected_option_id,omitempty"`
	ActorChannel         string               `json:"actor_channel,omitempty"`
	ActorRef             string               `json:"actor_ref,omitempty"`
	ExpiresAt            string               `json:"expires_at"`
	DecidedAt            string               `json:"decided_at,omitempty"`
	ConsumedAt           string               `json:"consumed_at,omitempty"`
	ToolApprovalBinding  *ToolApprovalBinding `json:"tool_approval_binding,omitempty"`
	CreatedAt            string               `json:"created_at"`
	UpdatedAt            string               `json:"updated_at"`
}

// DecisionSummary is the Emotion-facing persisted view of one paused decision.
type DecisionSummary struct {
	TaskID          string           `json:"task_id"`
	Status          string           `json:"status"`
	FailClosed      bool             `json:"fail_closed"`
	Category        string           `json:"category"`
	RiskLevel       string           `json:"risk_level"`
	GoalSummary     string           `json:"goal_summary"`
	Question        string           `json:"question"`
	Options         []DecisionOption `json:"options,omitempty"`
	Approval        *ApprovalSummary `json:"approval,omitempty"`
	Report          *TaskReport      `json:"report,omitempty"`
	CreatedAt       string           `json:"created_at"`
	StatusEnteredAt string           `json:"status_entered_at"`
	Claimable       bool             `json:"claimable"`
}

// DecisionRequest is an escalation from Work to Emotion.
//
// Deprecated: superseded by DecisionPacket. Retained for reference only;
// do not use in new code.
type DecisionRequest struct {
	TaskID            string   `json:"task_id"`
	Question          string   `json:"question"`
	WhyBlocked        string   `json:"why_blocked"`
	Options           []string `json:"options,omitempty"`
	RecommendedOption string   `json:"recommended_option,omitempty"`
	RiskLevel         string   `json:"risk_level"` // "low", "medium", "high"
}

// DecisionResponse is Emotion's decision back to Work (append-only, delta-only).
type DecisionResponse struct {
	TaskID           string   `json:"task_id"`
	Decision         string   `json:"decision"`
	Reason           string   `json:"reason,omitempty"`
	ConstraintsDelta []string `json:"constraints_delta,omitempty"`
}
