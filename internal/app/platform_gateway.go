package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/longyisang/emoagent/internal/chat"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/conversation"
	"github.com/longyisang/emoagent/internal/platform"
	"github.com/longyisang/emoagent/internal/storage"
)

type PlatformGateway struct {
	infra        *Infra
	conversation *ConversationService
	commands     *CommandService
	chat         *ChatService
	agentRuntime *AgentRuntimeService
	personas     *PersonaService
	receipts     platform.ReceiptStore
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
	if len(in.Parts) > 0 {
		return platform.HandleResult{}, fmt.Errorf("inbound message parts are not supported")
	}
	if strings.TrimSpace(in.Text) == "" {
		return platform.HandleResult{}, fmt.Errorf("inbound message text is required")
	}
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
			return platform.HandleResult{Handled: true, Duplicate: true}, nil
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
	turnResult, err := g.sendText(ctx, origin, binding.SessionID, agentRuntime, persona, in)
	if err != nil {
		g.failReceipt(ctx, receipt.ID, binding.SessionID, err)
		_ = emitPlatformError(ctx, sink, origin, binding.SessionID, personaKey, in.ExternalMessageID, err)
		return platform.HandleResult{}, err
	}
	if strings.TrimSpace(turnResult.Text) != "" {
		if err := sink.Emit(ctx, platform.OutboundEvent{
			Type:                     "message",
			Origin:                   origin,
			SessionID:                binding.SessionID,
			PersonaKey:               personaKey,
			Content:                  turnResult.Text,
			ReplyToExternalMessageID: in.ExternalMessageID,
		}); err != nil {
			g.failReceipt(ctx, receipt.ID, binding.SessionID, err)
			return platform.HandleResult{}, err
		}
	}
	g.completeReceipt(ctx, receipt.ID, binding.SessionID, turnResult.ResultType)
	return platform.HandleResult{Handled: true, SessionID: binding.SessionID}, nil
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
		unregister := g.conversation.RunRegistry().Register(conversation.RunRef{
			OriginKey: origin.OriginKey,
			SessionID: sessionID,
			Kind:      "platform_text",
		}, cancel)
		done = func() {
			unregister()
			cancel()
		}
	}
	defer done()
	return g.chat.SendPlatformTurn(runCtx, agentRuntime, sessionID, persona, in)
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
			return strings.TrimSpace(agents[0].ID)
		}
	}
	if len(g.infra.Config.AgentConfigs) > 0 {
		return strings.TrimSpace(g.infra.Config.AgentConfigs[0].ID)
	}
	return ""
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
		ID:        record.ID,
		Status:    platformReceiptStatus(record.Status),
		Duplicate: record.Duplicate,
	}, nil
}

func (s storageReceiptStore) CompleteInbound(ctx context.Context, receiptID string, sessionID string, resultType string) error {
	if s.db == nil {
		return fmt.Errorf("database is not configured")
	}
	return s.db.CompletePlatformMessageReceipt(ctx, receiptID, sessionID, resultType)
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
