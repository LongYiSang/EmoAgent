package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent/internal/agentaffect"
	"github.com/longyisang/emoagent/internal/config"
	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/media"
	"github.com/longyisang/emoagent/internal/progress"
	"github.com/longyisang/emoagent/internal/promptcenter"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/runtimeenv"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/work"
)

var errApprovalPending = errors.New("approval pending")

const memoryFinalizeReasonSessionResume = "session_resume"

type MemorySegmentRef = storage.MemorySegmentRef

type MemoryBridge interface {
	EnsureSegment(ctx context.Context, chatSessionID string, personaID string) (MemorySegmentRef, error)
	RolloverSegment(ctx context.Context, chatSessionID string, personaID string, reason string) (MemorySegmentRef, error)
	AppendUserEpisode(ctx context.Context, segmentID string, messageID string, content string) (string, error)
	AppendAssistantEpisode(ctx context.Context, segmentID string, messageID string, content string) (string, error)
	RetrievePromptBlock(ctx context.Context, chatSessionID string, query string, excludedEpisodeIDs ...string) (string, error)
	RetrievePromptSnapshot(ctx context.Context, chatSessionID string, query string, includePipelineTrace bool, excludedEpisodeIDs ...string) (string, any, error)
	FinalizeSegment(ctx context.Context, segmentID string, reason string, summary string) error
}

type AgentAffectRuntime interface {
	UpdateMode() string
	GetCurrentMood(ctx context.Context, req agentaffect.GetCurrentMoodRequest) (agentaffect.GetCurrentMoodResponse, error)
	SubmitMoodImpact(ctx context.Context, req agentaffect.SubmitMoodImpactRequest) (agentaffect.SubmitMoodImpactResponse, error)
	EnqueueTurnEvaluationJob(ctx context.Context, req agentaffect.EnqueueTurnEvaluationJobRequest) (agentaffect.AffectJobRecord, error)
	BuildPromptAffectBlock(ctx context.Context, req agentaffect.BuildPromptAffectBlockRequest) (string, error)
}

type memoryPromptSnapshot struct {
	PromptBlock    string
	PipelineTrace  any
	RecordMetadata bool
}

type manualMemoryNoticeBridge interface {
	TakeManualMemoryNotice(chatSessionID string) (string, bool)
}

// EngineConfig defines the dependencies for Engine.
type EngineConfig struct {
	LLM                llm.Client
	SummaryLLM         llm.Client
	DB                 *storage.DB
	Logger             *slog.Logger
	Model              string
	Params             llm.RequestParams
	SummaryModel       string
	SummaryParams      llm.RequestParams
	SummaryTemperature *float64
	SummaryMaxTokens   int
	MaxTokens          int
	Temperature        float64
	ContextConfig      config.ContextConfig
	Provider           string           // "openai" or "anthropic", needed by ResultsToMessages
	ProviderID         string           // configured provider id for model capability lookup
	ProviderName       string           // display name for UI metadata
	Registry           *tool.Registry   // nil disables tool support
	Dispatcher         *tool.Dispatcher // nil disables tool support
	Pending            *work.PendingRegistry
	Approvals          *work.ApprovalService
	Environment        runtimeenv.Facts
	RealtimeStreaming  bool
	Memory             MemoryBridge
	MemoryRetrieval    config.MemoryRetrievalConfig
	AgentAffect        AgentAffectRuntime
	MediaStore         media.Store
	MediaResolver      media.CapabilityResolver
	AgentID            string
	PersonaKey         string
	PromptResolver     *promptcenter.Resolver
	PromptStore        promptcenter.Store
	PromptSnapshots    config.PromptSnapshotConfig
}

// RuntimeConfig is the hot-swappable subset of EngineConfig used for new requests.
type RuntimeConfig struct {
	AgentID            string
	PersonaKey         string
	Provider           string
	ProviderName       string
	Model              string
	Params             llm.RequestParams
	SummaryModel       string
	SummaryParams      llm.RequestParams
	SummaryTemperature *float64
	SummaryMaxTokens   int
	MaxTokens          int
	Temperature        float64
	ContextConfig      config.ContextConfig
	RealtimeStreaming  bool
}

// Engine assembles conversation context and forwards requests to the LLM.
type Engine struct {
	mu                   sync.RWMutex
	llm                  llm.Client
	summaryLLM           llm.Client
	db                   *storage.DB
	logger               *slog.Logger
	model                string
	params               llm.RequestParams
	summaryModel         string
	summaryParams        llm.RequestParams
	summaryTemperature   *float64
	summaryMaxTokens     int
	maxTokens            int
	temperature          float64
	contextCfg           config.ContextConfig
	provider             string
	providerID           string
	providerName         string
	registry             *tool.Registry
	dispatcher           *tool.Dispatcher
	pending              *work.PendingRegistry
	approvals            *work.ApprovalService
	environment          runtimeenv.Facts
	realtimeStreaming    bool
	memory               MemoryBridge
	memoryRetrieval      config.MemoryRetrievalConfig
	agentAffect          AgentAffectRuntime
	mediaStore           media.Store
	mediaPlanner         *media.Planner
	agentID              string
	personaKey           string
	promptResolver       *promptcenter.Resolver
	promptStore          promptcenter.Store
	promptSnapshotConfig config.PromptSnapshotConfig
}

// UpdateConfig hot-swaps the active LLM client and request parameters for new sends.
func (e *Engine) UpdateConfig(client llm.Client, provider, model, summaryModel string, summaryTemperature *float64, summaryMaxTokens int, maxTokens int, temperature float64, contextCfg config.ContextConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if client != nil {
		e.llm = client
		e.summaryLLM = client
	}
	e.provider = provider
	e.providerName = provider
	e.model = model
	e.params = requestParamsFromLegacy(maxTokens, temperature, true)
	e.summaryModel = summaryModel
	e.summaryTemperature = cloneFloat64Ptr(summaryTemperature)
	e.summaryMaxTokens = summaryMaxTokens
	e.summaryParams = summaryParamsFromLegacy(summaryMaxTokens, summaryTemperature)
	e.maxTokens = maxTokens
	e.temperature = temperature
	if err := contextCfg.Validate(); err == nil {
		e.contextCfg = contextCfg
	}
}

func (e *Engine) UpdateAgentRuntime(agentID, personaKey string, mainClient, summaryClient llm.Client, provider, providerID, providerName, model string, params llm.RequestParams, summaryModel string, summaryParams llm.RequestParams, contextCfg config.ContextConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.agentID = strings.TrimSpace(agentID)
	e.personaKey = strings.TrimSpace(personaKey)
	if mainClient != nil {
		e.llm = mainClient
	}
	if summaryClient != nil {
		e.summaryLLM = summaryClient
	}
	e.provider = provider
	e.providerID = firstNonEmptyString(providerID, provider)
	e.providerName = providerDisplayName(providerName, provider)
	e.model = model
	e.params = cloneRequestParams(params)
	e.summaryModel = summaryModel
	e.summaryParams = cloneRequestParams(summaryParams)
	if err := contextCfg.Validate(); err == nil {
		e.contextCfg = contextCfg
	}
}

func (e *Engine) UpdateAgentAffect(runtime AgentAffectRuntime) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.agentAffect = runtime
}

// UpdateRealtimeStreaming hot-swaps the browser streaming mode for new sends.
func (e *Engine) UpdateRealtimeStreaming(enabled bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.realtimeStreaming = enabled
}

// NewEngine creates a chat engine from configuration.
func NewEngine(cfg EngineConfig) *Engine {
	contextCfg := cfg.ContextConfig
	if err := contextCfg.Validate(); err != nil {
		contextCfg = config.DefaultConfig().Context
	}
	promptStore := cfg.PromptStore
	if promptStore == nil {
		promptStore = cfg.DB
	}
	promptResolver := cfg.PromptResolver
	if promptResolver == nil {
		promptResolver = newPromptResolver(promptStore, cfg.Logger)
	}

	return &Engine{
		llm:                  cfg.LLM,
		summaryLLM:           firstClient(cfg.SummaryLLM, cfg.LLM),
		db:                   cfg.DB,
		logger:               cfg.Logger,
		model:                cfg.Model,
		params:               effectiveConfigParams(cfg.Params, cfg.MaxTokens, cfg.Temperature, true),
		summaryModel:         cfg.SummaryModel,
		summaryParams:        effectiveSummaryConfigParams(cfg.SummaryParams, cfg.SummaryMaxTokens, cfg.SummaryTemperature),
		summaryTemperature:   cloneFloat64Ptr(cfg.SummaryTemperature),
		summaryMaxTokens:     cfg.SummaryMaxTokens,
		maxTokens:            cfg.MaxTokens,
		temperature:          cfg.Temperature,
		contextCfg:           contextCfg,
		provider:             cfg.Provider,
		providerID:           firstNonEmptyString(cfg.ProviderID, cfg.Provider),
		providerName:         providerDisplayName(cfg.ProviderName, cfg.Provider),
		registry:             cfg.Registry,
		dispatcher:           cfg.Dispatcher,
		pending:              cfg.Pending,
		approvals:            cfg.Approvals,
		environment:          cfg.Environment,
		realtimeStreaming:    cfg.RealtimeStreaming,
		memory:               cfg.Memory,
		memoryRetrieval:      cfg.MemoryRetrieval,
		agentAffect:          cfg.AgentAffect,
		mediaStore:           cfg.MediaStore,
		mediaPlanner:         media.NewPlanner(cfg.MediaStore, cfg.MediaResolver),
		agentID:              strings.TrimSpace(cfg.AgentID),
		personaKey:           strings.TrimSpace(cfg.PersonaKey),
		promptResolver:       promptResolver,
		promptStore:          promptStore,
		promptSnapshotConfig: normalizePromptSnapshotConfig(cfg.PromptSnapshots),
	}
}

func newPromptResolver(store promptcenter.Store, logger *slog.Logger) *promptcenter.Resolver {
	catalog, err := promptcenter.DefaultCatalog()
	if err != nil {
		return nil
	}
	return promptcenter.NewResolverWithWarning(catalog, store, func(warning promptcenter.ResolveWarning) {
		if logger != nil {
			logger.Warn("prompt resolver fallback", "component_id", warning.ComponentID, "code", warning.Code, "message", warning.Message)
		}
	})
}

func (e *Engine) savePromptRenderSnapshot(ctx context.Context, store promptcenter.Store, input promptcenter.RenderSnapshot) {
	if store == nil {
		return
	}
	cfg := normalizePromptSnapshotConfig(e.promptSnapshotConfig)
	if !cfg.Enabled {
		return
	}
	snapshot, err := promptcenter.BuildRenderSnapshot(input, promptcenter.SnapshotRenderOptions{
		StoreRenderedText:    cfg.StoreRenderedText,
		MaxRenderedTextChars: cfg.MaxRenderedTextChars,
	})
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("failed to build prompt render snapshot", "purpose", input.Purpose, "error", err)
		}
		return
	}
	if err := store.SaveRenderSnapshot(context.WithoutCancel(ctx), snapshot); err != nil && e.logger != nil {
		e.logger.Warn("failed to save prompt render snapshot", "purpose", snapshot.Purpose, "session", snapshot.SessionID, "agent_id", snapshot.AgentID, "error", err)
	}
}

func (e *Engine) saveSummaryPromptSnapshots(ctx context.Context, store promptcenter.Store, report contextutil.SummaryUpdateReport, sessionID, agentID, personaKey, model, turnID, requestID string) {
	for _, audit := range []*contextutil.SummaryPromptAudit{report.PromptAudit, report.RepairPromptAudit} {
		if audit == nil || !audit.Attempted || strings.TrimSpace(audit.SystemPrompt) == "" {
			continue
		}
		snapshotModel := firstNonEmptyString(audit.Model, model)
		e.savePromptRenderSnapshot(ctx, store, promptcenter.RenderSnapshot{
			ID:           uuid.NewString(),
			RequestID:    requestID,
			TurnID:       turnID,
			SessionID:    sessionID,
			AgentID:      agentID,
			PersonaKey:   personaKey,
			Purpose:      audit.Purpose,
			Model:        snapshotModel,
			Components:   audit.Components,
			RenderedText: audit.SystemPrompt,
		})
	}
}

func normalizePromptSnapshotConfig(cfg config.PromptSnapshotConfig) config.PromptSnapshotConfig {
	if cfg == (config.PromptSnapshotConfig{}) {
		return config.DefaultConfig().PromptCenter.Snapshots
	}
	return cfg
}

// RuntimeConfig returns a snapshot of the engine's active request configuration.
func (e *Engine) RuntimeConfig() RuntimeConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return RuntimeConfig{
		AgentID:            e.agentID,
		PersonaKey:         e.personaKey,
		Provider:           e.provider,
		ProviderName:       e.providerName,
		Model:              e.model,
		Params:             cloneRequestParams(e.params),
		SummaryModel:       e.summaryModel,
		SummaryParams:      cloneRequestParams(e.summaryParams),
		SummaryTemperature: cloneFloat64Ptr(e.summaryTemperature),
		SummaryMaxTokens:   e.summaryMaxTokens,
		MaxTokens:          e.maxTokens,
		Temperature:        e.temperature,
		ContextConfig:      e.contextCfg,
		RealtimeStreaming:  e.realtimeStreaming,
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
	if e.memory != nil {
		if _, err := e.memory.EnsureSegment(ctx, sessionID, personaName); err != nil {
			e.logMemoryWarning("ensure memory segment", sessionID, err)
		}
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
	if e.memory != nil {
		if _, err := e.memory.RolloverSegment(ctx, sessionID, personaKey, memoryFinalizeReasonSessionResume); err != nil {
			e.logMemoryWarning("rollover memory segment", sessionID, err)
		}
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
	return e.sendTurn(ctx, sessionID, persona, cb, turnOptions{
		persistUser: true,
		userContent: userContent,
	})
}

func (e *Engine) SendMessageParts(ctx context.Context, sessionID string, persona *config.Persona, parts []llm.ContentBlock, cb func(delta string)) (string, error) {
	normalizedParts, err := normalizeUserParts("", parts)
	if err != nil {
		return "", err
	}
	content := renderUserParts(normalizedParts, llm.RenderForHistory)
	return e.sendTurn(ctx, sessionID, persona, cb, turnOptions{
		persistUser: true,
		userContent: content,
		userParts:   normalizedParts,
		turnID:      uuid.NewString(),
	})
}

type turnOptions struct {
	persistUser           bool
	userContent           string
	userParts             []llm.ContentBlock
	turnID                string
	requestID             string
	extraSystem           string
	extraSystemComponents []promptcenter.RenderComponent
	disableTools          bool
	deferCommit           bool
	output                *deferredTurnOutput
	preparedAnchor        turnMemoryAnchor
	hasPreparedAnchor     bool
}

type turnMemoryAnchor struct {
	memorySegment       MemorySegmentRef
	hasMemorySegment    bool
	userEpisodeID       string
	userMessageID       string
	userHistoryContent  string
	userMemoryContent   string
	userParts           []llm.ContentBlock
	turnID              string
	manualNotice        string
	manualNoticeHandled bool
}

type deferredTurnOutput struct {
	assistantContent string
	thinkingBlocks   []thinkingBlockMetadata
	memorySnapshot   *memoryPromptSnapshot
	memorySegment    MemorySegmentRef
	hasMemorySegment bool
}

type thinkingBlockMetadata struct {
	ID         string `json:"id"`
	Content    string `json:"content"`
	DurationMS int64  `json:"duration_ms"`
	Provider   string `json:"provider,omitempty"`
	Model      string `json:"model,omitempty"`
	Kind       string `json:"kind"`
}

type reasoningRoundTracker struct {
	id         string
	provider   string
	model      string
	startedAt  time.Time
	writer     func(WSMessage)
	content    strings.Builder
	started    bool
	ended      bool
	recorded   bool
	durationMS int64
	sink       *[]thinkingBlockMetadata
}

func newReasoningRoundTracker(id, provider, model string, startedAt time.Time, writer func(WSMessage), sink *[]thinkingBlockMetadata) *reasoningRoundTracker {
	return &reasoningRoundTracker{
		id:        id,
		provider:  provider,
		model:     model,
		startedAt: startedAt,
		writer:    writer,
		sink:      sink,
	}
}

func (r *reasoningRoundTracker) delta(content string) {
	if content == "" || r.ended {
		return
	}
	r.start()
	r.content.WriteString(content)
	if r.writer != nil {
		r.writer(WSMessage{
			Type: "reasoning_delta",
			Reasoning: &ReasoningActivity{
				ID:       r.id,
				Status:   "running",
				Content:  content,
				Provider: r.provider,
				Model:    r.model,
				Kind:     "reasoning_content",
			},
		})
	}
}

func (r *reasoningRoundTracker) start() {
	if r.started {
		return
	}
	r.started = true
	if r.writer != nil {
		r.writer(WSMessage{
			Type: "reasoning_start",
			Reasoning: &ReasoningActivity{
				ID:       r.id,
				Status:   "running",
				Provider: r.provider,
				Model:    r.model,
				Kind:     "reasoning_content",
			},
		})
	}
}

func (r *reasoningRoundTracker) end() {
	if !r.started || r.ended {
		return
	}
	r.ended = true
	r.durationMS = time.Since(r.startedAt).Milliseconds()
	content := r.content.String()
	if r.writer != nil {
		r.writer(WSMessage{
			Type: "reasoning_end",
			Reasoning: &ReasoningActivity{
				ID:         r.id,
				Status:     "done",
				Content:    content,
				DurationMS: r.durationMS,
				Provider:   r.provider,
				Model:      r.model,
				Kind:       "reasoning_content",
			},
		})
	}
	r.record(content)
}

func (r *reasoningRoundTracker) complete(finalContent string) {
	if strings.TrimSpace(finalContent) == "" {
		r.end()
		return
	}
	if r.started {
		r.end()
		return
	}
	r.start()
	r.content.WriteString(finalContent)
	if r.writer != nil {
		r.writer(WSMessage{
			Type: "reasoning_delta",
			Reasoning: &ReasoningActivity{
				ID:       r.id,
				Status:   "running",
				Content:  finalContent,
				Provider: r.provider,
				Model:    r.model,
				Kind:     "reasoning_content",
			},
		})
	}
	r.end()
}

func (r *reasoningRoundTracker) record(content string) {
	if r.recorded || strings.TrimSpace(content) == "" || r.sink == nil {
		return
	}
	r.recorded = true
	*r.sink = append(*r.sink, thinkingBlockMetadata{
		ID:         r.id,
		Content:    content,
		DurationMS: r.durationMS,
		Provider:   r.provider,
		Model:      r.model,
		Kind:       "reasoning_content",
	})
}

func (e *Engine) sendTurn(ctx context.Context, sessionID string, persona *config.Persona, cb func(delta string), opts turnOptions) (reply string, err error) {
	e.mu.RLock()
	client := e.llm
	summaryClient := e.summaryLLM
	model := e.model
	params := cloneRequestParams(e.params)
	summaryModel := e.summaryModel
	summaryParams := cloneRequestParams(e.summaryParams)
	summaryTemperature := cloneFloat64Ptr(e.summaryTemperature)
	summaryMaxTokens := e.summaryMaxTokens
	maxTokens := e.maxTokens
	temperature := e.temperature
	contextCfg := e.contextCfg
	provider := e.provider
	providerID := e.providerID
	providerName := providerDisplayName(e.providerName, provider)
	registry := e.registry
	dispatcher := e.dispatcher
	pending := e.pending
	approvals := e.approvals
	env := e.environment
	realtimeStreaming := e.realtimeStreaming
	memoryRetrieval := e.memoryRetrieval
	mediaPlanner := e.mediaPlanner
	agentID := e.agentID
	personaKey := e.personaKey
	promptResolver := e.promptResolver
	promptStore := e.promptStore
	e.mu.RUnlock()

	var mediaDeliveries []media.DeliveryRecord
	mediaDeliveryStatus := ""
	mediaDeliveryError := ""
	defer func() {
		if len(mediaDeliveries) == 0 {
			return
		}
		status := mediaDeliveryStatus
		errText := mediaDeliveryError
		if status == "" {
			if err != nil {
				status = media.DeliveryStatusFailed
				errText = err.Error()
			} else {
				status = media.DeliveryStatusSent
			}
		}
		e.recordMediaDeliveries(context.WithoutCancel(ctx), mediaDeliveries, status, errText)
	}()

	if client == nil {
		return "", errors.New("chat engine LLM client is not configured")
	}
	if summaryClient == nil {
		summaryClient = client
	}
	if e.db == nil {
		return "", errors.New("chat engine database is not configured")
	}
	if persona == nil {
		return "", errors.New("persona is required")
	}
	personaKey = firstNonEmptyString(personaKey, persona.Name)
	requestID := strings.TrimSpace(opts.requestID)
	if requestID == "" {
		requestID = uuid.NewString()
	}

	memoryAnchor, err := e.prepareInputAndMemoryAnchor(ctx, sessionID, opts)
	if err != nil {
		return "", err
	}
	if !opts.persistUser && opts.hasPreparedAnchor {
		memoryAnchor = opts.preparedAnchor
	}
	if !opts.persistUser && len(opts.userParts) > 0 {
		parts, err := normalizeUserParts(opts.userContent, opts.userParts)
		if err != nil {
			return "", err
		}
		turnID := opts.turnID
		if turnID == "" {
			turnID = memoryAnchor.turnID
		}
		if turnID == "" {
			turnID = uuid.NewString()
		}
		memoryAnchor.userHistoryContent = renderUserParts(parts, llm.RenderForHistory)
		memoryAnchor.userMemoryContent = renderUserParts(parts, llm.RenderForMemory)
		memoryAnchor.userParts = parts
		memoryAnchor.turnID = turnID
	}
	if memoryAnchor.manualNoticeHandled {
		return memoryAnchor.manualNotice, nil
	}
	memorySegment := memoryAnchor.memorySegment
	hasMemorySegment := memoryAnchor.hasMemorySegment
	userEpisodeID := memoryAnchor.userEpisodeID

	history, err := e.db.GetAllMessages(ctx, sessionID)
	if err != nil {
		e.logger.Error("failed to load message history", "session", sessionID, "error", err)
		return "", err
	}

	state, err := contextutil.LoadSessionState(ctx, e.db, sessionID, contextCfg)
	if err != nil {
		e.logger.Warn("failed to load session context state", "session", sessionID, "error", err)
		defaultState := contextutil.ContextState{
			ContextVersion:      contextutil.CurrentContextVersion,
			Mode:                contextutil.ModeEmotion,
			KeepRecentUserTurns: contextCfg.KeepRecentUserTurns,
		}
		state = &defaultState
	}

	if !hasRequestParams(summaryParams) {
		summaryParams = summaryParamsFromLegacy(summaryMaxTokens, summaryTemperature)
	}
	summaryCtx, cancelSummary := context.WithTimeout(ctx, 8*time.Second)
	promptScope := promptcenter.PromptScope{AgentID: agentID, PersonaKey: personaKey}
	summaryRequestModel := effectiveSummaryModel(model, summaryModel)
	nextState, report, updateErr := contextutil.UpdateRunningSummaryWithParamsAndPromptResolver(summaryCtx, summaryClient, summaryRequestModel, summaryParams, persona, history, state, contextCfg, promptResolver, promptScope)
	cancelSummary()
	if updateErr != nil {
		if nextState != nil {
			state = nextState
		}
		logSummaryUpdate(e.logger, slog.LevelWarn, sessionID, report, updateErr)
	} else {
		state = nextState
		if report.Attempted {
			logSummaryUpdate(e.logger, slog.LevelInfo, sessionID, report, nil)
		} else if report.Skipped && report.SkipReason == "summary_retry_cooldown" {
			logSummaryUpdate(e.logger, slog.LevelDebug, sessionID, report, nil)
		}
	}
	e.saveSummaryPromptSnapshots(ctx, promptStore, report, sessionID, agentID, personaKey, summaryRequestModel, memoryAnchor.turnID, requestID)

	var pendingDecisions []protocol.DecisionSummary
	if pending != nil {
		pendingDecisions = append(pendingDecisions, pending.ListInjectable(sessionID)...)
	}

	var assembled contextutil.AssembledContext
	if len(pendingDecisions) > 0 {
		assembled, err = contextutil.BuildEmotionContextWithPendingSummariesAndPromptResolver(ctx, persona, history, state, pendingDecisions, contextCfg, env, promptResolver, promptScope)
	} else {
		assembled, err = contextutil.BuildEmotionContextWithStateAndPromptResolver(ctx, persona, history, state, contextCfg, env, promptResolver, promptScope)
	}
	if err != nil {
		e.logger.Error("failed to assemble llm context", "session", sessionID, "error", err)
		return "", err
	}
	if opts.extraSystem != "" {
		assembled.System += "\n\n" + opts.extraSystem
		if len(opts.extraSystemComponents) > 0 {
			assembled.PromptComponents = append(assembled.PromptComponents, opts.extraSystemComponents...)
		} else {
			assembled.PromptComponents = append(assembled.PromptComponents, promptcenter.DynamicComponent(promptcenter.ComponentTurnExtraSystem, "extra_system", promptcenter.SourceExtraSystemDynamic, opts.extraSystem, nil))
		}
	}
	var memorySnapshot *memoryPromptSnapshot
	if opts.persistUser {
		query := firstNonEmptyString(memoryAnchor.userMemoryContent, opts.userContent)
		memorySnapshot, err = e.retrieveMemoryPrompt(ctx, sessionID, query, userEpisodeID, memoryRetrieval)
		if err != nil {
			return "", err
		}
	}
	if memorySnapshot != nil && memorySnapshot.PromptBlock != "" {
		assembled.System += "\n\n" + memorySnapshot.PromptBlock
		assembled.PromptComponents = append(assembled.PromptComponents, memoryPromptRenderComponent(memorySnapshot))
		assembled.Budget = contextutil.NewBudget(contextCfg, assembled.System, assembled.Messages)
		assembled.CompactReport.PreEstimatedTokens = assembled.Budget.EstimatedTokens
		assembled.CompactReport.PostEstimatedTokens = assembled.Budget.EstimatedTokens
	}
	if state != nil {
		state.ContextVersion = contextutil.CurrentContextVersion
		state.Mode = contextutil.ModeEmotion
		state.LastInputEstimate = assembled.Budget.EstimatedTokens
		state.KeepRecentUserTurns = contextCfg.KeepRecentUserTurns
		if err := contextutil.UpdateSessionContextState(ctx, e.db, sessionID, *state); err != nil {
			e.logger.Warn("failed to persist session context state", "session", sessionID, "error", err)
		}
	}
	messages := append([]llm.Message(nil), assembled.Messages...)
	messages = e.hydrateStoredMessageParts(ctx, sessionID, messages)
	if len(memoryAnchor.userParts) > 0 {
		messages = replaceLastUserMessageWithCurrentParts(messages, memoryAnchor)
	}
	if mediaPlanner != nil && messagesContainMedia(messages) {
		prepared, err := mediaPlanner.Prepare(ctx, media.PrepareRequest{
			ProviderID:    providerID,
			ModelID:       model,
			Messages:      messages,
			CurrentTurnID: memoryAnchor.turnID,
			Policy:        media.DefaultPolicy(),
		})
		if err != nil {
			return "", err
		}
		messages = prepared.Messages
		mediaDeliveries = prepared.Deliveries
	}
	mediaDeliveries = appendMissingMediaDeliveries(mediaDeliveries, e.historicalPlaceholderDeliveries(ctx, sessionID, messages, memoryAnchor.userMessageID, memoryAnchor.turnID, providerID, model))

	// maxToolRounds prevents infinite tool call loops.
	const maxToolRounds = 10

	// Populate available tools only when the execution pipeline is enabled.
	var tools []llm.ToolDef
	if !opts.disableTools && registry != nil && dispatcher != nil {
		tools = registry.ForScope(tool.ScopeEmotion)
	}

	req := llm.ChatRequest{
		Model:       model,
		Messages:    messages,
		System:      assembled.System,
		Params:      effectiveConfigParams(params, maxTokens, temperature, true),
		MaxTokens:   maxTokens,
		Temperature: temperature,
		Stream:      true,
		Tools:       tools,
	}
	if req.Params.MaxTokens > 0 {
		req.MaxTokens = req.Params.MaxTokens
	}
	if req.Params.Temperature != nil {
		req.Temperature = *req.Params.Temperature
	}
	e.savePromptRenderSnapshot(ctx, promptStore, promptcenter.RenderSnapshot{
		ID:           uuid.NewString(),
		RequestID:    requestID,
		TurnID:       memoryAnchor.turnID,
		SessionID:    sessionID,
		AgentID:      agentID,
		PersonaKey:   personaKey,
		Purpose:      "emotion_chat",
		Model:        model,
		Components:   assembled.PromptComponents,
		RenderedText: req.System,
	})
	e.logger.Info("llm request",
		"session", sessionID,
		"persona", persona.Name,
		"model", model,
		"history_len", len(messages),
		"estimated_tokens", assembled.Budget.EstimatedTokens,
		"tools_count", len(tools),
	)
	e.logger.Debug("llm context",
		"system", req.System,
		"messages", llm.RenderMessages(messages, llm.RenderForHistory, llm.RenderPolicy{}),
	)

	start := time.Now()
	var resp *llm.ChatResponse
	reactiveRetryUsed := false
	var reactiveRetryReport *contextutil.CompactReport
	ctx = work.WithSessionID(ctx, sessionID)
	var approvalSnapshot map[string]string
	rawWriter := wsWriterFromContext(ctx)
	if rawWriter != nil && approvals != nil {
		approvalSnapshot = snapshotApprovalStatuses(approvals.ListSessionApprovals(sessionID, nil))
	}
	if rawWriter != nil {
		throttler := progress.NewThrottler(3 * time.Second)
		personaPhrases := persona.WorkProgressPhrases
		ctx = progress.WithCallback(ctx, func(event progress.Event) {
			if event.Kind == progress.KindEnd {
				rawWriter(WSMessage{Type: "work_progress_end"})
				return
			}
			if !throttler.ShouldEmit(event) {
				return
			}
			phrase := progress.Resolve(event, personaPhrases)
			if phrase == "" {
				return
			}
			rawWriter(WSMessage{Type: "work_progress", Content: phrase})
		})
	}
	emitToolEvents := rawWriter != nil && (realtimeStreaming || forcedOutboundEventsFromContext(ctx))

	var assistantContent string
	var visibleBuilder strings.Builder
	var thinkingBlocks []thinkingBlockMetadata
	for round := 0; ; round++ {
		var roundDeltas []string
		roundStarted := time.Now()
		reasoning := newReasoningRoundTracker("reasoning-"+uuid.NewString(), providerName, model, roundStarted, rawWriter, &thinkingBlocks)
		resp, err = client.ChatStream(ctx, req, func(event llm.StreamEvent) {
			if event.ReasoningContent != "" {
				reasoning.delta(event.ReasoningContent)
			}
			if event.Content != "" {
				reasoning.end()
				roundDeltas = append(roundDeltas, event.Content)
				if realtimeStreaming {
					visibleBuilder.WriteString(event.Content)
					if cb != nil {
						cb(event.Content)
					}
				}
			}
			if event.ContentBlock != nil || event.Type == "tool_use" {
				reasoning.end()
			}
		})
		if err != nil {
			if !reactiveRetryUsed && llm.IsKind(err, llm.ErrorKindContextOverflow) {
				compacted, report, compactErr := contextutil.ApplyReactiveCompact(sessionID, req.Messages, state, effectiveSummaryModel(model, summaryModel), contextCfg)
				if compactErr != nil {
					e.logger.Warn("reactive compact failed",
						"session_id", sessionID,
						"mode", "reactive",
						"compact_reason", "reactive_overflow",
						"retry_attempt", 1,
						"error_kind", llm.ErrorKindContextOverflow,
						"error", compactErr,
					)
					return "", err
				}
				report.SessionID = sessionID
				logCompactReport(e.logger, slog.LevelInfo, report, 1, llm.ErrorKindContextOverflow, "")
				reportCopy := report
				reactiveRetryReport = &reportCopy
				messages = append([]llm.Message(nil), compacted...)
				req.Messages = messages
				reactiveRetryUsed = true
				round--
				continue
			}
			if reactiveRetryUsed && reactiveRetryReport != nil {
				logCompactFailure(e.logger, *reactiveRetryReport, 1, errorKindOf(err), err)
				return "", err
			}
			e.logger.Error("llm request failed", "session", sessionID, "round", round, "error", err)
			return "", err
		}
		reasoning.complete(resp.ReasoningContent)

		if resp.StopReason != "tool_use" {
			if realtimeStreaming {
				if visibleBuilder.Len() == 0 && resp.Content != "" {
					visibleBuilder.WriteString(resp.Content)
					if cb != nil {
						cb(resp.Content)
					}
				}
				assistantContent = visibleBuilder.String()
				if assistantContent == "" {
					assistantContent = resp.Content
				}
			} else if cb != nil {
				for _, delta := range roundDeltas {
					cb(delta)
				}
				assistantContent = resp.Content
			} else {
				assistantContent = resp.Content
			}
			break
		}

		if dispatcher == nil {
			e.logger.Error("llm requested tool_use but tool execution is disabled", "session", sessionID, "round", round)
			return "", errors.New("tool_use requested but tool execution is not enabled")
		}

		// Safety: prevent runaway tool loops.
		if round >= maxToolRounds {
			e.logger.Error("tool loop exceeded max rounds", "session", sessionID, "max_rounds", maxToolRounds)
			return "", fmt.Errorf("tool loop exceeded maximum rounds (%d)", maxToolRounds)
		}

		// --- Tool loop: execute called tools and continue. ---
		e.logger.Info("tool_use detected", "session", sessionID, "round", round)

		// 1. Append assistant message (with tool_use ContentBlocks) to in-memory
		//    context. These intermediate messages are NOT persisted to DB.
		assistantText := resp.Content
		if assistantText == "" && len(roundDeltas) > 0 {
			assistantText = strings.Join(roundDeltas, "")
		}
		assistantMsg := llm.Message{
			Role:             llm.RoleAssistant,
			Content:          assistantText,
			ContentBlocks:    resp.ContentBlocks,
			ReasoningContent: resp.ReasoningContent,
		}
		messages = append(messages, assistantMsg)

		// 2. Extract and execute tool calls.
		calls := tool.ExtractToolCalls(resp)
		if len(calls) == 0 {
			e.logger.Warn("tool_use stop reason but no tool calls extracted", "session", sessionID)
			if realtimeStreaming {
				assistantContent = visibleBuilder.String()
				if assistantContent == "" {
					assistantContent = resp.Content
				}
			} else {
				assistantContent = resp.Content
			}
			break
		}
		for _, c := range calls {
			e.logger.Info("tool call", "session", sessionID, "tool", c.Name, "call_id", c.ID)
		}

		snippedResults := make([]tool.Result, len(calls))
		for i, call := range calls {
			if emitToolEvents {
				rawWriter(WSMessage{
					Type: "tool_call_start",
					Tool: &ToolActivity{
						ID:     call.ID,
						Name:   call.Name,
						Status: "running",
					},
				})
			}
			toolStarted := time.Now()
			result := dispatcher.Execute(ctx, call, tool.PermReadOnly)
			digest := contextutil.SnipToolResult(
				call.Name,
				result.CallID,
				result.Content,
				contextCfg.ToolResultSoftTokens,
				contextCfg.ToolResultHardTokens,
			)
			if emitToolEvents {
				status := "success"
				if result.NeedsApproval {
					status = "approval_required"
				} else if result.IsError {
					status = "error"
				}
				rawWriter(WSMessage{
					Type: "tool_call_end",
					Tool: &ToolActivity{
						ID:          result.CallID,
						Name:        call.Name,
						Status:      status,
						DurationMS:  time.Since(toolStarted).Milliseconds(),
						Preview:     digest.Preview,
						Size:        digest.Size,
						Hash:        digest.Hash,
						IsTruncated: digest.IsTruncated,
					},
				})
			}
			snippedResults[i] = tool.Result{
				CallID:  result.CallID,
				Content: json.RawMessage(contextutil.ToolResultContent(digest)),
				IsError: result.IsError,
			}
		}

		// 3. Convert results to provider-specific messages and append.
		toolMsgs := tool.ResultsToMessages(provider, snippedResults)
		messages = append(messages, toolMsgs...)
		if rawWriter != nil && approvals != nil {
			var interrupted bool
			approvalSnapshot, interrupted = emitApprovalDiff(rawWriter, approvalSnapshot, approvals.ListSessionApprovals(sessionID, nil))
			if interrupted {
				e.logger.Info("approval required; interrupting current turn", "session", sessionID, "round", round)
				// By design: pre-tool narration streamed before an approval interrupt is intentionally
				// not persisted. The turn is incomplete; the full reply will be saved after approval resumes.
				return "", errApprovalPending
			}
		}

		// Rebuild request for next round.
		req.Messages = messages
	}
	if len(mediaDeliveries) > 0 {
		mediaDeliveryStatus = media.DeliveryStatusSent
	}

	e.logger.Info("llm response",
		"session", sessionID,
		"duration_ms", time.Since(start).Milliseconds(),
		"response_len", len(assistantContent),
		"response_content", assistantContent,
	)

	output := deferredTurnOutput{
		assistantContent: assistantContent,
		thinkingBlocks:   thinkingBlocks,
		memorySnapshot:   memorySnapshot,
		memorySegment:    memorySegment,
		hasMemorySegment: hasMemorySegment,
	}
	if opts.deferCommit {
		if opts.output != nil {
			*opts.output = output
		}
		return assistantContent, nil
	}
	if err := e.commitTurnOutput(ctx, sessionID, output.assistantContent, output.thinkingBlocks, output.memorySnapshot, output.memorySegment, output.hasMemorySegment); err != nil {
		return "", err
	}

	return assistantContent, nil
}

func (e *Engine) ListSessionApprovals(ctx context.Context, sessionID string) ([]protocol.ApprovalRequest, error) {
	e.mu.RLock()
	approvals := e.approvals
	e.mu.RUnlock()
	if approvals == nil {
		return []protocol.ApprovalRequest{}, nil
	}
	return approvals.ListSessionApprovals(sessionID, nil), nil
}

func (e *Engine) ApplyApprovalAction(ctx context.Context, sessionID, requestID, action, optionID string) (*protocol.ApprovalRequest, error) {
	e.mu.RLock()
	approvals := e.approvals
	e.mu.RUnlock()
	if approvals == nil {
		return nil, errors.New("approval service is not configured")
	}
	switch action {
	case "approve":
		if strings.TrimSpace(optionID) == "" {
			return nil, errors.New("option_id is required for approve")
		}
		return approvals.ApproveRequest(sessionID, requestID, optionID, "web", "")
	case "reject":
		return approvals.RejectRequest(sessionID, requestID, "web", "")
	default:
		return nil, fmt.Errorf("unsupported approval action %q", action)
	}
}

func (e *Engine) ContinueAfterApproval(ctx context.Context, sessionID string, persona *config.Persona, approval *protocol.ApprovalRequest, cb func(delta string)) (string, error) {
	if approval == nil {
		return "", errors.New("approval is required")
	}
	if note, handled, terminal, err := e.resumeApprovalDirectly(ctx, sessionID, approval); err != nil {
		return "", err
	} else if handled {
		return e.sendTurn(ctx, sessionID, persona, cb, turnOptions{
			persistUser:  false,
			extraSystem:  note,
			disableTools: terminal,
		})
	}
	note := buildApprovalContinuationNote(approval)
	return e.sendTurn(ctx, sessionID, persona, cb, turnOptions{
		persistUser: false,
		extraSystem: note,
	})
}

func (e *Engine) resumeApprovalDirectly(ctx context.Context, sessionID string, approval *protocol.ApprovalRequest) (string, bool, bool, error) {
	if approval == nil || strings.TrimSpace(sessionID) == "" {
		return "", false, false, nil
	}
	if approval.ID == "" || approval.TaskID == "" {
		return "", false, false, nil
	}

	e.mu.RLock()
	registry := e.registry
	dispatcher := e.dispatcher
	e.mu.RUnlock()
	if registry == nil || dispatcher == nil {
		return "", false, false, nil
	}
	if _, ok := registry.GetSpec("resume_work"); !ok {
		return "", false, false, nil
	}

	input, err := json.Marshal(map[string]string{
		"task_id":             approval.TaskID,
		"approval_request_id": approval.ID,
	})
	if err != nil {
		return "", false, false, err
	}

	resumeCtx := work.WithSessionID(ctx, sessionID)
	result := dispatcher.Execute(resumeCtx, tool.Call{
		ID:    "internal_resume_approval",
		Name:  "resume_work",
		Input: input,
	}, tool.PermReadOnly)
	if result.NeedsApproval {
		return "", false, false, fmt.Errorf("resume_work unexpectedly requested approval for %s", approval.ID)
	}
	if result.IsError {
		return "", false, false, decodeToolError(result.Content)
	}

	return buildApprovalOutcomeNote(approval, result.Content), true, isTerminalTaskReport(result.Content), nil
}

func effectiveSummaryModel(model, summaryModel string) string {
	if summaryModel != "" {
		return summaryModel
	}
	return model
}

func firstClient(primary, fallback llm.Client) llm.Client {
	if primary != nil {
		return primary
	}
	return fallback
}

func effectiveConfigParams(params llm.RequestParams, maxTokens int, temperature float64, stream bool) llm.RequestParams {
	if hasRequestParams(params) {
		return cloneRequestParams(params)
	}
	return requestParamsFromLegacy(maxTokens, temperature, stream)
}

func effectiveSummaryConfigParams(params llm.RequestParams, maxTokens int, temperature *float64) llm.RequestParams {
	if hasRequestParams(params) {
		return cloneRequestParams(params)
	}
	return summaryParamsFromLegacy(maxTokens, temperature)
}

func requestParamsFromLegacy(maxTokens int, temperature float64, stream bool) llm.RequestParams {
	return llm.RequestParams{
		MaxTokens:   maxTokens,
		Temperature: &temperature,
		Stream:      &stream,
	}
}

func summaryParamsFromLegacy(maxTokens int, temperature *float64) llm.RequestParams {
	params := llm.RequestParams{MaxTokens: maxTokens}
	if temperature != nil {
		params.Temperature = cloneFloat64Ptr(temperature)
	}
	stream := false
	params.Stream = &stream
	return params
}

func hasRequestParams(params llm.RequestParams) bool {
	return params.MaxTokens != 0 ||
		params.Temperature != nil ||
		params.TopP != nil ||
		params.PresencePenalty != nil ||
		params.FrequencyPenalty != nil ||
		params.ReasoningEffort != "" ||
		params.Thinking != nil ||
		params.Stream != nil ||
		len(params.Extra) > 0
}

func cloneRequestParams(params llm.RequestParams) llm.RequestParams {
	cp := params
	cp.Temperature = cloneFloat64Ptr(params.Temperature)
	cp.TopP = cloneFloat64Ptr(params.TopP)
	cp.PresencePenalty = cloneFloat64Ptr(params.PresencePenalty)
	cp.FrequencyPenalty = cloneFloat64Ptr(params.FrequencyPenalty)
	cp.Stream = cloneBoolPtr(params.Stream)
	if params.Thinking != nil {
		thinking := *params.Thinking
		if params.Thinking.BudgetTokens != nil {
			budget := *params.Thinking.BudgetTokens
			thinking.BudgetTokens = &budget
		}
		cp.Thinking = &thinking
	}
	if params.Extra != nil {
		cp.Extra = make(map[string]any, len(params.Extra))
		for key, value := range params.Extra {
			cp.Extra[key] = value
		}
	}
	return cp
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func cloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func providerDisplayName(name, fallback string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	return fallback
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeUserParts(content string, parts []llm.ContentBlock) ([]llm.ContentBlock, error) {
	if len(parts) > 0 {
		normalized := make([]llm.ContentBlock, 0, len(parts))
		for i, part := range parts {
			partType := strings.TrimSpace(part.Type)
			if partType == "" {
				if part.Media != nil {
					partType = string(llm.PartImage)
				} else {
					partType = string(llm.PartText)
				}
			}
			id := strings.TrimSpace(part.ID)
			if id == "" {
				id = uuid.NewString()
			}
			switch llm.ContentPartType(partType) {
			case llm.PartText:
				if part.Media != nil || part.Name != "" || len(part.Input) > 0 || part.Content != "" || part.IsError {
					return nil, fmt.Errorf("unsupported payload fields on user text part %d", i)
				}
				normalized = append(normalized, llm.ContentBlock{
					ID:   id,
					Type: string(llm.PartText),
					Text: part.Text,
				})
			case llm.PartImage:
				if part.Media == nil {
					return nil, fmt.Errorf("image part %d is missing media", i)
				}
				if part.Text != "" || part.Name != "" || len(part.Input) > 0 || part.Content != "" || part.IsError {
					return nil, fmt.Errorf("unsupported payload fields on user image part %d", i)
				}
				if part.Media.AltText != "" || part.Media.Transport != "" || len(part.Media.Data) > 0 || part.Media.StorageURI != "" || part.Media.ProviderRef != "" {
					return nil, fmt.Errorf("unsupported media fields on user image part %d", i)
				}
				mediaPart := llm.MediaPart{
					MediaAssetID: strings.TrimSpace(part.Media.MediaAssetID),
					Kind:         "image",
					MimeType:     strings.TrimSpace(part.Media.MimeType),
					Detail:       strings.TrimSpace(part.Media.Detail),
				}
				if mediaPart.MediaAssetID == "" {
					return nil, fmt.Errorf("image part %d is missing media_asset_id", i)
				}
				if part.Media.Kind != "" && part.Media.Kind != "image" {
					return nil, fmt.Errorf("image part %d has unsupported media kind %q", i, part.Media.Kind)
				}
				normalized = append(normalized, llm.ContentBlock{
					ID:    id,
					Type:  string(llm.PartImage),
					Media: &mediaPart,
				})
			default:
				return nil, fmt.Errorf("unsupported user content part type %q", partType)
			}
		}
		return normalized, nil
	}
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}
	return []llm.ContentBlock{{ID: uuid.NewString(), Type: string(llm.PartText), Text: content}}, nil
}

func renderUserParts(parts []llm.ContentBlock, mode llm.RenderMode) string {
	return llm.RenderMessage(llm.Message{Role: llm.RoleUser, ContentBlocks: parts}, mode, llm.RenderPolicy{}).Content
}

func storagePartsFromLLM(sessionID, messageID, role string, parts []llm.ContentBlock) []storage.MessagePartRecord {
	records := make([]storage.MessagePartRecord, 0, len(parts))
	for i, part := range parts {
		partID := part.ID
		if partID == "" {
			partID = uuid.NewString()
		}
		record := storage.MessagePartRecord{
			ID:                  partID,
			SessionID:           sessionID,
			MessageID:           messageID,
			Role:                role,
			Ordinal:             i,
			PartType:            part.Type,
			MemoryRenderPolicy:  "placeholder_only",
			HistoryRenderPolicy: "placeholder_only",
		}
		if part.Type == string(llm.PartText) {
			record.TextContent = part.Text
		}
		if part.Media != nil {
			record.MediaAssetID = part.Media.MediaAssetID
		}
		records = append(records, record)
	}
	return records
}

func (e *Engine) hydrateStoredMessageParts(ctx context.Context, sessionID string, messages []llm.Message) []llm.Message {
	if e.db == nil || len(messages) == 0 {
		return messages
	}
	partsByMessage, err := e.db.GetMessagePartsForSession(ctx, sessionID)
	if err != nil {
		e.logger.Warn("failed to hydrate message parts", "session", sessionID, "error", err)
		return messages
	}
	if len(partsByMessage) == 0 {
		return messages
	}
	type hydratedParts struct {
		messageID string
		blocks    []llm.ContentBlock
	}
	partsByContent := map[string]hydratedParts{}
	contentCounts := map[string]int{}
	for messageID, parts := range partsByMessage {
		blocks := contentBlocksFromStorageParts(parts)
		if len(blocks) == 0 {
			continue
		}
		rendered := llm.RenderMessage(llm.Message{ContentBlocks: blocks}, llm.RenderForHistory, llm.RenderPolicy{}).Content
		if strings.TrimSpace(rendered) == "" {
			continue
		}
		contentCounts[rendered]++
		partsByContent[rendered] = hydratedParts{messageID: messageID, blocks: blocks}
	}
	next := append([]llm.Message(nil), messages...)
	for i, msg := range next {
		var blocks []llm.ContentBlock
		if msg.ID == "" {
			if contentCounts[msg.Content] == 1 {
				match := partsByContent[msg.Content]
				next[i].ID = match.messageID
				blocks = match.blocks
			}
		} else {
			blocks = contentBlocksFromStorageParts(partsByMessage[msg.ID])
			if len(blocks) == 0 && contentCounts[msg.Content] == 1 {
				match := partsByContent[msg.Content]
				next[i].ID = match.messageID
				blocks = match.blocks
			}
		}
		if len(blocks) == 0 {
			continue
		}
		next[i].ContentBlocks = blocks
		next[i].Content = llm.RenderMessage(next[i], llm.RenderForHistory, llm.RenderPolicy{}).Content
	}
	return next
}

func contentBlocksFromStorageParts(parts []storage.MessagePartRecord) []llm.ContentBlock {
	blocks := make([]llm.ContentBlock, 0, len(parts))
	for _, part := range parts {
		block := llm.ContentBlock{
			ID:   part.ID,
			Type: part.PartType,
		}
		switch llm.ContentPartType(part.PartType) {
		case llm.PartText:
			block.Text = part.TextContent
		case llm.PartImage, llm.PartAudio, llm.PartVideo, llm.PartFile:
			block.Media = &llm.MediaPart{
				MediaAssetID: part.MediaAssetID,
				Kind:         part.PartType,
			}
		default:
			continue
		}
		blocks = append(blocks, block)
	}
	return blocks
}

func replaceLastUserMessageWithCurrentParts(messages []llm.Message, anchor turnMemoryAnchor) []llm.Message {
	if len(anchor.userParts) == 0 {
		return messages
	}
	next := append([]llm.Message(nil), messages...)
	for i := len(next) - 1; i >= 0; i-- {
		if next[i].Role != llm.RoleUser {
			continue
		}
		next[i] = llm.Message{
			ID:            anchor.userMessageID,
			TurnID:        anchor.turnID,
			Role:          llm.RoleUser,
			Content:       anchor.userHistoryContent,
			ContentBlocks: cloneContentBlocks(anchor.userParts),
		}
		return next
	}
	return next
}

func cloneContentBlocks(parts []llm.ContentBlock) []llm.ContentBlock {
	cloned := make([]llm.ContentBlock, len(parts))
	for i, part := range parts {
		cloned[i] = part
		if part.Media != nil {
			media := *part.Media
			cloned[i].Media = &media
		}
	}
	return cloned
}

func messagesContainMedia(messages []llm.Message) bool {
	for _, msg := range messages {
		for _, block := range msg.ContentBlocks {
			if block.Media != nil {
				return true
			}
		}
	}
	return false
}

func (e *Engine) historicalPlaceholderDeliveries(ctx context.Context, sessionID string, messages []llm.Message, currentMessageID string, currentTurnID string, providerID string, modelID string) []media.DeliveryRecord {
	if e.db == nil || len(messages) == 0 {
		return nil
	}
	partsByMessage, err := e.db.GetMessagePartsForSession(ctx, sessionID)
	if err != nil {
		e.logger.Warn("failed to load message parts for media delivery audit", "session", sessionID, "error", err)
		return nil
	}
	deliveries := make([]media.DeliveryRecord, 0)
	for _, msg := range messages {
		if msg.ID == "" || msg.ID == currentMessageID {
			continue
		}
		for _, part := range partsByMessage[msg.ID] {
			switch llm.ContentPartType(part.PartType) {
			case llm.PartImage, llm.PartAudio, llm.PartVideo, llm.PartFile:
			default:
				continue
			}
			if strings.TrimSpace(part.MediaAssetID) == "" {
				continue
			}
			deliveries = append(deliveries, media.DeliveryRecord{
				MessageID:     msg.ID,
				PartID:        part.ID,
				MediaAssetID:  part.MediaAssetID,
				ProviderID:    providerID,
				ModelID:       modelID,
				TurnID:        currentTurnID,
				DeliveryScope: media.DeliveryScopeHistoryPlaceholder,
				Transport:     media.TransportPlaceholder,
				Status:        media.DeliveryStatusOmitted,
			})
		}
	}
	return deliveries
}

func appendMissingMediaDeliveries(base []media.DeliveryRecord, extra []media.DeliveryRecord) []media.DeliveryRecord {
	if len(extra) == 0 {
		return base
	}
	seen := make(map[string]bool, len(base)+len(extra))
	for _, delivery := range base {
		seen[mediaDeliveryKey(delivery)] = true
	}
	for _, delivery := range extra {
		key := mediaDeliveryKey(delivery)
		if seen[key] {
			continue
		}
		base = append(base, delivery)
		seen[key] = true
	}
	return base
}

func mediaDeliveryKey(delivery media.DeliveryRecord) string {
	return strings.Join([]string{
		delivery.MessageID,
		delivery.PartID,
		delivery.MediaAssetID,
		delivery.ProviderID,
		delivery.ModelID,
		delivery.TurnID,
		delivery.DeliveryScope,
		delivery.Transport,
		delivery.Status,
	}, "\x00")
}

func (e *Engine) recordMediaDeliveries(ctx context.Context, deliveries []media.DeliveryRecord, terminalStatus string, errorText string) {
	if e.db == nil || len(deliveries) == 0 {
		return
	}
	records := make([]storage.MediaDeliveryRecord, 0, len(deliveries))
	for _, delivery := range deliveries {
		status := delivery.Status
		errMsg := llm.SanitizeImageDataForDiagnostics(delivery.ErrorMessage)
		if status == "" || status == media.DeliveryStatusPrepared {
			status = terminalStatus
			if status == media.DeliveryStatusFailed && errMsg == "" {
				errMsg = llm.SanitizeImageDataForDiagnostics(errorText)
			}
		}
		record := storage.MediaDeliveryRecord{
			ID:            uuid.NewString(),
			MessageID:     delivery.MessageID,
			PartID:        delivery.PartID,
			MediaAssetID:  delivery.MediaAssetID,
			ProviderID:    delivery.ProviderID,
			ModelID:       delivery.ModelID,
			TurnID:        delivery.TurnID,
			DeliveryScope: delivery.DeliveryScope,
			Transport:     delivery.Transport,
			Status:        status,
			ByteSizeSent:  delivery.ByteSizeSent,
			ErrorMessage:  errMsg,
		}
		records = append(records, record)
	}
	if err := e.db.AddMediaDeliveries(ctx, records); err != nil {
		e.logger.Warn("failed to record media deliveries", "error", err)
	}
}

func (e *Engine) prepareInputAndMemoryAnchor(ctx context.Context, sessionID string, opts turnOptions) (turnMemoryAnchor, error) {
	var anchor turnMemoryAnchor
	if !opts.persistUser {
		return anchor, nil
	}

	parts, err := normalizeUserParts(opts.userContent, opts.userParts)
	if err != nil {
		return anchor, err
	}
	historyContent := renderUserParts(parts, llm.RenderForHistory)
	memoryContent := renderUserParts(parts, llm.RenderForMemory)
	turnID := opts.turnID
	if turnID == "" {
		turnID = uuid.NewString()
	}
	userMessageID := uuid.NewString()
	if err := e.db.AddMessageWithMetadata(ctx, userMessageID, sessionID, "user", historyContent, visibleMessageMetadata("user", historyContent)); err != nil {
		e.logger.Error("failed to store user message", "session", sessionID, "error", err)
		return anchor, err
	}
	if err := e.db.AddMessageParts(ctx, storagePartsFromLLM(sessionID, userMessageID, "user", parts)); err != nil {
		e.logger.Error("failed to store message parts", "session", sessionID, "error", err)
		return anchor, err
	}
	anchor.userMessageID = userMessageID
	anchor.userHistoryContent = historyContent
	anchor.userMemoryContent = memoryContent
	anchor.userParts = parts
	anchor.turnID = turnID
	anchor.memorySegment, anchor.hasMemorySegment = e.ensureMemorySegment(ctx, sessionID)
	if anchor.hasMemorySegment {
		if episodeID, err := e.memory.AppendUserEpisode(ctx, anchor.memorySegment.SegmentID, userMessageID, memoryContent); err != nil {
			e.logMemoryWarning("append user memory episode", sessionID, err)
		} else {
			anchor.userEpisodeID = episodeID
		}
	}
	if err := e.db.UpdateSessionTimestamp(ctx, sessionID); err != nil {
		e.logger.Error("failed to update session timestamp", "session", sessionID, "error", err)
		return anchor, err
	}

	session, err := e.db.GetSession(ctx, sessionID)
	if err == nil && session != nil && session.Title == "" {
		title := historyContent
		if runeCount := len([]rune(title)); runeCount > 30 {
			title = string([]rune(title)[:30]) + "…"
		}
		if err := e.db.UpdateSessionTitle(ctx, sessionID, title); err != nil {
			e.logger.Warn("failed to set session title", "session", sessionID, "error", err)
		}
	}
	if notice, ok := e.takeManualMemoryNotice(sessionID); ok {
		assistantMessageID := uuid.NewString()
		if err := e.db.AddMessageWithMetadata(ctx, assistantMessageID, sessionID, "assistant", notice, visibleMessageMetadata("assistant", notice)); err != nil {
			e.logger.Error("failed to store assistant message", "session", sessionID, "error", err)
			return anchor, err
		}
		if err := e.db.UpdateSessionTimestamp(ctx, sessionID); err != nil {
			e.logger.Error("failed to update session timestamp", "session", sessionID, "error", err)
			return anchor, err
		}
		anchor.manualNotice = notice
		anchor.manualNoticeHandled = true
	}
	return anchor, nil
}

func (e *Engine) commitTurnOutput(ctx context.Context, sessionID string, assistantContent string, thinkingBlocks []thinkingBlockMetadata, memorySnapshot *memoryPromptSnapshot, memorySegment MemorySegmentRef, hasMemorySegment bool) error {
	assistantMessageID := uuid.NewString()
	if err := e.db.AddMessageWithMetadata(ctx, assistantMessageID, sessionID, "assistant", assistantContent, visibleMessageMetadataWithThinkingAndMemory("assistant", assistantContent, thinkingBlocks, memorySnapshot)); err != nil {
		e.logger.Error("failed to store assistant message", "session", sessionID, "error", err)
		return err
	}
	if !hasMemorySegment {
		memorySegment, hasMemorySegment = e.ensureMemorySegment(ctx, sessionID)
	}
	if hasMemorySegment {
		if _, err := e.memory.AppendAssistantEpisode(ctx, memorySegment.SegmentID, assistantMessageID, assistantContent); err != nil {
			e.logMemoryWarning("append assistant memory episode", sessionID, err)
		}
	}
	if err := e.db.UpdateSessionTimestamp(ctx, sessionID); err != nil {
		e.logger.Error("failed to update session timestamp", "session", sessionID, "error", err)
		return err
	}
	return nil
}

func (e *Engine) retrieveMemoryPrompt(ctx context.Context, sessionID string, query string, userEpisodeID string, memoryRetrieval config.MemoryRetrievalConfig) (*memoryPromptSnapshot, error) {
	if !memoryRetrieval.Enabled || !memoryRetrieval.InjectPrompt || e.memory == nil {
		return nil, nil
	}
	excludedEpisodeIDs := []string(nil)
	if strings.TrimSpace(userEpisodeID) != "" {
		excludedEpisodeIDs = append(excludedEpisodeIDs, userEpisodeID)
	}
	var memoryBlock string
	var pipelineTrace any
	var retrieveErr error
	if memoryRetrieval.PipelineDebug {
		memoryBlock, pipelineTrace, retrieveErr = e.memory.RetrievePromptSnapshot(ctx, sessionID, query, true, excludedEpisodeIDs...)
	} else {
		memoryBlock, retrieveErr = e.memory.RetrievePromptBlock(ctx, sessionID, query, excludedEpisodeIDs...)
	}
	if retrieveErr != nil {
		e.logMemoryWarning("retrieve memory prompt block", sessionID, retrieveErr)
		if !memoryRetrieval.FailOpen {
			return nil, fmt.Errorf("retrieve memory prompt block: %w", retrieveErr)
		}
		return nil, nil
	}
	memoryBlock = strings.TrimSpace(memoryBlock)
	if memoryBlock == "" && !memoryRetrieval.PipelineDebug {
		return nil, nil
	}
	return &memoryPromptSnapshot{PromptBlock: memoryBlock, PipelineTrace: pipelineTrace, RecordMetadata: memoryRetrieval.PipelineDebug}, nil
}

func memoryPromptRenderComponent(snapshot *memoryPromptSnapshot) promptcenter.RenderComponent {
	if snapshot == nil {
		return promptcenter.RenderComponent{}
	}
	return promptcenter.DynamicComponent(promptcenter.ComponentMemoryPromptBlock, "memory_context", promptcenter.SourceMemoryDynamic, snapshot.PromptBlock, map[string]any{
		"record_metadata":    snapshot.RecordMetadata,
		"has_pipeline_trace": snapshot.PipelineTrace != nil,
		"prompt_chars":       len([]rune(snapshot.PromptBlock)),
	})
}

func (e *Engine) ensureMemorySegment(ctx context.Context, sessionID string) (MemorySegmentRef, bool) {
	if e.memory == nil {
		return MemorySegmentRef{}, false
	}
	personaID, err := e.memoryPersonaID(ctx, sessionID)
	if err != nil {
		e.logMemoryWarning("load memory segment persona", sessionID, err)
		return MemorySegmentRef{}, false
	}
	segment, err := e.memory.EnsureSegment(ctx, sessionID, personaID)
	if err != nil {
		e.logMemoryWarning("ensure memory segment", sessionID, err)
		return MemorySegmentRef{}, false
	}
	return segment, true
}

func (e *Engine) takeManualMemoryNotice(sessionID string) (string, bool) {
	if e == nil || e.memory == nil {
		return "", false
	}
	bridge, ok := e.memory.(manualMemoryNoticeBridge)
	if !ok {
		return "", false
	}
	return bridge.TakeManualMemoryNotice(sessionID)
}

func (e *Engine) memoryPersonaID(ctx context.Context, sessionID string) (string, error) {
	if e.db == nil {
		return "", errors.New("chat engine database is not configured")
	}
	session, err := e.db.GetSession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if session == nil {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}
	return session.Persona, nil
}

func (e *Engine) logMemoryWarning(action string, sessionID string, err error) {
	if e.logger == nil || err == nil {
		return
	}
	e.logger.Warn(action+" failed", "session", sessionID, "error", err)
}

func visibleMessageMetadata(role, content string) map[string]any {
	return map[string]any{
		"kind":           "dialogue_" + role,
		"source":         role,
		"token_estimate": contextutil.EstimateTokens(content),
	}
}

func visibleMessageMetadataWithThinking(role, content string, thinkingBlocks []thinkingBlockMetadata) map[string]any {
	return visibleMessageMetadataWithThinkingAndMemory(role, content, thinkingBlocks, nil)
}

func visibleMessageMetadataWithThinkingAndMemory(role, content string, thinkingBlocks []thinkingBlockMetadata, memorySnapshot *memoryPromptSnapshot) map[string]any {
	metadata := visibleMessageMetadata(role, content)
	if len(thinkingBlocks) > 0 {
		metadata["thinking_blocks"] = thinkingBlocks
	}
	if memorySnapshot != nil && memorySnapshot.RecordMetadata {
		metadata["memory_pipeline"] = memoryPipelineMetadata(memorySnapshot)
	}
	return metadata
}

func memoryPipelineMetadata(snapshot *memoryPromptSnapshot) map[string]any {
	payload := map[string]any{
		"enabled":      true,
		"prompt_block": snapshot.PromptBlock,
	}
	if snapshot.PipelineTrace == nil {
		return payload
	}
	raw, err := json.Marshal(snapshot.PipelineTrace)
	if err != nil {
		return payload
	}
	var trace map[string]any
	if err := json.Unmarshal(raw, &trace); err != nil {
		return payload
	}
	for key, value := range trace {
		payload[key] = value
	}
	return payload
}

func buildApprovalContinuationNote(approval *protocol.ApprovalRequest) string {
	if approval == nil {
		return ""
	}
	return fmt.Sprintf(
		"## Internal Approval Continuation\nA user approval decision was received for a paused Work task.\nThis note is internal runtime state, not user-facing content.\n\nApproval request %s for task %s is now %s.\nSelected option: %s.\n\nIf the task is still paused and this approval has not been consumed, continue the paused task now by calling resume_work with the matching task_id and approval_request_id.\nIf the approval is rejected, resume with rejection so Work can stop or choose a safe alternative.\nDo not mention approval_request_id, task_id, internal approval flow, or raw protocol objects to the user.",
		approval.ID,
		approval.TaskID,
		approval.Status,
		approval.SelectedOptionID,
	)
}

func buildApprovalOutcomeNote(approval *protocol.ApprovalRequest, outcome json.RawMessage) string {
	if approval == nil {
		return ""
	}
	return fmt.Sprintf(
		"## Internal Approval Outcome\nApproval request %s for task %s is now %s. The user's decision has already been applied and the paused Work task has already been resumed internally. Do not call resume_work again for this approval_request_id.\n\n## Internal Resume Outcome\n%s\n\nUse the internal outcome above to continue naturally. If it is already a final result, explain it to the user in your own words. If the task paused again, continue from the current pending state and do not reuse the consumed approval_request_id. Never expose raw JSON, internal IDs, protocol JSON, or approval plumbing to the user.",
		approval.ID,
		approval.TaskID,
		approval.Status,
		string(outcome),
	)
}

func isTerminalTaskReport(outcome json.RawMessage) bool {
	var report protocol.TaskReport
	if err := json.Unmarshal(outcome, &report); err != nil {
		return false
	}
	switch strings.TrimSpace(report.Status) {
	case "completed", "failed", "partial":
		return strings.TrimSpace(report.Summary) != ""
	default:
		return false
	}
}

func decodeToolError(content json.RawMessage) error {
	var payload map[string]string
	if err := json.Unmarshal(content, &payload); err == nil {
		if msg := strings.TrimSpace(payload["error"]); msg != "" {
			return errors.New(msg)
		}
	}
	return fmt.Errorf("tool execution failed: %s", strings.TrimSpace(string(content)))
}

func snapshotApprovalStatuses(approvals []protocol.ApprovalRequest) map[string]string {
	if len(approvals) == 0 {
		return map[string]string{}
	}
	snapshot := make(map[string]string, len(approvals))
	for _, approval := range approvals {
		snapshot[approval.ID] = approval.Status
	}
	return snapshot
}

func emitApprovalDiff(emit func(WSMessage), previous map[string]string, current []protocol.ApprovalRequest) (map[string]string, bool) {
	next := snapshotApprovalStatuses(current)
	if emit == nil {
		return next, false
	}
	interrupted := false
	for i := range current {
		approval := current[i]
		prevStatus, existed := previous[approval.ID]
		if existed && prevStatus == approval.Status {
			continue
		}
		eventType := "approval_updated"
		if approval.Status == string(protocol.ApprovalStatusPending) {
			eventType = "approval_required"
			interrupted = true
		}
		approvalCopy := approval
		emit(WSMessage{Type: eventType, Approval: &approvalCopy})
	}
	return next, interrupted
}

func logSummaryUpdate(logger *slog.Logger, level slog.Level, sessionID string, report contextutil.SummaryUpdateReport, err error) {
	if logger == nil {
		return
	}
	message := "running summary updated"
	if err != nil {
		message = "failed to update running summary"
	} else if report.Skipped {
		message = "running summary update skipped"
	}
	record := slog.NewRecord(time.Now(), level, message, 0)
	record.AddAttrs(
		slog.String("session_id", sessionID),
		slog.String("summary_model", report.SummaryModel),
		slog.Int("delta_count", report.DeltaCount),
		slog.String("covered_until_before", report.CoveredUntilBefore),
		slog.String("covered_until_after", report.CoveredUntilAfter),
		slog.Int64("duration_ms", report.DurationMS),
		slog.String("stop_reason", report.StopReason),
		slog.String("raw_stop_reason", report.RawStopReason),
		slog.Int("content_len", report.ContentLength),
		slog.Int("reasoning_len", report.ReasoningLength),
		slog.Int("failure_count", report.FailureCount),
		slog.String("retry_after", report.RetryAfter),
	)
	if report.SkipReason != "" {
		record.AddAttrs(slog.String("skip_reason", report.SkipReason))
	}
	if err != nil {
		record.AddAttrs(slog.Any("error", err))
	}
	_ = logger.Handler().Handle(context.Background(), record)
}

func logCompactReport(logger *slog.Logger, level slog.Level, report contextutil.CompactReport, retryAttempt int, errorKind llm.ErrorKind, message string) {
	if logger == nil {
		return
	}
	if message == "" {
		message = "reactive compact applied"
	}
	record := slog.NewRecord(time.Now(), level, message, 0)
	record.AddAttrs(
		slog.String("session_id", report.SessionID),
		slog.String("mode", report.Mode),
		slog.String("compact_reason", report.CompactReason),
		slog.Int("pre_estimated_tokens", report.PreEstimatedTokens),
		slog.Int("post_estimated_tokens", report.PostEstimatedTokens),
		slog.Int("kept_recent_turns", report.KeptRecentTurns),
		slog.Int("snipped_tool_results_count", report.SnippedToolResultsCount),
		slog.String("summary_covered_until_message_id", report.SummaryCoveredUntilMessageID),
		slog.String("summary_model", report.SummaryModel),
		slog.Bool("degraded", report.Degraded),
		slog.Int("retry_attempt", retryAttempt),
		slog.String("error_kind", string(errorKind)),
	)
	_ = logger.Handler().Handle(context.Background(), record)
}

func logCompactFailure(logger *slog.Logger, report contextutil.CompactReport, retryAttempt int, errorKind llm.ErrorKind, err error) {
	if logger == nil {
		return
	}
	record := slog.NewRecord(time.Now(), slog.LevelWarn, "reactive compact retry failed", 0)
	record.AddAttrs(
		slog.String("session_id", report.SessionID),
		slog.String("mode", report.Mode),
		slog.String("compact_reason", report.CompactReason),
		slog.Int("pre_estimated_tokens", report.PreEstimatedTokens),
		slog.Int("post_estimated_tokens", report.PostEstimatedTokens),
		slog.Int("kept_recent_turns", report.KeptRecentTurns),
		slog.Int("snipped_tool_results_count", report.SnippedToolResultsCount),
		slog.String("summary_covered_until_message_id", report.SummaryCoveredUntilMessageID),
		slog.String("summary_model", report.SummaryModel),
		slog.Bool("degraded", report.Degraded),
		slog.Int("retry_attempt", retryAttempt),
		slog.String("error_kind", string(errorKind)),
		slog.Any("error", err),
	)
	_ = logger.Handler().Handle(context.Background(), record)
}

func errorKindOf(err error) llm.ErrorKind {
	if err == nil {
		return ""
	}
	var llmErr *llm.Error
	if errors.As(err, &llmErr) {
		return llmErr.Kind
	}
	return ""
}
