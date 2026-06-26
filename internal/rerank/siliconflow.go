package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tokenmeter"
)

type SiliconFlowConfig struct {
	APIKey   string
	BaseURL  string
	Path     string
	Model    string
	Timeout  time.Duration
	Client   *http.Client
	Recorder UsageRecorder
}

type siliconFlowProvider struct {
	cfg    SiliconFlowConfig
	client *http.Client
}

func NewSiliconFlowProvider(cfg SiliconFlowConfig) Provider {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return &siliconFlowProvider{cfg: cfg, client: client}
}

func (p *siliconFlowProvider) Name() string { return "siliconflow" }

func (p *siliconFlowProvider) Rerank(ctx context.Context, req Request) (Response, error) {
	start := time.Now()
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(p.cfg.Model)
	}
	body := siliconFlowRequest{
		Model:     model,
		Query:     req.Query,
		Documents: make([]string, 0, len(req.Documents)),
	}
	for _, doc := range req.Documents {
		body.Documents = append(body.Documents, doc.Text)
	}
	if req.TopK > 0 {
		body.TopN = req.TopK
	}

	payload, err := json.Marshal(body)
	if err != nil {
		p.recordUsage(ctx, req, model, start, Usage{}, "error", err, nil)
		return Response{}, fmt.Errorf("siliconflow rerank request encode failed")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint(), bytes.NewReader(payload))
	if err != nil {
		p.recordUsage(ctx, req, model, start, Usage{}, "error", err, nil)
		return Response{}, fmt.Errorf("siliconflow rerank request build failed")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		p.recordUsage(ctx, req, model, start, Usage{}, "error", err, nil)
		return Response{}, fmt.Errorf("siliconflow rerank request failed")
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		err := fmt.Errorf("siliconflow rerank failed with status %d", httpResp.StatusCode)
		p.recordUsage(ctx, req, model, start, Usage{}, "error", err, nil)
		return Response{}, fmt.Errorf("siliconflow rerank failed with status %d", httpResp.StatusCode)
	}
	var decoded siliconFlowResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&decoded); err != nil {
		p.recordUsage(ctx, req, model, start, Usage{}, "error", err, nil)
		return Response{}, fmt.Errorf("siliconflow rerank response parse failed")
	}

	results := make([]Result, 0, len(decoded.Results))
	for _, item := range decoded.Results {
		result := Result{
			ID:    fmt.Sprintf("%d", item.Index),
			Index: item.Index,
			Score: item.RelevanceScore,
		}
		if item.Index >= 0 && item.Index < len(req.Documents) {
			result.ID = req.Documents[item.Index].ID
			result.Index = req.Documents[item.Index].Index
		}
		results = append(results, Result{
			ID:    result.ID,
			Index: result.Index,
			Score: result.Score,
		})
	}
	tokens := decoded.Meta.Tokens
	usage := Usage{
		Documents:   len(req.Documents),
		InputTokens: tokens.InputTokens,
		TotalTokens: tokens.InputTokens + tokens.OutputTokens + tokens.ImageTokens,
	}
	rawUsage, _ := json.Marshal(decoded.Meta.Tokens)
	p.recordUsage(ctx, req, model, start, usage, "success", nil, rawUsage)
	return Response{
		Results: results,
		Usage:   usage,
	}, nil
}

func (p *siliconFlowProvider) recordUsage(ctx context.Context, req Request, model string, start time.Time, usage Usage, status string, err error, rawUsage []byte) {
	if p.cfg.Recorder == nil {
		return
	}
	scope, _ := tokenmeter.UsageScopeFromContext(ctx)
	estimate := estimateRerankInputTokens(ctx, req, model)
	source := llm.UsageSourceProvider
	if usage.TotalTokens == 0 && usage.InputTokens == 0 {
		source = llm.UsageSourceEstimated
	}
	event := storage.LLMUsageEvent{
		ID:                   uuid.NewString(),
		RequestID:            scope.RequestID,
		SessionID:            scope.SessionID,
		TurnID:               scope.TurnID,
		AgentID:              scope.AgentID,
		PersonaKey:           scope.PersonaKey,
		PluginID:             scope.PluginID,
		TaskID:               scope.TaskID,
		Component:            firstNonEmpty(scope.Component, "web_search"),
		Operation:            firstNonEmpty(scope.Operation, "rerank"),
		ProviderID:           firstNonEmpty(scope.ProviderID, "siliconflow"),
		ProviderName:         firstNonEmpty(scope.ProviderName, "SiliconFlow"),
		Protocol:             firstNonEmpty(scope.Protocol, "siliconflow_rerank"),
		Model:                firstNonEmpty(scope.Model, model),
		Endpoint:             p.endpoint(),
		Status:               status,
		DurationMS:           time.Since(start).Milliseconds(),
		EstimatedInputTokens: estimate,
		EstimatedTotalTokens: estimate,
		EstimateMethod:       tokenmeter.MethodHeuristicCJK,
		EstimateConfidence:   0.55,
		UsageSource:          source,
		InputTokens:          usage.InputTokens,
		TotalTokens:          usage.TotalTokens,
		ActualInputTokens:    usage.InputTokens,
		ActualTotalTokens:    usage.TotalTokens,
		RawUsageJSON:         firstNonEmpty(string(rawUsage), "{}"),
		MetadataJSON:         rerankMetadataJSON(req, usage),
	}
	if source == llm.UsageSourceEstimated {
		event.InputTokens = estimate
		event.TotalTokens = estimate
		event.ActualInputTokens = 0
		event.ActualTotalTokens = 0
	}
	if err != nil {
		event.ErrorMessage = err.Error()
	}
	if recordErr := p.cfg.Recorder.RecordLLMUsageEvent(context.WithoutCancel(ctx), event); recordErr != nil {
		return
	}
}

func estimateRerankInputTokens(ctx context.Context, req Request, model string) int {
	counter := tokenmeter.DefaultCounter()
	total := counter.CountText(ctx, "siliconflow", model, req.Query).InputTokens
	for _, doc := range req.Documents {
		total += counter.CountText(ctx, "siliconflow", model, doc.Text).InputTokens
	}
	return total
}

func rerankMetadataJSON(req Request, usage Usage) string {
	payload, err := json.Marshal(struct {
		Documents int `json:"documents"`
		TopK      int `json:"top_k,omitempty"`
	}{
		Documents: usage.Documents,
		TopK:      req.TopK,
	})
	if err != nil {
		return "{}"
	}
	return string(payload)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (p *siliconFlowProvider) endpoint() string {
	base := strings.TrimRight(strings.TrimSpace(p.cfg.BaseURL), "/")
	path := strings.TrimSpace(p.cfg.Path)
	if path == "" {
		path = "/v1/rerank"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

type siliconFlowRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n,omitempty"`
}

type siliconFlowResponse struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
	Meta struct {
		Tokens struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			ImageTokens  int `json:"image_tokens"`
		} `json:"tokens"`
	} `json:"meta"`
}
