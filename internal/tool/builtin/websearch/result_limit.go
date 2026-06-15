package websearch

import "context"

type resultLimitProvider struct {
	provider Provider
}

func (p *resultLimitProvider) Name() string { return p.provider.Name() }

func (p *resultLimitProvider) Search(ctx context.Context, query string, opts Options) (*Response, error) {
	resp, err := p.provider.Search(ctx, query, opts)
	if err != nil || resp == nil {
		return resp, err
	}
	limit := opts.MaxResults
	if limit > 0 && len(resp.Results) > limit {
		resp.Results = resp.Results[:limit]
	}
	return resp, nil
}
