package tokenmeter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
)

type UsageRecorder interface {
	RecordLLMUsageEvent(context.Context, storage.LLMUsageEvent) error
}

type MeteredClientConfig struct {
	Inner        llm.Client
	Counter      Counter
	Recorder     UsageRecorder
	ProviderID   string
	ProviderName string
	Protocol     string
	Endpoint     string
	ModelDefault string
	Logger       *slog.Logger
}

type MeteredClient struct {
	inner        llm.Client
	counter      Counter
	recorder     UsageRecorder
	providerID   string
	providerName string
	protocol     string
	endpoint     string
	modelDefault string
	logger       *slog.Logger
}

func NewMeteredClient(cfg MeteredClientConfig) *MeteredClient {
	counter := cfg.Counter
	if counter == nil {
		counter = DefaultCounter()
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &MeteredClient{
		inner:        cfg.Inner,
		counter:      counter,
		recorder:     cfg.Recorder,
		providerID:   cfg.ProviderID,
		providerName: cfg.ProviderName,
		protocol:     cfg.Protocol,
		endpoint:     cfg.Endpoint,
		modelDefault: cfg.ModelDefault,
		logger:       logger,
	}
}

func (c *MeteredClient) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	start := time.Now()
	estimate := c.countRequest(ctx, req)
	resp, err := c.inner.Chat(ctx, req)
	if err != nil {
		c.record(ctx, req, nil, llm.Usage{}, llm.Usage{}, estimate, start, statusForError(ctx, err), err, false)
		return resp, err
	}
	var usage llm.Usage
	var providerUsage llm.Usage
	if resp != nil {
		providerUsage = resp.Usage.NormalizeTotals()
		usage = c.resolveUsage(ctx, req, resp, estimate)
		resp.Usage = usage
	}
	eventID := c.record(ctx, req, resp, usage, providerUsage, estimate, start, "success", nil, false)
	if resp != nil && eventID != "" {
		resp.Usage.UsageEventID = eventID
	}
	return resp, nil
}

func (c *MeteredClient) ChatStream(ctx context.Context, req llm.ChatRequest, cb llm.StreamCallback) (*llm.ChatResponse, error) {
	start := time.Now()
	estimate := c.countRequest(ctx, req)
	resp, err := c.inner.ChatStream(ctx, req, cb)
	if err != nil {
		c.record(ctx, req, nil, llm.Usage{}, llm.Usage{}, estimate, start, statusForError(ctx, err), err, true)
		return resp, err
	}
	var usage llm.Usage
	var providerUsage llm.Usage
	if resp != nil {
		providerUsage = resp.Usage.NormalizeTotals()
		usage = c.resolveUsage(ctx, req, resp, estimate)
		resp.Usage = usage
	}
	eventID := c.record(ctx, req, resp, usage, providerUsage, estimate, start, "success", nil, true)
	if resp != nil && eventID != "" {
		resp.Usage.UsageEventID = eventID
	}
	return resp, nil
}

func (c *MeteredClient) countRequest(ctx context.Context, req llm.ChatRequest) CountResult {
	if c.counter == nil {
		return CountResult{}
	}
	return c.counter.CountChatRequest(ctx, CountRequest{
		ProviderID: c.providerID,
		Protocol:   c.protocol,
		Model:      requestModel(req, c.modelDefault),
		System:     req.System,
		Messages:   req.Messages,
		Tools:      req.Tools,
		Params:     req.Params,
		BudgetMode: true,
	})
}

func (c *MeteredClient) resolveUsage(ctx context.Context, req llm.ChatRequest, resp *llm.ChatResponse, estimate CountResult) llm.Usage {
	if resp == nil {
		return llm.Usage{}
	}
	usage := resp.Usage.NormalizeTotals()
	outputEstimate := CountResult{}
	if c.counter != nil {
		outputEstimate = c.counter.CountChatResponse(ctx, c.providerID, requestModel(req, c.modelDefault), resp)
	}
	if estimate.InputTokens > 0 {
		usage.EstimatedInputTokens = estimate.InputTokens
	}
	if outputEstimate.OutputTokens > 0 {
		usage.EstimatedOutputTokens = outputEstimate.OutputTokens
	}
	if usage.EstimatedInputTokens > 0 || usage.EstimatedOutputTokens > 0 {
		usage.EstimatedTotalTokens = usage.EstimatedInputTokens + usage.EstimatedOutputTokens
	}
	hasActualInput := usage.InputTokens > 0
	hasActualOutput := usage.OutputTokens > 0
	hasActualTotal := usage.TotalTokens > 0
	switch {
	case hasActualInput || hasActualOutput || hasActualTotal:
		usage.ActualInputTokens = usage.InputTokens
		usage.ActualOutputTokens = usage.OutputTokens
		usage.ActualTotalTokens = usage.EffectiveTotal()
		if !hasActualInput && estimate.InputTokens > 0 {
			usage.InputTokens = estimate.InputTokens
			usage.Source = llm.UsageSourceHybrid
		}
		if !hasActualOutput && outputEstimate.OutputTokens > 0 {
			usage.OutputTokens = outputEstimate.OutputTokens
			usage.Source = llm.UsageSourceHybrid
		}
		if usage.Source == "" {
			usage.Source = llm.UsageSourceProvider
		}
	default:
		usage.InputTokens = estimate.InputTokens
		usage.OutputTokens = outputEstimate.OutputTokens
		usage.Source = llm.UsageSourceEstimated
		usage.ActualInputTokens = 0
		usage.ActualOutputTokens = 0
		usage.ActualTotalTokens = 0
	}
	usage.EstimateMethod = estimate.Method
	usage.EstimateConfidence = estimate.Confidence
	return usage.NormalizeTotals()
}

func (c *MeteredClient) record(ctx context.Context, req llm.ChatRequest, resp *llm.ChatResponse, usage llm.Usage, providerUsage llm.Usage, estimate CountResult, start time.Time, status string, callErr error, stream bool) string {
	if c.recorder == nil {
		return ""
	}
	event := c.buildEvent(ctx, req, resp, usage, providerUsage, estimate, start, status, callErr, stream)
	recordCtx := context.WithoutCancel(ctx)
	if err := c.recorder.RecordLLMUsageEvent(recordCtx, event); err != nil && c.logger != nil {
		c.logger.Warn("record llm usage event failed", "error", err)
		return ""
	}
	return event.ID
}

func (c *MeteredClient) buildEvent(ctx context.Context, req llm.ChatRequest, resp *llm.ChatResponse, usage llm.Usage, providerUsage llm.Usage, estimate CountResult, start time.Time, status string, callErr error, stream bool) storage.LLMUsageEvent {
	scope, _ := UsageScopeFromContext(ctx)
	model := requestModel(req, firstNonEmpty(scope.Model, c.modelDefault))
	if usage.Source == "" && !usage.HasProviderTokens() {
		usage = llm.Usage{InputTokens: estimate.InputTokens, Source: llm.UsageSourceEstimated, EstimateMethod: estimate.Method, EstimateConfidence: estimate.Confidence}.NormalizeTotals()
	}
	event := storage.LLMUsageEvent{
		ID:                    uuid.NewString(),
		RequestID:             scope.RequestID,
		SessionID:             scope.SessionID,
		TurnID:                scope.TurnID,
		AgentID:               scope.AgentID,
		PersonaKey:            scope.PersonaKey,
		PluginID:              scope.PluginID,
		TaskID:                scope.TaskID,
		Component:             firstNonEmpty(scope.Component, "unknown"),
		Operation:             scope.Operation,
		ProviderID:            firstNonEmpty(scope.ProviderID, c.providerID),
		ProviderName:          firstNonEmpty(scope.ProviderName, c.providerName),
		Protocol:              firstNonEmpty(scope.Protocol, c.protocol),
		Model:                 model,
		Endpoint:              c.endpoint,
		Stream:                stream || req.Stream || (req.Params.Stream != nil && *req.Params.Stream),
		Status:                status,
		ErrorKind:             errorKindFor(ctx, callErr),
		DurationMS:            time.Since(start).Milliseconds(),
		EstimatedInputTokens:  firstPositive(usage.EstimatedInputTokens, estimate.InputTokens),
		EstimatedOutputTokens: usage.EstimatedOutputTokens,
		EstimatedTotalTokens:  usage.EstimatedTotalTokens,
		EstimateMethod:        estimate.Method,
		EstimateConfidence:    estimate.Confidence,
		UsageSource:           usage.Source,
		InputTokens:           usage.InputTokens,
		OutputTokens:          usage.OutputTokens,
		TotalTokens:           usage.EffectiveTotal(),
		CachedInputTokens:     usage.CachedInputTokens,
		CacheHitInputTokens:   usage.CacheHitInputTokens,
		CacheMissInputTokens:  usage.CacheMissInputTokens,
		CacheReadTokens:       usage.CacheReadTokens,
		CacheWriteTokens:      usage.CacheWriteTokens,
		ReasoningTokens:       usage.ReasoningTokens,
		ImageTokens:           usage.ImageTokens,
		AudioTokens:           usage.AudioTokens,
		PromptHash:            requestHash(req),
		RawUsageJSON:          firstNonEmpty(string(usage.RawUsage), "{}"),
		MetadataJSON:          "{}",
	}
	if resp != nil {
		event.ResponseID = resp.ID
		event.CompletionHash = completionHash(resp)
	}
	if providerUsage.HasProviderTokens() {
		event.ActualInputTokens = providerUsage.InputTokens
		event.ActualOutputTokens = providerUsage.OutputTokens
		event.ActualTotalTokens = providerUsage.EffectiveTotal()
	}
	if event.EstimatedTotalTokens == 0 && (event.EstimatedInputTokens > 0 || event.EstimatedOutputTokens > 0) {
		event.EstimatedTotalTokens = event.EstimatedInputTokens + event.EstimatedOutputTokens
	}
	if callErr != nil {
		event.ErrorMessage = callErr.Error()
		event.UsageSource = llm.UsageSourceEstimated
		event.InputTokens = estimate.InputTokens
		event.OutputTokens = 0
		event.TotalTokens = estimate.InputTokens
		event.EstimatedTotalTokens = estimate.InputTokens
	}
	return event
}

func statusForError(ctx context.Context, err error) string {
	if ctx != nil && ctx.Err() != nil {
		return "cancelled"
	}
	return "error"
}

func errorKindFor(ctx context.Context, err error) string {
	if err == nil {
		return ""
	}
	if ctx != nil && ctx.Err() != nil {
		return "cancelled"
	}
	var llmErr *llm.Error
	if errors.As(err, &llmErr) && llmErr.Kind != "" {
		return string(llmErr.Kind)
	}
	return "error"
}

func requestModel(req llm.ChatRequest, fallback string) string {
	return firstNonEmpty(req.Model, fallback)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func requestHash(req llm.ChatRequest) string {
	payload, _ := json.Marshal(struct {
		Model    string        `json:"model"`
		System   string        `json:"system,omitempty"`
		Messages []llm.Message `json:"messages,omitempty"`
		Tools    []llm.ToolDef `json:"tools,omitempty"`
	}{Model: req.Model, System: req.System, Messages: req.Messages, Tools: req.Tools})
	return sha256Hex(payload)
}

func completionHash(resp *llm.ChatResponse) string {
	if resp == nil {
		return ""
	}
	payload, _ := json.Marshal(struct {
		Content          string             `json:"content,omitempty"`
		ReasoningContent string             `json:"reasoning_content,omitempty"`
		ContentBlocks    []llm.ContentBlock `json:"content_blocks,omitempty"`
	}{Content: resp.Content, ReasoningContent: resp.ReasoningContent, ContentBlocks: resp.ContentBlocks})
	return sha256Hex(payload)
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
