package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type SiliconFlowConfig struct {
	APIKey  string
	BaseURL string
	Path    string
	Model   string
	Timeout time.Duration
	Client  *http.Client
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
		return Response{}, fmt.Errorf("siliconflow rerank request encode failed")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return Response{}, fmt.Errorf("siliconflow rerank request build failed")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("siliconflow rerank request failed")
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return Response{}, fmt.Errorf("siliconflow rerank failed with status %d", httpResp.StatusCode)
	}
	var decoded siliconFlowResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&decoded); err != nil {
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
	return Response{
		Results: results,
		Usage: Usage{
			Documents:   len(req.Documents),
			InputTokens: tokens.InputTokens,
			TotalTokens: tokens.InputTokens + tokens.OutputTokens + tokens.ImageTokens,
		},
	}, nil
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
