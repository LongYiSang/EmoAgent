package websearch

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/rerank"
)

type fakeRerankSearchProvider struct {
	resp *Response
}

func (f *fakeRerankSearchProvider) Name() string { return "fake-search" }

func (f *fakeRerankSearchProvider) Search(_ context.Context, query string, _ Options) (*Response, error) {
	if f.resp != nil {
		out := *f.resp
		out.Query = query
		out.Results = append([]Result(nil), f.resp.Results...)
		out.Warnings = append([]string(nil), f.resp.Warnings...)
		return &out, nil
	}
	return &Response{Query: query}, nil
}

type fakeRerankProvider struct {
	err error
	got rerank.Request
}

func (f *fakeRerankProvider) Name() string { return "fake-reranker" }

func (f *fakeRerankProvider) Rerank(_ context.Context, req rerank.Request) (rerank.Response, error) {
	f.got = req
	if f.err != nil {
		return rerank.Response{}, f.err
	}
	return rerank.Response{
		Results: []rerank.Result{
			{ID: "2", Index: 1, Score: 0.95},
			{ID: "0", Index: 0, Score: 0.40},
			{ID: "1", Index: 2, Score: 0.10},
		},
		Usage: rerank.Usage{Documents: len(req.Documents)},
	}, nil
}

func TestRerankProviderReordersAndFusesScores(t *testing.T) {
	search := &fakeRerankSearchProvider{resp: &Response{Results: []Result{
		{Title: "Alpha", URL: "https://example.com/a", Snippet: "alpha only", Score: 0.9},
		{Title: "Beta", URL: "https://example.com/b", Snippet: "beta only", Score: 0.8},
		{Title: "Gamma", URL: "https://example.com/c", Snippet: "answer about rerank", Score: 0.3},
	}}}
	reranker := &fakeRerankProvider{}
	provider := NewRerankProvider(search, reranker, config.WebSearchPipelineRerankConfig{
		Enabled:     true,
		Model:       "fake-model",
		InputTopN:   3,
		TopK:        3,
		MaxDocChars: 200,
	})

	resp, err := provider.Search(context.Background(), "rerank answer", Options{MaxResults: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(reranker.got.Documents) != 3 {
		t.Fatalf("rerank documents = %#v, want all search results", reranker.got.Documents)
	}
	if reranker.got.Query != "rerank answer" || reranker.got.Model != "fake-model" || reranker.got.TopK != 3 {
		t.Fatalf("rerank request = %#v, want query/model/top_k", reranker.got)
	}
	if resp.Results[0].Title != "Beta" || resp.Results[0].URL != "https://example.com/b" {
		t.Fatalf("top result = %#v, want reranked Beta", resp.Results[0])
	}
	if resp.Results[0].RerankScore != 0.95 {
		t.Fatalf("rerank_score = %v, want 0.95", resp.Results[0].RerankScore)
	}
	if resp.Results[0].FinalScore <= resp.Results[1].FinalScore {
		t.Fatalf("final scores = %#v, want fused descending order", resp.Results)
	}
	if resp.Results[0].TrustScore <= 0 || len(resp.Results[0].Reasons) == 0 {
		t.Fatalf("enhanced fields = %#v, want trust_score and reasons", resp.Results[0])
	}
	if resp.Usage == nil || resp.Usage.RerankDocuments != 3 {
		t.Fatalf("usage = %#v, want RerankDocuments=3", resp.Usage)
	}
}

func TestRerankProviderFallsBackWhenRerankerFails(t *testing.T) {
	search := &fakeRerankSearchProvider{resp: &Response{Results: []Result{
		{Title: "Low lexical", URL: "https://example.com/low", Snippet: "unrelated", Score: 0.9},
		{Title: "Query match", URL: "https://example.com/match", Snippet: "phase 3 rerank fallback", Score: 0.1},
	}}}
	reranker := &fakeRerankProvider{err: errors.New("remote rerank down")}
	provider := NewRerankProvider(search, reranker, config.WebSearchPipelineRerankConfig{
		Enabled:  true,
		Fallback: "heuristic",
		TopK:     2,
	})

	resp, err := provider.Search(context.Background(), "phase 3 rerank", Options{MaxResults: 2})
	if err != nil {
		t.Fatalf("Search returned error on reranker failure: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("results = %#v, want 2", resp.Results)
	}
	if !containsWarning(resp.Warnings, "rerank") || !containsWarning(resp.Warnings, "fallback") {
		t.Fatalf("warnings = %#v, want rerank fallback warning", resp.Warnings)
	}
	if resp.Results[0].Title != "Query match" || resp.Results[0].FinalScore == 0 {
		t.Fatalf("fallback result = %#v, want heuristic fallback or available final score", resp.Results[0])
	}
	if !containsReason(resp.Results[0].Reasons, "heuristic") || containsReason(resp.Results[0].Reasons, "fake-reranker") {
		t.Fatalf("fallback reasons = %#v, want actual heuristic provider", resp.Results[0].Reasons)
	}
}

func TestRerankProviderIncludesSnippetOnlyResultsAfterReaderWarnings(t *testing.T) {
	search := &fakeRerankSearchProvider{resp: &Response{
		Warnings: []string{"extract fallback: reader failed"},
		Results: []Result{
			{Title: "Snippet only", URL: "https://example.com/snippet", Snippet: "rerank this snippet", Score: 0.5},
		},
	}}
	reranker := &fakeRerankProvider{}
	provider := NewRerankProvider(search, reranker, config.WebSearchPipelineRerankConfig{Enabled: true, InputTopN: 1, TopK: 1})

	resp, err := provider.Search(context.Background(), "rerank snippet", Options{MaxResults: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(reranker.got.Documents) != 1 {
		t.Fatalf("rerank documents = %#v, want snippet-only result included", reranker.got.Documents)
	}
	if !strings.Contains(reranker.got.Documents[0].Text, "rerank this snippet") {
		t.Fatalf("rerank document text = %q, want snippet text", reranker.got.Documents[0].Text)
	}
	if !reflect.DeepEqual(resp.Warnings, []string{"extract fallback: reader failed"}) {
		t.Fatalf("warnings = %#v, want upstream reader warning preserved", resp.Warnings)
	}
}

func containsReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if strings.Contains(reason, want) {
			return true
		}
	}
	return false
}
