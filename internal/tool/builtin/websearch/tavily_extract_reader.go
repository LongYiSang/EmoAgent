package websearch

import (
	"context"
	"log/slog"
	"time"
	"unicode/utf8"

	tavilyapi "github.com/longyisang/emoagent/internal/tool/builtin/tavily"
)

type TavilyExtractConfig struct {
	APIKey         string
	BaseURL        string
	ExtractDepth   string
	Format         string
	Timeout        time.Duration
	MaxCharsPerDoc int
	MaxChunkChars  int
	Client         *tavilyapi.Client
}

type tavilyExtractReader struct {
	cfg    TavilyExtractConfig
	client *tavilyapi.Client
}

func NewTavilyExtractReader(cfg TavilyExtractConfig, logger *slog.Logger) Reader {
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
	cfg.Timeout = timeout
	return &tavilyExtractReader{cfg: cfg, client: client}
}

func (r *tavilyExtractReader) Name() string { return "tavily_extract" }

type tavilyExtractRequest struct {
	URLs         []string `json:"urls"`
	ExtractDepth string   `json:"extract_depth"`
	Format       string   `json:"format"`
	Timeout      int      `json:"timeout"`
	IncludeUsage bool     `json:"include_usage"`
}

type tavilyExtractResponse struct {
	Results       []tavilyExtractResult       `json:"results"`
	FailedResults []tavilyExtractFailedResult `json:"failed_results"`
}

type tavilyExtractResult struct {
	URL        string `json:"url"`
	RawContent string `json:"raw_content"`
}

type tavilyExtractFailedResult struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

func (r *tavilyExtractReader) Extract(ctx context.Context, urls []string, opts ExtractOptions) (*ExtractResponse, error) {
	cfg := r.options(opts)
	req := tavilyExtractRequest{
		URLs:         append([]string(nil), urls...),
		ExtractDepth: cfg.ExtractDepth,
		Format:       cfg.Format,
		Timeout:      int(cfg.Timeout.Seconds()),
		IncludeUsage: true,
	}

	var raw tavilyExtractResponse
	if err := r.client.PostJSON(ctx, "/extract", req, &raw, "extract"); err != nil {
		return nil, err
	}

	resp := &ExtractResponse{ByURL: make(map[string]ExtractResult, len(urls))}
	for _, url := range urls {
		resp.ByURL[url] = ExtractResult{URL: url}
	}
	for _, failed := range raw.FailedResults {
		resp.ByURL[failed.URL] = ExtractResult{URL: failed.URL, Error: "extract failed"}
	}
	for _, result := range raw.Results {
		if result.RawContent == "" {
			resp.ByURL[result.URL] = ExtractResult{URL: result.URL, Error: "extract returned empty content"}
			continue
		}
		text, truncated := truncateEvidenceText(result.RawContent, cfg.MaxCharsPerDoc, cfg.MaxChunkChars)
		resp.ByURL[result.URL] = ExtractResult{
			URL: result.URL,
			Chunks: []EvidenceChunk{{
				Text:      text,
				URL:       result.URL,
				Source:    "tavily_extract",
				Truncated: truncated,
			}},
		}
	}
	return resp, nil
}

func (r *tavilyExtractReader) options(opts ExtractOptions) TavilyExtractConfig {
	cfg := r.cfg
	if opts.ExtractDepth != "" {
		cfg.ExtractDepth = opts.ExtractDepth
	}
	if cfg.ExtractDepth == "" {
		cfg.ExtractDepth = "basic"
	}
	if opts.Format != "" {
		cfg.Format = opts.Format
	}
	if cfg.Format == "" {
		cfg.Format = "markdown"
	}
	if opts.TimeoutSec > 0 {
		cfg.Timeout = time.Duration(opts.TimeoutSec) * time.Second
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if opts.MaxCharsPerDoc > 0 {
		cfg.MaxCharsPerDoc = opts.MaxCharsPerDoc
	}
	if opts.MaxChunkChars > 0 {
		cfg.MaxChunkChars = opts.MaxChunkChars
	}
	return cfg
}

func truncateEvidenceText(raw string, maxDocChars, maxChunkChars int) (string, bool) {
	text := raw
	truncated := false
	if maxDocChars > 0 && utf8.RuneCountInString(text) > maxDocChars {
		text = truncateRunes(text, maxDocChars)
		truncated = true
	}
	if maxChunkChars > 0 && utf8.RuneCountInString(text) > maxChunkChars {
		text = truncateRunes(text, maxChunkChars)
		truncated = true
	}
	return text, truncated
}

func truncateRunes(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	count := 0
	for i := range text {
		if count == maxRunes {
			return text[:i]
		}
		count++
	}
	return text
}
