package turn

import (
	"context"
	"errors"
	"time"

	"github.com/longyisang/emoagent/internal/protocol"
)

type InboundSource string

const (
	SourceWebUI  InboundSource = "webui"
	SourceSystem InboundSource = "system"
)

type InboundKind string

const (
	InboundUserMessage    InboundKind = "user_message"
	InboundApprovalAction InboundKind = "approval_action"
	InboundSystemResume   InboundKind = "system_resume"
)

type InboundEnvelope struct {
	EnvelopeID     string
	Source         InboundSource
	SourceEventID  string
	IdempotencyKey string

	Kind       InboundKind
	ReceivedAt time.Time

	PersonaKey  string
	SessionID   string
	RequestID   string
	Content     string
	UserMessage *UserMessageInput
	Approval    *InboundApproval

	Traceparent string
	RawMeta     map[string]any
}

type UserMessageInput struct {
	Content string
}

type InboundApproval struct {
	RequestID string
	Action    string
	OptionID  string
}

type TurnState string

const (
	StateCreated                 TurnState = "created"
	StateNormalizing             TurnState = "normalizing"
	StateSessionBound            TurnState = "session_bound"
	StateMemoryPrepared          TurnState = "memory_prepared"
	StateEmotionPrepared         TurnState = "emotion_prepared"
	StateRunningEmotion          TurnState = "running_emotion"
	StateSynthesizing            TurnState = "synthesizing"
	StateOutboundCommitting      TurnState = "outbound_committing"
	StateMemoryCommitting        TurnState = "memory_committing"
	StateApprovalWait            TurnState = "approval_wait"
	StateDone                    TurnState = "done"
	StateFailed                  TurnState = "failed"
	StateCanceled                TurnState = "canceled"
	StateCommitFailedAfterOutput TurnState = "commit_failed_after_output"
)

type StageName string

const (
	StageIngress         StageName = "ingress"
	StageNormalize       StageName = "normalize"
	StageSessionBind     StageName = "session_bind"
	StageMemoryPrepare   StageName = "memory_prepare"
	StageEmotionPrepare  StageName = "emotion_prepare"
	StageEmotionLoop     StageName = "emotion_loop"
	StageApprovalWait    StageName = "approval_wait"
	StageSynthesizeReply StageName = "synthesize_reply"
	StageOutboundCommit  StageName = "outbound_commit"
	StageMemoryCommit    StageName = "memory_commit"
	StageDone            StageName = "done"
	StageApprovalApply   StageName = "approval_apply"
	StageResume          StageName = "resume"
)

var ErrApprovalPending = errors.New("approval pending")

type TurnContext struct {
	TurnID    string
	State     TurnState
	StartedAt time.Time

	Inbound     InboundEnvelope
	Journal     TurnJournal
	Stream      OutboundSink
	Diagnostics map[string]any
}

type Stage interface {
	Name() StageName
	Run(ctx context.Context, tc *TurnContext) (StageResult, error)
}

type StageFunc struct {
	NameValue StageName
	RunFunc   func(ctx context.Context, tc *TurnContext) (StageResult, error)
}

func (s StageFunc) Name() StageName {
	return s.NameValue
}

func (s StageFunc) Run(ctx context.Context, tc *TurnContext) (StageResult, error) {
	if s.RunFunc == nil {
		return StageResult{}, nil
	}
	return s.RunFunc(ctx, tc)
}

type StageResult struct {
	NextState TurnState
	Terminal  bool
	Status    string
	Err       error
	ErrorKind string

	Outbound []OutboundEvent
	Metrics  StageMetrics
}

type StageMetrics struct {
	Stage      StageName
	DurationMS int64
}

type OutboundEvent struct {
	Seq       int64
	TurnID    string
	Type      string
	Content   string
	Payload   map[string]any
	Tool      *ToolActivity
	Reasoning *ReasoningActivity
	Approval  *ApprovalActivity
	CreatedAt time.Time
	Safe      bool
}

type ToolActivity struct {
	ID          string
	Name        string
	Status      string
	DurationMS  int64
	Preview     string
	Size        int
	Hash        string
	IsTruncated bool
}

type ReasoningActivity struct {
	ID         string
	Status     string
	Content    string
	DurationMS int64
	Provider   string
	Model      string
	Kind       string
}

type ApprovalActivity struct {
	Request *protocol.ApprovalRequest
}
