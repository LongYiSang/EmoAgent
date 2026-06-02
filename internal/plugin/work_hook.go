package plugin

import (
	"context"
	"strings"

	"github.com/longyisang/emoagent/internal/protocol"
)

type WorkAnnotator struct {
	host *PluginHost
}

func NewWorkAnnotator(host *PluginHost) *WorkAnnotator {
	if host == nil || !host.Enabled() {
		return nil
	}
	return &WorkAnnotator{host: host}
}

func (a *WorkAnnotator) AnnotateTaskBrief(ctx context.Context, brief *protocol.TaskBrief) error {
	if a == nil || a.host == nil || a.host.bus == nil || brief == nil {
		return nil
	}
	hc := hookContextFromCorrelation(ctx, HookWorkDispatchAnnotate)
	hc.Work = &WorkView{
		TaskID:          brief.TaskID,
		GoalSummary:     brief.Goal,
		PermissionScope: brief.PermissionScope,
		ReadScope:       brief.ReadScope,
		ConstraintCount: len(brief.Constraints),
		AcceptanceCount: len(brief.AcceptanceCriteria),
	}
	result, err := a.host.bus.Dispatch(ctx, HookWorkDispatchAnnotate, hc)
	if err != nil {
		return err
	}
	for _, patch := range result.Patches {
		value, ok := patch.Value.(string)
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		switch patch.Type {
		case PatchWorkAddConstraintHint:
			if !containsString(brief.Constraints, value) {
				brief.Constraints = append(brief.Constraints, value)
			}
		case PatchWorkAddAcceptanceHint:
			if !containsString(brief.AcceptanceCriteria, value) {
				brief.AcceptanceCriteria = append(brief.AcceptanceCriteria, value)
			}
		}
	}
	return nil
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
