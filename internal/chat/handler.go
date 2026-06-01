package chat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/turn"
)

// WSMessage is the JSON envelope used for WebSocket chat events.
type WSMessage struct {
	Type      string                    `json:"type"`
	Content   string                    `json:"content,omitempty"`
	SessionID string                    `json:"session_id,omitempty"`
	Persona   string                    `json:"persona,omitempty"`
	IsNew     bool                      `json:"is_new,omitempty"`
	Messages  []storage.MessageRecord   `json:"messages,omitempty"`
	RequestID string                    `json:"request_id,omitempty"`
	Action    string                    `json:"action,omitempty"`
	OptionID  string                    `json:"option_id,omitempty"`
	Approval  *protocol.ApprovalRequest `json:"approval,omitempty"`
	Tool      *ToolActivity             `json:"tool,omitempty"`
	Reasoning *ReasoningActivity        `json:"reasoning,omitempty"`
}

// ToolActivity is the compact, UI-safe description of a live tool call.
type ToolActivity struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	DurationMS  int64  `json:"duration_ms,omitempty"`
	Preview     string `json:"preview,omitempty"`
	Size        int    `json:"size,omitempty"`
	Hash        string `json:"hash,omitempty"`
	IsTruncated bool   `json:"is_truncated,omitempty"`
}

// ReasoningActivity is the UI-safe description of a model thinking block.
type ReasoningActivity struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Content    string `json:"content,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Model      string `json:"model,omitempty"`
	Kind       string `json:"kind,omitempty"`
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
	ListSessionApprovals(ctx context.Context, sessionID string) ([]protocol.ApprovalRequest, error)
	ApplyApprovalAction(ctx context.Context, sessionID, requestID, action, optionID string) (*protocol.ApprovalRequest, error)
	ContinueAfterApproval(ctx context.Context, sessionID string, persona *config.Persona, approval *protocol.ApprovalRequest, cb func(delta string)) (string, error)
}

// Handler serves the WebSocket chat protocol.
type Handler struct {
	engine      conversationEngine
	app         AppInterface
	logger      *slog.Logger
	turnConfig  config.TurnPipelineConfig
	turnJournal turn.TurnJournal
	turnRuntime *chatTurnRuntime
}

type HandlerOption func(*Handler)

func WithTurnPipelineConfig(cfg config.TurnPipelineConfig) HandlerOption {
	return func(h *Handler) {
		h.turnConfig = cfg
	}
}

func WithTurnJournal(journal turn.TurnJournal) HandlerOption {
	return func(h *Handler) {
		h.turnJournal = journal
	}
}

// NewHandler creates a WebSocket chat handler.
func NewHandler(engine conversationEngine, app AppInterface, logger *slog.Logger, options ...HandlerOption) *Handler {
	h := &Handler{engine: engine, app: app, logger: logger}
	for _, option := range options {
		if option != nil {
			option(h)
		}
	}
	if h.turnJournal == nil {
		h.turnJournal = turn.NewMemoryJournal()
	}
	h.turnRuntime = newChatTurnRuntime(engine, h.turnConfig, h.turnJournal, logger)
	return h
}

// ServeHTTP upgrades the request to WebSocket and runs the chat loop.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		h.logger.Error("ws accept failed", "remote", r.RemoteAddr, "error", err)
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

	h.logger.Info("ws connected", "remote", r.RemoteAddr, "persona", personaName)

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
	h.logger.Info("ws session ready", "session", sessionID, "persona", personaName, "resumed", resumed)
	defer h.logger.Info("ws disconnected", "remote", r.RemoteAddr, "session", sessionID)

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
	// History is now loaded via REST on the frontend side.
	// Only send greeting for new sessions when not skipped (i.e. user hasn't typed a message yet).
	skipGreeting := strings.TrimSpace(r.URL.Query().Get("skip_greeting")) == "1"
	if !resumed && !skipGreeting && persona.Greeting != "" {
		if err := writeWSMessage(ctx, conn, WSMessage{Type: "greeting", Content: persona.Greeting}, &writeMu); err != nil {
			cancel()
			return
		}
	}

	for {
		var msg WSMessage
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			if errors.Is(err, context.Canceled) || websocket.CloseStatus(err) != -1 {
				h.logger.Debug("ws read closed", "remote", r.RemoteAddr)
			} else {
				h.logger.Warn("ws read error", "remote", r.RemoteAddr, "error", err)
			}
			return
		}

		switch msg.Type {
		case "message":
			if strings.TrimSpace(msg.Content) == "" {
				continue
			}
			if h.turnConfig.Shadow && !h.turnConfig.Enabled {
				_, _ = h.turnRuntime.Shadow(ctx, wsMessageToInbound(msg, sessionID, personaName))
			}
			if h.turnConfig.Enabled {
				sink := h.newWSOutboundSink(ctx, conn, &writeMu, cancel)
				env := wsMessageToInbound(msg, sessionID, personaName)
				if _, err := h.turnRuntime.Execute(ctx, env, persona, sink); err != nil {
					if writeErr := writeWSMessage(context.Background(), conn, WSMessage{Type: "error", Content: err.Error()}, &writeMu); writeErr != nil {
						return
					}
				}
				continue
			}
			if err := writeWSMessage(ctx, conn, WSMessage{Type: "stream_start"}, &writeMu); err != nil {
				cancel()
				return
			}

			msgCtx := withWSWriter(ctx, func(progressMsg WSMessage) {
				if writeErr := writeWSMessage(ctx, conn, progressMsg, &writeMu); writeErr != nil {
					if !errors.Is(ctx.Err(), context.Canceled) {
						h.logger.Warn("ws progress write failed", "session", sessionID, "error", writeErr)
					}
					cancel()
				}
			})

			streamedDelta := false
			reply, err := h.engine.SendMessage(msgCtx, sessionID, persona, msg.Content, func(delta string) {
				if delta == "" {
					return
				}
				streamedDelta = true
				if writeErr := writeWSMessage(ctx, conn, WSMessage{Type: "stream_delta", Content: delta}, &writeMu); writeErr != nil {
					if !errors.Is(ctx.Err(), context.Canceled) {
						h.logger.Warn("ws stream write failed", "session", sessionID, "error", writeErr)
					}
					cancel()
				}
			})
			if err != nil && !errors.Is(err, errApprovalPending) {
				if writeErr := writeWSMessage(context.Background(), conn, WSMessage{Type: "error", Content: err.Error()}, &writeMu); writeErr != nil {
					return
				}
				continue
			}
			if err == nil && !streamedDelta && reply != "" {
				if writeErr := writeWSMessage(ctx, conn, WSMessage{Type: "stream_delta", Content: reply}, &writeMu); writeErr != nil {
					if !errors.Is(ctx.Err(), context.Canceled) {
						h.logger.Warn("ws stream write failed", "session", sessionID, "error", writeErr)
					}
					cancel()
					return
				}
			}
			if err := writeWSMessage(ctx, conn, WSMessage{Type: "stream_end"}, &writeMu); err != nil {
				cancel()
				return
			}
			if err := h.emitApprovalEvents(ctx, conn, &writeMu, sessionID); err != nil {
				cancel()
				return
			}

		case "approval_action":
			if h.turnConfig.Shadow && !h.turnConfig.Enabled {
				_, _ = h.turnRuntime.Shadow(ctx, wsMessageToInbound(msg, sessionID, personaName))
			}
			if h.turnConfig.Enabled {
				sink := h.newWSOutboundSink(ctx, conn, &writeMu, cancel)
				env := wsMessageToInbound(msg, sessionID, personaName)
				if _, err := h.turnRuntime.Execute(ctx, env, persona, sink); err != nil {
					if writeErr := writeWSMessage(context.Background(), conn, WSMessage{Type: "error", Content: err.Error()}, &writeMu); writeErr != nil {
						return
					}
				}
				continue
			}
			if strings.TrimSpace(msg.RequestID) == "" {
				if err := writeWSMessage(context.Background(), conn, WSMessage{Type: "error", Content: "request_id is required"}, &writeMu); err != nil {
					return
				}
				continue
			}
			action := strings.TrimSpace(msg.Action)
			if action == "" {
				if err := writeWSMessage(context.Background(), conn, WSMessage{Type: "error", Content: "action is required"}, &writeMu); err != nil {
					return
				}
				continue
			}
			approval, err := h.engine.ApplyApprovalAction(ctx, sessionID, msg.RequestID, action, msg.OptionID)
			if err != nil {
				if writeErr := writeWSMessage(context.Background(), conn, WSMessage{Type: "error", Content: err.Error()}, &writeMu); writeErr != nil {
					return
				}
				continue
			}
			if err := writeWSMessage(ctx, conn, WSMessage{Type: "approval_updated", Approval: approval}, &writeMu); err != nil {
				cancel()
				return
			}
			if err := writeWSMessage(ctx, conn, WSMessage{Type: "stream_start"}, &writeMu); err != nil {
				cancel()
				return
			}
			msgCtx := withWSWriter(ctx, func(progressMsg WSMessage) {
				if writeErr := writeWSMessage(ctx, conn, progressMsg, &writeMu); writeErr != nil {
					if !errors.Is(ctx.Err(), context.Canceled) {
						h.logger.Warn("ws progress write failed", "session", sessionID, "error", writeErr)
					}
					cancel()
				}
			})
			if _, err := h.engine.ContinueAfterApproval(msgCtx, sessionID, persona, approval, func(delta string) {
				if delta == "" {
					return
				}
				if writeErr := writeWSMessage(ctx, conn, WSMessage{Type: "stream_delta", Content: delta}, &writeMu); writeErr != nil {
					if !errors.Is(ctx.Err(), context.Canceled) {
						h.logger.Warn("ws stream write failed", "session", sessionID, "error", writeErr)
					}
					cancel()
				}
			}); err != nil && !errors.Is(err, errApprovalPending) {
				if writeErr := writeWSMessage(context.Background(), conn, WSMessage{Type: "error", Content: err.Error()}, &writeMu); writeErr != nil {
					return
				}
				continue
			}
			if err := writeWSMessage(ctx, conn, WSMessage{Type: "stream_end"}, &writeMu); err != nil {
				cancel()
				return
			}
			if err := h.emitApprovalEvents(ctx, conn, &writeMu, sessionID); err != nil {
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

func (h *Handler) newWSOutboundSink(ctx context.Context, conn *websocket.Conn, mu *sync.Mutex, cancel context.CancelFunc) turn.OutboundSink {
	return turn.SinkFunc(func(_ context.Context, event turn.OutboundEvent) error {
		if err := writeWSMessage(ctx, conn, outboundEventToWSMessage(event), mu); err != nil {
			if !errors.Is(ctx.Err(), context.Canceled) && h.logger != nil {
				h.logger.Warn("ws outbound write failed", "error", err)
			}
			cancel()
			return err
		}
		return nil
	})
}

func (h *Handler) emitApprovalEvents(ctx context.Context, conn *websocket.Conn, mu *sync.Mutex, sessionID string) error {
	if h.engine == nil {
		return nil
	}
	approvals, err := h.engine.ListSessionApprovals(ctx, sessionID)
	if err != nil {
		return err
	}
	for i := range approvals {
		eventType := "approval_updated"
		if approvals[i].Status == string(protocol.ApprovalStatusPending) {
			eventType = "approval_required"
		}
		approval := approvals[i]
		if err := writeWSMessage(ctx, conn, WSMessage{Type: eventType, Approval: &approval}, mu); err != nil {
			return err
		}
	}
	return nil
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

func withWSWriter(ctx context.Context, fn func(WSMessage)) context.Context {
	if ctx == nil || fn == nil {
		return ctx
	}
	return turn.WithOutboundSink(ctx, turn.SinkFunc(func(_ context.Context, event turn.OutboundEvent) error {
		fn(outboundEventToWSMessage(event))
		return nil
	}))
}

func wsWriterFromContext(ctx context.Context) func(WSMessage) {
	if ctx == nil {
		return nil
	}
	sink := turn.OutboundSinkFromContext(ctx)
	if sink == nil {
		return nil
	}
	return func(msg WSMessage) {
		_ = sink.Emit(ctx, wsMessageToOutboundEvent(msg))
	}
}
