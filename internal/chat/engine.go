package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent/internal/config"
	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/progress"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/runtimeenv"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/work"
)

var errApprovalPending = errors.New("approval pending")

// EngineConfig defines the dependencies for Engine.
type EngineConfig struct {
	LLM                llm.Client
	DB                 *storage.DB
	Logger             *slog.Logger
	Model              string
	SummaryModel       string
	SummaryTemperature *float64
	MaxTokens          int
	Temperature        float64
	ContextConfig      config.ContextConfig
	Provider           string           // "openai" or "anthropic", needed by ResultsToMessages
	Registry           *tool.Registry   // nil disables tool support
	Dispatcher         *tool.Dispatcher // nil disables tool support
	Pending            *work.PendingRegistry
	Approvals          *work.ApprovalService
	Environment        runtimeenv.Facts
	RealtimeStreaming  bool
}

// RuntimeConfig is the hot-swappable subset of EngineConfig used for new requests.
type RuntimeConfig struct {
	Provider           string
	Model              string
	SummaryModel       string
	SummaryTemperature *float64
	MaxTokens          int
	Temperature        float64
	ContextConfig      config.ContextConfig
	RealtimeStreaming  bool
}

// Engine assembles conversation context and forwards requests to the LLM.
type Engine struct {
	mu                 sync.RWMutex
	llm                llm.Client
	db                 *storage.DB
	logger             *slog.Logger
	model              string
	summaryModel       string
	summaryTemperature *float64
	maxTokens          int
	temperature        float64
	contextCfg         config.ContextConfig
	provider           string
	registry           *tool.Registry
	dispatcher         *tool.Dispatcher
	pending            *work.PendingRegistry
	approvals          *work.ApprovalService
	environment        runtimeenv.Facts
	realtimeStreaming  bool
}

// UpdateConfig hot-swaps the active LLM client and request parameters for new sends.
func (e *Engine) UpdateConfig(client llm.Client, provider, model, summaryModel string, summaryTemperature *float64, maxTokens int, temperature float64, contextCfg config.ContextConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if client != nil {
		e.llm = client
	}
	e.provider = provider
	e.model = model
	e.summaryModel = summaryModel
	e.summaryTemperature = cloneFloat64Ptr(summaryTemperature)
	e.maxTokens = maxTokens
	e.temperature = temperature
	if err := contextCfg.Validate(); err == nil {
		e.contextCfg = contextCfg
	}
}

// UpdateRealtimeStreaming hot-swaps the browser streaming mode for new sends.
func (e *Engine) UpdateRealtimeStreaming(enabled bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.realtimeStreaming = enabled
}

// NewEngine creates a chat engine from configuration.
func NewEngine(cfg EngineConfig) *Engine {
	contextCfg := cfg.ContextConfig
	if err := contextCfg.Validate(); err != nil {
		contextCfg = config.DefaultConfig().Context
	}

	return &Engine{
		llm:                cfg.LLM,
		db:                 cfg.DB,
		logger:             cfg.Logger,
		model:              cfg.Model,
		summaryModel:       cfg.SummaryModel,
		summaryTemperature: cloneFloat64Ptr(cfg.SummaryTemperature),
		maxTokens:          cfg.MaxTokens,
		temperature:        cfg.Temperature,
		contextCfg:         contextCfg,
		provider:           cfg.Provider,
		registry:           cfg.Registry,
		dispatcher:         cfg.Dispatcher,
		pending:            cfg.Pending,
		approvals:          cfg.Approvals,
		environment:        cfg.Environment,
		realtimeStreaming:  cfg.RealtimeStreaming,
	}
}

// RuntimeConfig returns a snapshot of the engine's active request configuration.
func (e *Engine) RuntimeConfig() RuntimeConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return RuntimeConfig{
		Provider:           e.provider,
		Model:              e.model,
		SummaryModel:       e.summaryModel,
		SummaryTemperature: cloneFloat64Ptr(e.summaryTemperature),
		MaxTokens:          e.maxTokens,
		Temperature:        e.temperature,
		ContextConfig:      e.contextCfg,
		RealtimeStreaming:  e.realtimeStreaming,
	}
}

// StartSession creates and persists a new chat session.
func (e *Engine) StartSession(ctx context.Context, personaName string) (string, error) {
	if e.db == nil {
		return "", errors.New("chat engine database is not configured")
	}

	sessionID := uuid.NewString()
	if err := e.db.CreateSession(ctx, sessionID, personaName); err != nil {
		return "", err
	}
	return sessionID, nil
}

// ResumeSession validates an existing session against the requested persona key.
func (e *Engine) ResumeSession(ctx context.Context, sessionID string, personaKey string) (string, bool, error) {
	if sessionID == "" {
		return "", false, nil
	}
	if e.db == nil {
		return "", false, errors.New("chat engine database is not configured")
	}

	session, err := e.db.GetSession(ctx, sessionID)
	if err != nil {
		return "", false, err
	}
	if session == nil || session.Persona != personaKey {
		return "", false, nil
	}
	return sessionID, true, nil
}

// GetHistory returns the recent message history for a session.
func (e *Engine) GetHistory(ctx context.Context, sessionID string, limit int) ([]storage.MessageRecord, error) {
	if e.db == nil {
		return nil, errors.New("chat engine database is not configured")
	}
	return e.db.GetRecentMessages(ctx, sessionID, limit)
}

// SendMessage stores the user message, streams the model response, and persists the reply.
func (e *Engine) SendMessage(ctx context.Context, sessionID string, persona *config.Persona, userContent string, cb func(delta string)) (string, error) {
	return e.sendTurn(ctx, sessionID, persona, cb, turnOptions{
		persistUser: true,
		userContent: userContent,
	})
}

type turnOptions struct {
	persistUser bool
	userContent string
	extraSystem string
}

func (e *Engine) sendTurn(ctx context.Context, sessionID string, persona *config.Persona, cb func(delta string), opts turnOptions) (string, error) {
	e.mu.RLock()
	client := e.llm
	model := e.model
	summaryModel := e.summaryModel
	summaryTemperature := cloneFloat64Ptr(e.summaryTemperature)
	maxTokens := e.maxTokens
	temperature := e.temperature
	contextCfg := e.contextCfg
	provider := e.provider
	registry := e.registry
	dispatcher := e.dispatcher
	pending := e.pending
	approvals := e.approvals
	env := e.environment
	realtimeStreaming := e.realtimeStreaming
	e.mu.RUnlock()

	if client == nil {
		return "", errors.New("chat engine LLM client is not configured")
	}
	if e.db == nil {
		return "", errors.New("chat engine database is not configured")
	}
	if persona == nil {
		return "", errors.New("persona is required")
	}

	if opts.persistUser {
		if err := e.db.AddMessageWithMetadata(ctx, uuid.NewString(), sessionID, "user", opts.userContent, visibleMessageMetadata("user", opts.userContent)); err != nil {
			e.logger.Error("failed to store user message", "session", sessionID, "error", err)
			return "", err
		}
		if err := e.db.UpdateSessionTimestamp(ctx, sessionID); err != nil {
			e.logger.Error("failed to update session timestamp", "session", sessionID, "error", err)
			return "", err
		}

		// Auto-generate session title from the first user message.
		session, err := e.db.GetSession(ctx, sessionID)
		if err == nil && session != nil && session.Title == "" {
			title := opts.userContent
			if runeCount := len([]rune(title)); runeCount > 30 {
				title = string([]rune(title)[:30]) + "…"
			}
			if err := e.db.UpdateSessionTitle(ctx, sessionID, title); err != nil {
				e.logger.Warn("failed to set session title", "session", sessionID, "error", err)
			}
		}
	}

	history, err := e.db.GetAllMessages(ctx, sessionID)
	if err != nil {
		e.logger.Error("failed to load message history", "session", sessionID, "error", err)
		return "", err
	}

	state, err := contextutil.LoadSessionState(ctx, e.db, sessionID, contextCfg)
	if err != nil {
		e.logger.Warn("failed to load session context state", "session", sessionID, "error", err)
		defaultState := contextutil.ContextState{
			ContextVersion:      contextutil.CurrentContextVersion,
			Mode:                contextutil.ModeEmotion,
			KeepRecentUserTurns: contextCfg.KeepRecentUserTurns,
		}
		state = &defaultState
	}

	if nextState, updateErr := contextutil.UpdateRunningSummary(ctx, client, effectiveSummaryModel(model, summaryModel), summaryTemperature, persona, history, state, contextCfg); updateErr != nil {
		e.logger.Warn("failed to update running summary", "session", sessionID, "error", updateErr)
	} else {
		state = nextState
	}

	var pendingDecisions []protocol.DecisionSummary
	if pending != nil {
		pendingDecisions = append(pendingDecisions, pending.ListInjectable(sessionID)...)
	}

	var assembled contextutil.AssembledContext
	if len(pendingDecisions) > 0 {
		assembled, err = contextutil.BuildEmotionContextWithPendingSummaries(persona, history, state, pendingDecisions, contextCfg, env)
	} else {
		assembled, err = contextutil.BuildEmotionContextWithState(persona, history, state, contextCfg, env)
	}
	if err != nil {
		e.logger.Error("failed to assemble llm context", "session", sessionID, "error", err)
		return "", err
	}
	if opts.extraSystem != "" {
		assembled.System += "\n\n" + opts.extraSystem
	}
	if state != nil {
		state.ContextVersion = contextutil.CurrentContextVersion
		state.Mode = contextutil.ModeEmotion
		state.LastInputEstimate = assembled.Budget.EstimatedTokens
		state.KeepRecentUserTurns = contextCfg.KeepRecentUserTurns
		if err := contextutil.UpdateSessionContextState(ctx, e.db, sessionID, *state); err != nil {
			e.logger.Warn("failed to persist session context state", "session", sessionID, "error", err)
		}
	}
	messages := append([]llm.Message(nil), assembled.Messages...)

	// maxToolRounds prevents infinite tool call loops.
	const maxToolRounds = 10

	// Populate available tools only when the execution pipeline is enabled.
	var tools []llm.ToolDef
	if registry != nil && dispatcher != nil {
		tools = registry.ForScope(tool.ScopeEmotion)
	}

	req := llm.ChatRequest{
		Model:       model,
		Messages:    messages,
		System:      assembled.System,
		MaxTokens:   maxTokens,
		Temperature: temperature,
		Stream:      true,
		Tools:       tools,
	}
	e.logger.Info("llm request",
		"session", sessionID,
		"persona", persona.Name,
		"model", model,
		"history_len", len(messages),
		"estimated_tokens", assembled.Budget.EstimatedTokens,
		"tools_count", len(tools),
	)
	e.logger.Debug("llm context",
		"system", req.System,
		"messages", messages,
	)

	start := time.Now()
	var resp *llm.ChatResponse
	reactiveRetryUsed := false
	var reactiveRetryReport *contextutil.CompactReport
	ctx = work.WithSessionID(ctx, sessionID)
	var approvalSnapshot map[string]string
	rawWriter := wsWriterFromContext(ctx)
	if rawWriter != nil && approvals != nil {
		approvalSnapshot = snapshotApprovalStatuses(approvals.ListSessionApprovals(sessionID, nil))
	}
	if rawWriter != nil {
		throttler := progress.NewThrottler(3 * time.Second)
		personaPhrases := persona.WorkProgressPhrases
		ctx = progress.WithCallback(ctx, func(event progress.Event) {
			if event.Kind == progress.KindEnd {
				rawWriter(WSMessage{Type: "work_progress_end"})
				return
			}
			if !throttler.ShouldEmit(event) {
				return
			}
			phrase := progress.Resolve(event, personaPhrases)
			if phrase == "" {
				return
			}
			rawWriter(WSMessage{Type: "work_progress", Content: phrase})
		})
	}

	var assistantContent string
	var visibleBuilder strings.Builder
	for round := 0; ; round++ {
		var roundDeltas []string
		resp, err = client.ChatStream(ctx, req, func(event llm.StreamEvent) {
			if event.Content != "" {
				roundDeltas = append(roundDeltas, event.Content)
				if realtimeStreaming {
					visibleBuilder.WriteString(event.Content)
					if cb != nil {
						cb(event.Content)
					}
				}
			}
		})
		if err != nil {
			if !reactiveRetryUsed && llm.IsKind(err, llm.ErrorKindContextOverflow) {
				compacted, report, compactErr := contextutil.ApplyReactiveCompact(sessionID, req.Messages, state, effectiveSummaryModel(model, summaryModel), contextCfg)
				if compactErr != nil {
					e.logger.Warn("reactive compact failed",
						"session_id", sessionID,
						"mode", "reactive",
						"compact_reason", "reactive_overflow",
						"retry_attempt", 1,
						"error_kind", llm.ErrorKindContextOverflow,
						"error", compactErr,
					)
					return "", err
				}
				report.SessionID = sessionID
				logCompactReport(e.logger, slog.LevelInfo, report, 1, llm.ErrorKindContextOverflow, "")
				reportCopy := report
				reactiveRetryReport = &reportCopy
				messages = append([]llm.Message(nil), compacted...)
				req.Messages = messages
				reactiveRetryUsed = true
				round--
				continue
			}
			if reactiveRetryUsed && reactiveRetryReport != nil {
				logCompactFailure(e.logger, *reactiveRetryReport, 1, errorKindOf(err), err)
				return "", err
			}
			e.logger.Error("llm request failed", "session", sessionID, "round", round, "error", err)
			return "", err
		}

		if resp.StopReason != "tool_use" {
			if realtimeStreaming {
				if visibleBuilder.Len() == 0 && resp.Content != "" {
					visibleBuilder.WriteString(resp.Content)
					if cb != nil {
						cb(resp.Content)
					}
				}
				assistantContent = visibleBuilder.String()
				if assistantContent == "" {
					assistantContent = resp.Content
				}
			} else if cb != nil {
				for _, delta := range roundDeltas {
					cb(delta)
				}
				assistantContent = resp.Content
			} else {
				assistantContent = resp.Content
			}
			break
		}

		if dispatcher == nil {
			e.logger.Error("llm requested tool_use but tool execution is disabled", "session", sessionID, "round", round)
			return "", errors.New("tool_use requested but tool execution is not enabled")
		}

		// Safety: prevent runaway tool loops.
		if round >= maxToolRounds {
			e.logger.Error("tool loop exceeded max rounds", "session", sessionID, "max_rounds", maxToolRounds)
			return "", fmt.Errorf("tool loop exceeded maximum rounds (%d)", maxToolRounds)
		}

		// --- Tool loop: execute called tools and continue. ---
		e.logger.Info("tool_use detected", "session", sessionID, "round", round)

		// 1. Append assistant message (with tool_use ContentBlocks) to in-memory
		//    context. These intermediate messages are NOT persisted to DB.
		assistantText := resp.Content
		if assistantText == "" && len(roundDeltas) > 0 {
			assistantText = strings.Join(roundDeltas, "")
		}
		assistantMsg := llm.Message{
			Role:             llm.RoleAssistant,
			Content:          assistantText,
			ContentBlocks:    resp.ContentBlocks,
			ReasoningContent: resp.ReasoningContent,
		}
		messages = append(messages, assistantMsg)

		// 2. Extract and execute tool calls.
		calls := tool.ExtractToolCalls(resp)
		if len(calls) == 0 {
			e.logger.Warn("tool_use stop reason but no tool calls extracted", "session", sessionID)
			if realtimeStreaming {
				assistantContent = visibleBuilder.String()
				if assistantContent == "" {
					assistantContent = resp.Content
				}
			} else {
				assistantContent = resp.Content
			}
			break
		}
		for _, c := range calls {
			e.logger.Info("tool call", "session", sessionID, "tool", c.Name, "call_id", c.ID)
		}

		snippedResults := make([]tool.Result, len(calls))
		for i, call := range calls {
			if realtimeStreaming && rawWriter != nil {
				rawWriter(WSMessage{
					Type: "tool_call_start",
					Tool: &ToolActivity{
						ID:     call.ID,
						Name:   call.Name,
						Status: "running",
					},
				})
			}
			toolStarted := time.Now()
			result := dispatcher.Execute(ctx, call, tool.PermReadOnly)
			digest := contextutil.SnipToolResult(
				call.Name,
				result.CallID,
				result.Content,
				contextCfg.ToolResultSoftTokens,
				contextCfg.ToolResultHardTokens,
			)
			if realtimeStreaming && rawWriter != nil {
				status := "success"
				if result.NeedsApproval {
					status = "approval_required"
				} else if result.IsError {
					status = "error"
				}
				rawWriter(WSMessage{
					Type: "tool_call_end",
					Tool: &ToolActivity{
						ID:          result.CallID,
						Name:        call.Name,
						Status:      status,
						DurationMS:  time.Since(toolStarted).Milliseconds(),
						Preview:     digest.Preview,
						Size:        digest.Size,
						Hash:        digest.Hash,
						IsTruncated: digest.IsTruncated,
					},
				})
			}
			snippedResults[i] = tool.Result{
				CallID:  result.CallID,
				Content: json.RawMessage(contextutil.ToolResultContent(digest)),
				IsError: result.IsError,
			}
		}

		// 3. Convert results to provider-specific messages and append.
		toolMsgs := tool.ResultsToMessages(provider, snippedResults)
		messages = append(messages, toolMsgs...)
		if rawWriter != nil && approvals != nil {
			var interrupted bool
			approvalSnapshot, interrupted = emitApprovalDiff(rawWriter, approvalSnapshot, approvals.ListSessionApprovals(sessionID, nil))
			if interrupted {
				e.logger.Info("approval required; interrupting current turn", "session", sessionID, "round", round)
				return "", errApprovalPending
			}
		}

		// Rebuild request for next round.
		req.Messages = messages
	}

	e.logger.Info("llm response",
		"session", sessionID,
		"duration_ms", time.Since(start).Milliseconds(),
		"response_len", len(assistantContent),
		"response_content", assistantContent,
	)

	// Persist only the final assistant text reply to DB.
	if err := e.db.AddMessageWithMetadata(ctx, uuid.NewString(), sessionID, "assistant", assistantContent, visibleMessageMetadata("assistant", assistantContent)); err != nil {
		e.logger.Error("failed to store assistant message", "session", sessionID, "error", err)
		return "", err
	}
	if err := e.db.UpdateSessionTimestamp(ctx, sessionID); err != nil {
		e.logger.Error("failed to update session timestamp", "session", sessionID, "error", err)
		return "", err
	}

	return assistantContent, nil
}

func (e *Engine) ListSessionApprovals(ctx context.Context, sessionID string) ([]protocol.ApprovalRequest, error) {
	e.mu.RLock()
	approvals := e.approvals
	e.mu.RUnlock()
	if approvals == nil {
		return []protocol.ApprovalRequest{}, nil
	}
	return approvals.ListSessionApprovals(sessionID, nil), nil
}

func (e *Engine) ApplyApprovalAction(ctx context.Context, sessionID, requestID, action, optionID string) (*protocol.ApprovalRequest, error) {
	e.mu.RLock()
	approvals := e.approvals
	e.mu.RUnlock()
	if approvals == nil {
		return nil, errors.New("approval service is not configured")
	}
	switch action {
	case "approve":
		if strings.TrimSpace(optionID) == "" {
			return nil, errors.New("option_id is required for approve")
		}
		return approvals.ApproveRequest(sessionID, requestID, optionID, "web", "")
	case "reject":
		return approvals.RejectRequest(sessionID, requestID, "web", "")
	default:
		return nil, fmt.Errorf("unsupported approval action %q", action)
	}
}

func (e *Engine) ContinueAfterApproval(ctx context.Context, sessionID string, persona *config.Persona, approval *protocol.ApprovalRequest, cb func(delta string)) (string, error) {
	if approval == nil {
		return "", errors.New("approval is required")
	}
	if note, handled, err := e.resumeApprovalDirectly(ctx, sessionID, approval); err != nil {
		return "", err
	} else if handled {
		return e.sendTurn(ctx, sessionID, persona, cb, turnOptions{
			persistUser: false,
			extraSystem: note,
		})
	}
	note := buildApprovalContinuationNote(approval)
	return e.sendTurn(ctx, sessionID, persona, cb, turnOptions{
		persistUser: false,
		extraSystem: note,
	})
}

func (e *Engine) resumeApprovalDirectly(ctx context.Context, sessionID string, approval *protocol.ApprovalRequest) (string, bool, error) {
	if approval == nil || strings.TrimSpace(sessionID) == "" {
		return "", false, nil
	}
	if approval.ID == "" || approval.TaskID == "" {
		return "", false, nil
	}

	e.mu.RLock()
	registry := e.registry
	dispatcher := e.dispatcher
	e.mu.RUnlock()
	if registry == nil || dispatcher == nil {
		return "", false, nil
	}
	if _, ok := registry.GetSpec("resume_work"); !ok {
		return "", false, nil
	}

	input, err := json.Marshal(map[string]string{
		"task_id":             approval.TaskID,
		"approval_request_id": approval.ID,
	})
	if err != nil {
		return "", false, err
	}

	resumeCtx := work.WithSessionID(ctx, sessionID)
	result := dispatcher.Execute(resumeCtx, tool.Call{
		ID:    "internal_resume_approval",
		Name:  "resume_work",
		Input: input,
	}, tool.PermReadOnly)
	if result.NeedsApproval {
		return "", false, fmt.Errorf("resume_work unexpectedly requested approval for %s", approval.ID)
	}
	if result.IsError {
		return "", false, decodeToolError(result.Content)
	}

	return buildApprovalOutcomeNote(approval, result.Content), true, nil
}

func effectiveSummaryModel(model, summaryModel string) string {
	if summaryModel != "" {
		return summaryModel
	}
	return model
}

func cloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func visibleMessageMetadata(role, content string) map[string]any {
	return map[string]any{
		"kind":           "dialogue_" + role,
		"source":         role,
		"token_estimate": contextutil.EstimateTokens(content),
	}
}

func buildApprovalContinuationNote(approval *protocol.ApprovalRequest) string {
	if approval == nil {
		return ""
	}
	return fmt.Sprintf(
		"## Internal Approval Event\nApproval request %s for task %s is now %s. Selected option: %s. Continue the paused task immediately if appropriate. Use resume_work with task_id and approval_request_id. Do not mention internal approval IDs to the user unless necessary.",
		approval.ID,
		approval.TaskID,
		approval.Status,
		approval.SelectedOptionID,
	)
}

func buildApprovalOutcomeNote(approval *protocol.ApprovalRequest, outcome json.RawMessage) string {
	if approval == nil {
		return ""
	}
	return fmt.Sprintf(
		"## Internal Approval Event\nApproval request %s for task %s is now %s. The user's decision has already been applied and the paused Work task has already been resumed internally. Do not call resume_work again for this approval_request_id.\n\n## Internal Resume Outcome\n%s\n\nUse the internal outcome above to continue naturally. If it is already a final result, explain it to the user in your own words. If the task paused again, continue from the current pending state instead of reusing the consumed approval. Never expose raw JSON or internal IDs to the user.",
		approval.ID,
		approval.TaskID,
		approval.Status,
		string(outcome),
	)
}

func decodeToolError(content json.RawMessage) error {
	var payload map[string]string
	if err := json.Unmarshal(content, &payload); err == nil {
		if msg := strings.TrimSpace(payload["error"]); msg != "" {
			return errors.New(msg)
		}
	}
	return fmt.Errorf("tool execution failed: %s", strings.TrimSpace(string(content)))
}

func snapshotApprovalStatuses(approvals []protocol.ApprovalRequest) map[string]string {
	if len(approvals) == 0 {
		return map[string]string{}
	}
	snapshot := make(map[string]string, len(approvals))
	for _, approval := range approvals {
		snapshot[approval.ID] = approval.Status
	}
	return snapshot
}

func emitApprovalDiff(emit func(WSMessage), previous map[string]string, current []protocol.ApprovalRequest) (map[string]string, bool) {
	next := snapshotApprovalStatuses(current)
	if emit == nil {
		return next, false
	}
	interrupted := false
	for i := range current {
		approval := current[i]
		prevStatus, existed := previous[approval.ID]
		if existed && prevStatus == approval.Status {
			continue
		}
		eventType := "approval_updated"
		if approval.Status == string(protocol.ApprovalStatusPending) {
			eventType = "approval_required"
			interrupted = true
		}
		approvalCopy := approval
		emit(WSMessage{Type: eventType, Approval: &approvalCopy})
	}
	return next, interrupted
}

func logCompactReport(logger *slog.Logger, level slog.Level, report contextutil.CompactReport, retryAttempt int, errorKind llm.ErrorKind, message string) {
	if logger == nil {
		return
	}
	if message == "" {
		message = "reactive compact applied"
	}
	record := slog.NewRecord(time.Now(), level, message, 0)
	record.AddAttrs(
		slog.String("session_id", report.SessionID),
		slog.String("mode", report.Mode),
		slog.String("compact_reason", report.CompactReason),
		slog.Int("pre_estimated_tokens", report.PreEstimatedTokens),
		slog.Int("post_estimated_tokens", report.PostEstimatedTokens),
		slog.Int("kept_recent_turns", report.KeptRecentTurns),
		slog.Int("snipped_tool_results_count", report.SnippedToolResultsCount),
		slog.String("summary_covered_until_message_id", report.SummaryCoveredUntilMessageID),
		slog.String("summary_model", report.SummaryModel),
		slog.Bool("degraded", report.Degraded),
		slog.Int("retry_attempt", retryAttempt),
		slog.String("error_kind", string(errorKind)),
	)
	_ = logger.Handler().Handle(context.Background(), record)
}

func logCompactFailure(logger *slog.Logger, report contextutil.CompactReport, retryAttempt int, errorKind llm.ErrorKind, err error) {
	if logger == nil {
		return
	}
	record := slog.NewRecord(time.Now(), slog.LevelWarn, "reactive compact retry failed", 0)
	record.AddAttrs(
		slog.String("session_id", report.SessionID),
		slog.String("mode", report.Mode),
		slog.String("compact_reason", report.CompactReason),
		slog.Int("pre_estimated_tokens", report.PreEstimatedTokens),
		slog.Int("post_estimated_tokens", report.PostEstimatedTokens),
		slog.Int("kept_recent_turns", report.KeptRecentTurns),
		slog.Int("snipped_tool_results_count", report.SnippedToolResultsCount),
		slog.String("summary_covered_until_message_id", report.SummaryCoveredUntilMessageID),
		slog.String("summary_model", report.SummaryModel),
		slog.Bool("degraded", report.Degraded),
		slog.Int("retry_attempt", retryAttempt),
		slog.String("error_kind", string(errorKind)),
		slog.Any("error", err),
	)
	_ = logger.Handler().Handle(context.Background(), record)
}

func errorKindOf(err error) llm.ErrorKind {
	if err == nil {
		return ""
	}
	var llmErr *llm.Error
	if errors.As(err, &llmErr) {
		return llmErr.Kind
	}
	return ""
}
