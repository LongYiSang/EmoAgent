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
	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/conversation"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/replydelivery"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/turn"
)

// WSMessage is the JSON envelope used for WebSocket chat events.
type WSMessage struct {
	Type          string                    `json:"type"`
	Content       string                    `json:"content,omitempty"`
	Parts         []llm.ContentBlock        `json:"parts,omitempty"`
	SessionID     string                    `json:"session_id,omitempty"`
	TurnID        string                    `json:"turn_id,omitempty"`
	GroupID       string                    `json:"group_id,omitempty"`
	SegmentID     string                    `json:"segment_id,omitempty"`
	SegmentIndex  int                       `json:"segment_index,omitempty"`
	SegmentTotal  int                       `json:"segment_total,omitempty"`
	Status        string                    `json:"status,omitempty"`
	ErrorKind     string                    `json:"error_kind,omitempty"`
	Persona       string                    `json:"persona,omitempty"`
	OriginKey     string                    `json:"origin_key,omitempty"`
	IsNew         bool                      `json:"is_new,omitempty"`
	CommandID     string                    `json:"command_id,omitempty"`
	CommandName   string                    `json:"command_name,omitempty"`
	ReloadHistory bool                      `json:"reload_history,omitempty"`
	ReloadMemory  bool                      `json:"reload_memory,omitempty"`
	Messages      []storage.MessageRecord   `json:"messages,omitempty"`
	RequestID     string                    `json:"request_id,omitempty"`
	Action        string                    `json:"action,omitempty"`
	OptionID      string                    `json:"option_id,omitempty"`
	Approval      *protocol.ApprovalRequest `json:"approval,omitempty"`
	Tool          *ToolActivity             `json:"tool,omitempty"`
	Reasoning     *ReasoningActivity        `json:"reasoning,omitempty"`
	Payload       map[string]any            `json:"payload,omitempty"`
}

type CommandRequest struct {
	Content    string
	Origin     conversation.Origin
	SessionID  string
	AgentID    string
	PersonaKey string
	ActorID    string
	ActorName  string
	ActorRole  string
}

type CommandResponse struct {
	Messages   []WSMessage
	SessionID  string
	PersonaKey string
}

type CommandHandler interface {
	TryHandle(ctx context.Context, req CommandRequest) (CommandResponse, bool, error)
}

type ConversationBindings interface {
	EnsureCurrent(ctx context.Context, origin conversation.Origin, personaKey string) (conversation.Binding, error)
	BindSession(ctx context.Context, origin conversation.Origin, personaKey string, sessionID string, isNew bool) (conversation.Binding, error)
}

// ToolActivity is the compact, UI-safe description of a live tool call.
type ToolActivity struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Status               string   `json:"status"`
	DurationMS           int64    `json:"duration_ms,omitempty"`
	Preview              string   `json:"preview,omitempty"`
	Size                 int      `json:"size,omitempty"`
	Hash                 string   `json:"hash,omitempty"`
	IsTruncated          bool     `json:"is_truncated,omitempty"`
	Origin               string   `json:"origin,omitempty"`
	RuntimeKind          string   `json:"runtime_kind,omitempty"`
	ProducerID           string   `json:"producer_id,omitempty"`
	Executor             string   `json:"executor,omitempty"`
	Integrity            string   `json:"integrity,omitempty"`
	InstructionAuthority string   `json:"instruction_authority,omitempty"`
	Sensitivity          string   `json:"sensitivity,omitempty"`
	Redacted             bool     `json:"redacted,omitempty"`
	GrantIDs             []string `json:"grant_ids,omitempty"`
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
	SendMessageParts(ctx context.Context, sessionID string, persona *config.Persona, parts []llm.ContentBlock, cb func(delta string)) (string, error)
}

// Handler serves the WebSocket chat protocol.
type Handler struct {
	engine            conversationEngine
	app               AppInterface
	logger            *slog.Logger
	turnConfig        config.TurnPipelineConfig
	turnTimezone      string
	turnDB            *sql.DB
	turnJournal       turn.TurnJournal
	turnIDs           turn.IdempotencyStore
	turnRuntime       *chatTurnRuntime
	pluginHost        turnPluginHost
	bindings          ConversationBindings
	commandHandler    CommandHandler
	runs              *conversation.RunRegistry
	realtimeStreaming bool
	replyDelivery     config.ReplyDeliveryConfig
}

type HandlerOption func(*Handler)

func WithTurnPipelineConfig(cfg config.TurnPipelineConfig) HandlerOption {
	return func(h *Handler) {
		h.turnConfig = cfg
	}
}

func WithRealtimeStreaming(enabled bool) HandlerOption {
	return func(h *Handler) {
		h.realtimeStreaming = enabled
	}
}

func WithReplyDeliveryConfig(cfg config.ReplyDeliveryConfig) HandlerOption {
	return func(h *Handler) {
		h.replyDelivery = config.NormalizeReplyDeliveryConfig(cfg)
	}
}

func WithTurnTimezone(timezone string) HandlerOption {
	return func(h *Handler) {
		h.turnTimezone = timezone
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

func WithTurnRunner(runner *TurnRunner) HandlerOption {
	return func(h *Handler) {
		if runner != nil {
			h.turnRuntime = runner.runtime
		}
	}
}

func WithPluginHost(host turnPluginHost) HandlerOption {
	return func(h *Handler) {
		h.pluginHost = host
	}
}

func WithCommandHandler(handler CommandHandler) HandlerOption {
	return func(h *Handler) {
		h.commandHandler = handler
	}
}

func WithConversationBindings(bindings ConversationBindings) HandlerOption {
	return func(h *Handler) {
		h.bindings = bindings
	}
}

func WithRunRegistry(runs *conversation.RunRegistry) HandlerOption {
	return func(h *Handler) {
		h.runs = runs
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
	if h.turnRuntime == nil {
		if h.turnJournal == nil || h.turnIDs == nil {
			journal, ids := buildTurnRuntimeStores(h.turnConfig, h.turnDB, logger, h.turnTimezone)
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
		h.turnRuntime.realtimeStreaming = h.realtimeStreaming
		h.turnRuntime.replyDelivery = config.NormalizeReplyDeliveryConfig(h.replyDelivery)
	}
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

	origin, err := resolveWSOrigin(r)
	if err != nil {
		_ = writeWSMessage(ctx, conn, WSMessage{Type: "error", Content: err.Error()}, nil)
		return
	}
	personaName := h.resolvePersonaName(r)
	persona, ok := h.app.GetPersona(personaName)
	if !ok || persona == nil {
		_ = writeWSMessage(ctx, conn, WSMessage{Type: "error", Content: fmt.Sprintf("persona not found: %s", personaName)}, nil)
		return
	}

	h.logger.Info("ws connected", "remote", r.RemoteAddr, "persona", personaName)

	requestedSessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	sessionID, resumed, err := h.bootstrapSession(ctx, origin, personaName, requestedSessionID)
	if err != nil {
		_ = writeWSMessage(ctx, conn, WSMessage{Type: "error", Content: err.Error()}, nil)
		return
	}
	h.logger.Info("ws session ready", "session", sessionID, "persona", personaName, "resumed", resumed)
	defer h.logger.Info("ws disconnected", "remote", r.RemoteAddr, "session", sessionID)

	var writeMu sync.Mutex
	if err := writeWSMessage(ctx, conn, WSMessage{
		Type:      "session_ready",
		SessionID: sessionID,
		Persona:   personaName,
		OriginKey: origin.OriginKey,
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

	activeRun := &wsActiveRun{}
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
			if !hasMessageInput(msg) {
				continue
			}
			if currentSessionID, err := h.currentSession(ctx, origin, personaName, sessionID); err != nil {
				_ = writeWSMessage(context.Background(), conn, WSMessage{Type: "error", Content: err.Error()}, &writeMu)
				continue
			} else {
				sessionID = currentSessionID
			}
			if handled, commandSessionID := h.tryHandleCommand(ctx, conn, &writeMu, msg, origin, sessionID, personaName); handled {
				if commandSessionID != "" {
					sessionID = commandSessionID
				}
				continue
			}
			if !activeRun.TryStart() {
				_ = writeWSMessage(ctx, conn, WSMessage{Type: "error", Content: "已有回复正在运行，请先 /stop。"}, &writeMu)
				continue
			}
			runMsg := msg
			runSessionID := sessionID
			go func() {
				defer activeRun.Done()
				h.runWSMessage(ctx, cancel, conn, &writeMu, origin, runSessionID, personaName, persona, runMsg)
			}()

		case "approval_action":
			if currentSessionID, err := h.currentSession(ctx, origin, personaName, sessionID); err != nil {
				_ = writeWSMessage(context.Background(), conn, WSMessage{Type: "error", Content: err.Error()}, &writeMu)
				continue
			} else {
				sessionID = currentSessionID
			}
			useApprovalPipeline := shouldUseTurnPipeline(h.turnConfig, personaName, sessionID) && h.turnConfig.ApprovalStages
			if pluginHostEnabled(h.pluginHost) && !useApprovalPipeline {
				_ = writeWSMessage(ctx, conn, WSMessage{Type: "error", Content: "plugins.enabled requires Turn Pipeline approval stages for this session/persona"}, &writeMu)
				continue
			}
			if h.turnConfig.Shadow && !useApprovalPipeline {
				env, err := wsMessageToInbound(msg, sessionID, personaName)
				if err == nil {
					_, _ = h.turnRuntime.Shadow(ctx, env)
				}
			}
			if useApprovalPipeline {
				turnCtx := context.WithoutCancel(ctx)
				turnCtx, done := h.registerRun(turnCtx, origin, sessionID, "approval_turn")
				sink := h.newWSOutboundSink(ctx, conn, &writeMu)
				env, err := wsMessageToInbound(msg, sessionID, personaName)
				if err != nil {
					if writeErr := writeWSMessage(context.Background(), conn, WSMessage{Type: "error", Content: err.Error()}, &writeMu); writeErr != nil {
						return
					}
					closeOutboundSink(turnCtx, sink)
					done()
					continue
				}
				if _, err := h.turnRuntime.Execute(turnCtx, env, persona, sink); err != nil {
					if writeErr := writeWSMessage(context.Background(), conn, WSMessage{Type: "error", Content: err.Error()}, &writeMu); writeErr != nil {
						return
					}
				}
				closeOutboundSink(turnCtx, sink)
				done()
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

type wsActiveRun struct {
	mu     sync.Mutex
	active bool
}

func (r *wsActiveRun) TryStart() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active {
		return false
	}
	r.active = true
	return true
}

func (r *wsActiveRun) Done() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active = false
}

func (h *Handler) runWSMessage(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, writeMu *sync.Mutex, origin conversation.Origin, sessionID string, personaName string, persona *config.Persona, msg WSMessage) {
	if len(msg.Parts) > 0 {
		parts, err := normalizeUserParts(msg.Content, msg.Parts)
		if err != nil {
			if writeErr := writeWSMessage(context.Background(), conn, WSMessage{Type: "error", Content: err.Error()}, writeMu); writeErr != nil {
				cancel()
			}
			return
		}
		msg.Parts = parts
		msg.Content = renderUserParts(parts, llm.RenderForHistory)
	}
	usePipeline := shouldUseTurnPipeline(h.turnConfig, personaName, sessionID)
	if pluginHostEnabled(h.pluginHost) && !usePipeline {
		if err := writeWSMessage(ctx, conn, WSMessage{Type: "error", Content: "plugins.enabled requires Turn Pipeline for this session/persona"}, writeMu); err != nil {
			cancel()
		}
		return
	}
	if h.turnConfig.Shadow && !usePipeline {
		env, err := wsMessageToInbound(msg, sessionID, personaName)
		if err == nil {
			_, _ = h.turnRuntime.Shadow(ctx, env)
		}
	}
	if usePipeline {
		turnCtx := context.WithoutCancel(ctx)
		turnCtx, done := h.registerRun(turnCtx, origin, sessionID, "turn_pipeline")
		sink := h.newWSOutboundSink(ctx, conn, writeMu)
		defer func() {
			closeOutboundSink(turnCtx, sink)
			done()
		}()
		env, err := wsMessageToInbound(msg, sessionID, personaName)
		if err != nil {
			if writeErr := writeWSMessage(context.Background(), conn, WSMessage{Type: "error", Content: err.Error()}, writeMu); writeErr != nil {
				cancel()
			}
			return
		}
		if _, err := h.turnRuntime.Execute(turnCtx, env, persona, sink); err != nil {
			if writeErr := writeWSMessage(context.Background(), conn, WSMessage{Type: "error", Content: err.Error()}, writeMu); writeErr != nil {
				cancel()
			}
		}
		return
	}
	if err := writeWSMessage(ctx, conn, WSMessage{Type: "stream_start"}, writeMu); err != nil {
		cancel()
		return
	}

	runCtx, done := h.registerRun(ctx, origin, sessionID, "emotion_turn")
	msgCtx := withWSWriter(runCtx, func(progressMsg WSMessage) {
		if writeErr := writeWSMessage(ctx, conn, progressMsg, writeMu); writeErr != nil {
			if !errors.Is(ctx.Err(), context.Canceled) {
				h.logger.Warn("ws progress write failed", "session", sessionID, "error", writeErr)
			}
			cancel()
		}
	})

	streamedDelta := false
	replyCfg := h.currentReplyDeliveryConfig()
	realtimeStreaming := h.currentRealtimeStreaming()
	useReplyDelivery := replyDeliveryEnabled(replyCfg, realtimeStreaming)
	var promptMode contextutil.PromptMode
	if useReplyDelivery {
		msgCtx = replydelivery.WithPromptModeRecorder(msgCtx, func(mode contextutil.PromptMode) {
			promptMode = mode
		})
	}
	send := h.engine.SendMessage
	if len(msg.Parts) > 0 {
		send = func(ctx context.Context, sessionID string, persona *config.Persona, _ string, cb func(delta string)) (string, error) {
			return h.engine.SendMessageParts(ctx, sessionID, persona, msg.Parts, cb)
		}
	}
	deltaCB := func(delta string) {
		if delta == "" {
			return
		}
		streamedDelta = true
		if writeErr := writeWSMessage(ctx, conn, WSMessage{Type: "stream_delta", Content: delta}, writeMu); writeErr != nil {
			if !errors.Is(ctx.Err(), context.Canceled) {
				h.logger.Warn("ws stream write failed", "session", sessionID, "error", writeErr)
			}
			cancel()
		}
	}
	if useReplyDelivery {
		deltaCB = nil
	}
	reply, err := send(msgCtx, sessionID, persona, msg.Content, deltaCB)
	done()
	if err != nil && !errors.Is(err, errApprovalPending) {
		if writeErr := writeWSMessage(context.Background(), conn, WSMessage{Type: "error", Content: err.Error()}, writeMu); writeErr != nil {
			cancel()
		}
		return
	}
	if err == nil && useReplyDelivery && reply != "" {
		plan := replydelivery.BuildPlan(replyCfg, string(promptMode), realtimeStreaming, reply)
		sink := turn.SinkFunc(func(_ context.Context, event turn.OutboundEvent) error {
			return writeWSMessage(ctx, conn, outboundEventToWSMessage(event), writeMu)
		})
		groupID := msg.RequestID
		if groupID == "" {
			groupID = sessionID
		}
		emitted, emitErr := emitReplyDeliverySegments(ctx, sink, replyCfg, plan, groupID)
		if emitErr != nil {
			if !errors.Is(ctx.Err(), context.Canceled) {
				h.logger.Warn("ws assistant segment write failed", "session", sessionID, "error", emitErr)
			}
			cancel()
			return
		}
		if emitted {
			streamedDelta = true
		}
	}
	if err == nil && !streamedDelta && reply != "" {
		if writeErr := writeWSMessage(ctx, conn, WSMessage{Type: "stream_delta", Content: reply}, writeMu); writeErr != nil {
			if !errors.Is(ctx.Err(), context.Canceled) {
				h.logger.Warn("ws stream write failed", "session", sessionID, "error", writeErr)
			}
			cancel()
			return
		}
	}
	if err := writeWSMessage(ctx, conn, WSMessage{Type: "stream_end"}, writeMu); err != nil {
		cancel()
		return
	}
	if err := h.emitApprovalEvents(ctx, conn, writeMu, sessionID); err != nil {
		cancel()
	}
}

func (h *Handler) registerRun(ctx context.Context, origin conversation.Origin, sessionID string, kind string) (context.Context, func()) {
	if h == nil || h.runs == nil {
		return ctx, func() {}
	}
	runCtx, cancel := context.WithCancel(ctx)
	unregister := h.runs.Register(conversation.RunRef{
		OriginKey: origin.OriginKey,
		SessionID: sessionID,
		Kind:      kind,
	}, cancel)
	return runCtx, func() {
		unregister()
		cancel()
	}
}

func (h *Handler) newWSOutboundSink(ctx context.Context, conn *websocket.Conn, mu *sync.Mutex) turn.OutboundSink {
	raw := &wsBestEffortOutboundSink{
		ctx:    ctx,
		conn:   conn,
		mu:     mu,
		logger: h.logger,
	}
	return turn.NewBoundedOutboundSink(raw, turn.BoundedOutboundOptions{})
}

func (h *Handler) currentReplyDeliveryConfig() config.ReplyDeliveryConfig {
	if engine, ok := h.engine.(*Engine); ok {
		return engine.RuntimeConfig().ReplyDelivery
	}
	return config.NormalizeReplyDeliveryConfig(h.replyDelivery)
}

func (h *Handler) currentRealtimeStreaming() bool {
	if engine, ok := h.engine.(*Engine); ok {
		return engine.RuntimeConfig().RealtimeStreaming
	}
	return h.realtimeStreaming
}

type wsBestEffortOutboundSink struct {
	ctx    context.Context
	conn   *websocket.Conn
	mu     *sync.Mutex
	logger *slog.Logger

	stateMu  sync.Mutex
	detached bool
}

func (s *wsBestEffortOutboundSink) Emit(_ context.Context, event turn.OutboundEvent) error {
	if s.isDetached() {
		return nil
	}
	if err := writeWSMessage(s.ctx, s.conn, outboundEventToWSMessage(event), s.mu); err != nil {
		if s.detach() && s.logger != nil {
			s.logger.Debug("ws outbound detached after write failed", "error", err)
		}
	}
	return nil
}

func (s *wsBestEffortOutboundSink) isDetached() bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.detached
}

func (s *wsBestEffortOutboundSink) detach() bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.detached {
		return false
	}
	s.detached = true
	return true
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

func hasMessageInput(msg WSMessage) bool {
	if strings.TrimSpace(msg.Content) != "" {
		return true
	}
	for _, part := range msg.Parts {
		if strings.TrimSpace(part.Text) != "" || part.Media != nil {
			return true
		}
	}
	return false
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

func buildTurnRuntimeStores(cfg config.TurnPipelineConfig, db *sql.DB, logger *slog.Logger, timezone string) (turn.TurnJournal, turn.IdempotencyStore) {
	journal, journalErr := buildTurnJournal(cfg, db, timezone)
	ids, idsErr := buildIdempotencyStore(cfg, db, timezone)
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

func buildTurnJournal(cfg config.TurnPipelineConfig, db *sql.DB, timezone string) (turn.TurnJournal, error) {
	switch cfg.Journal.Mode {
	case "memory":
		return turn.NewMemoryJournal(), nil
	case "jsonl":
		return turn.NewJSONLJournal(cfg.Journal.JSONLDir), nil
	case "sqlite_jsonl":
		if db == nil {
			return nil, errors.New("sqlite database is not configured")
		}
		return turn.NewMultiJournal(turn.NewSQLiteJournalWithTimezone(db, timezone), turn.NewJSONLJournal(cfg.Journal.JSONLDir)), nil
	case "", "sqlite":
		if db == nil {
			return nil, errors.New("sqlite database is not configured")
		}
		return turn.NewSQLiteJournalWithTimezone(db, timezone), nil
	default:
		return nil, fmt.Errorf("unsupported turn journal mode %q", cfg.Journal.Mode)
	}
}

func buildIdempotencyStore(cfg config.TurnPipelineConfig, db *sql.DB, timezone string) (turn.IdempotencyStore, error) {
	switch cfg.Idempotency.Mode {
	case "memory":
		return turn.NewMemoryIdempotencyStore(), nil
	case "", "sqlite":
		if db == nil {
			return nil, errors.New("sqlite database is not configured")
		}
		return turn.NewSQLiteIdempotencyStoreWithTimezone(db, timezone), nil
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

func resolveWSOrigin(r *http.Request) (conversation.Origin, error) {
	query := r.URL.Query()
	return conversation.ResolveOrigin(conversation.OriginRequest{
		OriginKey:              strings.TrimSpace(query.Get("origin_key")),
		SourceType:             strings.TrimSpace(query.Get("source")),
		AdapterInstanceID:      strings.TrimSpace(query.Get("adapter_instance_id")),
		PlatformID:             strings.TrimSpace(query.Get("platform_id")),
		ChannelType:            strings.TrimSpace(query.Get("channel_type")),
		ExternalConversationID: strings.TrimSpace(query.Get("external_conversation_id")),
		ExternalActorID:        strings.TrimSpace(query.Get("external_actor_id")),
		DisplayName:            strings.TrimSpace(query.Get("display_name")),
	})
}

func (h *Handler) bootstrapSession(ctx context.Context, origin conversation.Origin, personaName string, requestedSessionID string) (string, bool, error) {
	if h.bindings == nil {
		sessionID, resumed, err := h.engine.ResumeSession(ctx, requestedSessionID, personaName)
		if err != nil {
			return "", false, err
		}
		if resumed {
			return sessionID, true, nil
		}
		sessionID, err = h.engine.StartSession(ctx, personaName)
		return sessionID, false, err
	}
	if requestedSessionID != "" {
		sessionID, resumed, err := h.engine.ResumeSession(ctx, requestedSessionID, personaName)
		if err != nil {
			return "", false, err
		}
		if !resumed {
			sessionID, err = h.engine.StartSession(ctx, personaName)
			if err != nil {
				return "", false, err
			}
		}
		binding, err := h.bindings.BindSession(ctx, origin, personaName, sessionID, !resumed)
		if err != nil {
			return "", false, err
		}
		return binding.SessionID, !binding.IsNew, nil
	}
	binding, err := h.bindings.EnsureCurrent(ctx, origin, personaName)
	if err != nil {
		return "", false, err
	}
	return binding.SessionID, !binding.IsNew, nil
}

func (h *Handler) currentSession(ctx context.Context, origin conversation.Origin, personaName string, fallback string) (string, error) {
	if h == nil || h.bindings == nil {
		return fallback, nil
	}
	binding, err := h.bindings.EnsureCurrent(ctx, origin, personaName)
	if err != nil {
		return "", err
	}
	return binding.SessionID, nil
}

func (h *Handler) tryHandleCommand(ctx context.Context, conn *websocket.Conn, mu *sync.Mutex, msg WSMessage, origin conversation.Origin, sessionID, personaName string) (bool, string) {
	if h == nil || h.commandHandler == nil || len(msg.Parts) > 0 {
		return false, ""
	}
	response, handled, err := h.commandHandler.TryHandle(ctx, CommandRequest{
		Content:    msg.Content,
		Origin:     origin,
		SessionID:  sessionID,
		PersonaKey: personaName,
		ActorID:    origin.ExternalActorID,
		ActorName:  origin.DisplayName,
		ActorRole:  "member",
	})
	if !handled {
		return false, ""
	}
	if err != nil {
		_ = writeWSMessage(context.Background(), conn, WSMessage{Type: "command_result", Status: "failed", Content: err.Error(), ErrorKind: "internal_error"}, mu)
		return true, ""
	}
	for _, out := range response.Messages {
		if out.OriginKey == "" {
			out.OriginKey = origin.OriginKey
		}
		if out.SessionID == "" {
			out.SessionID = sessionID
		}
		if out.Persona == "" {
			out.Persona = personaName
		}
		if err := writeWSMessage(ctx, conn, out, mu); err != nil {
			return true, response.SessionID
		}
	}
	return true, response.SessionID
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
