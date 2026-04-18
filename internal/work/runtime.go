package work

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/runtimeenv"
	"github.com/longyisang/emoagent/internal/tool"
)

const (
	defaultMaxEscalations        = 3
	defaultPendingSnapshotTokens = 60000
)

// RuntimeConfig describes the dependencies for one Work runtime instance.
type RuntimeConfig struct {
	LLM                      llm.Client
	Provider                 string
	Model                    string
	MaxTokens                int
	Temperature              float64
	MaxToolRounds            int
	MaxInputTokens           int
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
	PendingCallID   string
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
	return r.runLoop(ctx, brief, nil, 0, 0, journal)
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

	payload, err := json.Marshal(decision)
	if err != nil {
		report := failedReport(paused.Brief, "resume failed: marshal decision response: "+err.Error())
		return RunOutcome{Report: &report}
	}

	messages := append([]llm.Message(nil), paused.Messages...)
	result := tool.Result{
		CallID:  paused.PendingCallID,
		Content: payload,
		IsError: false,
	}
	messages = append(messages, tool.ResultsToMessages(r.cfg.Provider, []tool.Result{result})...)
	if journal != nil {
		journal.Write("task_resumed", paused.Round, map[string]any{
			"task_id":  paused.TaskID,
			"decision": decision.Decision,
		})
	}

	return r.runLoop(ctx, paused.Brief, messages, paused.Round+1, paused.EscalationCount, journal)
}

func (r *Runtime) runLoop(
	ctx context.Context,
	brief protocol.TaskBrief,
	seedMessages []llm.Message,
	startRound int,
	escalationCount int,
	journal *Journal,
) RunOutcome {
	system := BuildWorkSystem(brief, r.cfg.EnvironmentFacts)
	tools := r.cfg.Registry.ForScope(tool.ScopeWork)
	permission := tool.Permission(brief.PermissionScope)
	messages := append([]llm.Message(nil), seedMessages...)

	for round := startRound; round < r.cfg.MaxToolRounds; round++ {
		if err := ctx.Err(); err != nil {
			if journal != nil {
				journal.Write("task_error", round, map[string]any{"error": err.Error(), "last_round": round})
			}
			report := failedReport(brief, "context canceled: "+err.Error())
			return RunOutcome{Report: &report}
		}
		if r.cfg.MaxInputTokens > 0 &&
			estimateMessagesTokens(messages)+contextutil.EstimateTokens(system) > r.cfg.MaxInputTokens {
			report := partialReport(brief, fmt.Sprintf("max input tokens exceeded (%d)", r.cfg.MaxInputTokens))
			return RunOutcome{Report: &report}
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
			return RunOutcome{Report: &report}
		}
		if resp.StopReason != "tool_use" {
			report := ParseOrFallback(resp.Content, brief)
			return RunOutcome{Report: &report}
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
				messages = append(messages, tool.ResultsToMessages(r.cfg.Provider, results)...)
				continue
			}

			payload, err := ParseFinishTaskPayload(finishCall.Input)
			if err != nil {
				results := []tool.Result{errorToolResult(finishCall.ID, "invalid finish_task payload: "+err.Error())}
				logToolResults(journal, round, results)
				messages = append(messages, tool.ResultsToMessages(r.cfg.Provider, results)...)
				continue
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
			return RunOutcome{Report: &report}
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
				messages = append(messages, tool.ResultsToMessages(r.cfg.Provider, results)...)
				continue
			}

			var packet protocol.DecisionPacket
			if err := json.Unmarshal(decisionCall.Input, &packet); err != nil {
				results := []tool.Result{errorToolResult(decisionCall.ID, "invalid decision packet JSON: "+err.Error())}
				logToolResults(journal, round, results)
				messages = append(messages, tool.ResultsToMessages(r.cfg.Provider, results)...)
				continue
			}
			if packet.TaskID == "" {
				packet.TaskID = brief.TaskID
			}
			if packet.CreatedAt.IsZero() {
				packet.CreatedAt = time.Now().UTC()
			}
			if err := ValidateDecisionPacket(&packet, brief); err != nil {
				results := []tool.Result{errorToolResult(decisionCall.ID, "invalid decision packet: "+err.Error())}
				logToolResults(journal, round, results)
				messages = append(messages, tool.ResultsToMessages(r.cfg.Provider, results)...)
				continue
			}

			if journal != nil {
				journal.Write("decision_request", round, map[string]any{
					"task_id":  packet.TaskID,
					"category": packet.Category,
					"risk":     packet.RiskLevel,
				})
			}
			if escalationCount >= r.cfg.MaxEscalations {
				report := failedReport(brief, fmt.Sprintf("max escalations exceeded (%d)", r.cfg.MaxEscalations))
				return RunOutcome{Report: &report}
			}

			outcome, results := r.routeDecision(ctx, brief, decisionCall.ID, packet, messages, round, escalationCount, journal)
			if outcome.Report != nil || outcome.Paused != nil {
				return outcome
			}
			logToolResults(journal, round, results)
			messages = append(messages, tool.ResultsToMessages(r.cfg.Provider, results)...)
			escalationCount++
			continue
		}

		results := r.cfg.Dispatcher.ExecuteAll(ctx, calls, permission)
		logToolResults(journal, round, results)
		messages = append(messages, tool.ResultsToMessages(r.cfg.Provider, results)...)
	}

	report := partialReport(brief, fmt.Sprintf("max tool rounds exhausted (%d)", r.cfg.MaxToolRounds))
	return RunOutcome{Report: &report}
}

func (r *Runtime) routeDecision(
	ctx context.Context,
	brief protocol.TaskBrief,
	callID string,
	packet protocol.DecisionPacket,
	messages []llm.Message,
	round int,
	escalationCount int,
	journal *Journal,
) (RunOutcome, []tool.Result) {
	if packet.Category == protocol.CatExecutionOnly &&
		packet.RiskLevel == "low" &&
		!packet.SuggestsUserInput &&
		r.cfg.Decider != nil {
		decision, err := r.cfg.Decider.Decide(ctx, brief, packet)
		if err == nil && !decision.Escalate {
			response := protocol.DecisionResponse{
				TaskID:           brief.TaskID,
				Decision:         decision.Decision,
				Reason:           decision.Reason,
				ConstraintsDelta: decision.ConstraintsDelta,
				StyleDelta:       decision.StyleDelta,
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
		PendingCallID:   callID,
		Packet:          packet,
		Round:           round,
		EscalationCount: escalationCount + 1,
		CreatedAt:       time.Now().UTC(),
	}
	return RunOutcome{Paused: paused}, nil
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
