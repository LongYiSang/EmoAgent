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
For ordinary decision pauses, provide task_id, decision, reason, and optional constraints_delta.
For permission_escalation_required pauses, pass the user's approve/reject answer as decision and include permission_scope_override="approved-destructive" only when the user approved.
For approval-gated pauses, provide task_id and approval_request_id only after the matching approval is available.
If an internal approval outcome note says Work has already resumed, do not call resume_work again.`

var resumeToolSchema = json.RawMessage(`{
  "type":"object",
  "properties":{
    "task_id":{"type":"string"},
    "decision":{"type":"string"},
    "reason":{"type":"string"},
    "constraints_delta":{"type":"array","items":{"type":"string"}},
    "approval_request_id":{"type":"string"},
    "permission_scope_override":{"type":"string","enum":["approved-destructive"]}
  },
  "required":["task_id"],
  "additionalProperties":false
}`)

// NewResumeTool builds the Emotion-facing resume_work tool.
func NewResumeTool(runtime *Runtime, pending *PendingRegistry, journalDir string, logger *slog.Logger) (tool.Spec, tool.Handler) {
	return NewResumeToolWithFactory(func() (*Runtime, error) { return runtime, nil }, pending, journalDir, logger)
}

func NewResumeToolWithFactory(runtimeFactory func() (*Runtime, error), pending *PendingRegistry, journalDir string, logger *slog.Logger) (tool.Spec, tool.Handler) {
	spec := tool.Spec{
		Name:        "resume_work",
		Description: resumeToolDescription,
		Parameters:  resumeToolSchema,
		Scope:       tool.ScopeEmotion,
		Permission:  tool.PermReadOnly,
	}

	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var req struct {
			TaskID                  string   `json:"task_id"`
			Decision                string   `json:"decision"`
			Reason                  string   `json:"reason"`
			ConstraintsDelta        []string `json:"constraints_delta"`
			ApprovalRequestID       string   `json:"approval_request_id"`
			PermissionScopeOverride string   `json:"permission_scope_override"`
		}
		if err := decodeStrictJSON(input, &req); err != nil {
			return nil, fmt.Errorf("resume_work: invalid input: %w", err)
		}

		sessionID := SessionIDFromContext(ctx)
		var claim ClaimResult
		if pending != nil {
			claim = pending.ClaimForResume(sessionID, req.TaskID)
		}
		paused := claim.PausedWork
		if paused == nil {
			status := "expired"
			if claim.FinalState == finalStateClaimed {
				status = "busy"
			}
			output, _ := json.Marshal(map[string]string{
				"status":      status,
				"task_id":     req.TaskID,
				"final_state": claim.FinalState,
			})
			return output, nil
		}
		runtime, err := runtimeFactory()
		if err != nil {
			return nil, fmt.Errorf("resume_work: %w", err)
		}
		if runtime == nil {
			return nil, fmt.Errorf("resume_work: work runtime is not configured")
		}

		resp := protocol.DecisionResponse{
			TaskID:           req.TaskID,
			Decision:         req.Decision,
			Reason:           req.Reason,
			ConstraintsDelta: req.ConstraintsDelta,
		}
		resumeCtx := ctx
		releaseClaim := true
		defer func() {
			if releaseClaim && pending != nil {
				_ = pending.ReleaseClaim(sessionID, req.TaskID, claim.ClaimID)
			}
		}()

		approvalGated := requiresApprovalRequest(paused.Packet)
		if approvalGated {
			if pending == nil || pending.approvals == nil || req.ApprovalRequestID == "" || req.ApprovalRequestID != claim.ApprovalRequestID {
				output, _ := json.Marshal(map[string]string{
					"status":              "awaiting_approval",
					"task_id":             req.TaskID,
					"approval_request_id": claim.ApprovalRequestID,
				})
				return output, nil
			}
			approval, err := pending.approvals.consumeRequestForResume(sessionID, req.TaskID, req.ApprovalRequestID)
			if err != nil {
				output, _ := json.Marshal(map[string]string{
					"status":              "awaiting_approval",
					"task_id":             req.TaskID,
					"approval_request_id": claim.ApprovalRequestID,
				})
				return output, nil
			}
			resp.Decision = approval.Request.SelectedOptionID
			if resp.Reason == "" {
				resp.Reason = fmt.Sprintf("approval_request %s resolved via %s", approval.Request.ID, approval.PreviousStatus)
			}
			if approval.PreviousStatus == protocol.ApprovalStatusApproved {
				paused.Brief.PermissionScope = "approved-destructive"
				resumeCtx = tool.WithApproval(resumeCtx, tool.ApprovalContext{
					RequestID:        approval.Request.ID,
					AllowDestructive: true,
				})
			}
		} else if req.Decision == "" {
			return nil, fmt.Errorf("resume_work: decision is required when approval_request_id is absent")
		} else {
			if paused.Packet.Category == protocol.CatPermissionEscalationRequired {
				if req.Decision != paused.Packet.RejectOptionID {
					if req.PermissionScopeOverride != "approved-destructive" {
						return nil, fmt.Errorf("resume_work: permission_scope_override=\"approved-destructive\" is required when approving permission escalation")
					}
					paused.Brief.PermissionScope = req.PermissionScopeOverride
					resumeCtx = tool.WithApproval(resumeCtx, tool.ApprovalContext{
						RequestID:        fmt.Sprintf("emotion-permission-escalation:%s", req.TaskID),
						AllowDestructive: true,
					})
				} else if req.PermissionScopeOverride != "" {
					return nil, fmt.Errorf("resume_work: permission_scope_override is only valid when approving permission escalation")
				}
			} else if req.PermissionScopeOverride != "" {
				return nil, fmt.Errorf("resume_work: permission_scope_override is only valid for permission_escalation_required pauses")
			}
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

		if claim.WasStale {
			staleDuration := time.Since(claim.CreatedAt).Round(time.Minute)
			resp.Reason = fmt.Sprintf("[STALE CONTEXT: paused %s, re-verify assumptions] %s", staleDuration, resp.Reason)
		}
		if journal != nil {
			journal.Write("decision_response_emotion", paused.Round, map[string]any{
				"task_id":  req.TaskID,
				"decision": resp.Decision,
				"reason":   resp.Reason,
			})
		}

		releaseClaim = false
		outcome := runtime.Resume(resumeCtx, paused, resp, journal)
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
			if pending != nil {
				if err := pending.FinalizeResolved(sessionID, req.TaskID, claim.ClaimID, resp, outcome.Report); err != nil {
					return nil, fmt.Errorf("resume_work: finalize resolved: %w", err)
				}
			}
			return json.Marshal(outcome.Report)
		}

		if outcome.Paused == nil {
			return nil, fmt.Errorf("resume_work: runtime returned empty outcome")
		}
		if pending != nil {
			if err := pending.RequeuePaused(sessionID, outcome.Paused.TaskID, claim.ClaimID, outcome.Paused); err != nil {
				return nil, fmt.Errorf("resume_work: requeue paused: %w", err)
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
		return json.Marshal(NeedsEmotionDecision{
			Status:         "needs_emotion_decision",
			TaskID:         outcome.Paused.TaskID,
			DecisionPacket: outcome.Paused.Packet,
		})
	}

	return spec, handler
}
