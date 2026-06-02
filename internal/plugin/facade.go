package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type Facades struct {
	Memory   *MemoryFacade
	Work     *WorkFacade
	Approval *ApprovalFacade
}

func NewFacades(pluginID string, authorizer *Authorizer) Facades {
	return Facades{
		Memory:   &MemoryFacade{pluginID: pluginID, authorizer: authorizer},
		Work:     &WorkFacade{pluginID: pluginID, authorizer: authorizer},
		Approval: &ApprovalFacade{pluginID: pluginID, authorizer: authorizer},
	}
}

type MemoryFacade struct {
	pluginID   string
	authorizer *Authorizer
}

type PluginMemoryCandidate struct {
	Summary         string
	EvidenceRefs    []string
	CandidateType   string
	Confidence      float64
	SensitivityHint string
}

type PluginMemoryCandidateResult struct {
	ID       string
	PluginID string
	Status   string
}

type ForgetLevel string

const (
	ForgetLevelSoft         ForgetLevel = "soft_forget"
	ForgetLevelHard         ForgetLevel = "hard_forget"
	ForgetLevelSourceRedact ForgetLevel = "source_redact"
	ForgetLevelPurge        ForgetLevel = "purge"
)

type PluginForgetRequest struct {
	TargetSummary string
	NodeRef       *NodeRef
	Level         ForgetLevel
	Reason        string
	EvidenceRefs  []string
}

type PluginForgetRequestResult struct {
	ID             string
	PluginID       string
	Status         string
	RequestedLevel ForgetLevel
	FinalDecision  string
}

func (f *MemoryFacade) SubmitCandidate(ctx context.Context, candidate PluginMemoryCandidate) (PluginMemoryCandidateResult, error) {
	if err := ctx.Err(); err != nil {
		return PluginMemoryCandidateResult{}, err
	}
	if err := f.authorizer.Require(CapabilityMemoryCandidateSubmit); err != nil {
		return PluginMemoryCandidateResult{}, err
	}
	if strings.TrimSpace(candidate.Summary) == "" {
		return PluginMemoryCandidateResult{}, fmt.Errorf("candidate summary is required")
	}
	if candidate.Confidence < 0 || candidate.Confidence > 1 {
		return PluginMemoryCandidateResult{}, fmt.Errorf("candidate confidence must be between 0 and 1")
	}
	return PluginMemoryCandidateResult{ID: uuid.NewString(), PluginID: f.pluginID, Status: "queued"}, nil
}

func (f *MemoryFacade) RequestForget(ctx context.Context, request PluginForgetRequest) (PluginForgetRequestResult, error) {
	if err := ctx.Err(); err != nil {
		return PluginForgetRequestResult{}, err
	}
	if err := f.authorizer.Require(CapabilityMemoryForgetRequest); err != nil {
		return PluginForgetRequestResult{}, err
	}
	level := request.Level
	if level == "" {
		level = ForgetLevelSoft
	}
	switch level {
	case ForgetLevelSoft:
	case ForgetLevelHard, ForgetLevelSourceRedact, ForgetLevelPurge:
		if err := f.authorizer.Require(CapabilityMemoryForgetDestructive); err != nil {
			return PluginForgetRequestResult{}, err
		}
	default:
		return PluginForgetRequestResult{}, fmt.Errorf("unknown forget level %q", level)
	}
	if strings.TrimSpace(request.TargetSummary) == "" && request.NodeRef == nil {
		return PluginForgetRequestResult{}, fmt.Errorf("forget target is required")
	}
	return PluginForgetRequestResult{
		ID:             uuid.NewString(),
		PluginID:       f.pluginID,
		Status:         "requested",
		RequestedLevel: level,
		FinalDecision:  "pending_forget_manager",
	}, nil
}

type WorkFacade struct {
	pluginID   string
	authorizer *Authorizer
}

type WorkDispatchAnnotation struct {
	ConstraintHints []string
	AcceptanceHints []string
	BackgroundHints []string
}

type WorkDispatchPatch struct {
	PluginID        string
	ConstraintHints []string
	AcceptanceHints []string
	BackgroundHints []string
}

type DecisionPacketView struct {
	TaskID               string
	Category             string
	RiskLevel            string
	GoalSummary          string
	Question             string
	WhyBlocked           string
	RecommendedOption    string
	RejectOptionID       string
	SuggestsUserInput    bool
	ApprovalRequestID    string
	ApprovalStatus       string
	ToolName             string
	ToolApprovalKind     string
	OptionCount          int
	RelevantFindingCount int
}

func (f *WorkFacade) AnnotateTaskBrief(ctx context.Context, annotation WorkDispatchAnnotation) (WorkDispatchPatch, error) {
	if err := ctx.Err(); err != nil {
		return WorkDispatchPatch{}, err
	}
	if err := f.authorizer.Require(CapabilityWorkDispatchAnnotate); err != nil {
		return WorkDispatchPatch{}, err
	}
	return WorkDispatchPatch{
		PluginID:        f.pluginID,
		ConstraintHints: cleanStrings(annotation.ConstraintHints),
		AcceptanceHints: cleanStrings(annotation.AcceptanceHints),
		BackgroundHints: cleanStrings(annotation.BackgroundHints),
	}, nil
}

func (f *WorkFacade) ObserveDecisionPacket(ctx context.Context, view DecisionPacketView) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f.authorizer.Require(CapabilityWorkObserve); err != nil {
		return err
	}
	if strings.TrimSpace(view.TaskID) == "" {
		return fmt.Errorf("decision packet task id is required")
	}
	return nil
}

type ApprovalFacade struct {
	pluginID   string
	authorizer *Authorizer
}

type ApprovalLifecycleView struct {
	RequestID        string
	SessionID        string
	TaskID           string
	Category         string
	RiskLevel        string
	Status           string
	SelectedOptionID string
	ToolName         string
	ApprovalKind     string
}

func (f *ApprovalFacade) Observe(ctx context.Context, view ApprovalLifecycleView) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f.authorizer.Require(CapabilityApprovalObserve); err != nil {
		return err
	}
	if strings.TrimSpace(view.RequestID) == "" {
		return fmt.Errorf("approval request id is required")
	}
	return nil
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
