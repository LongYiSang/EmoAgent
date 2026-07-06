package chat

import (
	"context"
	"encoding/json"
	"testing"

	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/work"
)

func TestEmotionToolsForPromptModeFiltersWorkBridgeToolsInCasualMode(t *testing.T) {
	registry := tool.NewRegistry()
	registerTestTool(t, registry, "web_search", tool.ScopeBoth, tool.RoutingClassCasual)
	registerTestTool(t, registry, "weather", tool.ScopeEmotion, tool.RoutingClassCasual)
	registerTestTool(t, registry, "emotion_note", tool.ScopeEmotion)
	registerTestTool(t, registry, work.ToolNameDelegateToWork, tool.ScopeEmotion)
	registerTestTool(t, registry, work.ToolNameResumeWork, tool.ScopeEmotion)
	registerTestTool(t, registry, work.ToolNameListPendingDecisions, tool.ScopeEmotion)

	tools := emotionToolsForPromptMode(registry, contextutil.PromptModeCasualChat)
	names := toolNames(tools)
	for _, forbidden := range []string{work.ToolNameDelegateToWork, work.ToolNameResumeWork, work.ToolNameListPendingDecisions} {
		if names[forbidden] {
			t.Fatalf("casual tools include %s: %#v", forbidden, tools)
		}
	}
	for _, wanted := range []string{"web_search", "weather"} {
		if !names[wanted] {
			t.Fatalf("casual tools missing %s: %#v", wanted, tools)
		}
	}
	if names["emotion_note"] {
		t.Fatalf("casual tools include default work tool: %#v", tools)
	}
}

func TestEmotionToolsForPromptModeKeepsWorkBridgeToolsInWorkMode(t *testing.T) {
	registry := tool.NewRegistry()
	registerTestTool(t, registry, "web_search", tool.ScopeBoth, tool.RoutingClassCasual)
	registerTestTool(t, registry, work.ToolNameDelegateToWork, tool.ScopeEmotion)
	registerTestTool(t, registry, work.ToolNameResumeWork, tool.ScopeEmotion)
	registerTestTool(t, registry, work.ToolNameListPendingDecisions, tool.ScopeEmotion)

	tools := emotionToolsForPromptMode(registry, contextutil.PromptModeWorkMode)
	names := toolNames(tools)
	for _, wanted := range []string{"web_search", work.ToolNameDelegateToWork, work.ToolNameResumeWork, work.ToolNameListPendingDecisions} {
		if !names[wanted] {
			t.Fatalf("work tools missing %s: %#v", wanted, tools)
		}
	}
}

func registerTestTool(t *testing.T, registry *tool.Registry, name string, scope tool.Scope, routingClasses ...tool.RoutingClass) {
	t.Helper()
	routingClass := tool.RoutingClassWork
	if len(routingClasses) > 0 {
		routingClass = routingClasses[0]
	}
	registry.Register(tool.Spec{
		Name:         name,
		Parameters:   json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Scope:        scope,
		Permission:   tool.PermReadOnly,
		RoutingClass: routingClass,
	}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	})
}

func toolNames(tools []llm.ToolDef) map[string]bool {
	out := make(map[string]bool, len(tools))
	for _, item := range tools {
		out[item.Name] = true
	}
	return out
}
