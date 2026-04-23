package work

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
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
	Provider                 string
	Model                    string
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
			result = r.cfg.Dispatcher.Execute(ctx, *paused.PendingToolCall, permission)
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

	return r.runLoop(ctx, paused.Brief, messages, paused.Progress, paused.Round+1, paused.EscalationCount, journal)
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
			compressed, newProgress, err := compressWorkContext(
				ctx, r.cfg.SummaryClient, r.cfg.SummaryModel,
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

			resp, err := r.cfg.LLM.ChatStream(ctx, llm.ChatRequest{
				Model:       r.cfg.Model,
				Messages:    messages,
				System:      system,
				MaxTokens:   r.cfg.MaxTokens,
				Temperature: r.cfg.Temperature,
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
			if blockedCall, ok := findPermissionEscalationBlockedCall(r.cfg.Dispatcher, ctx, calls, permission); ok {
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
			if blockedCall, ok := findApprovalBlockedCall(r.cfg.Dispatcher, ctx, calls, permission); ok {
				messages[len(messages)-1] = filterAssistantMessageForBlockedCallPause(messages[len(messages)-1], blockedCall.ID)
				if journal != nil {
					journal.Write("tool_approval_intercepted", round, map[string]any{
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
				packet := buildToolApprovalPacket(brief, blockedCall)
				outcome, _ := r.routeDecision(ctx, brief, blockedCall.ID, packet, messages, workProgress, round, escalationCount, journal)
				if outcome.Paused != nil {
					callCopy := blockedCall
					outcome.Paused.PendingToolCall = &callCopy
				}
				return outcome, true
			}

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
						switch call.Name {
						case "finish_task":
							results = append(results, errorToolResult(call.ID, "finish_task must be the sole tool call in this round"))
						case "request_decision":
							results = append(results, errorToolResult(call.ID, "request_decision must be the sole tool call in this round"))
						default:
							results = append(results, r.cfg.Dispatcher.Execute(ctx, call, permission))
						}
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
						if call.Name == "request_decision" {
							results = append(results, errorToolResult(call.ID, "request_decision must be the sole tool call in this round"))
							continue
						}
						results = append(results, r.cfg.Dispatcher.Execute(ctx, call, permission))
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

func findApprovalBlockedCall(dispatcher *tool.Dispatcher, ctx context.Context, calls []tool.Call, maxPermission tool.Permission) (tool.Call, bool) {
	if dispatcher == nil {
		return tool.Call{}, false
	}
	for _, call := range calls {
		if dispatcher.WouldNeedApproval(ctx, call, maxPermission) {
			return call, true
		}
	}
	return tool.Call{}, false
}

func findPermissionEscalationBlockedCall(dispatcher *tool.Dispatcher, ctx context.Context, calls []tool.Call, maxPermission tool.Permission) (tool.Call, bool) {
	if dispatcher == nil {
		return tool.Call{}, false
	}
	for _, call := range calls {
		if dispatcher.WouldNeedPermissionEscalation(ctx, call, maxPermission) {
			return call, true
		}
	}
	return tool.Call{}, false
}

func buildToolApprovalPacket(brief protocol.TaskBrief, call tool.Call) protocol.DecisionPacket {
	return protocol.DecisionPacket{
		TaskID:               brief.TaskID,
		Category:             protocol.CatToolApproval,
		GoalSummary:          brief.Goal,
		Question:             toolApprovalQuestion(call),
		WhyBlocked:           fmt.Sprintf(`Tool %q requires explicit human approval before execution.`, call.Name),
		Options:              []protocol.DecisionOption{{ID: "allow", Summary: "Allow execution"}, {ID: "deny", Summary: "Deny execution"}},
		RejectOptionID:       "deny",
		RecommendedOption:    "allow",
		RecommendationReason: toolApprovalRecommendation(brief),
		SuggestsUserInput:    false,
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

func toolApprovalQuestion(call tool.Call) string {
	lines := []string{"操作：" + toolApprovalOperation(call)}
	if command := bashCommandPreview(call.Input); command != "" {
		lines = append(lines, "命令："+command)
		return strings.Join(lines, "\n")
	}
	lines = append(lines, "调用："+toolApprovalCallPreview(call))
	return strings.Join(lines, "\n")
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
	if summary := summarizeToolApprovalInput(call.Input); summary != "" {
		preview, _ := truncateContent(name+" ("+summary+")", 320)
		return preview
	}
	preview, _ := truncateContent(name, 320)
	return preview
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

func summarizeToolApprovalInput(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}

	var object map[string]any
	if err := json.Unmarshal(input, &object); err == nil && len(object) > 0 {
		keys := make([]string, 0, len(object))
		for key := range object {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		parts := make([]string, 0, minInt(len(keys), 4))
		for i, key := range keys {
			if i >= 4 {
				parts = append(parts, "...")
				break
			}
			parts = append(parts, key+"="+formatToolApprovalValue(object[key]))
		}
		preview, _ := truncateContent(strings.Join(parts, ", "), 240)
		return preview
	}

	var scalar any
	if err := json.Unmarshal(input, &scalar); err == nil {
		preview, _ := truncateContent(formatToolApprovalValue(scalar), 240)
		return preview
	}
	return ""
}

func formatToolApprovalValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		if typed == "" {
			return `""`
		}
		return typed
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case []any:
		if len(typed) == 0 {
			return "[]"
		}
		parts := make([]string, 0, minInt(len(typed), 3))
		for i, item := range typed {
			if i >= 3 {
				parts = append(parts, "...")
				break
			}
			parts = append(parts, formatToolApprovalValue(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		return "<object>"
	default:
		return fmt.Sprint(typed)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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
