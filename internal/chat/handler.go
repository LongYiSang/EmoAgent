package chat

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/storage"
)

// WSMessage is the JSON envelope used for WebSocket chat events.
type WSMessage struct {
	Type      string                  `json:"type"`
	Content   string                  `json:"content,omitempty"`
	SessionID string                  `json:"session_id,omitempty"`
	Persona   string                  `json:"persona,omitempty"`
	IsNew     bool                    `json:"is_new,omitempty"`
	Messages  []storage.MessageRecord `json:"messages,omitempty"`
}

// AppInterface exposes the persona methods the handler needs from App.
type AppInterface interface {
	GetPersona(name string) (*config.Persona, bool)
	GetDefaultPersonaName() string
}

type conversationEngine interface {
	StartSession(ctx context.Context, personaName string) (string, error)
	ResumeSession(ctx context.Context, sessionID string, personaName string) (string, bool, error)
	SendMessage(ctx context.Context, sessionID string, persona *config.Persona, userContent string, cb func(delta string)) (string, error)
	GetHistory(ctx context.Context, sessionID string, limit int) ([]storage.MessageRecord, error)
}

// Handler serves the WebSocket chat protocol.
type Handler struct {
	engine conversationEngine
	app    AppInterface
	logger *slog.Logger
}

// NewHandler creates a WebSocket chat handler.
func NewHandler(engine conversationEngine, app AppInterface, logger *slog.Logger) *Handler {
	return &Handler{engine: engine, app: app, logger: logger}
}

// ServeHTTP upgrades the request to WebSocket and runs the chat loop.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	personaName := h.resolvePersonaName(r)
	persona, ok := h.app.GetPersona(personaName)
	if !ok || persona == nil {
		_ = writeWSMessage(ctx, conn, WSMessage{Type: "error", Content: fmt.Sprintf("persona not found: %s", personaName)}, nil)
		return
	}

	requestedSessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	sessionID, resumed, err := h.engine.ResumeSession(ctx, requestedSessionID, personaName)
	if err != nil {
		_ = writeWSMessage(ctx, conn, WSMessage{Type: "error", Content: err.Error()}, nil)
		return
	}
	if !resumed {
		sessionID, err = h.engine.StartSession(ctx, personaName)
		if err != nil {
			_ = writeWSMessage(ctx, conn, WSMessage{Type: "error", Content: err.Error()}, nil)
			return
		}
	}

	var writeMu sync.Mutex
	if err := writeWSMessage(ctx, conn, WSMessage{
		Type:      "session_ready",
		SessionID: sessionID,
		Persona:   personaName,
		IsNew:     !resumed,
	}, &writeMu); err != nil {
		cancel()
		return
	}
	if resumed {
		history, err := h.engine.GetHistory(ctx, sessionID, 50)
		if err != nil {
			_ = writeWSMessage(ctx, conn, WSMessage{Type: "error", Content: err.Error()}, &writeMu)
			return
		}
		if len(history) > 0 {
			if err := writeWSMessage(ctx, conn, WSMessage{Type: "history", Messages: history}, &writeMu); err != nil {
				cancel()
				return
			}
		} else if persona.Greeting != "" {
			if err := writeWSMessage(ctx, conn, WSMessage{Type: "greeting", Content: persona.Greeting}, &writeMu); err != nil {
				cancel()
				return
			}
		}
	} else if persona.Greeting != "" {
		if err := writeWSMessage(ctx, conn, WSMessage{Type: "greeting", Content: persona.Greeting}, &writeMu); err != nil {
			cancel()
			return
		}
	}

	for {
		var msg WSMessage
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return
		}

		switch msg.Type {
		case "message":
			if strings.TrimSpace(msg.Content) == "" {
				continue
			}
			if err := writeWSMessage(ctx, conn, WSMessage{Type: "stream_start"}, &writeMu); err != nil {
				cancel()
				return
			}

			_, err := h.engine.SendMessage(ctx, sessionID, persona, msg.Content, func(delta string) {
				if delta == "" {
					return
				}
				if writeErr := writeWSMessage(ctx, conn, WSMessage{Type: "stream_delta", Content: delta}, &writeMu); writeErr != nil {
					cancel()
				}
			})
			if err != nil {
				if writeErr := writeWSMessage(context.Background(), conn, WSMessage{Type: "error", Content: err.Error()}, &writeMu); writeErr != nil {
					return
				}
				continue
			}
			if err := writeWSMessage(ctx, conn, WSMessage{Type: "stream_end"}, &writeMu); err != nil {
				cancel()
				return
			}

		case "ping":
			if err := writeWSMessage(ctx, conn, WSMessage{Type: "pong"}, &writeMu); err != nil {
				cancel()
				return
			}
		}
	}
}

func (h *Handler) resolvePersonaName(r *http.Request) string {
	personaName := strings.TrimSpace(r.URL.Query().Get("persona"))
	if personaName != "" {
		return personaName
	}
	return h.app.GetDefaultPersonaName()
}

func writeWSMessage(ctx context.Context, conn *websocket.Conn, msg WSMessage, mu *sync.Mutex) error {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	return wsjson.Write(ctx, conn, msg)
}
