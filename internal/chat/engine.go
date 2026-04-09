package chat

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
)

// EngineConfig defines the dependencies for Engine.
type EngineConfig struct {
	LLM          llm.Client
	DB           *storage.DB
	Logger       *slog.Logger
	Model        string
	MaxTokens    int
	Temperature  float64
	HistoryLimit int
}

// Engine assembles conversation context and forwards requests to the LLM.
type Engine struct {
	mu           sync.RWMutex
	llm          llm.Client
	db           *storage.DB
	logger       *slog.Logger
	model        string
	maxTokens    int
	temperature  float64
	historyLimit int
}

// UpdateConfig hot-swaps the active LLM client and request parameters for new sends.
func (e *Engine) UpdateConfig(client llm.Client, model string, maxTokens int, temperature float64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if client != nil {
		e.llm = client
	}
	e.model = model
	e.maxTokens = maxTokens
	e.temperature = temperature
}

// NewEngine creates a chat engine from configuration.
func NewEngine(cfg EngineConfig) *Engine {
	historyLimit := cfg.HistoryLimit
	if historyLimit <= 0 {
		historyLimit = 20
	}

	return &Engine{
		llm:          cfg.LLM,
		db:           cfg.DB,
		logger:       cfg.Logger,
		model:        cfg.Model,
		maxTokens:    cfg.MaxTokens,
		temperature:  cfg.Temperature,
		historyLimit: historyLimit,
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
	historyLimit := e.historyLimit
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

	history, err := e.db.GetRecentMessages(ctx, sessionID, historyLimit)
	if err != nil {
		e.logger.Error("failed to load message history", "session", sessionID, "error", err)
		return "", err
	}

	messages := make([]llm.Message, 0, len(history))
	for _, msg := range history {
		messages = append(messages, llm.Message{
			Role:    llm.Role(msg.Role),
			Content: msg.Content,
		})
	}

	req := llm.ChatRequest{
		Model:       model,
		Messages:    messages,
		System:      persona.SystemPrompt,
		MaxTokens:   maxTokens,
		Temperature: temperature,
		Stream:      true,
	}
	e.logger.Info("llm request",
		"session", sessionID,
		"persona", persona.Name,
		"model", model,
		"history_len", len(messages),
	)
	e.logger.Debug("llm context",
		"system", req.System,
		"messages", messages,
	)

	start := time.Now()
	resp, err := client.ChatStream(ctx, req, func(event llm.StreamEvent) {
		if cb != nil && event.Content != "" {
			cb(event.Content)
		}
	})
	if err != nil {
		e.logger.Error("llm request failed", "session", sessionID, "error", err)
		return "", err
	}
	e.logger.Info("llm response",
		"session", sessionID,
		"duration_ms", time.Since(start).Milliseconds(),
		"response_len", len(resp.Content),
		"response_content", resp.Content,
	)

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
