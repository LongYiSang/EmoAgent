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
	tools := registry.ForScope(tool.ScopeEmotion)
	if contextutil.NormalizePromptMode(mode) == contextutil.PromptModeWorkMode {
		return tools
	}
	return filterOutWorkBridgeTools(tools)
}

func filterOutWorkBridgeTools(tools []llm.ToolDef) []llm.ToolDef {
	out := make([]llm.ToolDef, 0, len(tools))
	for _, item := range tools {
		switch item.Name {
		case work.ToolNameDelegateToWork, work.ToolNameResumeWork, work.ToolNameListPendingDecisions:
			continue
		default:
			out = append(out, item)
		}
	}
	return out
}
