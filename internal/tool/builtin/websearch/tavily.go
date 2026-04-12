package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// TavilyConfig holds configuration for the Tavily search provider.
type TavilyConfig struct {
	APIKey        string
	BaseURL       string        // if empty, defaults to "https://api.tavily.com"
	Timeout       time.Duration // if 0, defaults to 30s
	IncludeAnswer bool          // default false
}

type tavilyProvider struct {
	cfg        TavilyConfig
	httpClient *http.Client
	logger     *slog.Logger
}

// NewTavilyProvider creates a new Tavily search provider.
func NewTavilyProvider(cfg TavilyConfig, logger *slog.Logger) Provider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.tavily.com"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &tavilyProvider{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: timeout},
		logger:     logger,
	}
}

func (p *tavilyProvider) Name() string { return "tavily" }

// tavilyRequest is the JSON body sent to the Tavily /search endpoint.
type tavilyRequest struct {
	Query          string   `json:"query"`
	SearchDepth    string   `json:"search_depth"`
	MaxResults     int      `json:"max_results"`
	IncludeAnswer  bool     `json:"include_answer"`
	IncludeDomains []string `json:"include_domains"`
	ExcludeDomains []string `json:"exclude_domains"`
}

// tavilyResult is a single result item from the Tavily API response.
// Tavily uses "content" for the snippet text.
type tavilyResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

// tavilyResponse is the raw JSON response from the Tavily API.
type tavilyResponse struct {
	Query   string         `json:"query"`
	Answer  string         `json:"answer"`
	Results []tavilyResult `json:"results"`
}

func (p *tavilyProvider) Search(ctx context.Context, query string, opts Options) (*Response, error) {
	searchDepth := opts.SearchDepth
	if searchDepth == "" {
		searchDepth = "basic"
	}
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}

	includeDomains := opts.IncludeDomains
	if includeDomains == nil {
		includeDomains = []string{}
	}
	excludeDomains := opts.ExcludeDomains
	if excludeDomains == nil {
		excludeDomains = []string{}
	}

	reqBody := tavilyRequest{
		Query:          query,
		SearchDepth:    searchDepth,
		MaxResults:     maxResults,
		IncludeAnswer:  p.cfg.IncludeAnswer,
		IncludeDomains: includeDomains,
		ExcludeDomains: excludeDomains,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("tavily: marshal request: %w", err)
	}

	url := p.cfg.BaseURL + "/search"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tavily: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	p.logger.Debug("tavily search", "query", query, "max_results", maxResults, "depth", searchDepth)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tavily: http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tavily: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tavily: status %d: %s", resp.StatusCode, string(respBody))
	}

	var tavilyResp tavilyResponse
	if err := json.Unmarshal(respBody, &tavilyResp); err != nil {
		return nil, fmt.Errorf("tavily: decode response: %w", err)
	}

	results := make([]Result, len(tavilyResp.Results))
	for i, r := range tavilyResp.Results {
		results[i] = Result{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Content, // Tavily uses "content" for the snippet text
			Score:   r.Score,
		}
	}

	return &Response{
		Query:   tavilyResp.Query,
		Answer:  tavilyResp.Answer,
		Results: results,
	}, nil
}
