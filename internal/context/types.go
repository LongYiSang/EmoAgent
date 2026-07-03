package context

import (
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/promptcenter"
	"github.com/longyisang/emoagent/internal/storage"
)

const CurrentContextVersion = 1

type PromptMode string

const (
	PromptModeCasualChat PromptMode = "casual_chat"
	PromptModeWorkMode   PromptMode = "work_mode"
)

func NormalizePromptMode(mode PromptMode) PromptMode {
	switch mode {
	case PromptModeCasualChat:
		return PromptModeCasualChat
	case PromptModeWorkMode, "":
		return PromptModeWorkMode
	default:
		return PromptModeWorkMode
	}
}

func NormalizePromptModeOrEmpty(mode PromptMode) PromptMode {
	switch mode {
	case PromptModeCasualChat, PromptModeWorkMode:
		return mode
	default:
		return ""
	}
}

// Budget captures the configured limits and current estimate for one request.
type Budget struct {
	InputBudgetTokens   int
	SoftLimitTokens     int
	HardLimitTokens     int
	ReserveOutputTokens int
	EstimatedTokens     int
}

// CompactReport describes the deterministic compact decisions for one request.
type CompactReport struct {
	SessionID                    string
	Mode                         string
	CompactReason                string
	PreEstimatedTokens           int
	PostEstimatedTokens          int
	KeptRecentTurns              int
	SnippedToolResultsCount      int
	SummaryCoveredUntilMessageID string
	SummaryModel                 string
	Degraded                     bool

	KeptRecentUserTurns int
	SnippedToolResults  int
	UsedToolDigest      bool
}

// SummaryUpdateReport captures one attempted or skipped running-summary update.
type SummaryUpdateReport struct {
	Attempted          bool
	Skipped            bool
	SkipReason         string
	SummaryModel       string
	DeltaCount         int
	CoveredUntilBefore string
	CoveredUntilAfter  string
	DurationMS         int64
	StopReason         string
	RawStopReason      string
	ContentLength      int
	ReasoningLength    int
	FailureCount       int
	RetryAfter         string
	PromptAudit        *SummaryPromptAudit
	RepairPromptAudit  *SummaryPromptAudit
}

type SummaryPromptAudit struct {
	Purpose      string
	SystemPrompt string
	Components   []promptcenter.RenderComponent
	Model        string
	Attempted    bool
}

type ContextResetBarrier struct {
	Epoch          int    `json:"epoch,omitempty"`
	AfterMessageID string `json:"after_message_id,omitempty"`
	ResetAt        string `json:"reset_at,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

// ContextState is the persisted session-level context metadata stored in sessions.metadata.
type ContextState struct {
	ContextVersion               int                  `json:"context_version"`
	Mode                         Mode                 `json:"mode"`
	PromptRoute                  PromptRouteState     `json:"prompt_route,omitempty"`
	RunningSummary               RunningSummary       `json:"running_summary"`
	SummaryCoveredUntilMessageID string               `json:"summary_covered_until_message_id"`
	SummaryUpdatedAt             string               `json:"summary_updated_at"`
	SummaryFailedAt              string               `json:"summary_failed_at,omitempty"`
	SummaryRetryAfter            string               `json:"summary_retry_after,omitempty"`
	SummaryFailureCount          int                  `json:"summary_failure_count,omitempty"`
	SummaryLastError             string               `json:"summary_last_error,omitempty"`
	LastCompactReason            string               `json:"last_compact_reason"`
	LastInputEstimate            int                  `json:"last_input_estimate"`
	KeepRecentUserTurns          int                  `json:"keep_recent_user_turns"`
	LastContextStats             *ContextStats        `json:"last_context_stats,omitempty"`
	ResetBarrier                 *ContextResetBarrier `json:"reset_barrier,omitempty"`
}

type PromptRouteState struct {
	LastMode            PromptMode `json:"last_mode,omitempty"`
	WorkStickyRemaining int        `json:"work_sticky_remaining,omitempty"`
}

type EmotionContextOptions struct {
	PromptMode PromptMode
}

// RunningSummary is the structured rolling summary injected ahead of recent turns.
type RunningSummary struct {
	SessionGoal       string            `json:"session_goal"`
	UserFacts         []string          `json:"user_facts"`
	RelationshipState RelationshipState `json:"relationship_state"`
	OpenLoops         []string          `json:"open_loops"`
	Decisions         []string          `json:"decisions"`
	DoNotForget       []string          `json:"do_not_forget"`
}

// RelationshipState captures the assistant/user interaction state preserved in the running summary.
type RelationshipState struct {
	Tone          string   `json:"tone"`
	RecentEmotion string   `json:"recent_emotion"`
	PromisesMade  []string `json:"promises_made"`
}

// ToolDigest is the transport-safe representation of a tool result after snipping.
type ToolDigest struct {
	ToolName    string
	CallID      string
	Size        int
	Preview     string
	Hash        string
	FullContent string
	IsTruncated bool
}

// AssembledContext is the final context sent to the model.
type AssembledContext struct {
	System           string
	PromptComponents []promptcenter.RenderComponent
	ToolDigests      []ToolDigest
	Messages         []llm.Message
	Budget           Budget
	CompactReport    CompactReport
}

type ContextStats struct {
	SessionID                 string `json:"session_id,omitempty"`
	TurnID                    string `json:"turn_id,omitempty"`
	RequestID                 string `json:"request_id,omitempty"`
	ProviderID                string `json:"provider_id,omitempty"`
	Model                     string `json:"model,omitempty"`
	Round                     int    `json:"round"`
	EstimatedInputTokens      int    `json:"estimated_input_tokens"`
	ContextLimitTokens        int    `json:"context_limit_tokens"`
	InputBudgetTokens         int    `json:"input_budget_tokens"`
	ReserveOutputTokens       int    `json:"reserve_output_tokens"`
	MaxOutputTokens           int    `json:"max_output_tokens"`
	RawHistoryEstimatedTokens int    `json:"raw_history_estimated_tokens"`
	CompactReason             string `json:"compact_reason,omitempty"`
	Source                    string `json:"source,omitempty"`
	ProviderInputTokens       int    `json:"provider_input_tokens,omitempty"`
	ProviderOutputTokens      int    `json:"provider_output_tokens,omitempty"`
	UpdatedAt                 string `json:"updated_at,omitempty"`
}

type ContextStatsInput struct {
	SessionID     string
	TurnID        string
	RequestID     string
	ProviderID    string
	Round         int
	Request       llm.ChatRequest
	RawHistory    []storage.MessageRecord
	ContextConfig config.ContextConfig
	CompactReason string
	Source        string
	UpdatedAt     string
}

// IsZero reports whether the running summary is empty and can be skipped during assembly.
func (s RunningSummary) IsZero() bool {
	return s.SessionGoal == "" &&
		len(s.UserFacts) == 0 &&
		s.RelationshipState.Tone == "" &&
		s.RelationshipState.RecentEmotion == "" &&
		len(s.RelationshipState.PromisesMade) == 0 &&
		len(s.OpenLoops) == 0 &&
		len(s.Decisions) == 0 &&
		len(s.DoNotForget) == 0
}
