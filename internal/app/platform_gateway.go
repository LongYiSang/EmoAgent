package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/chat"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/conversation"
	"github.com/longyisang/emoagent/internal/platform"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/turn"
)

type PlatformGateway struct {
	infra        *Infra
	conversation *ConversationService
	commands     *CommandService
	chat         *ChatService
	agentRuntime *AgentRuntimeService
	personas     *PersonaService
	receipts     platform.ReceiptStore
	proactive    *ProactiveService
}

// SetProactive wires the proactive service so inbound messages can be attributed
// as replies to proactive ones.
func (g *PlatformGateway) SetProactive(service *ProactiveService) {
	if g != nil {
		g.proactive = service
	}
}

func NewPlatformGateway(infra *Infra, conversation *ConversationService, commands *CommandService, chat *ChatService, agentRuntime *AgentRuntimeService, personas *PersonaService, receipts platform.ReceiptStore) *PlatformGateway {
	return &PlatformGateway{
		infra:        infra,
		conversation: conversation,
		commands:     commands,
		chat:         chat,
		agentRuntime: agentRuntime,
		personas:     personas,
		receipts:     receipts,
	}
}

func (g *PlatformGateway) HandleInbound(ctx context.Context, in platform.InboundMessage, sink platform.OutboundSink) (platform.HandleResult, error) {
	if g == nil {
		return platform.HandleResult{}, fmt.Errorf("platform gateway is not configured")
	}
	hasParts := len(in.Parts) > 0
	if strings.TrimSpace(in.Text) == "" && !hasParts {
		return platform.HandleResult{}, fmt.Errorf("inbound message text is required")
	}
	// A user message on this origin may be an answer to a proactive message.
	// Attributing it is the only real feedback the gate ever gets about whether
	// its interruptions were welcome.
	defer func() {
		if g.proactive != nil {
			if attributed, attrErr := platform.OriginFromInbound(in, in.OriginScope); attrErr == nil {
				g.proactive.NoteUserMessage(ctx, attributed.OriginKey, time.Now())
			}
		}
	}()
	origin, err := platform.OriginFromInbound(in, in.OriginScope)
	if err != nil {
		return platform.HandleResult{}, err
	}
	var receipt platform.ReceiptResult
	if strings.TrimSpace(in.ExternalMessageID) != "" && g.receipts != nil {
		receipt, err = g.receipts.BeginInbound(ctx, in, origin)
		if err != nil {
			return platform.HandleResult{}, err
		}
		if receipt.Duplicate {
			if receipt.DuplicateKind == "running" {
				_ = emitPlatformMessage(ctx, sink, origin, "", "", in.ExternalMessageID, platformBusyMessage())
			}
			return platform.HandleResult{Handled: true, Duplicate: true}, nil
		}
		if receipt.Retry {
			in.RequestID = platformRetryRequestID(in.ExternalMessageID, receipt.AttemptCount)
		}
	}
	agentID := g.platformAgentID()
	personaKey := strings.TrimSpace(in.PersonaKey)
	if agentID != "" {
		personaKey = g.platformAgentPersonaKey(agentID, personaKey)
	}
	if personaKey == "" {
		personaKey = g.defaultPersonaKey()
	}
	if personaKey == "" {
		personaKey = "default"
	}
	binding, err := g.ensureCurrent(ctx, origin, personaKey)
	if err != nil {
		g.failReceipt(ctx, receipt.ID, "", err)
		return platform.HandleResult{}, err
	}
	actorID := firstNonEmptyCommandValue(in.Actor.ID, in.ExternalActorID)
	actorRole := string(in.Actor.Role)
	if actorRole == "" {
		actorRole = string(platform.ActorRoleMember)
	}
	if hasParts && g.isPlatformCommandText(in.Text) {
		err := fmt.Errorf("带图片的消息不支持平台命令，请单独发送命令文本")
		g.failReceipt(ctx, receipt.ID, binding.SessionID, err)
		_ = emitPlatformError(ctx, sink, origin, binding.SessionID, personaKey, in.ExternalMessageID, err)
		return platform.HandleResult{}, err
	}
	if handled, result, err := g.tryHandlePlatformApprovalCommand(ctx, origin, binding.SessionID, agentID, personaKey, in, sink, receipt.ID); handled {
		return result, err
	}
	if g.commands != nil {
		response, handled, err := g.commands.TryHandle(ctx, chat.CommandRequest{
			Content:    in.Text,
			Origin:     origin,
			SessionID:  binding.SessionID,
			AgentID:    agentID,
			PersonaKey: personaKey,
			ActorID:    actorID,
			ActorName:  in.Actor.DisplayName,
			ActorRole:  actorRole,
		})
		if err != nil {
			g.failReceipt(ctx, receipt.ID, binding.SessionID, err)
			_ = emitPlatformError(ctx, sink, origin, binding.SessionID, personaKey, in.ExternalMessageID, err)
			return platform.HandleResult{}, err
		}
		if handled {
			sessionID := firstNonEmptyCommandValue(response.SessionID, binding.SessionID)
			for _, msg := range response.Messages {
				if err := sink.Emit(ctx, wsMessageToPlatformEvent(msg, origin, sessionID, personaKey, in.ExternalMessageID)); err != nil {
					g.failReceipt(ctx, receipt.ID, sessionID, err)
					return platform.HandleResult{}, err
				}
			}
			g.completeReceipt(ctx, receipt.ID, sessionID, "command_result")
			return platform.HandleResult{Handled: true, SessionID: sessionID}, nil
		}
	}
	agentRuntime, err := g.platformAgentRuntime(agentID)
	if err != nil {
		g.failReceipt(ctx, receipt.ID, binding.SessionID, err)
		_ = emitPlatformError(ctx, sink, origin, binding.SessionID, personaKey, in.ExternalMessageID, err)
		return platform.HandleResult{}, err
	}
	if runtimePersona := strings.TrimSpace(agentRuntime.PersonaKey); runtimePersona != "" && runtimePersona != personaKey {
		personaKey = runtimePersona
		binding, err = g.ensureCurrent(ctx, origin, personaKey)
		if err != nil {
			g.failReceipt(ctx, receipt.ID, "", err)
			return platform.HandleResult{}, err
		}
	}
	persona, ok := g.persona(personaKey)
	if !ok {
		persona = &config.Persona{Name: personaKey}
	}
	if g.infra != nil && g.infra.Logger != nil {
		g.infra.Logger.Info("platform turn started",
			"origin_key", origin.OriginKey,
			"external_message_id", in.ExternalMessageID,
			"session_id", binding.SessionID,
			"agent_id", agentRuntime.ID,
			"persona_key", personaKey,
		)
	}
	turnResult, err := g.sendText(ctx, origin, binding.SessionID, agentRuntime, persona, in)
	if err != nil {
		g.failReceipt(ctx, receipt.ID, binding.SessionID, err)
		_ = emitPlatformError(ctx, sink, origin, binding.SessionID, personaKey, in.ExternalMessageID, err)
		return platform.HandleResult{}, err
	}
	if err := emitPlatformTurnResult(ctx, sink, origin, binding.SessionID, personaKey, in.ExternalMessageID, turnResult); err != nil {
		g.failReceipt(ctx, receipt.ID, binding.SessionID, err)
		return platform.HandleResult{}, err
	}
	g.completeReceiptWithMeta(ctx, receipt.ID, binding.SessionID, turnResult.ResultType, turnResult.TurnID, agentRuntime.ID, personaKey)
	if g.infra != nil && g.infra.Logger != nil {
		g.infra.Logger.Info("platform turn completed",
			"origin_key", origin.OriginKey,
			"external_message_id", in.ExternalMessageID,
			"session_id", binding.SessionID,
			"turn_id", turnResult.TurnID,
			"agent_id", agentRuntime.ID,
			"persona_key", personaKey,
			"result_type", turnResult.ResultType,
			"status", turnResult.Status,
			"error_kind", turnResult.ErrorKind,
		)
	}
	return platform.HandleResult{Handled: true, SessionID: binding.SessionID}, nil
}

func (g *PlatformGateway) isPlatformCommandText(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	prefixes := []string{"/"}
	if g != nil && g.infra != nil && g.infra.Config != nil && len(g.infra.Config.Platforms.Common.CommandPrefixes) > 0 {
		prefixes = g.infra.Config.Platforms.Common.CommandPrefixes
	}
	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix != "" && strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func (g *PlatformGateway) ensureCurrent(ctx context.Context, origin conversation.Origin, personaKey string) (conversation.Binding, error) {
	if g == nil || g.conversation == nil || g.conversation.Bindings() == nil {
		return conversation.Binding{}, fmt.Errorf("conversation binding service is not configured")
	}
	return g.conversation.Bindings().EnsureCurrent(ctx, origin, personaKey)
}

func (g *PlatformGateway) sendText(ctx context.Context, origin conversation.Origin, sessionID string, agentRuntime *ActiveAgentRuntime, persona *config.Persona, in platform.InboundMessage) (PlatformTurnResult, error) {
	if g == nil || g.chat == nil {
		return PlatformTurnResult{}, fmt.Errorf("chat engine is not configured")
	}
	runCtx := ctx
	done := func() {}
	if g.conversation != nil && g.conversation.RunRegistry() != nil {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithCancel(ctx)
		unregister, ok := g.conversation.RunRegistry().TryRegister(conversation.RunRef{
			OriginKey: origin.OriginKey,
			SessionID: sessionID,
			Kind:      "platform_text",
		}, cancel)
		if !ok {
			cancel()
			return PlatformTurnResult{
				Text:       platformBusyMessage(),
				ResultType: "busy",
			}, nil
		}
		done = func() {
			unregister()
			cancel()
		}
	}
	defer done()
	return g.chat.SendPlatformTurn(runCtx, agentRuntime, sessionID, persona, origin, in)
}

func platformBusyMessage() string {
	return "上一条消息还在处理中，请等我回复完，或发送 /stop 后再试。"
}

func (g *PlatformGateway) sendApproval(ctx context.Context, origin conversation.Origin, sessionID string, agentRuntime *ActiveAgentRuntime, persona *config.Persona, in platform.InboundMessage, approval turn.InboundApproval) (PlatformTurnResult, error) {
	if g == nil || g.chat == nil {
		return PlatformTurnResult{}, fmt.Errorf("chat engine is not configured")
	}
	return g.chat.SendPlatformApprovalTurn(ctx, agentRuntime, sessionID, persona, origin, in, approval)
}

func (g *PlatformGateway) tryHandlePlatformApprovalCommand(ctx context.Context, origin conversation.Origin, sessionID string, agentID string, personaKey string, in platform.InboundMessage, sink platform.OutboundSink, receiptID string) (bool, platform.HandleResult, error) {
	command, args, ok := parsePlatformApprovalCommand(in.Text)
	if !ok {
		return false, platform.HandleResult{}, nil
	}
	switch command {
	case "approvals":
		content := formatPlatformApprovals(g.pendingPlatformApprovals(sessionID))
		if err := emitPlatformMessage(ctx, sink, origin, sessionID, personaKey, in.ExternalMessageID, content); err != nil {
			g.failReceipt(ctx, receiptID, sessionID, err)
			return true, platform.HandleResult{}, err
		}
		g.completeReceipt(ctx, receiptID, sessionID, "command_result")
		return true, platform.HandleResult{Handled: true, SessionID: sessionID}, nil
	case "approve", "reject":
		if len(args) == 0 {
			content := "用法：/" + command + " <request_id>"
			if command == "approve" {
				content = "用法：/approve <request_id> [option_id]"
			}
			if err := emitPlatformMessage(ctx, sink, origin, sessionID, personaKey, in.ExternalMessageID, content); err != nil {
				g.failReceipt(ctx, receiptID, sessionID, err)
				return true, platform.HandleResult{}, err
			}
			g.completeReceipt(ctx, receiptID, sessionID, "command_result")
			return true, platform.HandleResult{Handled: true, SessionID: sessionID}, nil
		}
		result, err := g.handlePlatformApprovalAction(ctx, origin, sessionID, agentID, personaKey, in, sink, receiptID, command, args)
		return true, result, err
	default:
		return false, platform.HandleResult{}, nil
	}
}

func (g *PlatformGateway) handlePlatformApprovalAction(ctx context.Context, origin conversation.Origin, sessionID string, agentID string, personaKey string, in platform.InboundMessage, sink platform.OutboundSink, receiptID string, action string, args []string) (platform.HandleResult, error) {
	agentRuntime, err := g.platformAgentRuntime(agentID)
	if err != nil {
		g.failReceipt(ctx, receiptID, sessionID, err)
		_ = emitPlatformError(ctx, sink, origin, sessionID, personaKey, in.ExternalMessageID, err)
		return platform.HandleResult{}, err
	}
	if runtimePersona := strings.TrimSpace(agentRuntime.PersonaKey); runtimePersona != "" && runtimePersona != personaKey {
		personaKey = runtimePersona
		binding, err := g.ensureCurrent(ctx, origin, personaKey)
		if err != nil {
			g.failReceipt(ctx, receiptID, "", err)
			return platform.HandleResult{}, err
		}
		sessionID = binding.SessionID
	}
	persona, ok := g.persona(personaKey)
	if !ok {
		persona = &config.Persona{Name: personaKey}
	}
	approval := turn.InboundApproval{
		RequestID: args[0],
		Action:    action,
	}
	if action == "approve" && len(args) > 1 {
		approval.OptionID = args[1]
	}
	turnResult, err := g.sendApproval(ctx, origin, sessionID, agentRuntime, persona, in, approval)
	if err != nil {
		g.failReceipt(ctx, receiptID, sessionID, err)
		_ = emitPlatformError(ctx, sink, origin, sessionID, personaKey, in.ExternalMessageID, err)
		return platform.HandleResult{}, err
	}
	if err := emitPlatformTurnResult(ctx, sink, origin, sessionID, personaKey, in.ExternalMessageID, turnResult); err != nil {
		g.failReceipt(ctx, receiptID, sessionID, err)
		return platform.HandleResult{}, err
	}
	g.completeReceiptWithMeta(ctx, receiptID, sessionID, turnResult.ResultType, turnResult.TurnID, agentRuntime.ID, personaKey)
	return platform.HandleResult{Handled: true, SessionID: sessionID}, nil
}

func (g *PlatformGateway) pendingPlatformApprovals(sessionID string) []protocol.ApprovalRequest {
	if g == nil || g.chat == nil || g.chat.work == nil || g.chat.work.Approvals() == nil {
		return []protocol.ApprovalRequest{}
	}
	return g.chat.work.Approvals().ListSessionApprovals(sessionID, []protocol.ApprovalStatus{protocol.ApprovalStatusPending})
}

func parsePlatformApprovalCommand(text string) (string, []string, bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) == 0 {
		return "", nil, false
	}
	if !strings.HasPrefix(fields[0], "/") {
		return "", nil, false
	}
	name := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	switch name {
	case "approvals", "approve", "reject":
		return name, fields[1:], true
	default:
		return "", nil, false
	}
}

func platformRetryRequestID(externalMessageID string, attemptCount int) string {
	externalMessageID = strings.TrimSpace(externalMessageID)
	if externalMessageID == "" || attemptCount <= 1 {
		return ""
	}
	return externalMessageID + ":retry:" + fmt.Sprint(attemptCount)
}

func formatPlatformApprovals(approvals []protocol.ApprovalRequest) string {
	if len(approvals) == 0 {
		return "当前没有待审批请求。"
	}
	lines := []string{"待审批请求："}
	for _, approval := range approvals {
		subject := firstNonEmptyCommandValue(approval.Question, approval.GoalSummary, approval.ID)
		lines = append(lines, "- "+approval.ID+"："+subject)
	}
	lines = append(lines, "", "查看详情请等待对应审批提示，或使用：")
	lines = append(lines, "/approve <request_id> <option_id>", "/reject <request_id>")
	return strings.Join(lines, "\n")
}

func emitPlatformTurnResult(ctx context.Context, sink platform.OutboundSink, origin conversation.Origin, sessionID string, personaKey string, replyTo string, result PlatformTurnResult) error {
	for _, event := range result.Events {
		if err := sink.Emit(ctx, fillPlatformEvent(event, origin, sessionID, personaKey, replyTo)); err != nil {
			return err
		}
	}
	if result.ResultType == "approval_wait" {
		return nil
	}
	outbound := result.Segments
	if len(outbound) == 0 && strings.TrimSpace(result.Text) != "" {
		outbound = []string{result.Text}
	}
	for _, content := range outbound {
		if err := emitPlatformMessage(ctx, sink, origin, sessionID, personaKey, replyTo, content); err != nil {
			return err
		}
	}
	return nil
}

func fillPlatformEvent(event platform.OutboundEvent, origin conversation.Origin, sessionID string, personaKey string, replyTo string) platform.OutboundEvent {
	if event.Origin.OriginKey == "" {
		event.Origin = origin
	}
	if event.SessionID == "" {
		event.SessionID = sessionID
	}
	if event.PersonaKey == "" {
		event.PersonaKey = personaKey
	}
	if event.ReplyToExternalMessageID == "" {
		event.ReplyToExternalMessageID = replyTo
	}
	return event
}

func emitPlatformMessage(ctx context.Context, sink platform.OutboundSink, origin conversation.Origin, sessionID string, personaKey string, replyTo string, content string) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	return sink.Emit(ctx, platform.OutboundEvent{
		Type:                     "message",
		Origin:                   origin,
		SessionID:                sessionID,
		PersonaKey:               personaKey,
		Content:                  content,
		ReplyToExternalMessageID: replyTo,
	})
}

func (g *PlatformGateway) platformAgentID() string {
	if g == nil || g.infra == nil || g.infra.Config == nil {
		return ""
	}
	agentID := strings.TrimSpace(g.infra.Config.Platforms.Common.DefaultAgentID)
	if agentID != "" {
		return agentID
	}
	if g.agentRuntime != nil {
		agents, err := g.agentRuntime.ListAgentConfigs()
		if err == nil && len(agents) > 0 {
			g.warnPlatformAgentFallback(strings.TrimSpace(agents[0].ID))
			return strings.TrimSpace(agents[0].ID)
		}
	}
	if len(g.infra.Config.AgentConfigs) > 0 {
		g.warnPlatformAgentFallback(strings.TrimSpace(g.infra.Config.AgentConfigs[0].ID))
		return strings.TrimSpace(g.infra.Config.AgentConfigs[0].ID)
	}
	return ""
}

func (g *PlatformGateway) warnPlatformAgentFallback(agentID string) {
	if g == nil || g.infra == nil || g.infra.Config == nil || g.infra.Logger == nil {
		return
	}
	if !g.infra.Config.Platforms.Enabled || len(g.infra.Config.Platforms.Adapters) == 0 {
		return
	}
	g.infra.Logger.Warn("platform default agent is not configured; falling back to first agent",
		"agent_id", agentID,
	)
}

func (g *PlatformGateway) platformAgentPersonaKey(agentID string, fallback string) string {
	agentID = strings.TrimSpace(agentID)
	if agentID != "" && g != nil && g.agentRuntime != nil {
		if agent, err := g.agentRuntime.GetAgentConfig(agentID); err == nil && agent != nil && strings.TrimSpace(agent.PersonaKey) != "" {
			return strings.TrimSpace(agent.PersonaKey)
		}
	}
	return strings.TrimSpace(fallback)
}

func (g *PlatformGateway) platformAgentRuntime(agentID string) (*ActiveAgentRuntime, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("platform agent config is not configured")
	}
	if g.agentRuntime == nil {
		return nil, fmt.Errorf("platform agent config %q is configured but agent runtime service is unavailable", agentID)
	}
	runtime, err := g.agentRuntime.Build(agentID, true)
	if err != nil {
		return nil, fmt.Errorf("platform agent config %q: %w", agentID, err)
	}
	return runtime, nil
}

func (g *PlatformGateway) persona(personaKey string) (*config.Persona, bool) {
	if g == nil || g.personas == nil {
		return nil, false
	}
	return g.personas.Get(personaKey)
}

func (g *PlatformGateway) defaultPersonaKey() string {
	if g != nil && g.infra != nil && g.infra.Config != nil && strings.TrimSpace(g.infra.Config.Platforms.Common.DefaultPersona) != "" {
		return strings.TrimSpace(g.infra.Config.Platforms.Common.DefaultPersona)
	}
	if g != nil && g.infra != nil && g.infra.Config != nil && len(g.infra.Config.AgentConfigs) > 0 {
		return strings.TrimSpace(g.infra.Config.AgentConfigs[0].PersonaKey)
	}
	return "default"
}

func (g *PlatformGateway) completeReceipt(ctx context.Context, receiptID string, sessionID string, resultType string) {
	if g != nil && g.receipts != nil && receiptID != "" {
		_ = g.receipts.CompleteInbound(ctx, receiptID, sessionID, resultType)
	}
}

func (g *PlatformGateway) completeReceiptWithMeta(ctx context.Context, receiptID string, sessionID string, resultType string, turnID string, agentID string, personaKey string) {
	if g == nil || g.receipts == nil || receiptID == "" {
		return
	}
	if store, ok := g.receipts.(interface {
		CompleteInboundWithMeta(context.Context, storage.PlatformMessageReceiptCompletion) error
	}); ok {
		_ = store.CompleteInboundWithMeta(ctx, storage.PlatformMessageReceiptCompletion{
			ReceiptID:          receiptID,
			SessionID:          sessionID,
			ResultType:         resultType,
			TurnID:             turnID,
			AgentID:            agentID,
			ResolvedPersonaKey: personaKey,
		})
		return
	}
	_ = g.receipts.CompleteInbound(ctx, receiptID, sessionID, resultType)
}

func (g *PlatformGateway) failReceipt(ctx context.Context, receiptID string, sessionID string, err error) {
	if g != nil && g.receipts != nil && receiptID != "" {
		_ = g.receipts.FailInbound(ctx, receiptID, sessionID, err)
	}
}

func wsMessageToPlatformEvent(msg chat.WSMessage, origin conversation.Origin, sessionID string, personaKey string, replyTo string) platform.OutboundEvent {
	return platform.OutboundEvent{
		Type:                     msg.Type,
		Origin:                   origin,
		SessionID:                firstNonEmptyCommandValue(msg.SessionID, sessionID),
		PersonaKey:               firstNonEmptyCommandValue(msg.Persona, personaKey),
		Content:                  msg.Content,
		Status:                   msg.Status,
		ErrorKind:                msg.ErrorKind,
		Payload:                  msg.Payload,
		ReplyToExternalMessageID: replyTo,
	}
}

func emitPlatformError(ctx context.Context, sink platform.OutboundSink, origin conversation.Origin, sessionID string, personaKey string, replyTo string, err error) error {
	if sink == nil {
		return nil
	}
	return sink.Emit(ctx, platform.OutboundEvent{
		Type:                     "error",
		Origin:                   origin,
		SessionID:                sessionID,
		PersonaKey:               personaKey,
		Content:                  err.Error(),
		Status:                   "failed",
		ErrorKind:                "internal_error",
		ReplyToExternalMessageID: replyTo,
	})
}

type storageReceiptStore struct {
	db *storage.DB
}

func NewStorageReceiptStore(db *storage.DB) platform.ReceiptStore {
	return storageReceiptStore{db: db}
}

func (s storageReceiptStore) BeginInbound(ctx context.Context, in platform.InboundMessage, origin conversation.Origin) (platform.ReceiptResult, error) {
	if s.db == nil {
		return platform.ReceiptResult{}, fmt.Errorf("database is not configured")
	}
	record, err := s.db.BeginPlatformMessageReceipt(ctx, storage.PlatformMessageReceiptRecord{
		ExternalMessageID:      in.ExternalMessageID,
		SourceType:             in.SourceType,
		AdapterInstanceID:      in.AdapterInstanceID,
		PlatformID:             in.PlatformID,
		OriginKey:              origin.OriginKey,
		SessionID:              "",
		PersonaKey:             in.PersonaKey,
		ChannelType:            in.ChannelType,
		ExternalConversationID: in.ExternalConversationID,
		ExternalActorID:        in.ExternalActorID,
		Text:                   in.Text,
		RawEventHash:           in.RawEventHash,
	})
	if err != nil {
		return platform.ReceiptResult{}, err
	}
	return platform.ReceiptResult{
		ID:             record.ID,
		Status:         platformReceiptStatus(record.Status),
		Duplicate:      record.Duplicate,
		Retry:          record.Retry,
		DuplicateKind:  record.DuplicateKind,
		ExistingStatus: string(record.ExistingStatus),
		AttemptCount:   record.AttemptCount,
	}, nil
}

func (s storageReceiptStore) CompleteInbound(ctx context.Context, receiptID string, sessionID string, resultType string) error {
	if s.db == nil {
		return fmt.Errorf("database is not configured")
	}
	return s.db.CompletePlatformMessageReceipt(ctx, receiptID, sessionID, resultType)
}

func (s storageReceiptStore) CompleteInboundWithMeta(ctx context.Context, completion storage.PlatformMessageReceiptCompletion) error {
	if s.db == nil {
		return fmt.Errorf("database is not configured")
	}
	return s.db.CompletePlatformMessageReceiptWithMeta(ctx, completion)
}

func (s storageReceiptStore) FailInbound(ctx context.Context, receiptID string, sessionID string, err error) error {
	if s.db == nil {
		return fmt.Errorf("database is not configured")
	}
	return s.db.FailPlatformMessageReceipt(ctx, receiptID, sessionID, err)
}

func platformReceiptStatus(status storage.PlatformMessageReceiptStatus) platform.ReceiptStatus {
	switch status {
	case storage.PlatformMessageReceiptStatusHandled:
		return platform.ReceiptStatusHandled
	case storage.PlatformMessageReceiptStatusDuplicate:
		return platform.ReceiptStatusDuplicate
	case storage.PlatformMessageReceiptStatusFailed:
		return platform.ReceiptStatusFailed
	case storage.PlatformMessageReceiptStatusIgnored:
		return platform.ReceiptStatusIgnored
	default:
		return platform.ReceiptStatusProcessing
	}
}
