package chat

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent/internal/agentaffect"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/turn"
)

type chatTurnRuntime struct {
	engine  conversationEngine
	cfg     config.TurnPipelineConfig
	rt      *turn.Runtime
	journal turn.TurnJournal
	ids     turn.IdempotencyStore
	logger  *slog.Logger
	plugin  turnPluginHost
}

type turnPluginHost interface {
	Enabled() bool
	WrapStages([]turn.Stage) []turn.Stage
	WrapOutboundSink(turn.OutboundSink) turn.OutboundSink
	DispatchTurnEnd(context.Context, turn.TurnResult, turn.InboundEnvelope)
	DispatchTurnError(context.Context, turn.TurnResult, error, turn.InboundEnvelope)
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
	return newChatTurnRuntimeWithStore(engine, cfg, journal, turn.NewMemoryIdempotencyStore(), logger)
}

func newChatTurnRuntimeWithStore(engine conversationEngine, cfg config.TurnPipelineConfig, journal turn.TurnJournal, ids turn.IdempotencyStore, logger *slog.Logger, pluginHosts ...turnPluginHost) *chatTurnRuntime {
	if journal == nil {
		journal = turn.NewMemoryJournal()
	}
	if ids == nil {
		ids = turn.NewMemoryIdempotencyStore()
	}
	var plugin turnPluginHost
	if len(pluginHosts) > 0 {
		plugin = pluginHosts[0]
	}
	return &chatTurnRuntime{
		engine:  engine,
		cfg:     cfg,
		rt:      turn.NewRuntime(turn.RuntimeConfig{Journal: journal}),
		journal: journal,
		ids:     ids,
		logger:  logger,
		plugin:  plugin,
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
		return r.replayDuplicate(ctx, idem, sink)
	}

	finalStatus := "failed"
	defer func() {
		_ = r.ids.Complete(env.IdempotencyKey, finalStatus)
	}()

	tc := turn.TurnContext{
		TurnID:  turnID,
		State:   turn.StateCreated,
		Inbound: env,
		Journal: r.journal,
	}
	ctx = turn.WithCorrelationContext(ctx, turn.CorrelationContext{
		TurnID:     turnID,
		SessionID:  env.SessionID,
		PersonaKey: env.PersonaKey,
		RequestID:  env.RequestID,
		Kind:       env.Kind,
	})
	tc.Stream = newJournalingSink(sink, r.journal, turnID)
	if r.plugin != nil && r.plugin.Enabled() {
		tc.Stream = r.plugin.WrapOutboundSink(tc.Stream)
	}
	stages := r.stages(env, persona)
	if r.plugin != nil && r.plugin.Enabled() {
		stages = r.plugin.WrapStages(stages)
	}
	result, execErr := r.rt.Execute(ctx, tc, stages)
	if closeErr := closeTurnStream(ctx, tc.Stream); closeErr != nil && execErr == nil {
		_ = r.journal.RecordEvent(ctx, turnID, turn.JournalEvent{
			Stage: turn.StageOutboundCommit,
			Type:  "outbound_failed",
			Payload: map[string]any{
				"error": closeErr.Error(),
			},
		})
		_ = r.journal.CompleteTurn(ctx, turnID, "failed", "outbound_failed")
		result.State = turn.StateFailed
		result.Status = "failed"
		result.ErrorKind = "outbound_failed"
		execErr = closeErr
	}
	if r.plugin != nil && r.plugin.Enabled() {
		if execErr != nil {
			r.plugin.DispatchTurnError(ctx, result, execErr, env)
		} else {
			r.plugin.DispatchTurnEnd(ctx, result, env)
		}
	}
	status := result.Status
	if status == "" {
		status = "failed"
	}
	finalStatus = status
	return result, execErr
}

func closeTurnStream(ctx context.Context, sink turn.OutboundSink) error {
	closer, ok := sink.(interface {
		Close(context.Context) error
	})
	if !ok {
		return nil
	}
	return closer.Close(ctx)
}

func (r *chatTurnRuntime) replayDuplicate(ctx context.Context, idem turn.IdempotencyResult, sink turn.OutboundSink) (turn.TurnResult, error) {
	status := idem.Status
	if status == "" {
		status = "running"
	}
	resultStatus := status
	switch status {
	case "running":
		if r.cfg.Idempotency.DuplicateRunning == "status" {
			resultStatus = "running"
		} else {
			resultStatus = "busy"
		}
		_ = emitTurnStatus(ctx, sink, idem.TurnID, resultStatus, "")
		return turn.TurnResult{TurnID: idem.TurnID, State: turn.StateRunningEmotion, Status: resultStatus}, nil
	case "failed":
		resultStatus = "previous_failed"
		errorKind := r.lookupTurnErrorKind(ctx, idem.TurnID)
		_ = emitTurnStatus(ctx, sink, idem.TurnID, resultStatus, errorKind)
		return turn.TurnResult{TurnID: idem.TurnID, State: turn.StateFailed, Status: resultStatus, ErrorKind: errorKind}, nil
	case "approval_wait":
		_ = emitTurnStatus(ctx, sink, idem.TurnID, "approval_wait", "")
		_ = r.replayOutbound(ctx, idem.TurnID, sink, func(event turn.OutboundEvent) bool {
			return event.Type == turn.EventApprovalRequired
		})
		return turn.TurnResult{TurnID: idem.TurnID, State: turn.StateApprovalWait, Status: "approval_wait"}, nil
	default:
		_ = emitTurnStatus(ctx, sink, idem.TurnID, status, "")
		if r.cfg.Idempotency.DuplicateDone != "noop" {
			_ = r.replayOutbound(ctx, idem.TurnID, sink, func(event turn.OutboundEvent) bool {
				return event.Type == turn.EventStreamDelta || event.Type == turn.EventStreamEnd || event.Type == turn.EventApprovalRequired || event.Type == turn.EventApprovalUpdated
			})
		}
		return turn.TurnResult{TurnID: idem.TurnID, State: turn.StateDone, Status: status}, nil
	}
}

func emitTurnStatus(ctx context.Context, sink turn.OutboundSink, turnID, status, errorKind string) error {
	if sink == nil {
		return nil
	}
	payload := map[string]any{
		"turn_id": turnID,
		"status":  status,
	}
	if errorKind != "" {
		payload["error_kind"] = errorKind
	}
	return sink.Emit(ctx, turn.OutboundEvent{
		TurnID:  turnID,
		Type:    turn.EventTurnStatus,
		Payload: payload,
	})
}

func (r *chatTurnRuntime) lookupTurnErrorKind(ctx context.Context, turnID string) string {
	switch journal := r.journal.(type) {
	case interface {
		GetTurn(context.Context, string) (turn.TurnSnapshot, bool, error)
	}:
		snapshot, ok, err := journal.GetTurn(ctx, turnID)
		if err == nil && ok {
			return snapshot.ErrorKind
		}
	case interface {
		GetTurn(string) (turn.TurnSnapshot, bool)
	}:
		snapshot, ok := journal.GetTurn(turnID)
		if ok {
			return snapshot.ErrorKind
		}
	}
	return ""
}

func (r *chatTurnRuntime) replayOutbound(ctx context.Context, turnID string, sink turn.OutboundSink, keep func(turn.OutboundEvent) bool) error {
	if sink == nil {
		return nil
	}
	lister, ok := r.journal.(interface {
		ListOutbound(context.Context, string) ([]turn.OutboundEvent, error)
	})
	if !ok {
		return nil
	}
	events, err := lister.ListOutbound(ctx, turnID)
	if err != nil {
		return err
	}
	for _, event := range events {
		if keep != nil && !keep(event) {
			continue
		}
		if err := sink.Emit(ctx, event); err != nil {
			return err
		}
	}
	return nil
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
		if r.cfg.MemoryStages {
			return []turn.Stage{
				r.normalizeStage(),
				r.memoryPrepareStage(),
				r.emotionPrepareStage(),
				r.messageStage(persona),
				r.memoryCommitStage(),
				r.emitApprovalsStage(),
			}
		}
		return []turn.Stage{
			r.normalizeStage(),
			r.messageStage(persona),
			r.emitApprovalsStage(),
		}
	}
}

func (r *chatTurnRuntime) memoryPrepareStage() turn.Stage {
	return turn.StageFunc{
		NameValue: turn.StageMemoryPrepare,
		RunFunc: func(ctx context.Context, tc *turn.TurnContext) (turn.StageResult, error) {
			engine, ok := r.engine.(*Engine)
			if !ok {
				return turn.StageResult{NextState: turn.StateMemoryPrepared}, nil
			}
			anchor, err := engine.prepareInputAndMemoryAnchor(ctx, tc.Inbound.SessionID, turnOptions{
				persistUser: true,
				userContent: tc.Inbound.UserMessage.Content,
			})
			if err != nil {
				return turn.StageResult{NextState: turn.StateFailed, Terminal: true, Status: "failed", ErrorKind: "memory_prepare_failed"}, err
			}
			ensureDiagnostics(tc)
			tc.Diagnostics["memory_anchor"] = anchor
			tc.Diagnostics["memory_prepared"] = true
			if anchor.manualNoticeHandled {
				tc.Diagnostics["manual_notice"] = anchor.manualNotice
			}
			return turn.StageResult{NextState: turn.StateMemoryPrepared}, nil
		},
	}
}

func (r *chatTurnRuntime) emotionPrepareStage() turn.Stage {
	return turn.StageFunc{
		NameValue: turn.StageEmotionPrepare,
		RunFunc: func(ctx context.Context, tc *turn.TurnContext) (turn.StageResult, error) {
			engine, ok := r.engine.(*Engine)
			if !ok || tc.Diagnostics == nil {
				return turn.StageResult{NextState: turn.StateEmotionPrepared}, nil
			}
			if _, handled := tc.Diagnostics["manual_notice"].(string); handled {
				return turn.StageResult{NextState: turn.StateEmotionPrepared}, nil
			}
			anchor, _ := tc.Diagnostics["memory_anchor"].(turnMemoryAnchor)
			engine.mu.RLock()
			memoryRetrieval := engine.memoryRetrieval
			agentAffect := engine.agentAffect
			engine.mu.RUnlock()
			snapshot, err := engine.retrieveMemoryPrompt(ctx, tc.Inbound.SessionID, tc.Inbound.UserMessage.Content, anchor.userEpisodeID, memoryRetrieval)
			if err != nil {
				return turn.StageResult{NextState: turn.StateFailed, Terminal: true, Status: "failed", ErrorKind: "emotion_prepare_failed"}, err
			}
			if snapshot != nil && snapshot.PromptBlock != "" {
				tc.Diagnostics["memory_prompt_block"] = snapshot.PromptBlock
				tc.Diagnostics["memory_prompt_snapshot"] = snapshot
			}
			if agentAffect != nil && tc.Inbound.Kind == turn.InboundUserMessage && tc.Inbound.UserMessage != nil {
				memoryBlock := ""
				if snapshot != nil {
					memoryBlock = snapshot.PromptBlock
				}
				affectResp, err := agentAffect.SubmitMoodImpact(ctx, agentaffect.SubmitMoodImpactRequest{
					PersonaID:         tc.Inbound.PersonaKey,
					SessionID:         tc.Inbound.SessionID,
					TurnID:            tc.TurnID,
					MemoryPromptBlock: memoryBlock,
					CommitMode:        agentaffect.CommitModeCommitIfAllowed,
					Trigger: agentaffect.TriggerDescriptor{
						TriggerType:   "user_message",
						SourceKind:    "turn",
						SourceRefType: "episode",
						SourceRefID:   anchor.userEpisodeID,
					},
					Input: agentaffect.MoodImpactInput{Mode: "raw", Text: tc.Inbound.UserMessage.Content},
				})
				if err != nil {
					tc.Diagnostics["agent_affect_error"] = err.Error()
				} else {
					tc.Diagnostics["agent_affect_snapshot"] = affectResp.Mood
					tc.Diagnostics["agent_affect_evaluation"] = affectResp
					block, err := agentAffect.BuildPromptAffectBlock(ctx, agentaffect.BuildPromptAffectBlockRequest{
						PersonaID: tc.Inbound.PersonaKey,
						SessionID: tc.Inbound.SessionID,
						Mood:      affectResp.Mood,
					})
					if err != nil {
						tc.Diagnostics["agent_affect_error"] = err.Error()
					} else if strings.TrimSpace(block) != "" {
						tc.Diagnostics["agent_affect_prompt_block"] = block
					}
				}
			}
			return turn.StageResult{NextState: turn.StateEmotionPrepared}, nil
		},
	}
}

func (r *chatTurnRuntime) memoryCommitStage() turn.Stage {
	return turn.StageFunc{
		NameValue: turn.StageMemoryCommit,
		RunFunc: func(ctx context.Context, tc *turn.TurnContext) (turn.StageResult, error) {
			ensureDiagnostics(tc)
			engine, ok := r.engine.(*Engine)
			if !ok {
				tc.Diagnostics["memory_commit_observed"] = true
				return turn.StageResult{NextState: tc.State}, nil
			}
			output, ok := tc.Diagnostics["turn_output"].(deferredTurnOutput)
			if !ok || output.assistantContent == "" {
				tc.Diagnostics["memory_commit_observed"] = true
				return turn.StageResult{NextState: tc.State}, nil
			}
			if err := engine.commitTurnOutput(ctx, tc.Inbound.SessionID, output.assistantContent, output.thinkingBlocks, output.memorySnapshot, output.memorySegment, output.hasMemorySegment); err != nil {
				return turn.StageResult{NextState: turn.StateCommitFailedAfterOutput, Terminal: true, Status: "commit_failed_after_output", ErrorKind: "memory_commit_failed"}, err
			}
			tc.Diagnostics["memory_committed"] = true
			return turn.StageResult{NextState: tc.State}, nil
		},
	}
}

func ensureDiagnostics(tc *turn.TurnContext) {
	if tc.Diagnostics == nil {
		tc.Diagnostics = map[string]any{}
	}
}

func stringDiagnostic(tc *turn.TurnContext, key string) string {
	if tc == nil || tc.Diagnostics == nil {
		return ""
	}
	value, _ := tc.Diagnostics[key].(string)
	return value
}

func joinSystemBlocks(blocks ...string) string {
	out := make([]string, 0, len(blocks))
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block != "" {
			out = append(out, block)
		}
	}
	return strings.Join(out, "\n\n")
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
			ctx = turn.WithCorrelationStage(ctx, turn.StageEmotionLoop)
			if r.engine == nil {
				return turn.StageResult{NextState: turn.StateFailed, Terminal: true, Status: "failed", ErrorKind: "engine_unconfigured"}, errors.New("chat engine is not configured")
			}
			ctx = withForcedOutboundEvents(turn.WithOutboundSink(ctx, tc.Stream))
			if notice, ok := tc.Diagnostics["manual_notice"].(string); ok && notice != "" {
				return turn.StageResult{
					NextState: turn.StateDone,
					Terminal:  false,
					Status:    "done",
					Outbound: []turn.OutboundEvent{
						{Type: turn.EventStreamDelta, Content: notice},
						{Type: turn.EventStreamEnd},
					},
				}, nil
			}
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
			reply := ""
			var err error
			if r.cfg.MemoryStages {
				if engine, ok := r.engine.(*Engine); ok {
					extraSystem := joinSystemBlocks(
						stringDiagnostic(tc, "memory_prompt_block"),
						stringDiagnostic(tc, "agent_affect_prompt_block"),
					)
					var output deferredTurnOutput
					reply, err = engine.sendTurn(ctx, tc.Inbound.SessionID, persona, func(delta string) {
						if delta == "" || tc.Stream == nil {
							return
						}
						streamedDelta = true
						_ = tc.Stream.Emit(ctx, turn.OutboundEvent{Type: turn.EventStreamDelta, Content: delta})
					}, turnOptions{
						persistUser: false,
						userContent: tc.Inbound.UserMessage.Content,
						extraSystem: extraSystem,
						deferCommit: true,
						output:      &output,
					})
					if snapshot, ok := tc.Diagnostics["memory_prompt_snapshot"].(*memoryPromptSnapshot); ok {
						output.memorySnapshot = snapshot
					}
					if anchor, ok := tc.Diagnostics["memory_anchor"].(turnMemoryAnchor); ok {
						output.memorySegment = anchor.memorySegment
						output.hasMemorySegment = anchor.hasMemorySegment
					}
					ensureDiagnostics(tc)
					tc.Diagnostics["turn_output"] = output
				} else {
					reply, err = r.engine.SendMessage(ctx, tc.Inbound.SessionID, persona, tc.Inbound.UserMessage.Content, func(delta string) {
						if delta == "" || tc.Stream == nil {
							return
						}
						streamedDelta = true
						_ = tc.Stream.Emit(ctx, turn.OutboundEvent{Type: turn.EventStreamDelta, Content: delta})
					})
				}
			} else {
				reply, err = r.engine.SendMessage(ctx, tc.Inbound.SessionID, persona, tc.Inbound.UserMessage.Content, func(delta string) {
					if delta == "" || tc.Stream == nil {
						return
					}
					streamedDelta = true
					_ = tc.Stream.Emit(ctx, turn.OutboundEvent{Type: turn.EventStreamDelta, Content: delta})
				})
			}
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
			ctx = turn.WithCorrelationStage(ctx, turn.StageResume)
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
		if recorder, ok := s.journal.(interface {
			RecordOutbound(context.Context, string, turn.OutboundEvent) error
		}); ok {
			journalEvent := event
			journalEvent.Payload = outboundJournalPayload(event)
			_ = recorder.RecordOutbound(ctx, s.turnID, journalEvent)
		} else {
			_ = s.journal.RecordEvent(ctx, s.turnID, turn.JournalEvent{
				Stage:   turn.StageOutboundCommit,
				Type:    event.Type,
				Payload: outboundJournalPayload(event),
			})
		}
	}
	if s.next == nil {
		return nil
	}
	return s.next.Emit(ctx, event)
}

func (s *journalingSink) Close(ctx context.Context) error {
	if s == nil || s.next == nil {
		return nil
	}
	closer, ok := s.next.(interface {
		Close(context.Context) error
	})
	if !ok {
		return nil
	}
	return closer.Close(ctx)
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
