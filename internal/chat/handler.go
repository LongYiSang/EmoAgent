package chat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
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
	TurnID    string                    `json:"turn_id,omitempty"`
	Status    string                    `json:"status,omitempty"`
	ErrorKind string                    `json:"error_kind,omitempty"`
	Persona   string                    `json:"persona,omitempty"`
	IsNew     bool                      `json:"is_new,omitempty"`
	Messages  []storage.MessageRecord   `json:"messages,omitempty"`
	RequestID string                    `json:"request_id,omitempty"`
	Action    string                    `json:"action,omitempty"`
	OptionID  string                    `json:"option_id,omitempty"`
	Approval  *protocol.ApprovalRequest `json:"approval,omitempty"`
	Tool      *ToolActivity             `json:"tool,omitempty"`
	Reasoning *ReasoningActivity        `json:"reasoning,omitempty"`
	Payload   map[string]any            `json:"payload,omitempty"`
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
	turnDB      *sql.DB
	turnJournal turn.TurnJournal
	turnIDs     turn.IdempotencyStore
	turnRuntime *chatTurnRuntime
	pluginHost  turnPluginHost
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

func WithTurnDB(db *sql.DB) HandlerOption {
	return func(h *Handler) {
		h.turnDB = db
	}
}

func WithPluginHost(host turnPluginHost) HandlerOption {
	return func(h *Handler) {
		h.pluginHost = host
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
	if h.turnJournal == nil || h.turnIDs == nil {
		journal, ids := buildTurnRuntimeStores(h.turnConfig, h.turnDB, logger)
		if h.turnJournal == nil {
			h.turnJournal = journal
		}
		if h.turnIDs == nil {
			h.turnIDs = ids
		}
	}
	if setter, ok := h.pluginHost.(interface {
		SetTurnJournal(turn.TurnJournal)
	}); ok {
		setter.SetTurnJournal(h.turnJournal)
	}
	h.turnRuntime = newChatTurnRuntimeWithStore(engine, h.turnConfig, h.turnJournal, h.turnIDs, logger, h.pluginHost)
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
			usePipeline := shouldUseTurnPipeline(h.turnConfig, personaName, sessionID)
			if pluginHostEnabled(h.pluginHost) && !usePipeline {
				_ = writeWSMessage(ctx, conn, WSMessage{Type: "error", Content: "plugins.enabled requires Turn Pipeline for this session/persona"}, &writeMu)
				continue
			}
			if h.turnConfig.Shadow && !usePipeline {
				_, _ = h.turnRuntime.Shadow(ctx, wsMessageToInbound(msg, sessionID, personaName))
			}
			if usePipeline {
				sink := h.newWSOutboundSink(ctx, conn, &writeMu, cancel)
				env := wsMessageToInbound(msg, sessionID, personaName)
				if _, err := h.turnRuntime.Execute(ctx, env, persona, sink); err != nil {
					if writeErr := writeWSMessage(context.Background(), conn, WSMessage{Type: "error", Content: err.Error()}, &writeMu); writeErr != nil {
						return
					}
				}
				closeOutboundSink(ctx, sink)
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
			useApprovalPipeline := shouldUseTurnPipeline(h.turnConfig, personaName, sessionID) && h.turnConfig.ApprovalStages
			if pluginHostEnabled(h.pluginHost) && !useApprovalPipeline {
				_ = writeWSMessage(ctx, conn, WSMessage{Type: "error", Content: "plugins.enabled requires Turn Pipeline approval stages for this session/persona"}, &writeMu)
				continue
			}
			if h.turnConfig.Shadow && !useApprovalPipeline {
				_, _ = h.turnRuntime.Shadow(ctx, wsMessageToInbound(msg, sessionID, personaName))
			}
			if useApprovalPipeline {
				sink := h.newWSOutboundSink(ctx, conn, &writeMu, cancel)
				env := wsMessageToInbound(msg, sessionID, personaName)
				if _, err := h.turnRuntime.Execute(ctx, env, persona, sink); err != nil {
					if writeErr := writeWSMessage(context.Background(), conn, WSMessage{Type: "error", Content: err.Error()}, &writeMu); writeErr != nil {
						return
					}
				}
				closeOutboundSink(ctx, sink)
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
	raw := turn.SinkFunc(func(_ context.Context, event turn.OutboundEvent) error {
		if err := writeWSMessage(ctx, conn, outboundEventToWSMessage(event), mu); err != nil {
			if !errors.Is(ctx.Err(), context.Canceled) && h.logger != nil {
				h.logger.Warn("ws outbound write failed", "error", err)
			}
			cancel()
			return err
		}
		return nil
	})
	return turn.NewBoundedOutboundSink(raw, turn.BoundedOutboundOptions{})
}

func closeOutboundSink(ctx context.Context, sink turn.OutboundSink) {
	closer, ok := sink.(interface{ Close(context.Context) error })
	if ok {
		_ = closer.Close(ctx)
	}
}

func shouldUseTurnPipeline(cfg config.TurnPipelineConfig, personaName, sessionID string) bool {
	if stringInList(sessionID, cfg.DenySessions) {
		return false
	}
	if stringInList(personaName, cfg.AllowPersonas) || stringInList(sessionID, cfg.AllowSessions) {
		return true
	}
	if !cfg.Enabled {
		return false
	}
	if cfg.RolloutPercent <= 0 {
		return false
	}
	if cfg.RolloutPercent >= 100 {
		return true
	}
	key := personaName + ":" + sessionID
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32()%100) < cfg.RolloutPercent
}

func pluginHostEnabled(host turnPluginHost) bool {
	return host != nil && host.Enabled()
}

func stringInList(value string, list []string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, item := range list {
		if strings.TrimSpace(item) == value {
			return true
		}
	}
	return false
}

func buildTurnRuntimeStores(cfg config.TurnPipelineConfig, db *sql.DB, logger *slog.Logger) (turn.TurnJournal, turn.IdempotencyStore) {
	journal, journalErr := buildTurnJournal(cfg, db)
	ids, idsErr := buildIdempotencyStore(cfg, db)
	if journalErr == nil && idsErr == nil {
		return journal, ids
	}
	if cfg.Journal.FailClosed {
		err := firstErr(journalErr, idsErr)
		return failingJournal{err: err}, failingIdempotencyStore{err: err}
	}
	if logger != nil {
		logger.Warn("turn runtime persistence degraded", "journal_error", journalErr, "idempotency_error", idsErr)
	}
	memory := turn.NewMemoryJournal()
	_ = memory.StartTurn(context.Background(), turn.TurnRecord{TurnID: "journal_degraded", Kind: turn.InboundSystemResume, State: turn.StateCreated, Status: "degraded"})
	_ = memory.RecordEvent(context.Background(), "journal_degraded", turn.JournalEvent{
		Stage: turn.StageIngress,
		Type:  "journal_degraded",
		Payload: map[string]any{
			"journal_error":     errorString(journalErr),
			"idempotency_error": errorString(idsErr),
		},
	})
	_ = memory.CompleteTurn(context.Background(), "journal_degraded", "degraded", "")
	return memory, turn.NewMemoryIdempotencyStore()
}

func buildTurnJournal(cfg config.TurnPipelineConfig, db *sql.DB) (turn.TurnJournal, error) {
	switch cfg.Journal.Mode {
	case "memory":
		return turn.NewMemoryJournal(), nil
	case "jsonl":
		return turn.NewJSONLJournal(cfg.Journal.JSONLDir), nil
	case "sqlite_jsonl":
		if db == nil {
			return nil, errors.New("sqlite database is not configured")
		}
		return turn.NewMultiJournal(turn.NewSQLiteJournal(db), turn.NewJSONLJournal(cfg.Journal.JSONLDir)), nil
	case "", "sqlite":
		if db == nil {
			return nil, errors.New("sqlite database is not configured")
		}
		return turn.NewSQLiteJournal(db), nil
	default:
		return nil, fmt.Errorf("unsupported turn journal mode %q", cfg.Journal.Mode)
	}
}

func buildIdempotencyStore(cfg config.TurnPipelineConfig, db *sql.DB) (turn.IdempotencyStore, error) {
	switch cfg.Idempotency.Mode {
	case "memory":
		return turn.NewMemoryIdempotencyStore(), nil
	case "", "sqlite":
		if db == nil {
			return nil, errors.New("sqlite database is not configured")
		}
		return turn.NewSQLiteIdempotencyStore(db), nil
	default:
		return nil, fmt.Errorf("unsupported turn idempotency mode %q", cfg.Idempotency.Mode)
	}
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type failingJournal struct {
	err error
}

func (j failingJournal) StartTurn(context.Context, turn.TurnRecord) error {
	return j.err
}
func (j failingJournal) RecordTransition(context.Context, string, turn.TurnState, turn.TurnState, turn.StageMetrics) error {
	return j.err
}
func (j failingJournal) RecordEvent(context.Context, string, turn.JournalEvent) error {
	return j.err
}
func (j failingJournal) CompleteTurn(context.Context, string, string, string) error {
	return j.err
}

type failingIdempotencyStore struct {
	err error
}

func (s failingIdempotencyStore) Begin(string, string) (turn.IdempotencyResult, error) {
	return turn.IdempotencyResult{}, s.err
}
func (s failingIdempotencyStore) Complete(string, string) error {
	return s.err
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
