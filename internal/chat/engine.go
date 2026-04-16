package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent/internal/config"
	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tool"
)

// EngineConfig defines the dependencies for Engine.
type EngineConfig struct {
	LLM           llm.Client
	DB            *storage.DB
	Logger        *slog.Logger
	Model         string
	MaxTokens     int
	Temperature   float64
	ContextConfig config.ContextConfig
	Provider      string           // "openai" or "anthropic", needed by ResultsToMessages
	Registry      *tool.Registry   // nil disables tool support
	Dispatcher    *tool.Dispatcher // nil disables tool support
}

// Engine assembles conversation context and forwards requests to the LLM.
type Engine struct {
	mu          sync.RWMutex
	llm         llm.Client
	db          *storage.DB
	logger      *slog.Logger
	model       string
	maxTokens   int
	temperature float64
	contextCfg  config.ContextConfig
	provider    string
	registry    *tool.Registry
	dispatcher  *tool.Dispatcher
}

// UpdateConfig hot-swaps the active LLM client and request parameters for new sends.
func (e *Engine) UpdateConfig(client llm.Client, provider, model string, maxTokens int, temperature float64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if client != nil {
		e.llm = client
	}
	e.provider = provider
	e.model = model
	e.maxTokens = maxTokens
	e.temperature = temperature
}

// NewEngine creates a chat engine from configuration.
func NewEngine(cfg EngineConfig) *Engine {
	contextCfg := cfg.ContextConfig
	if err := contextCfg.Validate(); err != nil {
		contextCfg = config.DefaultConfig().Context
	}

	return &Engine{
		llm:         cfg.LLM,
		db:          cfg.DB,
		logger:      cfg.Logger,
		model:       cfg.Model,
		maxTokens:   cfg.MaxTokens,
		temperature: cfg.Temperature,
		contextCfg:  contextCfg,
		provider:    cfg.Provider,
		registry:    cfg.Registry,
		dispatcher:  cfg.Dispatcher,
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
	e.mu.RLock()
	client := e.llm
	model := e.model
	maxTokens := e.maxTokens
	temperature := e.temperature
	contextCfg := e.contextCfg
	provider := e.provider
	registry := e.registry
	dispatcher := e.dispatcher
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

	if err := e.db.AddMessage(ctx, uuid.NewString(), sessionID, "user", userContent); err != nil {
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
		title := userContent
		if runeCount := len([]rune(title)); runeCount > 30 {
			title = string([]rune(title)[:30]) + "…"
		}
		if err := e.db.UpdateSessionTitle(ctx, sessionID, title); err != nil {
			e.logger.Warn("failed to set session title", "session", sessionID, "error", err)
		}
	}

	history, err := e.db.GetAllMessages(ctx, sessionID)
	if err != nil {
		e.logger.Error("failed to load message history", "session", sessionID, "error", err)
		return "", err
	}

	assembled, err := contextutil.BuildEmotionContext(persona, history, contextCfg)
	if err != nil {
		e.logger.Error("failed to assemble llm context", "session", sessionID, "error", err)
		return "", err
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

	for round := 0; ; round++ {
		var roundDeltas []string
		resp, err = client.ChatStream(ctx, req, func(event llm.StreamEvent) {
			if event.Content != "" {
				roundDeltas = append(roundDeltas, event.Content)
			}
		})
		if err != nil {
			e.logger.Error("llm request failed", "session", sessionID, "round", round, "error", err)
			return "", err
		}

		if resp.StopReason != "tool_use" {
			if cb != nil {
				for _, delta := range roundDeltas {
					cb(delta)
				}
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
		assistantMsg := llm.Message{
			Role:             llm.RoleAssistant,
			Content:          resp.Content,
			ContentBlocks:    resp.ContentBlocks,
			ReasoningContent: resp.ReasoningContent,
		}
		messages = append(messages, assistantMsg)

		// 2. Extract and execute tool calls.
		calls := tool.ExtractToolCalls(resp)
		if len(calls) == 0 {
			e.logger.Warn("tool_use stop reason but no tool calls extracted", "session", sessionID)
			break
		}
		for _, c := range calls {
			e.logger.Info("tool call", "session", sessionID, "tool", c.Name, "call_id", c.ID)
		}

		results := dispatcher.ExecuteAll(ctx, calls, tool.PermReadOnly)
		callNames := make(map[string]string, len(calls))
		for _, call := range calls {
			callNames[call.ID] = call.Name
		}

		snippedResults := make([]tool.Result, len(results))
		for i, result := range results {
			digest := contextutil.SnipToolResult(
				callNames[result.CallID],
				result.CallID,
				result.Content,
				contextCfg.ToolResultSoftTokens,
				contextCfg.ToolResultHardTokens,
			)
			snippedResults[i] = tool.Result{
				CallID:  result.CallID,
				Content: json.RawMessage(contextutil.ToolResultContent(digest)),
				IsError: result.IsError,
			}
		}

		// 3. Convert results to provider-specific messages and append.
		toolMsgs := tool.ResultsToMessages(provider, snippedResults)
		messages = append(messages, toolMsgs...)

		// Rebuild request for next round.
		req.Messages = messages
	}

	e.logger.Info("llm response",
		"session", sessionID,
		"duration_ms", time.Since(start).Milliseconds(),
		"response_len", len(resp.Content),
		"response_content", resp.Content,
	)

	// Persist only the final assistant text reply to DB.
	if err := e.db.AddMessage(ctx, uuid.NewString(), sessionID, "assistant", resp.Content); err != nil {
		e.logger.Error("failed to store assistant message", "session", sessionID, "error", err)
		return "", err
	}
	if err := e.db.UpdateSessionTimestamp(ctx, sessionID); err != nil {
		e.logger.Error("failed to update session timestamp", "session", sessionID, "error", err)
		return "", err
	}

	return resp.Content, nil
}
