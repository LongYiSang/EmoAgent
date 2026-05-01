package websearch

import (
	"context"
	"log/slog"
	"time"

	tavilyapi "github.com/longyisang/emoagent/internal/tool/builtin/tavily"
)

// TavilyConfig holds configuration for the Tavily search provider.
type TavilyConfig struct {
	APIKey        string
	BaseURL       string        // if empty, defaults to "https://api.tavily.com"
	Timeout       time.Duration // if 0, defaults to 30s
	IncludeAnswer bool          // default false
	Client        *tavilyapi.Client
}

type tavilyProvider struct {
	cfg    TavilyConfig
	client *tavilyapi.Client
	logger *slog.Logger
}

// NewTavilyProvider creates a new Tavily search provider.
func NewTavilyProvider(cfg TavilyConfig, logger *slog.Logger) Provider {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := cfg.Client
	if client == nil {
		client = tavilyapi.NewClient(tavilyapi.Config{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
			Timeout: timeout,
		}, logger)
	}
	return &tavilyProvider{
		cfg:    cfg,
		client: client,
		logger: logger,
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

	if p.logger != nil {
		p.logger.DebugContext(ctx, "tavily search", "query", query, "max_results", maxResults, "depth", searchDepth)
	}

	var tavilyResp tavilyResponse
	if err := p.client.PostJSON(ctx, "/search", reqBody, &tavilyResp, "search"); err != nil {
		return nil, err
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
