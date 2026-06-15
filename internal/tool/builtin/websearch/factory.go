package websearch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/rerank"
)

// NewProvider constructs the appropriate Provider based on the given config.
// It reads the API key from the environment variable named by cfg.APIKeyEnv.
func NewProvider(cfg config.WebSearchConfig, logger *slog.Logger) (Provider, error) {
	apiKey := os.Getenv(cfg.APIKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("%s not set", cfg.APIKeyEnv)
	}
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	tavily := NewTavilyProvider(TavilyConfig{
		APIKey:        apiKey,
		BaseURL:       cfg.BaseURL,
		Timeout:       timeout,
		IncludeAnswer: cfg.IncludeAnswer,
	}, logger)
	switch strings.ToLower(cfg.Provider) {
	case "pipeline":
		if !cfg.Pipeline.Enabled {
			return tavily, nil
		}
		return assemblePipelineProvider(tavily, cfg, apiKey, timeout, logger), nil
	case "tavily":
		return tavily, nil
	default:
		return nil, fmt.Errorf("unsupported websearch provider %q", cfg.Provider)
	}
}

func assemblePipelineProvider(tavily Provider, cfg config.WebSearchConfig, apiKey string, timeout time.Duration, logger *slog.Logger) Provider {
	provider := NewPipelineProvider(tavily, NewPlanner(cfg.Pipeline), logger)
	readerCfg := readerConfigFromConfig(cfg.Pipeline.Reader)
	if readerCfg.Enabled {
		reader := NewTavilyExtractReader(TavilyExtractConfig{
			APIKey:         apiKey,
			BaseURL:        cfg.BaseURL,
			ExtractDepth:   readerCfg.ExtractDepth,
			Format:         readerCfg.Format,
			Timeout:        timeout,
			MaxCharsPerDoc: readerCfg.MaxCharsPerDoc,
			MaxChunkChars:  readerCfg.MaxChunkChars,
		}, logger)
		provider = NewReaderProvider(provider, reader, readerCfg)
	}
	reranker, warning := rerankerFromConfig(cfg.Pipeline.Rerank)
	if reranker != nil {
		provider = NewRerankProvider(provider, reranker, cfg.Pipeline.Rerank)
	}
	if warning != "" {
		provider = &warningProvider{provider: provider, warning: warning}
	}
	provider = &fetchGuidanceProvider{provider: provider}
	if logger != nil {
		provider = &observedProvider{provider: provider, logger: logger}
	}
	return provider
}

func readerConfigFromConfig(cfg config.WebSearchPipelineReaderConfig) ReaderConfig {
	if cfg.ExtractDepth == "" {
		cfg.ExtractDepth = "basic"
	}
	if cfg.Format == "" {
		cfg.Format = "markdown"
	}
	if cfg.TimeoutSec == 0 {
		cfg.TimeoutSec = 20
	}
	if cfg.MaxCharsPerDoc == 0 {
		cfg.MaxCharsPerDoc = 12000
	}
	if cfg.MaxChunkChars == 0 {
		cfg.MaxChunkChars = 2000
	}
	return ReaderConfig{
		Enabled:        cfg.Enabled,
		TopN:           cfg.TopN,
		ExtractDepth:   cfg.ExtractDepth,
		Format:         cfg.Format,
		TimeoutSec:     cfg.TimeoutSec,
		MaxCharsPerDoc: cfg.MaxCharsPerDoc,
		MaxChunkChars:  cfg.MaxChunkChars,
	}
}

func rerankerFromConfig(cfg config.WebSearchPipelineRerankConfig) (rerank.Provider, string) {
	if !cfg.Enabled || strings.EqualFold(cfg.Provider, "disabled") {
		return nil, ""
	}
	switch strings.ToLower(cfg.Provider) {
	case "siliconflow":
		apiKey := os.Getenv(cfg.APIKeyEnv)
		if strings.TrimSpace(apiKey) == "" {
			if strings.EqualFold(cfg.Fallback, "heuristic") {
				return rerank.NewHeuristicProvider(), "rerank fallback: heuristic used because siliconflow api key is unavailable"
			}
			return nil, "rerank disabled: siliconflow api key is unavailable"
		}
		model := cfg.Model
		if strings.TrimSpace(model) == "" || model == "heuristic" {
			model = "BAAI/bge-reranker-v2-m3"
		}
		timeout := time.Duration(cfg.TimeoutSec) * time.Second
		return rerank.NewSiliconFlowProvider(rerank.SiliconFlowConfig{
			APIKey:  apiKey,
			BaseURL: cfg.BaseURL,
			Path:    cfg.Path,
			Model:   model,
			Timeout: timeout,
		}), ""
	case "heuristic", "":
		return rerank.NewHeuristicProvider(), ""
	default:
		return nil, ""
	}
}

type warningProvider struct {
	provider Provider
	warning  string
}

func (p *warningProvider) Name() string { return p.provider.Name() }

func (p *warningProvider) Search(ctx context.Context, query string, opts Options) (*Response, error) {
	resp, err := p.provider.Search(ctx, query, opts)
	if err != nil || resp == nil || p.warning == "" {
		return resp, err
	}
	resp.Warnings = append(resp.Warnings, p.warning)
	return resp, nil
}

type observedProvider struct {
	provider Provider
	logger   *slog.Logger
}

func (p *observedProvider) Name() string { return p.provider.Name() }

func (p *observedProvider) Search(ctx context.Context, query string, opts Options) (*Response, error) {
	start := time.Now()
	resp, err := p.provider.Search(ctx, query, opts)
	usage := SearchUsage{}
	results := 0
	warnings := 0
	if resp != nil {
		results = len(resp.Results)
		warnings = len(resp.Warnings)
		if resp.Usage != nil {
			usage = *resp.Usage
		}
	}
	p.logger.InfoContext(ctx, "websearch pipeline completed",
		"provider", p.provider.Name(),
		"results", results,
		"warnings", warnings,
		"search_queries", usage.SearchQueries,
		"extract_urls", usage.ExtractURLs,
		"rerank_documents", usage.RerankDocuments,
		"duration_ms", time.Since(start).Milliseconds(),
		"error", err != nil,
	)
	return resp, err
}
