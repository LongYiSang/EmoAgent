package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
)

type ProviderGateway struct {
	storage  PluginProviderUsageStorage
	cfg      config.PluginProviderGatewayConfig
	resolver ProviderClientResolver
	fallback ProviderModelFallbackResolver

	manifests map[string]ManifestV2
}

type PluginProviderUsageStorage interface {
	RecordPluginProviderUsage(context.Context, storage.PluginProviderUsage) error
}

type ProviderClientResolver func(context.Context, string) (llm.Client, error)
type ProviderModelFallbackResolver func(context.Context) (providerID string, model string, ok bool, err error)

type PluginGenerateRequest struct {
	Purpose     string            `json:"purpose"`
	ProviderID  string            `json:"provider_id"`
	Model       string            `json:"model"`
	System      string            `json:"system"`
	Messages    []llm.Message     `json:"messages"`
	Params      llm.RequestParams `json:"params"`
	MaxTokens   int               `json:"max_tokens"`
	Temperature *float64          `json:"temperature"`
}

type PluginGenerateResponse struct {
	Content    string    `json:"content"`
	Model      string    `json:"model"`
	Usage      llm.Usage `json:"usage"`
	StopReason string    `json:"stop_reason"`
}

func NewProviderGateway(storage PluginProviderUsageStorage, cfg config.PluginProviderGatewayConfig, resolver ProviderClientResolver) *ProviderGateway {
	return &ProviderGateway{storage: storage, cfg: cfg, resolver: resolver, manifests: map[string]ManifestV2{}}
}

func (g *ProviderGateway) SetFallbackResolver(resolver ProviderModelFallbackResolver) {
	if g != nil {
		g.fallback = resolver
	}
}

func (g *ProviderGateway) AddPlugin(manifest ManifestV2) {
	if g == nil {
		return
	}
	if g.manifests == nil {
		g.manifests = map[string]ManifestV2{}
	}
	g.manifests[manifest.ID] = manifest
}

func (g *ProviderGateway) Generate(ctx context.Context, pluginID string, req PluginGenerateRequest) (PluginGenerateResponse, error) {
	start := time.Now()
	if g == nil {
		return PluginGenerateResponse{}, fmt.Errorf("provider gateway is not configured")
	}
	providerID, model, err := g.resolveProviderModel(ctx, pluginID, req)
	usage := storage.PluginProviderUsage{
		PluginID:        pluginID,
		ProviderID:      providerID,
		Model:           model,
		Purpose:         req.Purpose,
		EstimatedTokens: estimatePluginRequestTokens(req),
		Status:          "error",
	}
	defer func() {
		usage.DurationMS = time.Since(start).Milliseconds()
		if g != nil && g.storage != nil {
			_ = g.storage.RecordPluginProviderUsage(context.Background(), usage)
		}
	}()
	if err != nil {
		usage.ErrorMessage = err.Error()
		return PluginGenerateResponse{}, err
	}
	if !g.cfg.Enabled {
		err := fmt.Errorf("provider gateway is disabled")
		usage.ErrorMessage = err.Error()
		return PluginGenerateResponse{}, err
	}
	if g.resolver == nil {
		err := fmt.Errorf("provider gateway resolver is not configured")
		usage.ErrorMessage = err.Error()
		return PluginGenerateResponse{}, err
	}
	client, err := g.resolver(ctx, providerID)
	if err != nil {
		usage.ErrorMessage = err.Error()
		return PluginGenerateResponse{}, err
	}
	chatReq := llm.ChatRequest{
		Model:     model,
		System:    req.System,
		Messages:  append([]llm.Message(nil), req.Messages...),
		Params:    req.Params,
		MaxTokens: req.MaxTokens,
	}
	if req.Temperature != nil {
		chatReq.Temperature = *req.Temperature
		chatReq.Params.Temperature = req.Temperature
	}
	if chatReq.Params.MaxTokens == 0 {
		chatReq.Params.MaxTokens = req.MaxTokens
	}
	resp, err := client.Chat(ctx, chatReq)
	if err != nil {
		usage.ErrorMessage = err.Error()
		return PluginGenerateResponse{}, err
	}
	if resp != nil {
		usage.InputTokens = resp.Usage.InputTokens
		usage.OutputTokens = resp.Usage.OutputTokens
		usage.Status = "success"
		return PluginGenerateResponse{
			Content:    resp.Content,
			Model:      resp.Model,
			Usage:      resp.Usage,
			StopReason: resp.StopReason,
		}, nil
	}
	err = fmt.Errorf("provider returned nil response")
	usage.ErrorMessage = err.Error()
	return PluginGenerateResponse{}, err
}

func (g *ProviderGateway) GenerateRaw(ctx context.Context, pluginID string, params json.RawMessage) (json.RawMessage, error) {
	var req PluginGenerateRequest
	if len(params) == 0 {
		params = json.RawMessage("{}")
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("decode provider.generate params: %w", err)
	}
	resp, err := g.Generate(ctx, pluginID, req)
	if err != nil {
		return nil, err
	}
	return marshalRaw(resp)
}

func (g *ProviderGateway) resolveProviderModel(ctx context.Context, pluginID string, req PluginGenerateRequest) (string, string, error) {
	providerID := strings.TrimSpace(req.ProviderID)
	model := strings.TrimSpace(req.Model)
	if manifest, ok := g.manifests[pluginID]; ok {
		if req.ProviderID != "" && len(manifest.Provider.AllowedProviderIDs) > 0 && !providerStringAllowed(manifest.Provider.AllowedProviderIDs, req.ProviderID) {
			return "", "", fmt.Errorf("provider_id %q is not allowed for plugin %s", req.ProviderID, pluginID)
		}
		if req.Model != "" && len(manifest.Provider.AllowedModels) > 0 && !providerStringAllowed(manifest.Provider.AllowedModels, req.Model) {
			return "", "", fmt.Errorf("model %q is not allowed for plugin %s", req.Model, pluginID)
		}
		if providerID == "" {
			providerID = strings.TrimSpace(manifest.Provider.DefaultProviderID)
		}
		if model == "" {
			model = strings.TrimSpace(manifest.Provider.DefaultModel)
		}
	}
	if providerID == "" {
		providerID = strings.TrimSpace(g.cfg.DefaultProviderID)
	}
	if model == "" {
		model = strings.TrimSpace(g.cfg.DefaultModel)
	}
	if (providerID == "" || model == "") && g.fallback != nil {
		fallbackProvider, fallbackModel, ok, err := g.fallback(ctx)
		if err != nil {
			return providerID, model, err
		}
		if ok {
			if providerID == "" {
				providerID = strings.TrimSpace(fallbackProvider)
			}
			if model == "" {
				model = strings.TrimSpace(fallbackModel)
			}
		}
	}
	if providerID == "" {
		return "", model, fmt.Errorf("provider_id is required")
	}
	if model == "" {
		return providerID, "", fmt.Errorf("model is required")
	}
	return providerID, model, nil
}

func providerStringAllowed(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func estimatePluginRequestTokens(req PluginGenerateRequest) int {
	chars := len(req.System)
	for _, message := range req.Messages {
		chars += len(message.Content)
		for _, block := range message.ContentBlocks {
			chars += len(block.Text) + len(block.Content) + len(block.Input)
		}
	}
	if chars == 0 {
		return 0
	}
	return (chars + 3) / 4
}
