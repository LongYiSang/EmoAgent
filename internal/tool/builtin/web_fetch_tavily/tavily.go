package web_fetch_tavily

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// TavilyConfig holds configuration for the Tavily extract provider.
type TavilyConfig struct {
	APIKey         string
	BaseURL        string
	Timeout        time.Duration
	TimeoutSec     int
	ExtractDepth   string
	Format         string
	IncludeImages  bool
	IncludeFavicon bool
	IncludeUsage   bool
}

type tavilyProvider struct {
	cfg        TavilyConfig
	httpClient *http.Client
	logger     *slog.Logger
}

// NewTavilyProvider creates a provider backed by Tavily /extract.
func NewTavilyProvider(cfg TavilyConfig, logger *slog.Logger) Provider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.tavily.com"
	}
	if cfg.ExtractDepth == "" {
		cfg.ExtractDepth = "basic"
	}
	if cfg.Format == "" {
		cfg.Format = "markdown"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = int(timeout / time.Second)
	}
	return &tavilyProvider{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: timeout},
		logger:     logger,
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

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("tavily extract: marshal request: %w", err)
	}

	endpoint := strings.TrimRight(p.cfg.BaseURL, "/") + "/extract"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tavily extract: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	if p.logger != nil {
		p.logger.DebugContext(ctx, "tavily extract", "url", sourceURL, "depth", p.cfg.ExtractDepth, "format", p.cfg.Format)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tavily extract: http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tavily extract: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tavily extract: status %d: %s", resp.StatusCode, truncateForError(string(respBody), 512))
	}

	var tavilyResp tavilyExtractResponse
	if err := json.Unmarshal(respBody, &tavilyResp); err != nil {
		return nil, fmt.Errorf("tavily extract: decode response: %w", err)
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
