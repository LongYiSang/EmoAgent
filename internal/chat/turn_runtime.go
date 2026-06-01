package chat

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/turn"
)

type chatTurnRuntime struct {
	engine  conversationEngine
	cfg     config.TurnPipelineConfig
	rt      *turn.Runtime
	journal turn.TurnJournal
	ids     *turn.MemoryIdempotencyStore
	logger  *slog.Logger
}

type forcedOutboundEventsKey struct{}

func withForcedOutboundEvents(ctx context.Context) context.Context {
	return context.WithValue(ctx, forcedOutboundEventsKey{}, true)
}

func forcedOutboundEventsFromContext(ctx context.Context) bool {
	enabled, _ := ctx.Value(forcedOutboundEventsKey{}).(bool)
	return enabled
}

func newChatTurnRuntime(engine conversationEngine, cfg config.TurnPipelineConfig, journal turn.TurnJournal, logger *slog.Logger) *chatTurnRuntime {
	if journal == nil {
		journal = turn.NewMemoryJournal()
	}
	return &chatTurnRuntime{
		engine:  engine,
		cfg:     cfg,
		rt:      turn.NewRuntime(turn.RuntimeConfig{Journal: journal}),
		journal: journal,
		ids:     turn.NewMemoryIdempotencyStore(),
		logger:  logger,
	}
}

func (r *chatTurnRuntime) Execute(ctx context.Context, env turn.InboundEnvelope, persona *config.Persona, sink turn.OutboundSink) (turn.TurnResult, error) {
	if r == nil {
		return turn.TurnResult{}, errors.New("turn runtime is not configured")
	}
	if env.IdempotencyKey == "" {
		env.IdempotencyKey = turn.BuildIdempotencyKey(env)
	}
	turnID := env.EnvelopeID
	if turnID == "" {
		turnID = uuid.NewString()
	}
	idem, err := r.ids.Begin(env.IdempotencyKey, turnID)
	if err != nil {
		return turn.TurnResult{}, err
	}
	if idem.Duplicate {
		return turn.TurnResult{TurnID: idem.TurnID, State: turn.StateDone, Status: idem.Status}, nil
	}

	tc := turn.TurnContext{
		TurnID:  turnID,
		State:   turn.StateCreated,
		Inbound: env,
		Stream:  newJournalingSink(sink, r.journal, turnID),
		Journal: r.journal,
	}
	stages := r.stages(env, persona)
	result, execErr := r.rt.Execute(ctx, tc, stages)
	status := result.Status
	if status == "" {
		status = "failed"
	}
	if execErr == nil {
		_ = r.ids.Complete(env.IdempotencyKey, status)
	}
	return result, execErr
}

func (r *chatTurnRuntime) Shadow(ctx context.Context, env turn.InboundEnvelope) (turn.TurnResult, error) {
	if r == nil {
		return turn.TurnResult{}, errors.New("turn runtime is not configured")
	}
	if env.IdempotencyKey == "" {
		env.IdempotencyKey = turn.BuildIdempotencyKey(env)
	}
	turnID := env.EnvelopeID
	if turnID == "" {
		turnID = uuid.NewString()
	}
	record := turn.TurnRecord{
		TurnID:         turnID,
		IdempotencyKey: env.IdempotencyKey,
		Kind:           env.Kind,
		SessionID:      env.SessionID,
		PersonaKey:     env.PersonaKey,
		State:          turn.StateCreated,
	}
	if err := r.journal.StartTurn(ctx, record); err != nil {
		return turn.TurnResult{TurnID: turnID, State: turn.StateFailed, Status: "failed", ErrorKind: "journal_failed"}, err
	}
	for _, eventType := range []string{"turn_started", "normalized", "done_mock"} {
		if err := r.journal.RecordEvent(ctx, turnID, turn.JournalEvent{
			Stage: turn.StageIngress,
			Type:  eventType,
			Payload: map[string]any{
				"kind":       string(env.Kind),
				"session_id": env.SessionID,
				"request_id": env.RequestID,
			},
		}); err != nil {
			return turn.TurnResult{TurnID: turnID, State: turn.StateFailed, Status: "failed", ErrorKind: "journal_failed"}, err
		}
	}
	if err := r.journal.CompleteTurn(ctx, turnID, "done_mock", ""); err != nil {
		return turn.TurnResult{TurnID: turnID, State: turn.StateFailed, Status: "failed", ErrorKind: "journal_failed"}, err
	}
	return turn.TurnResult{TurnID: turnID, State: turn.StateDone, Status: "done_mock"}, nil
}

func (r *chatTurnRuntime) stages(env turn.InboundEnvelope, persona *config.Persona) []turn.Stage {
	switch env.Kind {
	case turn.InboundApprovalAction:
		return []turn.Stage{
			r.normalizeStage(),
			r.approvalApplyStage(),
			r.resumeStage(persona),
			r.emitApprovalsStage(),
		}
	default:
		return []turn.Stage{
			r.normalizeStage(),
			r.messageStage(persona),
			r.emitApprovalsStage(),
		}
	}
}

func (r *chatTurnRuntime) normalizeStage() turn.Stage {
	return turn.StageFunc{
		NameValue: turn.StageNormalize,
		RunFunc: func(ctx context.Context, tc *turn.TurnContext) (turn.StageResult, error) {
			switch tc.Inbound.Kind {
			case turn.InboundApprovalAction:
				if tc.Inbound.Approval == nil || strings.TrimSpace(tc.Inbound.Approval.RequestID) == "" {
					return turn.StageResult{NextState: turn.StateFailed, Terminal: true, Status: "failed", ErrorKind: "validation_error"}, errors.New("request_id is required")
				}
				if strings.TrimSpace(tc.Inbound.Approval.Action) == "" {
					return turn.StageResult{NextState: turn.StateFailed, Terminal: true, Status: "failed", ErrorKind: "validation_error"}, errors.New("action is required")
				}
			case turn.InboundUserMessage:
				if tc.Inbound.UserMessage == nil || strings.TrimSpace(tc.Inbound.UserMessage.Content) == "" {
					return turn.StageResult{NextState: turn.StateFailed, Terminal: true, Status: "failed", ErrorKind: "validation_error"}, errors.New("message content is required")
				}
			}
			return turn.StageResult{NextState: turn.StateNormalizing}, nil
		},
	}
}

func (r *chatTurnRuntime) messageStage(persona *config.Persona) turn.Stage {
	return turn.StageFunc{
		NameValue: turn.StageEmotionLoop,
		RunFunc: func(ctx context.Context, tc *turn.TurnContext) (turn.StageResult, error) {
			if r.engine == nil {
				return turn.StageResult{NextState: turn.StateFailed, Terminal: true, Status: "failed", ErrorKind: "engine_unconfigured"}, errors.New("chat engine is not configured")
			}
			ctx = withForcedOutboundEvents(turn.WithOutboundSink(ctx, tc.Stream))
			if tc.Stream != nil {
				if err := tc.Stream.Emit(ctx, turn.OutboundEvent{Type: turn.EventStreamStart}); err != nil {
					return turn.StageResult{NextState: turn.StateFailed, Terminal: true, Status: "failed", ErrorKind: "outbound_failed"}, err
				}
			}
			result := turn.StageResult{
				NextState: turn.StateDone,
				Terminal:  true,
				Status:    "done",
			}
			streamedDelta := false
			reply, err := r.engine.SendMessage(ctx, tc.Inbound.SessionID, persona, tc.Inbound.UserMessage.Content, func(delta string) {
				if delta == "" || tc.Stream == nil {
					return
				}
				streamedDelta = true
				_ = tc.Stream.Emit(ctx, turn.OutboundEvent{Type: turn.EventStreamDelta, Content: delta})
			})
			if err != nil && !errors.Is(err, errApprovalPending) {
				return turn.StageResult{NextState: turn.StateFailed, Terminal: true, Status: "failed", ErrorKind: "llm_failed"}, err
			}
			if err == nil && !streamedDelta && reply != "" {
				result.Outbound = append(result.Outbound, turn.OutboundEvent{Type: turn.EventStreamDelta, Content: reply})
			}
			result.Outbound = append(result.Outbound, turn.OutboundEvent{Type: turn.EventStreamEnd})
			if errors.Is(err, errApprovalPending) {
				result.NextState = turn.StateApprovalWait
				result.Status = "approval_wait"
				result.ErrorKind = "tool_approval"
			}
			return result, nil
		},
	}
}

func (r *chatTurnRuntime) approvalApplyStage() turn.Stage {
	return turn.StageFunc{
		NameValue: turn.StageApprovalApply,
		RunFunc: func(ctx context.Context, tc *turn.TurnContext) (turn.StageResult, error) {
			approval, err := r.engine.ApplyApprovalAction(ctx, tc.Inbound.SessionID, tc.Inbound.Approval.RequestID, tc.Inbound.Approval.Action, tc.Inbound.Approval.OptionID)
			if err != nil {
				return turn.StageResult{NextState: turn.StateFailed, Terminal: true, Status: "failed", ErrorKind: "approval_failed"}, err
			}
			tc.Diagnostics = map[string]any{"approval": approval}
			return turn.StageResult{
				NextState: turn.StateRunningEmotion,
				Outbound:  []turn.OutboundEvent{{Type: turn.EventApprovalUpdated, Approval: &turn.ApprovalActivity{Request: approval}}},
			}, nil
		},
	}
}

func (r *chatTurnRuntime) resumeStage(persona *config.Persona) turn.Stage {
	return turn.StageFunc{
		NameValue: turn.StageResume,
		RunFunc: func(ctx context.Context, tc *turn.TurnContext) (turn.StageResult, error) {
			approval, _ := tc.Diagnostics["approval"].(*protocol.ApprovalRequest)
			if approval == nil {
				return turn.StageResult{NextState: turn.StateFailed, Terminal: true, Status: "failed", ErrorKind: "approval_failed"}, errors.New("approval is required")
			}
			ctx = withForcedOutboundEvents(turn.WithOutboundSink(ctx, tc.Stream))
			if tc.Stream != nil {
				if err := tc.Stream.Emit(ctx, turn.OutboundEvent{Type: turn.EventStreamStart}); err != nil {
					return turn.StageResult{NextState: turn.StateFailed, Terminal: true, Status: "failed", ErrorKind: "outbound_failed"}, err
				}
			}
			result := turn.StageResult{
				NextState: turn.StateDone,
				Terminal:  false,
				Status:    "done",
			}
			streamedDelta := false
			reply, err := r.engine.ContinueAfterApproval(ctx, tc.Inbound.SessionID, persona, approval, func(delta string) {
				if delta == "" || tc.Stream == nil {
					return
				}
				streamedDelta = true
				_ = tc.Stream.Emit(ctx, turn.OutboundEvent{Type: turn.EventStreamDelta, Content: delta})
			})
			if err != nil && !errors.Is(err, errApprovalPending) {
				return turn.StageResult{NextState: turn.StateFailed, Terminal: true, Status: "failed", ErrorKind: "approval_failed"}, err
			}
			if err == nil && !streamedDelta && reply != "" {
				result.Outbound = append(result.Outbound, turn.OutboundEvent{Type: turn.EventStreamDelta, Content: reply})
			}
			result.Outbound = append(result.Outbound, turn.OutboundEvent{Type: turn.EventStreamEnd})
			if errors.Is(err, errApprovalPending) {
				result.NextState = turn.StateApprovalWait
				result.Status = "approval_wait"
				result.ErrorKind = "tool_approval"
			}
			return result, nil
		},
	}
}

func (r *chatTurnRuntime) emitApprovalsStage() turn.Stage {
	return turn.StageFunc{
		NameValue: turn.StageApprovalWait,
		RunFunc: func(ctx context.Context, tc *turn.TurnContext) (turn.StageResult, error) {
			approvals, err := r.engine.ListSessionApprovals(ctx, tc.Inbound.SessionID)
			if err != nil {
				return turn.StageResult{NextState: tc.State, Terminal: true, Status: statusFromState(tc.State), ErrorKind: "approval_failed"}, err
			}
			events := make([]turn.OutboundEvent, 0, len(approvals))
			for i := range approvals {
				eventType := turn.EventApprovalUpdated
				if approvals[i].Status == string(protocol.ApprovalStatusPending) {
					eventType = turn.EventApprovalRequired
				}
				approval := approvals[i]
				events = append(events, turn.OutboundEvent{Type: eventType, Approval: &turn.ApprovalActivity{Request: &approval}})
			}
			return turn.StageResult{NextState: tc.State, Terminal: true, Status: statusFromState(tc.State), Outbound: events}, nil
		},
	}
}

func statusFromState(state turn.TurnState) string {
	if state == turn.StateApprovalWait {
		return "approval_wait"
	}
	if state == turn.StateFailed {
		return "failed"
	}
	return "done"
}

type journalingSink struct {
	next    turn.OutboundSink
	journal turn.TurnJournal
	turnID  string
	seq     int64
}

func newJournalingSink(next turn.OutboundSink, journal turn.TurnJournal, turnID string) turn.OutboundSink {
	return &journalingSink{next: next, journal: journal, turnID: turnID}
}

func (s *journalingSink) Emit(ctx context.Context, event turn.OutboundEvent) error {
	if event.TurnID == "" {
		event.TurnID = s.turnID
	}
	s.seq++
	if event.Seq == 0 {
		event.Seq = s.seq
	}
	if s.journal != nil {
		_ = s.journal.RecordEvent(ctx, s.turnID, turn.JournalEvent{
			Stage:   turn.StageOutboundCommit,
			Type:    event.Type,
			Payload: outboundJournalPayload(event),
		})
	}
	if s.next == nil {
		return nil
	}
	return s.next.Emit(ctx, event)
}

func outboundJournalPayload(event turn.OutboundEvent) map[string]any {
	payload := map[string]any{"outbound_type": event.Type}
	if event.Tool != nil {
		payload["tool"] = event.Tool.Name
		payload["tool_status"] = event.Tool.Status
		payload["hash"] = event.Tool.Hash
		payload["size"] = event.Tool.Size
		payload["is_truncated"] = event.Tool.IsTruncated
		for key, value := range safeWorkToolSummary(event.Tool.Name, event.Tool.Preview) {
			payload[key] = value
		}
	}
	if event.Approval != nil && event.Approval.Request != nil {
		payload["approval_request_id"] = event.Approval.Request.ID
		payload["task_id"] = event.Approval.Request.TaskID
		payload["status"] = event.Approval.Request.Status
	}
	return payload
}

func safeWorkToolSummary(toolName, preview string) map[string]any {
	if toolName != "delegate_to_work" && toolName != "resume_work" {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(preview), &raw); err != nil {
		return nil
	}
	summary := make(map[string]any)
	copyStringField(summary, raw, "task_id")
	copyStringField(summary, raw, "status")
	copyStringField(summary, raw, "summary")
	copyStringField(summary, raw, "approval_request_id")
	if packet, ok := raw["decision_packet"].(map[string]any); ok {
		decision := make(map[string]any)
		copyStringField(decision, packet, "category")
		copyStringField(decision, packet, "risk_level")
		copyStringField(decision, packet, "question")
		copyStringField(decision, packet, "goal_summary")
		copyStringField(decision, packet, "recommended_option_id")
		copyStringField(decision, packet, "reject_option_id")
		if len(decision) > 0 {
			summary["decision_packet"] = decision
		}
	}
	if len(summary) == 0 {
		return nil
	}
	return summary
}

func copyStringField(dst map[string]any, src map[string]any, key string) {
	value, ok := src[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return
	}
	dst[key] = value
}
