package work

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/progress"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/runtimeenv"
	"github.com/longyisang/emoagent/internal/tool"
)

const (
	defaultMaxEscalations        = 3
	defaultPendingSnapshotTokens = 60000
	progressHeartbeatInterval    = 8 * time.Second
)

// RuntimeConfig describes the dependencies for one Work runtime instance.
type RuntimeConfig struct {
	LLM                      llm.Client
	SummaryClient            llm.Client
	SummaryModel             string
	SummaryParams            llm.RequestParams
	Provider                 string
	Model                    string
	Params                   llm.RequestParams
	MaxTokens                int
	Temperature              float64
	MaxToolRounds            int
	MaxInputTokens           int
	CompressSoftRatio        float64
	CompressKeepRounds       int
	ToolSnipSoftTokens       int
	ToolSnipHardTokens       int
	Registry                 *tool.Registry
	Dispatcher               *tool.Dispatcher
	Logger                   *slog.Logger
	Decider                  RuntimeDecider
	MaxEscalations           int
	PendingSnapshotMaxTokens int
	EnvironmentFacts         runtimeenv.Facts
}

// RunOutcome is the result of a runtime execution.
// Exactly one field is non-nil.
type RunOutcome struct {
	Report *protocol.TaskReport
	Paused *PausedWork
}

// PausedWork holds frozen Work state while waiting for an Emotion decision.
type PausedWork struct {
	TaskID          string
	Brief           protocol.TaskBrief
	Messages        []llm.Message
	Progress        WorkProgress
	PendingCallID   string
	PendingToolCall *tool.Call
	Packet          protocol.DecisionPacket
	Round           int
	EscalationCount int
	CreatedAt       time.Time
}

// NeedsEmotionDecision is returned by Emotion-facing tools when Work pauses.
type NeedsEmotionDecision struct {
	Status         string                  `json:"status"`
	TaskID         string                  `json:"task_id"`
	DecisionPacket protocol.DecisionPacket `json:"decision_packet"`
}

// Runtime executes one isolated Work task.
type Runtime struct {
	cfg RuntimeConfig
}

// NewRuntime constructs a Work runtime from the provided dependencies.
func NewRuntime(cfg RuntimeConfig) *Runtime {
	if cfg.MaxEscalations <= 0 {
		cfg.MaxEscalations = defaultMaxEscalations
	}
	if cfg.PendingSnapshotMaxTokens <= 0 {
		cfg.PendingSnapshotMaxTokens = defaultPendingSnapshotTokens
	}
	return &Runtime{cfg: cfg}
}

// Run executes the Work tool loop. Work always starts with an empty message
// history so Emotion history cannot leak into the delegated task.
func (r *Runtime) Run(ctx context.Context, brief protocol.TaskBrief, journal *Journal) RunOutcome {
	return r.runLoop(ctx, brief, nil, WorkProgress{}, 0, 0, journal)
}

// Resume continues a previously paused Work task by appending DecisionResponse
// as tool_result for the pending request_decision call.
func (r *Runtime) Resume(ctx context.Context, paused *PausedWork, resp protocol.DecisionResponse, journal *Journal) RunOutcome {
	if paused == nil {
		report := failedReport(protocol.TaskBrief{TaskID: resp.TaskID, Goal: "resume"}, "resume failed: paused task is nil")
		return RunOutcome{Report: &report}
	}
	ctx = tool.WithReadScope(ctx, readScopeFromBrief(paused.Brief))

	decision := resp
	if decision.TaskID == "" {
		decision.TaskID = paused.TaskID
	}

	messages := append([]llm.Message(nil), paused.Messages...)
	if paused.PendingToolCall != nil {
		var result tool.Result
		if paused.Packet.RejectOptionID != "" && decision.Decision == paused.Packet.RejectOptionID {
			result = errorToolResult(paused.PendingToolCall.ID, "approval denied: tool not executed")
		} else {
			permission := tool.Permission(paused.Brief.PermissionScope)
			classification := r.cfg.Dispatcher.ClassifyCall(ctx, *paused.PendingToolCall, permission)
			result = r.cfg.Dispatcher.Execute(ctx, *paused.PendingToolCall, permission)
			writeToolApprovalResumeEvent(ctx, journal, paused.Round, *paused.PendingToolCall, result, classification.DestructiveReason)
			if result.NeedsApproval && journal != nil && isApprovalBindingMismatchResult(result) {
				writeApprovalBindingMismatchEvent(ctx, journal, paused.Round, *paused.PendingToolCall)
			}
		}
		messages = append(messages, tool.ResultsToMessages(r.cfg.Provider, []tool.Result{result})...)
	} else {
		payload, err := json.Marshal(decision)
		if err != nil {
			report := failedReport(paused.Brief, "resume failed: marshal decision response: "+err.Error())
			return RunOutcome{Report: &report}
		}
		result := tool.Result{
			CallID:  paused.PendingCallID,
			Content: payload,
			IsError: false,
		}
		messages = append(messages, tool.ResultsToMessages(r.cfg.Provider, []tool.Result{result})...)
	}
	if journal != nil {
		journal.Write("task_resumed", paused.Round, map[string]any{
			"task_id":  paused.TaskID,
			"decision": decision.Decision,
		})
	}

	return r.runLoop(tool.WithApproval(ctx, tool.ApprovalContext{}), paused.Brief, messages, paused.Progress, paused.Round+1, paused.EscalationCount, journal)
}

func isApprovalBindingMismatchResult(result tool.Result) bool {
	return strings.Contains(string(result.Content), "approval binding mismatch")
}

func writeToolApprovalResumeEvent(ctx context.Context, journal *Journal, round int, call tool.Call, result tool.Result, destructiveReason string) {
	if journal == nil {
		return
	}
	approval, ok := tool.ApprovalFromContext(ctx)
	if !ok || (!approval.AllowToolCall && !approval.AllowDestructive) {
		return
	}
	kind := tool.ApprovalKind(approval.ApprovalKind)
	if kind == "" {
		kind = tool.ApprovalKindDestructiveWrite
	}
	actual, _ := tool.BuildApprovalBinding(call, approval.RequestID, kind)
	bindingMatched := approvalMatchesBindingForJournal(approval, actual)
	journal.Write("tool_approval_resume", round, map[string]any{
		"approval_request_id":   approval.RequestID,
		"approval_kind":         actual.ApprovalKind,
		"tool_name":             call.Name,
		"normalized_input_hash": actual.NormalizedInputHash,
		"path_digest":           actual.PathDigest,
		"destructive_reason":    destructiveReason,
		"binding_match":         bindingMatched,
		"executed":              bindingMatched && !result.NeedsApproval,
	})
}

func approvalMatchesBindingForJournal(approval tool.ApprovalContext, binding tool.ApprovalBinding) bool {
	kindMatches := approval.ApprovalKind == binding.ApprovalKind ||
		(binding.ApprovalKind == string(tool.ApprovalKindDestructiveWrite) &&
			approval.AllowDestructive &&
			approval.ApprovalKind == "")
	return approval.RequestID != "" &&
		kindMatches &&
		approval.ToolName == binding.ToolName &&
		approval.NormalizedInputHash == binding.NormalizedInputHash &&
		approval.PathDigest == binding.PathDigest
}

func writeApprovalBindingMismatchEvent(ctx context.Context, journal *Journal, round int, call tool.Call) {
	if journal == nil {
		return
	}
	approval, _ := tool.ApprovalFromContext(ctx)
	kind := tool.ApprovalKind(approval.ApprovalKind)
	if kind == "" {
		kind = tool.ApprovalKindDestructiveWrite
	}
	actual, _ := tool.BuildApprovalBinding(call, approval.RequestID, kind)
	journal.Write("tool_approval_binding_mismatch", round, map[string]any{
		"approval_request_id":            approval.RequestID,
		"expected_approval_kind":         approval.ApprovalKind,
		"actual_approval_kind":           actual.ApprovalKind,
		"tool_name":                      call.Name,
		"expected_tool_name":             approval.ToolName,
		"actual_tool_name":               actual.ToolName,
		"expected_normalized_input_hash": approval.NormalizedInputHash,
		"actual_normalized_input_hash":   actual.NormalizedInputHash,
		"expected_path_digest":           approval.PathDigest,
		"actual_path_digest":             actual.PathDigest,
	})
}

func (r *Runtime) runLoop(
	ctx context.Context,
	brief protocol.TaskBrief,
	seedMessages []llm.Message,
	seedProgress WorkProgress,
	startRound int,
	escalationCount int,
	journal *Journal,
) RunOutcome {
	ctx = tool.WithReadScope(ctx, readScopeFromBrief(brief))
	system := BuildWorkSystem(brief, r.cfg.EnvironmentFacts)
	tools := r.cfg.Registry.ForScope(tool.ScopeWork)
	permission := tool.Permission(brief.PermissionScope)
	messages := append([]llm.Message(nil), seedMessages...)
	progressCB := progress.CallbackFromContext(ctx)
	workProgress := seedProgress

	for round := startRound; round < r.cfg.MaxToolRounds; round++ {
		if err := ctx.Err(); err != nil {
			if journal != nil {
				journal.Write("task_error", round, map[string]any{"error": err.Error(), "last_round": round})
			}
			report := failedReport(brief, "context canceled: "+err.Error())
			return RunOutcome{Report: &report}
		}
		preCompressTokens := estimateMessagesTokens(messages) + contextutil.EstimateTokens(system)
		if r.cfg.SummaryClient != nil && r.cfg.CompressSoftRatio > 0 {
			compressed, newProgress, err := compressWorkContextWithParams(
				ctx, r.cfg.SummaryClient, r.cfg.SummaryModel, r.cfg.SummaryParams,
				messages, workProgress, system,
				r.cfg.MaxInputTokens, r.cfg.CompressSoftRatio,
				r.cfg.CompressKeepRounds,
			)
			if err != nil {
				if r.cfg.Logger != nil {
					r.cfg.Logger.Warn("work context compression failed, continuing without",
						"error", err, "round", round)
				}
			} else {
				messages = compressed
				workProgress = newProgress
				if journal != nil && !workProgress.IsZero() {
					journal.Write("work_context_compressed", round, map[string]any{
						"before_tokens":   preCompressTokens,
						"after_tokens":    estimateMessagesTokens(messages) + contextutil.EstimateTokens(system),
						"progress_tokens": estimateProgressTokens(workProgress),
					})
				}
			}
		}

		if r.cfg.MaxInputTokens > 0 &&
			estimateMessagesTokens(messages)+contextutil.EstimateTokens(system) > r.cfg.MaxInputTokens {
			report := partialReport(brief, fmt.Sprintf("max input tokens exceeded (%d)", r.cfg.MaxInputTokens))
			return RunOutcome{Report: &report}
		}

		if progressCB != nil && round == startRound {
			progressCB(progress.Event{
				Kind:   progress.KindStart,
				Round:  round,
				TaskID: brief.TaskID,
			})
		}

		outcome, shouldReturn := func() (RunOutcome, bool) {
			hbCtx, hbCancel := context.WithCancel(ctx)
			defer hbCancel()
			if progressCB != nil {
				go heartbeatTicker(hbCtx, progressCB, round, brief.TaskID, progressHeartbeatInterval)
			}

			params := effectiveRuntimeParams(r.cfg.Params, r.cfg.MaxTokens, r.cfg.Temperature)
			resp, err := r.cfg.LLM.ChatStream(ctx, llm.ChatRequest{
				Model:       r.cfg.Model,
				Messages:    messages,
				System:      system,
				Params:      params,
				MaxTokens:   params.MaxTokens,
				Temperature: derefFloat(params.Temperature, r.cfg.Temperature),
				Stream:      false,
				Tools:       tools,
			}, func(llm.StreamEvent) {})
			if err != nil {
				if journal != nil {
					journal.Write("task_error", round, map[string]any{"error": err.Error(), "last_round": round})
				}
				report := failedReport(brief, "llm request failed: "+err.Error())
				return RunOutcome{Report: &report}, true
			}
			if resp.StopReason != "tool_use" {
				report := ParseOrFallback(resp.Content, brief)
				return RunOutcome{Report: &report}, true
			}

			messages = append(messages, llm.Message{
				Role:             llm.RoleAssistant,
				Content:          resp.Content,
				ContentBlocks:    resp.ContentBlocks,
				ReasoningContent: resp.ReasoningContent,
			})

			calls := tool.ExtractToolCalls(resp)
			for _, call := range calls {
				if journal != nil {
					journal.Write("tool_call", round, map[string]any{
						"call_id": call.ID,
						"name":    call.Name,
						"input":   string(call.Input),
					})
				}
			}
			if progressCB != nil {
				emitToolProgress(progressCB, calls, round, brief.TaskID)
			}
			classifications := classifyToolCalls(r.cfg.Dispatcher, ctx, calls, permission)

			finishCall, hasFinish, mixedFinishCalls := pickFinishTaskCall(calls)
			if hasFinish {
				if mixedFinishCalls {
					if journal != nil {
						journal.Write("finish_violation", round, map[string]any{
							"reason": "finish_task must be sole call in the round",
						})
					}
					results := make([]tool.Result, 0, len(calls))
					for _, call := range calls {
						results = append(results, errorToolResult(call.ID, "finish_task must be the sole tool call in this round"))
					}
					logToolResults(journal, round, results)
					messages = r.appendResultsAndSnip(messages, results, journal, round)
					return RunOutcome{}, false
				}

				payload, err := ParseFinishTaskPayload(finishCall.Input)
				if err != nil {
					results := []tool.Result{errorToolResult(finishCall.ID, "invalid finish_task payload: "+err.Error())}
					logToolResults(journal, round, results)
					messages = r.appendResultsAndSnip(messages, results, journal, round)
					return RunOutcome{}, false
				}
				if progressCB != nil {
					progressCB(progress.Event{
						Kind:   progress.KindFinishing,
						Round:  round,
						TaskID: brief.TaskID,
					})
				}

				report := protocol.TaskReport{
					TaskID:        brief.TaskID,
					Status:        payload.Status,
					Goal:          brief.Goal,
					Summary:       payload.Summary,
					Findings:      append([]string(nil), payload.Findings...),
					OpenQuestions: append([]string(nil), payload.OpenQuestions...),
					CreatedAt:     time.Now().UTC(),
				}
				return RunOutcome{Report: &report}, true
			}

			decisionCall, hasDecision, mixedDecisionCalls := pickDecisionCall(calls)
			if hasDecision {
				if mixedDecisionCalls {
					if journal != nil {
						journal.Write("decision_violation", round, map[string]any{
							"reason": "request_decision must be sole call in the round",
						})
					}
					results := make([]tool.Result, 0, len(calls))
					for _, call := range calls {
						results = append(results, errorToolResult(call.ID, "request_decision must be the sole tool call in this round"))
					}
					logToolResults(journal, round, results)
					messages = r.appendResultsAndSnip(messages, results, journal, round)
					return RunOutcome{}, false
				}

				var packet protocol.DecisionPacket
				if err := json.Unmarshal(decisionCall.Input, &packet); err != nil {
					results := []tool.Result{errorToolResult(decisionCall.ID, "invalid decision packet JSON: "+err.Error())}
					logToolResults(journal, round, results)
					messages = r.appendResultsAndSnip(messages, results, journal, round)
					return RunOutcome{}, false
				}
				packetTaskID := strings.TrimSpace(packet.TaskID)
				switch {
				case packetTaskID == "", strings.EqualFold(packetTaskID, "unknown"):
					packet.TaskID = brief.TaskID
				case brief.TaskID != "" && packetTaskID != brief.TaskID:
					if r.cfg.Logger != nil {
						r.cfg.Logger.Warn("request_decision task_id mismatch; overriding to brief task_id",
							"packet_task_id", packet.TaskID,
							"brief_task_id", brief.TaskID,
						)
					}
					packet.TaskID = brief.TaskID
				}
				if packet.CreatedAt.IsZero() {
					packet.CreatedAt = time.Now().UTC()
				}
				if err := ValidateDecisionPacket(&packet, brief); err != nil {
					results := []tool.Result{errorToolResult(decisionCall.ID, "invalid decision packet: "+err.Error())}
					logToolResults(journal, round, results)
					messages = r.appendResultsAndSnip(messages, results, journal, round)
					return RunOutcome{}, false
				}

				if journal != nil {
					journal.Write("decision_request", round, map[string]any{
						"task_id":  packet.TaskID,
						"category": packet.Category,
						"risk":     derivedRiskLevel(packet.Category),
					})
				}
				if escalationCount >= r.cfg.MaxEscalations {
					report := failedReport(brief, fmt.Sprintf("max escalations exceeded (%d)", r.cfg.MaxEscalations))
					return RunOutcome{Report: &report}, true
				}

				outcome, results := r.routeDecision(ctx, brief, decisionCall.ID, packet, messages, workProgress, round, escalationCount, journal)
				if outcome.Report != nil || outcome.Paused != nil {
					return outcome, true
				}
				logToolResults(journal, round, results)
				messages = r.appendResultsAndSnip(messages, results, journal, round)
				escalationCount++
				return RunOutcome{}, false
			}

			if blocked, ok := firstClassificationByAction(classifications, tool.CallActionPermissionEscalationRequired); ok {
				blockedCall := blocked.Call
				messages[len(messages)-1] = filterAssistantMessageForBlockedCallPause(messages[len(messages)-1], blockedCall.ID)
				if journal != nil {
					journal.Write("permission_escalation_intercepted", round, map[string]any{
						"task_id": brief.TaskID,
						"call_id": blockedCall.ID,
						"name":    blockedCall.Name,
						"input":   string(blockedCall.Input),
					})
				}
				if escalationCount >= r.cfg.MaxEscalations {
					report := failedReport(brief, fmt.Sprintf("max escalations exceeded (%d)", r.cfg.MaxEscalations))
					return RunOutcome{Report: &report}, true
				}
				packet := buildPermissionEscalationPacket(brief, blockedCall)
				outcome, _ := r.routeDecision(ctx, brief, blockedCall.ID, packet, messages, workProgress, round, escalationCount, journal)
				if outcome.Paused != nil {
					callCopy := blockedCall
					outcome.Paused.PendingToolCall = &callCopy
				}
				return outcome, true
			}
			if blocked, ok := firstClassificationByAction(classifications, tool.CallActionToolApprovalRequired); ok {
				blockedCall := blocked.Call
				messages[len(messages)-1] = filterAssistantMessageForBlockedCallPause(messages[len(messages)-1], blockedCall.ID)
				packet := buildToolApprovalPacket(brief, blocked)
				if journal != nil {
					fields := map[string]any{
						"task_id":             brief.TaskID,
						"call_id":             blockedCall.ID,
						"name":                blockedCall.Name,
						"input":               string(blockedCall.Input),
						"approval_request_id": "",
						"approval_kind":       blocked.ApprovalKind,
						"approval_reason":     blocked.ApprovalReason,
						"destructive_reason":  blocked.DestructiveReason,
					}
					if packet.ToolApprovalBinding != nil {
						fields["binding_approval_kind"] = packet.ToolApprovalBinding.ApprovalKind
						fields["tool_name"] = packet.ToolApprovalBinding.ToolName
						fields["normalized_input_hash"] = packet.ToolApprovalBinding.NormalizedInputHash
						fields["path_digest"] = packet.ToolApprovalBinding.PathDigest
					}
					journal.Write("tool_approval_intercepted", round, fields)
				}
				if escalationCount >= r.cfg.MaxEscalations {
					report := failedReport(brief, fmt.Sprintf("max escalations exceeded (%d)", r.cfg.MaxEscalations))
					return RunOutcome{Report: &report}, true
				}
				outcome, _ := r.routeDecision(ctx, brief, blockedCall.ID, packet, messages, workProgress, round, escalationCount, journal)
				if outcome.Paused != nil {
					callCopy := blockedCall
					outcome.Paused.PendingToolCall = &callCopy
				}
				return outcome, true
			}

			results := r.cfg.Dispatcher.ExecuteAll(ctx, calls, permission)
			logToolResults(journal, round, results)
			messages = r.appendResultsAndSnip(messages, results, journal, round)
			return RunOutcome{}, false
		}()
		if shouldReturn {
			return outcome
		}
	}

	report := partialReport(brief, fmt.Sprintf("max tool rounds exhausted (%d)", r.cfg.MaxToolRounds))
	return RunOutcome{Report: &report}
}

func readScopeFromBrief(brief protocol.TaskBrief) tool.ReadScope {
	switch tool.ReadScope(strings.TrimSpace(brief.ReadScope)) {
	case tool.ReadScopeAll:
		return tool.ReadScopeAll
	default:
		return tool.ReadScopeWorkspace
	}
}

func emitToolProgress(cb progress.Callback, calls []tool.Call, round int, taskID string) {
	if cb == nil {
		return
	}
	for _, call := range calls {
		name := strings.TrimSpace(strings.ToLower(call.Name))
		if name == "" || name == "finish_task" || name == "request_decision" {
			continue
		}
		cb(progress.Event{
			Kind:     progress.KindTool,
			ToolName: name,
			Round:    round,
			TaskID:   taskID,
		})
		return
	}
}

func (r *Runtime) appendResultsAndSnip(messages []llm.Message, results []tool.Result, journal *Journal, round int) []llm.Message {
	preLen := len(messages)
	resultMessages := tool.ResultsToMessages(r.cfg.Provider, results)
	messages = append(messages, resultMessages...)

	if r.cfg.ToolSnipSoftTokens > 0 {
		preSnipTokens := estimateMessagesTokens(messages)
		currentRoundStart := preLen - 1
		if currentRoundStart < 0 {
			currentRoundStart = 0
		}
		messages = snipConsumedToolResults(messages, currentRoundStart, r.cfg.ToolSnipSoftTokens, r.cfg.ToolSnipHardTokens)
		if journal != nil {
			postSnipTokens := estimateMessagesTokens(messages)
			if preSnipTokens != postSnipTokens {
				journal.Write("tool_result_snipped", round, map[string]any{
					"before_tokens": preSnipTokens,
					"after_tokens":  postSnipTokens,
				})
			}
		}
	}

	return messages
}

func heartbeatTicker(ctx context.Context, cb progress.Callback, round int, taskID string, interval time.Duration) {
	if cb == nil || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cb(progress.Event{
				Kind:   progress.KindHeartbeat,
				Round:  round,
				TaskID: taskID,
			})
		}
	}
}

func (r *Runtime) routeDecision(
	ctx context.Context,
	brief protocol.TaskBrief,
	callID string,
	packet protocol.DecisionPacket,
	messages []llm.Message,
	workProgress WorkProgress,
	round int,
	escalationCount int,
	journal *Journal,
) (RunOutcome, []tool.Result) {
	if packet.Category == protocol.CatAuto &&
		!packet.SuggestsUserInput &&
		r.cfg.Decider != nil {
		decision, err := r.cfg.Decider.Decide(ctx, brief, packet)
		if err == nil && !decision.Escalate {
			response := protocol.DecisionResponse{
				TaskID:           brief.TaskID,
				Decision:         decision.Decision,
				Reason:           decision.Reason,
				ConstraintsDelta: decision.ConstraintsDelta,
			}
			payload, marshalErr := json.Marshal(response)
			if marshalErr == nil {
				if journal != nil {
					journal.Write("decision_response_runtime", round, map[string]any{
						"decision": response.Decision,
						"reason":   response.Reason,
					})
				}
				return RunOutcome{}, []tool.Result{{
					CallID:  callID,
					Content: payload,
					IsError: false,
				}}
			}
		}
	}

	compactedMessages, err := compactForPause(messages, r.cfg.PendingSnapshotMaxTokens)
	if err != nil {
		report := failedReport(brief, "failed to compact paused snapshot: "+err.Error())
		return RunOutcome{Report: &report}, nil
	}
	if journal != nil && estimateMessagesTokens(compactedMessages) != estimateMessagesTokens(messages) {
		journal.Write("task_paused_compaction", round, map[string]any{
			"before_tokens": estimateMessagesTokens(messages),
			"after_tokens":  estimateMessagesTokens(compactedMessages),
		})
	}

	paused := &PausedWork{
		TaskID:          brief.TaskID,
		Brief:           brief,
		Messages:        compactedMessages,
		Progress:        workProgress,
		PendingCallID:   callID,
		PendingToolCall: nil,
		Packet:          packet,
		Round:           round,
		EscalationCount: escalationCount + 1,
		CreatedAt:       time.Now().UTC(),
	}
	return RunOutcome{Paused: paused}, nil
}

func classifyToolCalls(dispatcher *tool.Dispatcher, ctx context.Context, calls []tool.Call, maxPermission tool.Permission) []tool.CallClassification {
	if dispatcher == nil {
		return nil
	}
	classifications := make([]tool.CallClassification, 0, len(calls))
	for _, call := range calls {
		classifications = append(classifications, dispatcher.ClassifyCall(ctx, call, maxPermission))
	}
	return classifications
}

func firstClassificationByAction(classifications []tool.CallClassification, action tool.CallAction) (tool.CallClassification, bool) {
	for _, classification := range classifications {
		if classification.Action == action {
			return classification, true
		}
	}
	return tool.CallClassification{}, false
}

func buildToolApprovalPacket(brief protocol.TaskBrief, classification tool.CallClassification) protocol.DecisionPacket {
	call := classification.Call
	kind := classification.ApprovalKind
	if kind == "" {
		kind = tool.ApprovalKindDestructiveWrite
	}
	var packetBinding *protocol.ToolApprovalBinding
	if binding, err := tool.BuildApprovalBinding(call, "", kind); err == nil {
		packetBinding = &protocol.ToolApprovalBinding{
			ApprovalKind:        binding.ApprovalKind,
			ToolName:            binding.ToolName,
			NormalizedInputHash: binding.NormalizedInputHash,
			PathDigest:          binding.PathDigest,
			InputPreview:        binding.InputPreview,
		}
	}
	return protocol.DecisionPacket{
		TaskID:               brief.TaskID,
		Category:             protocol.CatToolApproval,
		GoalSummary:          brief.Goal,
		Question:             toolApprovalQuestion(classification),
		WhyBlocked:           toolApprovalWhyBlocked(classification),
		Options:              []protocol.DecisionOption{{ID: "allow", Summary: "允许执行"}, {ID: "deny", Summary: "拒绝"}},
		RejectOptionID:       "deny",
		RecommendedOption:    "allow",
		RecommendationReason: toolApprovalRecommendation(brief),
		SuggestsUserInput:    false,
		ToolApprovalBinding:  packetBinding,
		CreatedAt:            time.Now().UTC(),
	}
}

func buildPermissionEscalationPacket(brief protocol.TaskBrief, call tool.Call) protocol.DecisionPacket {
	return protocol.DecisionPacket{
		TaskID:            brief.TaskID,
		Category:          protocol.CatPermissionEscalationRequired,
		GoalSummary:       brief.Goal,
		Question:          permissionEscalationQuestion(call),
		WhyBlocked:        fmt.Sprintf(`Tool %q requires destructive permission beyond the task's current scope.`, call.Name),
		Options:           []protocol.DecisionOption{{ID: "approve", Summary: "User approves destructive permission"}, {ID: "reject", Summary: "User rejects destructive permission"}},
		RejectOptionID:    "reject",
		SuggestsUserInput: true,
		CreatedAt:         time.Now().UTC(),
	}
}

func toolApprovalQuestion(classification tool.CallClassification) string {
	call := classification.Call
	if classification.ApprovalKind == tool.ApprovalKindSensitiveRead {
		return sensitiveReadApprovalQuestion(call)
	}
	switch strings.TrimSpace(call.Name) {
	case "bash":
		command := bashCommandPreview(call.Input)
		if command == "" {
			command = "<empty>"
		}
		return strings.Join([]string{
			"我准备执行一个受限命令，尚未执行。",
			"",
			"操作：执行 bash 命令",
			"命令：" + command,
			"风险：命令可能删除、覆盖、移动或重置文件。",
			"",
			"确认执行请点击“允许执行”；取消请点击“拒绝”。",
		}, "\n")
	case "write_file":
		target := toolApprovalPathTarget(call)
		return strings.Join([]string{
			"我准备执行一个受限文件操作，尚未执行。",
			"",
			"操作：写入文件",
			"目标：" + target,
			"风险：这可能覆盖已有文件或触碰敏感路径。",
			"影响：文件内容将被替换为本次工具输入中的新内容。",
			"",
			"确认执行请点击“允许执行”；取消请点击“拒绝”。",
		}, "\n")
	case "edit_file":
		target := toolApprovalPathTarget(call)
		risk := "这会修改敏感路径或大范围文件内容。"
		impact := "匹配的 old_string 会被替换为 new_string。"
		if editFileReplaceAll(call.Input) {
			risk = "replace_all=true，可能同时修改多个位置。"
			impact = "所有匹配的 old_string 都会被替换。"
		}
		return strings.Join([]string{
			"我准备执行一个受限文件编辑，尚未执行。",
			"",
			"操作：编辑文件",
			"目标：" + target,
			"风险：" + risk,
			"影响：" + impact,
			"",
			"确认执行请点击“允许执行”；取消请点击“拒绝”。",
		}, "\n")
	default:
		return strings.Join([]string{
			"我准备执行一个受限操作，尚未执行。",
			"",
			"操作：" + toolApprovalOperation(call),
			"目标：" + toolApprovalCallPreview(call),
			"风险：该工具调用需要显式批准。",
			"影响：批准后才会执行这一次工具输入对应的操作。",
			"",
			"确认执行请点击“允许执行”；取消请点击“拒绝”。",
		}, "\n")
	}
}

func toolApprovalWhyBlocked(classification tool.CallClassification) string {
	if classification.ApprovalKind == tool.ApprovalKindSensitiveRead {
		if reason := strings.TrimSpace(classification.ApprovalReason); reason != "" {
			return fmt.Sprintf(`Tool %q requires explicit sensitive-read approval before execution: %s.`, classification.Call.Name, reason)
		}
		return fmt.Sprintf(`Tool %q requires explicit sensitive-read approval before execution.`, classification.Call.Name)
	}
	return fmt.Sprintf(`Tool %q requires explicit human approval before execution.`, classification.Call.Name)
}

func sensitiveReadApprovalQuestion(call tool.Call) string {
	operation := "读取文件"
	if strings.TrimSpace(call.Name) == "list_dir" {
		operation = "列出目录"
	}
	return strings.Join([]string{
		"我准备执行一次敏感读取，尚未执行。",
		"",
		"操作：" + operation,
		"目标：" + toolApprovalPathTarget(call),
		"原因：目标位于敏感路径或可能包含凭据、密钥、令牌、账号配置等信息。",
		"影响：确认后，我会把该文件/目录内容作为本次任务证据读取；不会修改任何文件。",
		"",
		"确认读取请点击“允许执行”；取消请点击“拒绝”。",
	}, "\n")
}

func permissionEscalationQuestion(call tool.Call) string {
	if call.Name == "bash" {
		var payload struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(call.Input, &payload); err == nil {
			command := strings.TrimSpace(payload.Command)
			if command != "" {
				return "Ask the user whether to approve destructive permission for: " + command
			}
		}
	}
	return fmt.Sprintf(`Ask the user whether to grant destructive permission for tool %q.`, call.Name)
}

func toolApprovalOperation(call tool.Call) string {
	if strings.EqualFold(strings.TrimSpace(call.Name), "bash") {
		return "执行 bash 命令"
	}
	if name := strings.TrimSpace(call.Name); name != "" {
		return "执行工具 " + name
	}
	return "执行受限操作"
}

func toolApprovalCallPreview(call tool.Call) string {
	name := strings.TrimSpace(call.Name)
	if name == "" {
		name = "unknown_tool"
	}
	if summary := tool.InputPreviewForCall(call); summary != "" {
		preview, _ := truncateContent(name+" ("+summary+")", 320)
		return preview
	}
	preview, _ := truncateContent(name, 320)
	return preview
}

func toolApprovalPathTarget(call tool.Call) string {
	var payload struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(call.Input, &payload); err == nil && strings.TrimSpace(payload.Path) != "" {
		preview, _ := truncateContent(filepath.ToSlash(filepath.Clean(strings.TrimSpace(payload.Path))), 320)
		return preview
	}
	return toolApprovalCallPreview(call)
}

func editFileReplaceAll(input json.RawMessage) bool {
	var payload struct {
		ReplaceAll bool `json:"replace_all"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return false
	}
	return payload.ReplaceAll
}

func toolApprovalRecommendation(brief protocol.TaskBrief) string {
	reason := "为完成当前任务，需要执行这一步。"
	if goal := strings.TrimSpace(brief.Goal); goal != "" {
		reason += " 任务目标：" + goal
	}
	preview, _ := truncateContent(reason, 380)
	return preview
}

func bashCommandPreview(input json.RawMessage) string {
	var payload struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return ""
	}
	command := strings.TrimSpace(payload.Command)
	if command == "" {
		return ""
	}
	preview, _ := truncateContent(command, 320)
	return preview
}

func filterAssistantMessageForBlockedCallPause(message llm.Message, blockedCallID string) llm.Message {
	if blockedCallID == "" || len(message.ContentBlocks) == 0 {
		return message
	}
	filtered := make([]llm.ContentBlock, 0, len(message.ContentBlocks))
	for _, block := range message.ContentBlocks {
		if block.Type != "tool_use" {
			filtered = append(filtered, block)
			continue
		}
		if block.ID == blockedCallID {
			filtered = append(filtered, block)
		}
	}
	message.ContentBlocks = filtered
	return message
}

func pickDecisionCall(calls []tool.Call) (tool.Call, bool, bool) {
	var picked tool.Call
	count := 0
	for _, call := range calls {
		if call.Name != "request_decision" {
			continue
		}
		if count == 0 {
			picked = call
		}
		count++
	}
	if count == 0 {
		return tool.Call{}, false, false
	}
	return picked, true, len(calls) > 1
}

func pickFinishTaskCall(calls []tool.Call) (tool.Call, bool, bool) {
	var picked tool.Call
	count := 0
	for _, call := range calls {
		if call.Name != "finish_task" {
			continue
		}
		if count == 0 {
			picked = call
		}
		count++
	}
	if count == 0 {
		return tool.Call{}, false, false
	}
	return picked, true, len(calls) > 1
}

func logToolResults(journal *Journal, round int, results []tool.Result) {
	for _, result := range results {
		preview, truncated := truncateContent(string(result.Content), 500)
		if journal != nil {
			journal.Write("tool_result", round, map[string]any{
				"call_id":         result.CallID,
				"content_preview": preview,
				"truncated":       truncated,
				"is_error":        result.IsError,
			})
		}
	}
}

func errorToolResult(callID, message string) tool.Result {
	errJSON, _ := json.Marshal(map[string]string{"error": message})
	return tool.Result{
		CallID:  callID,
		Content: errJSON,
		IsError: true,
	}
}

func failedReport(brief protocol.TaskBrief, reason string) protocol.TaskReport {
	return protocol.TaskReport{
		TaskID:    brief.TaskID,
		Status:    "failed",
		Goal:      brief.Goal,
		Summary:   reason,
		CreatedAt: brief.CreatedAt,
	}
}

func partialReport(brief protocol.TaskBrief, reason string) protocol.TaskReport {
	return protocol.TaskReport{
		TaskID:    brief.TaskID,
		Status:    "partial",
		Goal:      brief.Goal,
		Summary:   reason,
		CreatedAt: brief.CreatedAt,
	}
}

func truncateContent(text string, maxRunes int) (string, bool) {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text, false
	}
	return string(runes[:maxRunes]) + "...", true
}

func estimateMessagesTokens(messages []llm.Message) int {
	total := 0
	for _, message := range messages {
		total += contextutil.EstimateTokens(message.Content)
		total += contextutil.EstimateTokens(message.ReasoningContent)
		for _, block := range message.ContentBlocks {
			total += contextutil.EstimateTokens(block.Text)
			total += contextutil.EstimateTokens(string(block.Input))
			total += contextutil.EstimateTokens(block.Content)
		}
	}
	return total
}

func effectiveRuntimeParams(params llm.RequestParams, maxTokens int, temperature float64) llm.RequestParams {
	if hasRuntimeParams(params) {
		return params
	}
	stream := false
	return llm.RequestParams{
		MaxTokens:   maxTokens,
		Temperature: &temperature,
		Stream:      &stream,
	}
}

func hasRuntimeParams(params llm.RequestParams) bool {
	return params.MaxTokens != 0 ||
		params.Temperature != nil ||
		params.TopP != nil ||
		params.PresencePenalty != nil ||
		params.FrequencyPenalty != nil ||
		params.ReasoningEffort != "" ||
		params.Thinking != nil ||
		params.Stream != nil ||
		len(params.Extra) > 0
}

func derefFloat(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}
