package chat

import (
	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/work"
)

func emotionToolsForPromptMode(registry *tool.Registry, mode contextutil.PromptMode) []llm.ToolDef {
	if registry == nil {
		return nil
	}
	specs := registry.Specs()
	if contextutil.NormalizePromptMode(mode) == contextutil.PromptModeWorkMode {
		out := make([]llm.ToolDef, 0, len(specs))
		for _, spec := range specs {
			if toolMatchesScope(spec, tool.ScopeEmotion) {
				out = append(out, spec.ToToolDef())
			}
		}
		return out
	}
	out := make([]llm.ToolDef, 0, len(specs))
	for _, spec := range specs {
		if !casualToolVisibleToEmotion(spec) {
			continue
		}
		out = append(out, spec.ToToolDef())
	}
	return out
}

func promptRouteCasualToolHints(registry *tool.Registry) []PromptRouteToolHint {
	if registry == nil {
		return nil
	}
	specs := registry.Specs()
	out := make([]PromptRouteToolHint, 0, len(specs))
	for _, spec := range specs {
		if casualToolVisibleToEmotion(spec) {
			out = append(out, PromptRouteToolHint{Name: spec.Name, Description: spec.Description})
		}
	}
	return out
}

func casualToolVisibleToEmotion(spec tool.Spec) bool {
	return toolMatchesScope(spec, tool.ScopeEmotion) &&
		tool.NormalizeRoutingClass(spec.RoutingClass) == tool.RoutingClassCasual &&
		!isWorkBridgeToolName(spec.Name)
}

func toolMatchesScope(spec tool.Spec, scope tool.Scope) bool {
	return spec.Scope == scope || spec.Scope == tool.ScopeBoth
}

func isWorkBridgeToolName(name string) bool {
	switch name {
	case work.ToolNameDelegateToWork, work.ToolNameResumeWork, work.ToolNameListPendingDecisions:
		return true
	default:
		return false
	}
}
