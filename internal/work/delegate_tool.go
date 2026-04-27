package work

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/longyisang/emoagent/internal/progress"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/tool"
)

const delegateToolDescription = `Delegate a high-effort or noisy sub-task to the Work subagent.

Use this when a task needs multiple tool calls, file inspection, or verification work that should stay out of the main conversation.
Give Work an outcome, not a script:
- goal is the concrete result to produce
- background is only the relevant context Work needs
- constraints are hard limits, files, permissions, and things not to do
- acceptance_criteria must contain at least one observable success condition

Permission guidance:
- use read-only for analysis only
- use workspace-write for non-destructive writes/edits
- use approved-destructive when the goal includes delete/remove/move/rename/overwrite or equivalent irreversible file operations
- approved-destructive may only be used after explicit user approval

The result is one of:
1. A TaskReport JSON (task completed normally)
2. A {"status":"needs_emotion_decision","task_id":"...","decision_packet":{...}} JSON (task paused, needs your decision)

When you receive needs_emotion_decision: you are the main agent. Read the decision_packet carefully. Use your persona, conversation history, and relationship memory to decide. If you can decide confidently, call resume_work immediately. Only ask the user if you genuinely lack information they have never provided.`

var delegateToolSchema = json.RawMessage(`{
	"type":"object",
	"properties":{
		"goal":{"type":"string"},
		"background":{"type":"string"},
		"constraints":{"type":"array","items":{"type":"string"}},
		"acceptance_criteria":{"type":"array","items":{"type":"string"},"minItems":1},
		"permission_scope":{"type":"string","enum":["read-only","workspace-write","approved-destructive"]}
	},
	"required":["goal","acceptance_criteria","permission_scope"],
	"additionalProperties":false
}`)

// NewDelegateTool builds the Emotion-facing delegate_to_work tool without
// wiring it into the app-level registry.
func NewDelegateTool(runtime *Runtime, pending *PendingRegistry, journalDir string, logger *slog.Logger) (tool.Spec, tool.Handler) {
	return NewDelegateToolWithFactory(func() (*Runtime, error) { return runtime, nil }, pending, journalDir, logger)
}

func NewDelegateToolWithFactory(runtimeFactory func() (*Runtime, error), pending *PendingRegistry, journalDir string, logger *slog.Logger) (tool.Spec, tool.Handler) {
	spec := tool.Spec{
		Name:        "delegate_to_work",
		Description: delegateToolDescription,
		Parameters:  delegateToolSchema,
		Scope:       tool.ScopeEmotion,
		Permission:  tool.PermReadOnly,
	}

	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var brief protocol.TaskBrief
		if err := decodeStrictJSON(input, &brief); err != nil {
			return nil, fmt.Errorf("delegate_to_work: invalid input: %w", err)
		}
		if err := ValidateAndComplete(&brief); err != nil {
			return nil, fmt.Errorf("delegate_to_work: %w", err)
		}
		runtime, err := runtimeFactory()
		if err != nil {
			return nil, fmt.Errorf("delegate_to_work: %w", err)
		}
		if runtime == nil {
			return nil, fmt.Errorf("delegate_to_work: work runtime is not configured")
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
		outcome := runtime.Run(ctx, brief, journal)
		progressCB := progress.CallbackFromContext(ctx)
		if outcome.Report != nil {
			if progressCB != nil {
				progressCB(progress.Event{
					Kind:   progress.KindEnd,
					TaskID: brief.TaskID,
				})
			}
			journal.Write("task_end", 0, outcome.Report)
			output, err := json.Marshal(outcome.Report)
			if err != nil {
				return nil, fmt.Errorf("delegate_to_work: marshal report: %w", err)
			}
			return output, nil
		}
		if outcome.Paused == nil {
			return nil, fmt.Errorf("delegate_to_work: runtime returned empty outcome")
		}

		sessionID := SessionIDFromContext(ctx)
		if pending != nil {
			if err := pending.Put(sessionID, outcome.Paused.TaskID, outcome.Paused); err != nil {
				return nil, fmt.Errorf("delegate_to_work: persist paused task: %w", err)
			}
		}
		if journal != nil {
			journal.Write("task_paused", outcome.Paused.Round, map[string]any{
				"task_id":  outcome.Paused.TaskID,
				"category": outcome.Paused.Packet.Category,
				"risk":     derivedRiskLevel(outcome.Paused.Packet.Category),
			})
		}
		if progressCB != nil {
			progressCB(progress.Event{
				Kind:   progress.KindPaused,
				Round:  outcome.Paused.Round,
				TaskID: outcome.Paused.TaskID,
			})
		}

		output, err := json.Marshal(NeedsEmotionDecision{
			Status:         "needs_emotion_decision",
			TaskID:         outcome.Paused.TaskID,
			DecisionPacket: outcome.Paused.Packet,
		})
		if err != nil {
			return nil, fmt.Errorf("delegate_to_work: marshal paused outcome: %w", err)
		}
		return output, nil
	}

	return spec, handler
}
