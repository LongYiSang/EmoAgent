package websearch

import "context"

type Reader interface {
	Name() string
	Extract(ctx context.Context, urls []string, opts ExtractOptions) (*ExtractResponse, error)
}

type ReaderConfig struct {
	Enabled        bool
	TopN           int
	ExtractDepth   string
	Format         string
	TimeoutSec     int
	MaxCharsPerDoc int
	MaxChunkChars  int
}

type ExtractOptions struct {
	ExtractDepth   string
	Format         string
	TimeoutSec     int
	MaxCharsPerDoc int
	MaxChunkChars  int
}

type ExtractResponse struct {
	ByURL map[string]ExtractResult
}

type ExtractResult struct {
	URL      string
	Chunks   []EvidenceChunk
	Error    string
	Warnings []string
}

type readerProvider struct {
	search Provider
	reader Reader
	cfg    ReaderConfig
}

func NewReaderProvider(search Provider, reader Reader, cfg ReaderConfig) Provider {
	return &readerProvider{search: search, reader: reader, cfg: cfg}
}

func (p *readerProvider) Name() string {
	if p.search == nil {
		return "reader"
	}
	return p.search.Name()
}

func (p *readerProvider) Search(ctx context.Context, query string, opts Options) (*Response, error) {
	resp, err := p.search.Search(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	if p.reader == nil || !p.cfg.Enabled || p.cfg.TopN == 0 || len(resp.Results) == 0 {
		applyFetchGuidance(resp.Results, "")
		return resp, nil
	}

	urls := topResultURLs(resp.Results, p.cfg.TopN)
	if len(urls) == 0 {
		return resp, nil
	}
	usage := ensureSearchUsage(resp)
	usage.ExtractURLs += len(urls)

	extracted, err := p.reader.Extract(ctx, urls, ExtractOptions{
		ExtractDepth:   p.cfg.ExtractDepth,
		Format:         p.cfg.Format,
		TimeoutSec:     p.cfg.TimeoutSec,
		MaxCharsPerDoc: p.cfg.MaxCharsPerDoc,
		MaxChunkChars:  p.cfg.MaxChunkChars,
	})
	if err != nil {
		resp.Warnings = append(resp.Warnings, "extract fallback: reader unavailable")
		applyFetchGuidance(resp.Results, "reader fallback evidence")
		return resp, nil
	}
	if extracted == nil {
		resp.Warnings = append(resp.Warnings, "extract fallback: reader returned no results")
		applyFetchGuidance(resp.Results, "reader fallback evidence")
		return resp, nil
	}

	for i := range resp.Results {
		url := resp.Results[i].URL
		result, ok := extracted.ByURL[url]
		if !ok {
			applyResultFetchGuidance(&resp.Results[i], "")
			continue
		}
		if result.Error != "" {
			resp.Results[i].Warnings = append(resp.Results[i].Warnings, "extract failed for this result")
			applyResultFetchGuidance(&resp.Results[i], "reader fallback evidence")
			continue
		}
		resp.Results[i].Evidence = append(resp.Results[i].Evidence, result.Chunks...)
		resp.Results[i].Warnings = append(resp.Results[i].Warnings, result.Warnings...)
		applyResultFetchGuidance(&resp.Results[i], "")
	}
	applyFetchGuidance(resp.Results, "")
	return resp, nil
}

func topResultURLs(results []Result, topN int) []string {
	limit := topN
	if limit < 0 || limit > len(results) {
		limit = len(results)
	}
	urls := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		if results[i].URL != "" {
			urls = append(urls, results[i].URL)
		}
	}
	return urls
}

func ensureSearchUsage(resp *Response) *SearchUsage {
	if resp.Usage == nil {
		resp.Usage = &SearchUsage{}
	}
	return resp.Usage
}
