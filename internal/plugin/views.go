package plugin

import (
	"time"

	"github.com/longyisang/emoagent/internal/turn"
)

type TurnView struct {
	TurnID           string
	State            turn.TurnState
	Kind             turn.InboundKind
	SessionID        string
	PersonaKey       string
	RequestID        string
	UserContentBytes int
	UserContentHash  string
	StartedAt        time.Time
}

type MemoryView struct {
	Prepared       bool
	Retrieved      bool
	RecordMetadata bool
	Blocks         []SafeMemoryBlock
	Diagnostics    map[string]any
}

type SafeMemoryBlock struct {
	BlockType     string
	Summary       string
	UsageGuidance string
	Confidence    float64
	NodeRef       *NodeRef
}

type NodeRef struct {
	NodeType string
	NodeID   string
}

type ToolCallView struct {
	CallID             string
	Name               string
	AgentScope         string
	RequiredPermission string
	Action             string
	InputBytes         int
	InputHash          string
	ResultStatus       string
	ResultBytes        int
	ResultHash         string
}

type WorkView struct {
	TaskID            string
	GoalSummary       string
	PermissionScope   string
	ReadScope         string
	ConstraintCount   int
	AcceptanceCount   int
	DecisionCategory  string
	DecisionRiskLevel string
	ApprovalRequestID string
	ApprovalStatus    string
}

type OutboundView struct {
	Type         string
	TurnID       string
	Seq          int64
	ContentBytes int
	ContentHash  string
	HasTool      bool
	HasReasoning bool
	HasApproval  bool
	Safe         bool
}

func NewHookContext(tc *turn.TurnContext, hook HookName, stage turn.StageName) HookContext {
	view := TurnView{}
	if tc != nil {
		content := ""
		if tc.Inbound.UserMessage != nil {
			content = tc.Inbound.UserMessage.Content
		}
		view = TurnView{
			TurnID:           tc.TurnID,
			State:            tc.State,
			Kind:             tc.Inbound.Kind,
			SessionID:        tc.Inbound.SessionID,
			PersonaKey:       tc.Inbound.PersonaKey,
			RequestID:        tc.Inbound.RequestID,
			UserContentBytes: len([]byte(content)),
			UserContentHash:  contentHash(content),
			StartedAt:        tc.StartedAt,
		}
	}
	return HookContext{
		Envelope: HookEnvelope{
			Hook:       hook,
			TurnID:     view.TurnID,
			Stage:      stage,
			State:      view.State,
			SessionID:  view.SessionID,
			PersonaKey: view.PersonaKey,
		},
		Turn: view,
	}
}

func MemoryViewFromDiagnostics(diagnostics map[string]any) *MemoryView {
	if diagnostics == nil {
		return nil
	}
	view := &MemoryView{Diagnostics: map[string]any{}}
	if _, ok := diagnostics["memory_anchor"]; ok {
		view.Prepared = true
	}
	if _, ok := diagnostics["memory_prompt_snapshot"]; ok {
		view.Retrieved = true
	}
	if _, ok := diagnostics["memory_prompt_block"].(string); ok {
		view.Retrieved = true
		view.Diagnostics["has_prompt_block"] = true
	}
	if snapshot, ok := diagnostics["memory_prompt_snapshot"].(interface{ RecordPluginSafeMemoryView() MemoryView }); ok {
		safe := snapshot.RecordPluginSafeMemoryView()
		return &safe
	}
	if len(view.Diagnostics) == 0 && !view.Prepared && !view.Retrieved {
		return nil
	}
	return view
}

func OutboundViewFromEvent(event turn.OutboundEvent) OutboundView {
	return OutboundView{
		Type:         event.Type,
		TurnID:       event.TurnID,
		Seq:          event.Seq,
		ContentBytes: len([]byte(event.Content)),
		ContentHash:  contentHash(event.Content),
		HasTool:      event.Tool != nil,
		HasReasoning: event.Reasoning != nil,
		HasApproval:  event.Approval != nil,
		Safe:         event.Safe,
	}
}
