package websearch

import (
	"context"
	"strings"
)

type fetchGuidanceProvider struct {
	provider Provider
}

func (p *fetchGuidanceProvider) Name() string { return p.provider.Name() }

func (p *fetchGuidanceProvider) Search(ctx context.Context, query string, opts Options) (*Response, error) {
	resp, err := p.provider.Search(ctx, query, opts)
	if err != nil || resp == nil {
		return resp, err
	}
	applyFetchGuidance(resp.Results, "")
	fillFinalScores(resp.Results)
	return resp, nil
}

func applyFetchGuidance(results []Result, fallbackReason string) {
	for i := range results {
		applyResultFetchGuidance(&results[i], fallbackReason)
	}
}

func applyResultFetchGuidance(result *Result, fallbackReason string) {
	if result == nil {
		return
	}
	reason := strings.TrimSpace(fallbackReason)
	if reason == "" {
		reason = fetchGuidanceReason(*result)
	}
	if reason == "" {
		result.NeedsFetch = false
		result.FetchHint = ""
		return
	}
	result.NeedsFetch = true
	result.FetchHint = "Use web_fetch for this URL if the answer depends on " + reason + "."
}

func fetchGuidanceReason(result Result) string {
	for _, warning := range result.Warnings {
		if strings.Contains(strings.ToLower(warning), "extract") {
			return "reader fallback evidence"
		}
	}
	if len(result.Evidence) == 0 {
		return "evidence not returned by the reader"
	}
	for _, chunk := range result.Evidence {
		if chunk.Truncated {
			return "truncated evidence"
		}
	}
	return ""
}
