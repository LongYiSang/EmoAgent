package websearch

import (
	"context"
	"reflect"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
)

type fakeSearchProvider struct {
	gotQuery string
	gotOpts  Options
}

func (f *fakeSearchProvider) Name() string { return "fake-search" }

func (f *fakeSearchProvider) Search(_ context.Context, query string, opts Options) (*Response, error) {
	f.gotQuery = query
	f.gotOpts = opts
	return &Response{
		Query: query,
		Results: []Result{
			{Title: "First", URL: "https://Example.com/a?utm_source=x#frag", Snippet: "one", Score: 0.9},
			{Title: "Duplicate", URL: "https://example.com/a", Snippet: "two", Score: 0.8},
		},
	}, nil
}

func TestPipelineProviderPlansOptionsAndCleansResults(t *testing.T) {
	base := &fakeSearchProvider{}
	provider := NewPipelineProvider(base, NewPlanner())

	opts := Options{MaxResults: 3}
	setPipelineStringOption(t, &opts, "Profile", "auto")
	resp, err := provider.Search(context.Background(), "Go 2026 release", opts)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if base.gotQuery != "Go 2026 release" {
		t.Fatalf("provider query = %q, want original query", base.gotQuery)
	}
	if base.gotOpts.SearchDepth != "advanced" {
		t.Fatalf("planned SearchDepth = %q, want advanced", base.gotOpts.SearchDepth)
	}
	assertPipelineStringOption(t, base.gotOpts, "Topic", "news")
	if resp.Query != "Go 2026 release" {
		t.Fatalf("response query = %q, want original query", resp.Query)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("len(results) = %d, want deduped 1: %#v", len(resp.Results), resp.Results)
	}
	got := resp.Results[0]
	if got.Title != "First" || got.URL != "https://example.com/a" || got.Snippet != "one" || got.Score != 0.9 {
		t.Fatalf("result fields = %#v, want legacy fields preserved with normalized URL", got)
	}
	if resp.Usage == nil || resp.Usage.SearchQueries != 1 {
		t.Fatalf("usage = %#v, want SearchQueries=1", resp.Usage)
	}
}

func TestPipelineReaderRerankPreservesResultContractWithFetchGuidance(t *testing.T) {
	const (
		completeURL  = "https://example.com/complete"
		truncatedURL = "https://example.com/truncated"
	)
	search := &fakeReaderSearchProvider{results: []Result{
		{Title: "Complete Evidence", URL: completeURL, Snippet: "complete snippet", Score: 0.91},
		{Title: "Needs Fetch", URL: truncatedURL, Snippet: "truncated snippet", Score: 0.82},
	}}
	reader := &fakeExtractReader{results: map[string]ExtractResult{
		completeURL: {
			URL: completeURL,
			Chunks: []EvidenceChunk{{
				Text:   "complete evidence",
				URL:    completeURL,
				Source: "fake_reader",
			}},
		},
		truncatedURL: {
			URL: truncatedURL,
			Chunks: []EvidenceChunk{{
				Text:      "partial evidence",
				URL:       truncatedURL,
				Source:    "fake_reader",
				Truncated: true,
			}},
		},
	}}

	pipeline := NewPipelineProvider(search, NewPlanner())
	readerStage := NewReaderProvider(pipeline, reader, ReaderConfig{Enabled: true, TopN: 2})
	reranker := &fakeRerankProvider{}
	provider := NewRerankProvider(readerStage, reranker, config.WebSearchPipelineRerankConfig{
		Enabled:     true,
		Model:       "fake-model",
		InputTopN:   2,
		TopK:        2,
		MaxDocChars: 200,
	})

	resp, err := provider.Search(context.Background(), "phase 4 result contract", Options{MaxResults: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("results = %#v, want 2", resp.Results)
	}

	got := resp.Results[0]
	if got.Title != "Needs Fetch" || got.URL != truncatedURL || got.Snippet != "truncated snippet" || got.Score != 0.82 {
		t.Fatalf("legacy fields = %#v, want title/url/snippet/score preserved after pipeline/rerank", got)
	}
	if len(got.Reasons) == 0 {
		t.Fatalf("reasons = %#v, want rerank reason", got.Reasons)
	}
	if len(got.Evidence) != 1 || !got.Evidence[0].Truncated {
		t.Fatalf("evidence = %#v, want truncated reader evidence preserved", got.Evidence)
	}
	assertResultFetchGuidance(t, got, true, "web_fetch", "truncated")
}

func setPipelineStringOption(t *testing.T, opts *Options, name, value string) {
	t.Helper()
	field := reflect.ValueOf(opts).Elem().FieldByName(name)
	if !field.IsValid() {
		return
	}
	if field.Kind() != reflect.String {
		t.Fatalf("Options.%s kind = %s, want string", name, field.Kind())
	}
	field.SetString(value)
}

func assertPipelineStringOption(t *testing.T, opts Options, name, want string) {
	t.Helper()
	field := reflect.ValueOf(opts).FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("Options missing %s", name)
	}
	if field.Kind() != reflect.String {
		t.Fatalf("Options.%s kind = %s, want string", name, field.Kind())
	}
	if got := field.String(); got != want {
		t.Fatalf("Options.%s = %q, want %q", name, got, want)
	}
}
