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

// DecisionRequest is an escalation from Work to Emotion.
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
