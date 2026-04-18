package protocol

import "time"

// TaskBrief is the task contract from Emotion to Work.
type TaskBrief struct {
	TaskID             string           `json:"task_id"`
	Goal               string           `json:"goal"`
	Background         string           `json:"background,omitempty"`
	Constraints        []string         `json:"constraints,omitempty"`
	AcceptanceCriteria []string         `json:"acceptance_criteria,omitempty"`
	PermissionScope    string           `json:"permission_scope"`
	ExpressionBrief    *ExpressionBrief `json:"expression_brief,omitempty"`
	CreatedAt          time.Time        `json:"created_at"`
}

// ExpressionBrief carries style hints for Work's output formatting.
type ExpressionBrief struct {
	Tone                string   `json:"tone,omitempty"`
	Directness          string   `json:"directness,omitempty"`
	UserPreferenceHints []string `json:"user_preference_hints,omitempty"`
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
	CatExecutionOnly         EscalationCategory = "execution_only"
	CatPreferenceSensitive   EscalationCategory = "preference_sensitive"
	CatEmotionSensitive      EscalationCategory = "emotion_sensitive"
	CatToneSensitive         EscalationCategory = "tone_sensitive"
	CatRelationshipSensitive EscalationCategory = "relationship_sensitive"
	CatAmbiguousGoal         EscalationCategory = "ambiguous_goal"
	CatStrategyShift         EscalationCategory = "strategy_shift"
	CatHighRisk              EscalationCategory = "high_risk"
	CatIrreversible          EscalationCategory = "irreversible"
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

// DecisionPacket is the structured escalation payload from Work to Emotion.
// It carries summarized context without leaking raw Work traces.
type DecisionPacket struct {
	TaskID               string             `json:"task_id"`
	Category             EscalationCategory `json:"category"`
	RiskLevel            string             `json:"risk_level"`
	GoalSummary          string             `json:"goal_summary"`
	Question             string             `json:"question"`
	WhyBlocked           string             `json:"why_blocked"`
	Options              []DecisionOption   `json:"options"`
	RelevantFindings     []DecisionEvidence `json:"relevant_findings,omitempty"`
	KeyTradeoffs         []DecisionTradeoff `json:"key_tradeoffs,omitempty"`
	RecommendedOption    string             `json:"recommended_option,omitempty"`
	RecommendationReason string             `json:"recommendation_reason,omitempty"`
	SuggestsUserInput    bool               `json:"suggests_user_input"`
	CreatedAt            time.Time          `json:"created_at"`
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
	StyleDelta       string   `json:"style_delta,omitempty"`
}
