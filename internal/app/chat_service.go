package app

import (
	"context"
	"fmt"

	"github.com/longyisang/emoagent/internal/chat"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/media"
	"github.com/longyisang/emoagent/internal/tool"
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

func (s *ChatService) SendPlatformMessage(ctx context.Context, runtime *ActiveAgentRuntime, sessionID string, persona *config.Persona, text string, cb func(string)) (string, error) {
	if s == nil {
		return "", fmt.Errorf("chat service is not configured")
	}
	if runtime == nil {
		return "", fmt.Errorf("platform agent runtime is not configured")
	}
	engine := s.newEngine(runtime, s.dispatcher)
	if engine == nil {
		return "", fmt.Errorf("chat engine is not configured")
	}
	runCtx := workctx.WithAgentID(ctx, runtime.ID)
	return engine.SendMessage(runCtx, sessionID, persona, text, cb)
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
	return options
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
