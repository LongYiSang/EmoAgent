package websearch

import (
	"context"
	"log/slog"
)

type pipelineProvider struct {
	search  Provider
	planner *Planner
	logger  *slog.Logger
}

func NewPipelineProvider(search Provider, planner *Planner, loggers ...*slog.Logger) Provider {
	if planner == nil {
		planner = NewPlanner()
	}
	var logger *slog.Logger
	if len(loggers) > 0 {
		logger = loggers[0]
	}
	return &pipelineProvider{search: search, planner: planner, logger: logger}
}

func (p *pipelineProvider) Name() string { return "pipeline" }

func (p *pipelineProvider) Search(ctx context.Context, query string, opts Options) (*Response, error) {
	planned := p.planner.Plan(query, opts)
	planned.MaxResults = p.candidateMaxResults(planned.MaxResults)
	if p.logger != nil {
		p.logger.DebugContext(ctx, "websearch pipeline executing", "provider", p.search.Name(), "depth", planned.SearchDepth, "profile", planned.Profile)
	}
	resp, err := p.search.Search(ctx, query, planned)
	if err != nil {
		return nil, err
	}
	ensureSearchUsage(resp).SearchQueries++
	resp.Results = CleanResults(resp.Results)
	resp.Provider = p.Name()
	resp.SearchMode = planned.SearchDepth
	return resp, nil
}

func (p *pipelineProvider) candidateMaxResults(requested int) int {
	target := requested
	if target < 0 {
		target = 0
	}
	if p.planner == nil {
		return target
	}
	cfg := p.planner.cfg
	if cfg.Search.CandidateCap > target {
		target = cfg.Search.CandidateCap
	}
	if cfg.Reader.Enabled && cfg.Reader.TopN > target {
		target = cfg.Reader.TopN
	}
	if cfg.Rerank.Enabled && cfg.Rerank.InputTopN > target {
		target = cfg.Rerank.InputTopN
	}
	if cfg.Rerank.Enabled && cfg.Rerank.TopK > target {
		target = cfg.Rerank.TopK
	}
	return target
}
