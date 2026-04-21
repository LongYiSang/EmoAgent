package work

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/tool"
)

func TestRuntime_CompressesOnSoftLimit(t *testing.T) {
	summaryLLM := &scriptedLLM{
		responses: []*llm.ChatResponse{{
			Content: `{"work_progress":{"task_goal":"test","steps_completed":["did stuff"],"key_findings":[],"errors_encountered":[],"current_approach":"finishing","decisions_received":[]}}`,
		}},
	}

	finishPayload, _ := json.Marshal(map[string]any{
		"status":  "completed",
		"summary": "done",
	})

	mainLLM := &scriptedLLM{
		responses: []*llm.ChatResponse{
			toolUseResp("c1", "echo_large", `{"x":"a"}`),
			toolUseResp("c2", "echo_large", `{"x":"b"}`),
			toolUseResp("c3", "echo_large", `{"x":"c"}`),
			toolUseResp("c4", "finish_task", string(finishPayload)),
		},
	}

	registry := tool.NewRegistry()
	registry.Register(tool.Spec{
		Name:        "echo_large",
		Description: "returns large content",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}},"additionalProperties":false}`),
		Scope:       tool.ScopeWork,
		Permission:  tool.PermReadOnly,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(largeContent(5000)), nil
	})
	registry.Register(NewFinishTaskTool(), FinishTaskPlaceholderHandler)
	registry.Register(NewRequestDecisionTool(), RequestDecisionPlaceholderHandler)

	dispatcher := tool.NewDispatcher(registry, tool.MinimalSchemaValidator{}, testLogger())

	rt := NewRuntime(RuntimeConfig{
		LLM:                      mainLLM,
		SummaryClient:            summaryLLM,
		SummaryModel:             "test-summary",
		Provider:                 "openai",
		Model:                    "test",
		MaxTokens:                4096,
		MaxToolRounds:            10,
		MaxInputTokens:           2500,
		CompressSoftRatio:        0.7,
		CompressKeepRounds:       2,
		ToolSnipSoftTokens:       500,
		ToolSnipHardTokens:       2000,
		Registry:                 registry,
		Dispatcher:               dispatcher,
		Logger:                   testLogger(),
		MaxEscalations:           3,
		PendingSnapshotMaxTokens: 4000,
	})

	brief := protocol.TaskBrief{
		TaskID:          "t1",
		Goal:            "test compression",
		PermissionScope: "read-only",
	}

	outcome := rt.Run(context.Background(), brief, nil)
	if outcome.Report == nil {
		t.Fatal("expected a report")
	}
	if outcome.Report.Status != "completed" {
		t.Fatalf("status = %q, want completed", outcome.Report.Status)
	}
	if len(summaryLLM.calls) == 0 {
		t.Fatal("expected summary LLM to be called for compression")
	}
}
