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

const resumeToolDescription = `Resume a paused Work task after making an Emotion-level decision.

Use this when delegate_to_work returned {"status":"needs_emotion_decision", ...}.
Provide task_id and your decision fields.`

var resumeToolSchema = json.RawMessage(`{
  "type":"object",
  "properties":{
    "task_id":{"type":"string"},
    "decision":{"type":"string"},
    "reason":{"type":"string"},
    "constraints_delta":{"type":"array","items":{"type":"string"}}
  },
  "required":["task_id","decision"],
  "additionalProperties":false
}`)

// NewResumeTool builds the Emotion-facing resume_work tool.
func NewResumeTool(runtime *Runtime, pending *PendingRegistry, journalDir string, logger *slog.Logger) (tool.Spec, tool.Handler) {
	spec := tool.Spec{
		Name:        "resume_work",
		Description: resumeToolDescription,
		Parameters:  resumeToolSchema,
		Scope:       tool.ScopeEmotion,
		Permission:  tool.PermReadOnly,
	}

	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var req struct {
			TaskID           string   `json:"task_id"`
			Decision         string   `json:"decision"`
			Reason           string   `json:"reason"`
			ConstraintsDelta []string `json:"constraints_delta"`
		}
		if err := decodeStrictJSON(input, &req); err != nil {
			return nil, fmt.Errorf("resume_work: invalid input: %w", err)
		}

		sessionID := SessionIDFromContext(ctx)
		var paused *PausedWork
		if pending != nil {
			paused = pending.Take(sessionID, req.TaskID)
		}
		if paused == nil {
			output, _ := json.Marshal(map[string]string{
				"status":  "expired",
				"task_id": req.TaskID,
			})
			return output, nil
		}

		journal, err := Open(journalDir, req.TaskID, time.Now().UTC(), logger)
		if err != nil {
			if logger != nil {
				logger.Warn("resume_work journal disabled", "error", err)
			}
			journal = nil
		}
		defer func() {
			if closeErr := journal.Close(); closeErr != nil && logger != nil {
				logger.Warn("resume_work journal close failed", "error", closeErr)
			}
		}()

		resp := protocol.DecisionResponse{
			TaskID:           req.TaskID,
			Decision:         req.Decision,
			Reason:           req.Reason,
			ConstraintsDelta: req.ConstraintsDelta,
		}
		if journal != nil {
			journal.Write("decision_response_emotion", paused.Round, map[string]any{
				"task_id":  req.TaskID,
				"decision": req.Decision,
				"reason":   req.Reason,
			})
		}

		outcome := runtime.Resume(ctx, paused, resp, journal)
		progressCB := progress.CallbackFromContext(ctx)
		if outcome.Report != nil {
			if progressCB != nil {
				progressCB(progress.Event{
					Kind:   progress.KindEnd,
					TaskID: req.TaskID,
				})
			}
			if journal != nil {
				journal.Write("task_end", 0, outcome.Report)
			}
			return json.Marshal(outcome.Report)
		}

		if outcome.Paused == nil {
			return nil, fmt.Errorf("resume_work: runtime returned empty outcome")
		}
		if pending != nil {
			pending.Put(sessionID, outcome.Paused.TaskID, outcome.Paused)
		}
		if journal != nil {
			journal.Write("task_paused", outcome.Paused.Round, map[string]any{
				"task_id":  outcome.Paused.TaskID,
				"category": outcome.Paused.Packet.Category,
				"risk":     outcome.Paused.Packet.RiskLevel,
			})
		}
		if progressCB != nil {
			progressCB(progress.Event{
				Kind:   progress.KindPaused,
				Round:  outcome.Paused.Round,
				TaskID: outcome.Paused.TaskID,
			})
		}
		return json.Marshal(NeedsEmotionDecision{
			Status:         "needs_emotion_decision",
			TaskID:         outcome.Paused.TaskID,
			DecisionPacket: outcome.Paused.Packet,
		})
	}

	return spec, handler
}
