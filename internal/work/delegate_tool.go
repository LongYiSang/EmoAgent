package work

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/tool"
)

const delegateToolDescription = `Delegate a high-effort or noisy sub-task to the Work subagent.

Use this when a task needs multiple tool calls, file inspection, or verification work that should stay out of the main conversation.
The result is a structured TaskReport JSON object.`

var delegateToolSchema = json.RawMessage(`{
	"type":"object",
	"properties":{
		"goal":{"type":"string"},
		"background":{"type":"string"},
		"constraints":{"type":"array","items":{"type":"string"}},
		"acceptance_criteria":{"type":"array","items":{"type":"string"}},
		"permission_scope":{"type":"string","enum":["read-only"]},
		"expression_brief":{
			"type":"object",
			"properties":{
				"tone":{"type":"string"},
				"directness":{"type":"string"},
				"user_preference_hints":{"type":"array","items":{"type":"string"}}
			},
			"additionalProperties":false
		}
	},
	"required":["goal","permission_scope"],
	"additionalProperties":false
}`)

// NewDelegateTool builds the Emotion-facing delegate_to_work tool without
// wiring it into the app-level registry.
func NewDelegateTool(runtime *Runtime, journalDir string, logger *slog.Logger) (tool.Spec, tool.Handler) {
	spec := tool.Spec{
		Name:        "delegate_to_work",
		Description: delegateToolDescription,
		Parameters:  delegateToolSchema,
		Scope:       tool.ScopeEmotion,
		Permission:  tool.PermReadOnly,
	}

	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var brief protocol.TaskBrief
		if err := json.Unmarshal(input, &brief); err != nil {
			return nil, fmt.Errorf("delegate_to_work: invalid input: %w", err)
		}
		if err := ValidateAndComplete(&brief); err != nil {
			return nil, fmt.Errorf("delegate_to_work: %w", err)
		}

		journal, err := Open(journalDir, brief.TaskID, time.Now().UTC(), logger)
		if err != nil {
			if logger != nil {
				logger.Warn("delegate_to_work journal disabled", "error", err)
			}
			journal = nil
		}
		defer func() {
			if closeErr := journal.Close(); closeErr != nil && logger != nil {
				logger.Warn("delegate_to_work journal close failed", "error", closeErr)
			}
		}()

		journal.Write("task_start", 0, brief)
		report := runtime.Run(ctx, brief, journal)
		journal.Write("task_end", 0, report)

		output, err := json.Marshal(report)
		if err != nil {
			return nil, fmt.Errorf("delegate_to_work: marshal report: %w", err)
		}
		return output, nil
	}

	return spec, handler
}
