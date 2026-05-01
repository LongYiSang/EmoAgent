package webfetch

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	tavilyapi "github.com/longyisang/emoagent/internal/tool/builtin/tavily"
)

// TavilyConfig holds configuration for the Tavily extract provider.
type TavilyConfig struct {
	APIKey         string
	BaseURL        string
	TimeoutSec     int
	ExtractDepth   string
	Format         string
	IncludeImages  bool
	IncludeFavicon bool
	IncludeUsage   bool
	Client         *tavilyapi.Client
}

type tavilyProvider struct {
	cfg    TavilyConfig
	client *tavilyapi.Client
	logger *slog.Logger
}

// NewTavilyProvider creates a provider backed by Tavily /extract.
func NewTavilyProvider(cfg TavilyConfig, logger *slog.Logger) Provider {
	if cfg.ExtractDepth == "" {
		cfg.ExtractDepth = "basic"
	}
	if cfg.Format == "" {
		cfg.Format = "markdown"
	}
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = 20
	}
	client := cfg.Client
	if client == nil {
		client = tavilyapi.NewClient(tavilyapi.Config{
			APIKey:  cfg.APIKey,
			BaseURL: cfg.BaseURL,
		}, logger)
	}
	return &tavilyProvider{
		cfg:    cfg,
		client: client,
		logger: logger,
	}
}

func (p *tavilyProvider) Name() string { return "tavily" }

type tavilyExtractRequest struct {
	URLs           string  `json:"urls"`
	ExtractDepth   string  `json:"extract_depth"`
	Format         string  `json:"format"`
	Timeout        float64 `json:"timeout,omitempty"`
	IncludeImages  bool    `json:"include_images"`
	IncludeFavicon bool    `json:"include_favicon"`
	IncludeUsage   bool    `json:"include_usage"`
}

type tavilyExtractResult struct {
	URL        string   `json:"url"`
	RawContent string   `json:"raw_content"`
	Images     []string `json:"images"`
	Favicon    string   `json:"favicon"`
}

type tavilyExtractResponse struct {
	Results       []tavilyExtractResult `json:"results"`
	FailedResults []FailedResult        `json:"failed_results"`
	ResponseTime  float64               `json:"response_time"`
	Usage         map[string]any        `json:"usage"`
	RequestID     string                `json:"request_id"`
}

func (p *tavilyProvider) Fetch(ctx context.Context, sourceURL string, opts Options) (*Response, error) {
	if !isHTTPURL(sourceURL) {
		return nil, fmt.Errorf("web_fetch: only http and https schemes are allowed")
	}

	reqBody := tavilyExtractRequest{
		URLs:           sourceURL,
		ExtractDepth:   p.cfg.ExtractDepth,
		Format:         p.cfg.Format,
		IncludeImages:  p.cfg.IncludeImages,
		IncludeFavicon: p.cfg.IncludeFavicon,
		IncludeUsage:   p.cfg.IncludeUsage,
	}
	if p.cfg.TimeoutSec > 0 {
		reqBody.Timeout = float64(p.cfg.TimeoutSec)
	}

	if p.logger != nil {
		p.logger.DebugContext(ctx, "tavily extract", "url", sourceURL, "depth", p.cfg.ExtractDepth, "format", p.cfg.Format)
	}

	var tavilyResp tavilyExtractResponse
	if err := p.client.PostJSON(ctx, "/extract", reqBody, &tavilyResp, "extract"); err != nil {
		return nil, err
	}
	if len(tavilyResp.Results) == 0 {
		return nil, fmt.Errorf("tavily extract: no successful results%s%s",
			formatRequestID(tavilyResp.RequestID),
			formatFailedResults(tavilyResp.FailedResults),
		)
	}

	result := tavilyResp.Results[0]
	text, truncated := truncateText(result.RawContent, opts.MaxBytes)
	finalURL := result.URL
	if finalURL == "" {
		finalURL = sourceURL
	}

	return &Response{
		URL:           sourceURL,
		FinalURL:      finalURL,
		Text:          text,
		Truncated:     truncated,
		Provider:      p.Name(),
		RequestID:     tavilyResp.RequestID,
		Images:        result.Images,
		Favicon:       result.Favicon,
		Usage:         tavilyResp.Usage,
		FailedResults: tavilyResp.FailedResults,
	}, nil
}

func truncateText(text string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text, false
	}
	var b strings.Builder
	for _, r := range text {
		if b.Len()+len(string(r)) > maxBytes {
			return b.String(), true
		}
		b.WriteRune(r)
	}
	return b.String(), false
}

func truncateForError(text string, maxBytes int) string {
	out, _ := truncateText(text, maxBytes)
	return out
}

func formatRequestID(requestID string) string {
	if requestID == "" {
		return ""
	}
	return " (request_id " + requestID + ")"
}

func formatFailedResults(results []FailedResult) string {
	if len(results) == 0 {
		return ""
	}
	first := results[0]
	if first.Error == "" {
		return ": " + first.URL
	}
	return ": " + first.URL + ": " + first.Error
}
