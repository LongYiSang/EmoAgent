package app

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/longyisang/emoagent/internal/chat"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/conversation"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/media"
	"github.com/longyisang/emoagent/internal/platform"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/turn"
	workctx "github.com/longyisang/emoagent/internal/work"
)

type ChatService struct {
	infra        *Infra
	agentRuntime *AgentRuntimeService
	tools        *ToolService
	plugins      *PluginService
	work         *WorkService
	memory       *MemoryService
	media        *MediaService
	llmProviders *LLMProviderService
	agentAffect  *AgentAffectService
	conversation *ConversationService
	commands     *CommandService
	dispatcher   *tool.Dispatcher
	engine       *chat.Engine
	turnMu       sync.Mutex
	turnStores   chat.TurnStores
	turnReady    bool
}

func (s *ChatService) Engine() *chat.Engine {
	return s.engine
}

func (s *ChatService) BuildEngine(dispatcher *tool.Dispatcher) *chat.Engine {
	s.dispatcher = dispatcher
	s.engine = s.newEngine(s.agentRuntime.Active(), dispatcher)
	if s.conversation != nil && s.conversation.Bindings() != nil {
		s.conversation.Bindings().SetSessionStarter(s.engine)
	}
	return s.engine
}

type PlatformTurnResult struct {
	Text       string
	Segments   []string
	Events     []platform.OutboundEvent
	TurnID     string
	Status     string
	ErrorKind  string
	ResultType string
}

func (s *ChatService) SendPlatformTurn(ctx context.Context, runtime *ActiveAgentRuntime, sessionID string, persona *config.Persona, origin conversation.Origin, in platform.InboundMessage) (PlatformTurnResult, error) {
	env := platformTurnEnvelope(in, sessionID, runtimePersonaKey(runtime))
	return s.executePlatformTurn(ctx, runtime, sessionID, persona, origin, in, env)
}

func (s *ChatService) SendPlatformApprovalTurn(ctx context.Context, runtime *ActiveAgentRuntime, sessionID string, persona *config.Persona, origin conversation.Origin, in platform.InboundMessage, approval turn.InboundApproval) (PlatformTurnResult, error) {
	env := platformApprovalEnvelope(in, sessionID, runtimePersonaKey(runtime), approval)
	return s.executePlatformTurn(ctx, runtime, sessionID, persona, origin, in, env)
}

func (s *ChatService) executePlatformTurn(ctx context.Context, runtime *ActiveAgentRuntime, sessionID string, persona *config.Persona, origin conversation.Origin, in platform.InboundMessage, env turn.InboundEnvelope) (PlatformTurnResult, error) {
	if s == nil {
		return PlatformTurnResult{}, fmt.Errorf("chat service is not configured")
	}
	if runtime == nil {
		return PlatformTurnResult{}, fmt.Errorf("platform agent runtime is not configured")
	}
	cfg := config.DefaultConfig()
	if s.infra != nil && s.infra.Config != nil {
		cfg = s.infra.Config
	}
	if !cfg.Chat.TurnPipeline.Enabled || !cfg.Chat.TurnPipeline.MemoryStages {
		return PlatformTurnResult{}, fmt.Errorf("platform text requires chat.turn_pipeline.enabled and memory_stages")
	}
	engine := s.newEngine(runtime, s.dispatcher)
	if engine == nil {
		return PlatformTurnResult{}, fmt.Errorf("chat engine is not configured")
	}
	replyDelivery := cfg.Chat.ReplyDelivery
	replyDelivery.Timing.Enabled = false
	engine.UpdateReplyDeliveryConfig(replyDelivery)
	sink := &platformTurnSink{
		origin:     origin,
		sessionID:  sessionID,
		personaKey: runtime.PersonaKey,
		replyTo:    in.ExternalMessageID,
	}
	runCtx := workctx.WithAgentID(ctx, runtime.ID)
	result, err := s.turnRunnerForEngine(engine).Execute(runCtx, env, persona, sink)
	out := PlatformTurnResult{
		Text:      sink.Text(),
		Segments:  sink.Segments(),
		Events:    sink.Events(),
		TurnID:    result.TurnID,
		Status:    result.Status,
		ErrorKind: result.ErrorKind,
	}
	switch {
	case sink.ResultType() != "":
		out.ResultType = sink.ResultType()
	case result.Status == "approval_wait":
		out.ResultType = "approval_wait"
	case strings.TrimSpace(out.Text) == "":
		out.ResultType = "no_output"
	default:
		out.ResultType = "message"
	}
	if err != nil {
		return out, err
	}
	return out, nil
}

func (s *ChatService) newEngine(activeRuntime *ActiveAgentRuntime, dispatcher *tool.Dispatcher) *chat.Engine {
	cfg := config.DefaultConfig()
	if s.infra.Config != nil {
		cfg = s.infra.Config
	}

	model := ""
	params := llm.RequestParams{}
	summaryModel := ""
	summaryParams := llm.RequestParams{}
	maxTokens := 0
	temperature := 0.0
	provider := ""
	providerID := ""
	providerName := ""
	agentID := ""
	personaKey := ""
	userAddress := config.AgentUserAddressConfig{}
	currentClient := s.infra.LLM
	summaryClient := s.infra.LLM
	contextCfg := s.agentRuntime.GlobalContextConfig()
	if activeRuntime != nil {
		agentID = activeRuntime.ID
		personaKey = activeRuntime.PersonaKey
		userAddress = activeRuntime.UserAddress
		currentClient = activeRuntime.EmotionMain.Client
		summaryClient = activeRuntime.EmotionSummary.Client
		model = activeRuntime.EmotionMain.Model
		params = cloneRequestParams(activeRuntime.EmotionMain.Params)
		summaryModel = activeRuntime.EmotionSummary.Model
		summaryParams = cloneRequestParams(activeRuntime.EmotionSummary.Params)
		maxTokens = params.MaxTokens
		temperature = derefFloat64(params.Temperature, 0)
		provider = toolProviderName(activeRuntime.EmotionMain.Provider.Protocol)
		providerID = activeRuntime.EmotionMain.Provider.ID
		providerName = providerDisplayName(activeRuntime.EmotionMain.Provider)
		contextCfg = activeRuntime.Context
	}
	var affectRuntime chat.AgentAffectRuntime
	if s.agentAffect != nil {
		affectRuntime = s.agentAffect.Runtime()
	}

	var mediaStore media.Store
	if s.media != nil {
		mediaStore = s.media.Store()
	}
	var mediaResolver media.CapabilityResolver
	if s.llmProviders != nil {
		mediaResolver = s.llmProviders
	}
	return chat.NewEngine(chat.EngineConfig{
		LLM:                currentClient,
		SummaryLLM:         summaryClient,
		DB:                 s.infra.DB,
		Logger:             s.infra.Logger,
		Model:              model,
		Params:             params,
		SummaryModel:       summaryModel,
		SummaryParams:      summaryParams,
		SummaryTemperature: summaryParams.Temperature,
		SummaryMaxTokens:   summaryParams.MaxTokens,
		MaxTokens:          maxTokens,
		Temperature:        temperature,
		ContextConfig:      contextCfg,
		PromptRouter:       cfg.Chat.PromptRouter,
		ReplyDelivery:      cfg.Chat.ReplyDelivery,
		Provider:           provider,
		ProviderID:         providerID,
		ProviderName:       providerName,
		Registry:           s.tools.Registry(),
		Dispatcher:         dispatcher,
		Pending:            s.work.Pending(),
		Approvals:          s.work.Approvals(),
		Environment:        s.infra.Environment,
		RealtimeStreaming:  cfg.Chat.RealtimeStreaming,
		Memory:             s.memory.Bridge(),
		MemoryRetrieval:    cfg.Memory.Retrieval,
		AgentAffect:        affectRuntime,
		UserAddress:        userAddress,
		MediaStore:         mediaStore,
		MediaResolver:      mediaResolver,
		AgentID:            agentID,
		PersonaKey:         personaKey,
		PromptSnapshots:    cfg.PromptCenter.Snapshots,
	})
}

func (s *ChatService) EnsureTurnStores() chat.TurnStores {
	if s == nil {
		return chat.TurnStores{}
	}
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	if s.turnReady {
		return s.turnStores
	}
	cfg := config.DefaultConfig()
	if s.infra != nil && s.infra.Config != nil {
		cfg = s.infra.Config
	}
	var db *sql.DB
	if s.infra != nil && s.infra.DB != nil {
		db = s.infra.DB.SqlDB()
	}
	var logger *slog.Logger
	if s.infra != nil {
		logger = s.infra.Logger
	}
	s.turnStores = chat.NewTurnStores(cfg.Chat.TurnPipeline, db, logger, cfg.Time.Timezone)
	if s.plugins != nil && s.plugins.Host() != nil {
		if setter, ok := any(s.plugins.Host()).(interface {
			SetTurnJournal(turn.TurnJournal)
		}); ok {
			setter.SetTurnJournal(s.turnStores.Journal)
		}
	}
	s.turnReady = true
	return s.turnStores
}

func (s *ChatService) HandlerOptions() []chat.HandlerOption {
	cfg := s.infra.Config
	options := []chat.HandlerOption{
		chat.WithTurnPipelineConfig(cfg.Chat.TurnPipeline),
		chat.WithRealtimeStreaming(cfg.Chat.RealtimeStreaming),
		chat.WithReplyDeliveryConfig(cfg.Chat.ReplyDelivery),
		chat.WithTurnTimezone(cfg.Time.Timezone),
	}
	if s.infra.DB != nil {
		options = append(options, chat.WithTurnDB(s.infra.DB.SqlDB()))
	}
	if s.conversation != nil {
		options = append(options, chat.WithConversationBindings(s.conversation.Bindings()))
		options = append(options, chat.WithRunRegistry(s.conversation.RunRegistry()))
	}
	if s.commands != nil {
		options = append(options, chat.WithCommandHandler(s.commands))
	}
	if s.plugins.Host() != nil && s.plugins.Host().Enabled() {
		options = append(options, chat.WithPluginHost(s.plugins.Host()))
	}
	if s.engine != nil {
		options = append(options, chat.WithTurnRunner(s.turnRunnerForEngine(s.engine)))
	}
	return options
}

func (s *ChatService) turnRunnerForEngine(engine *chat.Engine) *chat.TurnRunner {
	cfg := config.DefaultConfig()
	if s != nil && s.infra != nil && s.infra.Config != nil {
		cfg = s.infra.Config
	}
	var logger *slog.Logger
	if s != nil && s.infra != nil {
		logger = s.infra.Logger
	}
	var host chat.TurnPluginHost
	if s != nil && s.plugins != nil && s.plugins.Host() != nil && s.plugins.Host().Enabled() {
		host = s.plugins.Host()
	}
	return chat.NewTurnRunnerWithStores(engine, cfg.Chat.TurnPipeline, s.EnsureTurnStores(), logger, host)
}

type platformTurnSink struct {
	text     strings.Builder
	segments []string
	events   []platform.OutboundEvent

	origin     conversation.Origin
	sessionID  string
	personaKey string
	replyTo    string
	resultType string
}

func (s *platformTurnSink) Emit(_ context.Context, event turn.OutboundEvent) error {
	switch event.Type {
	case turn.EventStreamDelta:
		s.text.WriteString(event.Content)
	case turn.EventAssistantSegment:
		s.text.WriteString(event.Content)
		if strings.TrimSpace(event.Content) != "" {
			s.segments = append(s.segments, event.Content)
		}
	case turn.EventApprovalRequired:
		if event.Approval != nil && event.Approval.Request != nil {
			s.resultType = "approval_wait"
			s.events = append(s.events, s.messageEvent(platformApprovalRequiredMessage(event.Approval.Request)))
		}
	case turn.EventApprovalUpdated:
		if event.Approval != nil && event.Approval.Request != nil {
			s.events = append(s.events, s.messageEvent(platformApprovalUpdatedMessage(event.Approval.Request)))
		}
	case turn.EventTurnStatus:
		status := platformEventStatus(event)
		if status == "busy" || status == "previous_failed" {
			s.resultType = status
			s.events = append(s.events, s.messageEvent(platformTurnStatusMessage(status)))
		}
	case turn.EventError:
		s.resultType = "error"
		s.events = append(s.events, platform.OutboundEvent{
			Type:                     "error",
			Origin:                   s.origin,
			SessionID:                s.sessionID,
			PersonaKey:               s.personaKey,
			Content:                  firstNonEmptyCommandValue(event.Content, "平台回复失败。"),
			Status:                   "failed",
			ErrorKind:                platformEventErrorKind(event),
			ReplyToExternalMessageID: s.replyTo,
		})
	}
	return nil
}

func (s *platformTurnSink) Text() string {
	if s == nil {
		return ""
	}
	return s.text.String()
}

func (s *platformTurnSink) Segments() []string {
	if s == nil || len(s.segments) == 0 {
		return nil
	}
	return append([]string(nil), s.segments...)
}

func (s *platformTurnSink) Events() []platform.OutboundEvent {
	if s == nil || len(s.events) == 0 {
		return nil
	}
	return append([]platform.OutboundEvent(nil), s.events...)
}

func (s *platformTurnSink) ResultType() string {
	if s == nil {
		return ""
	}
	return s.resultType
}

func (s *platformTurnSink) messageEvent(content string) platform.OutboundEvent {
	return platform.OutboundEvent{
		Type:                     "message",
		Origin:                   s.origin,
		SessionID:                s.sessionID,
		PersonaKey:               s.personaKey,
		Content:                  content,
		ReplyToExternalMessageID: s.replyTo,
	}
}

func platformTurnEnvelope(in platform.InboundMessage, sessionID string, personaKey string) turn.InboundEnvelope {
	content := strings.TrimSpace(in.Text)
	sourceEventID := platformSourceEventID(in)
	env := turn.InboundEnvelope{
		Source:        turn.SourcePlatform,
		SourceEventID: sourceEventID,
		Kind:          turn.InboundUserMessage,
		SessionID:     strings.TrimSpace(sessionID),
		PersonaKey:    strings.TrimSpace(personaKey),
		RequestID:     strings.TrimSpace(in.RequestID),
		Content:       content,
		UserMessage: &turn.UserMessageInput{
			Content: content,
			Parts:   clonePlatformTurnParts(in.Parts),
		},
		RawMeta: map[string]any{
			"source_type":         strings.TrimSpace(in.SourceType),
			"adapter_instance_id": strings.TrimSpace(in.AdapterInstanceID),
			"platform_id":         strings.TrimSpace(in.PlatformID),
			"channel_type":        strings.TrimSpace(in.ChannelType),
			"inbound_id":          strings.TrimSpace(in.ID),
			"raw_event_hash":      strings.TrimSpace(in.RawEventHash),
		},
	}
	env.IdempotencyKey = turn.BuildIdempotencyKey(env)
	return env
}

func platformApprovalEnvelope(in platform.InboundMessage, sessionID string, personaKey string, approval turn.InboundApproval) turn.InboundEnvelope {
	env := turn.InboundEnvelope{
		Source:        turn.SourcePlatform,
		SourceEventID: platformSourceEventID(in),
		Kind:          turn.InboundApprovalAction,
		SessionID:     strings.TrimSpace(sessionID),
		PersonaKey:    strings.TrimSpace(personaKey),
		Approval: &turn.InboundApproval{
			RequestID: strings.TrimSpace(approval.RequestID),
			Action:    strings.TrimSpace(strings.ToLower(approval.Action)),
			OptionID:  strings.TrimSpace(approval.OptionID),
		},
		RawMeta: map[string]any{
			"source_type":         strings.TrimSpace(in.SourceType),
			"adapter_instance_id": strings.TrimSpace(in.AdapterInstanceID),
			"platform_id":         strings.TrimSpace(in.PlatformID),
			"channel_type":        strings.TrimSpace(in.ChannelType),
			"inbound_id":          strings.TrimSpace(in.ID),
			"raw_event_hash":      strings.TrimSpace(in.RawEventHash),
		},
	}
	env.IdempotencyKey = turn.BuildIdempotencyKey(env)
	return env
}

func platformSourceEventID(in platform.InboundMessage) string {
	sourceEventID := strings.TrimSpace(in.ExternalMessageID)
	if sourceEventID == "" {
		sourceEventID = strings.TrimSpace(in.ID)
	}
	if sourceEventID == "" {
		sourceEventID = strings.TrimSpace(in.RawEventHash)
	}
	return sourceEventID
}

func runtimePersonaKey(runtime *ActiveAgentRuntime) string {
	if runtime == nil {
		return ""
	}
	return runtime.PersonaKey
}

func platformApprovalRequiredMessage(req *protocol.ApprovalRequest) string {
	if req == nil {
		return "需要你的确认。"
	}
	subject := firstNonEmptyCommandValue(req.Question, req.GoalSummary, "需要执行一项操作")
	lines := []string{"需要你的确认：" + subject}
	if strings.TrimSpace(req.RiskLevel) != "" {
		lines = append(lines, "风险等级："+strings.TrimSpace(req.RiskLevel))
	}
	if len(req.Options) > 0 {
		lines = append(lines, "", "可选项：")
		for _, option := range req.Options {
			summary := strings.TrimSpace(option.Summary)
			if summary == "" {
				summary = option.ID
			}
			lines = append(lines, "- "+strings.TrimSpace(option.ID)+"："+summary)
		}
	}
	lines = append(lines, "", "可选操作：")
	if len(req.Options) > 0 {
		lines = append(lines, "/approve "+req.ID+" <option_id>")
	} else {
		lines = append(lines, "/approve "+req.ID)
	}
	lines = append(lines, "/reject "+req.ID, "/approvals")
	return strings.Join(lines, "\n")
}

func platformApprovalUpdatedMessage(req *protocol.ApprovalRequest) string {
	if req == nil {
		return "审批状态已更新。"
	}
	status := firstNonEmptyCommandValue(req.Status, "updated")
	return "审批状态已更新：" + req.ID + " -> " + status
}

func platformTurnStatusMessage(status string) string {
	switch status {
	case "busy":
		return platformBusyMessage()
	case "previous_failed":
		return "上一轮回复失败，未重复执行。请重新发送消息再试。"
	default:
		return "平台回复状态：" + status
	}
}

func platformEventStatus(event turn.OutboundEvent) string {
	if event.Payload != nil {
		if status, ok := event.Payload["status"].(string); ok {
			return strings.TrimSpace(status)
		}
	}
	return strings.TrimSpace(event.Content)
}

func platformEventErrorKind(event turn.OutboundEvent) string {
	if event.Payload != nil {
		if kind, ok := event.Payload["error_kind"].(string); ok && strings.TrimSpace(kind) != "" {
			return strings.TrimSpace(kind)
		}
	}
	return "turn_error"
}

func clonePlatformTurnParts(parts []llm.ContentBlock) []llm.ContentBlock {
	if len(parts) == 0 {
		return nil
	}
	cloned := make([]llm.ContentBlock, len(parts))
	for i, part := range parts {
		cloned[i] = part
		if part.Media != nil {
			mediaPart := *part.Media
			cloned[i].Media = &mediaPart
		}
	}
	return cloned
}

func (s *ChatService) UpdateRealtimeStreaming(enabled bool) {
	if s.engine != nil {
		s.engine.UpdateRealtimeStreaming(enabled)
	}
}

func (s *ChatService) UpdatePromptRouterConfig(cfg config.PromptRouterConfig) {
	if s.engine != nil {
		s.engine.UpdatePromptRouterConfig(cfg)
	}
}

func (s *ChatService) UpdateReplyDeliveryConfig(cfg config.ReplyDeliveryConfig) {
	if s.engine != nil {
		s.engine.UpdateReplyDeliveryConfig(cfg)
	}
}

func (s *ChatService) UpdateAgentAffect() {
	if s.engine != nil && s.agentAffect != nil {
		s.engine.UpdateAgentAffect(s.agentAffect.Runtime())
	}
}

func (s *ChatService) UpdateAgentRuntime(runtime *ActiveAgentRuntime) {
	if s.engine == nil || runtime == nil {
		return
	}
	s.engine.UpdateAgentRuntime(
		runtime.ID,
		runtime.PersonaKey,
		runtime.EmotionMain.Client,
		runtime.EmotionSummary.Client,
		toolProviderName(runtime.EmotionMain.Provider.Protocol),
		runtime.EmotionMain.Provider.ID,
		providerDisplayName(runtime.EmotionMain.Provider),
		runtime.EmotionMain.Model,
		runtime.EmotionMain.Params,
		runtime.EmotionSummary.Model,
		runtime.EmotionSummary.Params,
		runtime.Context,
		runtime.UserAddress,
	)
	s.UpdateAgentAffect()
}

func (s *ChatService) StartBackground(ctx context.Context) {
	s.memory.StartBackground(ctx)
	if s.agentAffect != nil {
		s.agentAffect.StartBackground(ctx)
	}
}
