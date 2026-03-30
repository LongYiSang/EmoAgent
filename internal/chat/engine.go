package chat

import (
	"context"
	"errors"
	"log/slog"

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
	llm          llm.Client
	db           *storage.DB
	logger       *slog.Logger
	model        string
	maxTokens    int
	temperature  float64
	historyLimit int
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

// SendMessage stores the user message, streams the model response, and persists the reply.
func (e *Engine) SendMessage(ctx context.Context, sessionID string, persona *config.Persona, userContent string, cb func(delta string)) (string, error) {
	if e.llm == nil {
		return "", errors.New("chat engine LLM client is not configured")
	}
	if e.db == nil {
		return "", errors.New("chat engine database is not configured")
	}
	if persona == nil {
		return "", errors.New("persona is required")
	}

	if err := e.db.AddMessage(ctx, uuid.NewString(), sessionID, "user", userContent); err != nil {
		return "", err
	}

	history, err := e.db.GetRecentMessages(ctx, sessionID, e.historyLimit)
	if err != nil {
		return "", err
	}

	messages := make([]llm.Message, 0, len(history))
	for _, msg := range history {
		messages = append(messages, llm.Message{
			Role:    llm.Role(msg.Role),
			Content: msg.Content,
		})
	}

	resp, err := e.llm.ChatStream(ctx, llm.ChatRequest{
		Model:       e.model,
		Messages:    messages,
		System:      persona.SystemPrompt,
		MaxTokens:   e.maxTokens,
		Temperature: e.temperature,
		Stream:      true,
	}, func(event llm.StreamEvent) {
		if cb != nil && event.Content != "" {
			cb(event.Content)
		}
	})
	if err != nil {
		return "", err
	}

	if err := e.db.AddMessage(ctx, uuid.NewString(), sessionID, "assistant", resp.Content); err != nil {
		return "", err
	}
	if err := e.db.UpdateSessionTimestamp(ctx, sessionID); err != nil {
		return "", err
	}

	return resp.Content, nil
}
