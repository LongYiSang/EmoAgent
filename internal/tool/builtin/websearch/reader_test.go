package websearch

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeReaderSearchProvider struct {
	results []Result
}

func (f *fakeReaderSearchProvider) Name() string { return "fake-search" }

func (f *fakeReaderSearchProvider) Search(_ context.Context, query string, _ Options) (*Response, error) {
	return &Response{Query: query, Results: append([]Result(nil), f.results...)}, nil
}

type fakeExtractReader struct {
	err      error
	failURL  string
	results  map[string]ExtractResult
	requests [][]string
}

func (f *fakeExtractReader) Name() string { return "fake-reader" }

func (f *fakeExtractReader) Extract(_ context.Context, urls []string, _ ExtractOptions) (*ExtractResponse, error) {
	f.requests = append(f.requests, append([]string(nil), urls...))
	if f.err != nil {
		return nil, f.err
	}
	resp := &ExtractResponse{ByURL: map[string]ExtractResult{}}
	for _, url := range urls {
		if result, ok := f.results[url]; ok {
			if result.URL == "" {
				result.URL = url
			}
			resp.ByURL[url] = result
			continue
		}
		if url == f.failURL {
			resp.ByURL[url] = ExtractResult{URL: url, Error: "extract failed"}
			continue
		}
		resp.ByURL[url] = ExtractResult{
			URL: url,
			Chunks: []EvidenceChunk{{
				Text:   "evidence for " + url,
				URL:    url,
				Source: "fake_reader",
			}},
		}
	}
	return resp, nil
}

func TestReaderProviderAddsEvidenceAndUsage(t *testing.T) {
	search := &fakeReaderSearchProvider{results: []Result{
		{Title: "one", URL: "https://example.com/1", Snippet: "snippet 1", Score: 0.9},
		{Title: "two", URL: "https://example.com/2", Snippet: "snippet 2", Score: 0.8},
		{Title: "three", URL: "https://example.com/3", Snippet: "snippet 3", Score: 0.7},
	}}
	reader := &fakeExtractReader{}
	provider := NewReaderProvider(search, reader, ReaderConfig{Enabled: true, TopN: 2})

	resp, err := provider.Search(context.Background(), "phase 2", Options{MaxResults: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(resp.Results))
	}
	if len(resp.Results[0].Evidence) != 1 || resp.Results[0].Evidence[0].Text == "" {
		t.Fatalf("first result evidence = %#v, want one chunk", resp.Results[0].Evidence)
	}
	if len(resp.Results[1].Evidence) != 1 {
		t.Fatalf("second result evidence = %#v, want one chunk", resp.Results[1].Evidence)
	}
	if len(resp.Results[2].Evidence) != 0 {
		t.Fatalf("third result evidence = %#v, want top_n limited snippet-only", resp.Results[2].Evidence)
	}
	if resp.Usage == nil || resp.Usage.ExtractURLs != 2 {
		t.Fatalf("usage = %#v, want ExtractURLs=2", resp.Usage)
	}
}

func TestReaderProviderMarksFetchGuidanceFromEvidenceState(t *testing.T) {
	const (
		completeURL  = "https://example.com/complete"
		truncatedURL = "https://example.com/truncated"
		missingURL   = "https://example.com/missing"
	)
	search := &fakeReaderSearchProvider{results: []Result{
		{Title: "complete", URL: completeURL, Snippet: "complete snippet", Score: 0.9},
		{Title: "truncated", URL: truncatedURL, Snippet: "truncated snippet", Score: 0.8},
		{Title: "missing", URL: missingURL, Snippet: "missing snippet", Score: 0.7},
	}}
	reader := &fakeExtractReader{results: map[string]ExtractResult{
		completeURL: {
			URL: completeURL,
			Chunks: []EvidenceChunk{{
				Text:   "complete evidence from the reader",
				URL:    completeURL,
				Source: "fake_reader",
			}},
		},
		truncatedURL: {
			URL: truncatedURL,
			Chunks: []EvidenceChunk{{
				Text:      "partial evidence from the reader",
				URL:       truncatedURL,
				Source:    "fake_reader",
				Truncated: true,
			}},
		},
		missingURL: {URL: missingURL},
	}}
	provider := NewReaderProvider(search, reader, ReaderConfig{Enabled: true, TopN: 3})

	resp, err := provider.Search(context.Background(), "phase 4 fetch guidance", Options{MaxResults: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 3 {
		t.Fatalf("results = %#v, want 3", resp.Results)
	}

	assertResultFetchGuidance(t, resp.Results[0], false)
	assertResultFetchGuidance(t, resp.Results[1], true, "web_fetch", "truncated")
	assertResultFetchGuidance(t, resp.Results[2], true, "web_fetch", "evidence")
}

func TestReaderProviderMarksTopNExcludedResultsAsNeedsFetch(t *testing.T) {
	search := &fakeReaderSearchProvider{results: []Result{
		{Title: "one", URL: "https://example.com/1", Snippet: "snippet 1", Score: 0.9},
		{Title: "two", URL: "https://example.com/2", Snippet: "snippet 2", Score: 0.8},
	}}
	reader := &fakeExtractReader{}
	provider := NewReaderProvider(search, reader, ReaderConfig{Enabled: true, TopN: 1})

	resp, err := provider.Search(context.Background(), "phase 4 topn guidance", Options{MaxResults: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	assertResultFetchGuidance(t, resp.Results[0], false)
	assertResultFetchGuidance(t, resp.Results[1], true, "web_fetch", "evidence")
}

func TestReaderProviderDisabledMarksSnippetOnlyAsNeedsFetch(t *testing.T) {
	search := &fakeReaderSearchProvider{results: []Result{
		{Title: "one", URL: "https://example.com/1", Snippet: "snippet 1", Score: 0.9},
	}}
	reader := &fakeExtractReader{}
	provider := NewReaderProvider(search, reader, ReaderConfig{Enabled: false, TopN: 1})

	resp, err := provider.Search(context.Background(), "phase 4 disabled reader", Options{MaxResults: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	assertResultFetchGuidance(t, resp.Results[0], true, "web_fetch", "evidence")
}

func TestReaderProviderFallsBackToSnippetOnlyWhenReaderFails(t *testing.T) {
	search := &fakeReaderSearchProvider{results: []Result{
		{Title: "one", URL: "https://example.com/1", Snippet: "snippet 1", Score: 0.9},
		{Title: "two", URL: "https://example.com/2", Snippet: "snippet 2", Score: 0.8},
	}}
	reader := &fakeExtractReader{err: errors.New("reader unavailable")}
	provider := NewReaderProvider(search, reader, ReaderConfig{Enabled: true, TopN: 2})

	resp, err := provider.Search(context.Background(), "phase 2", Options{MaxResults: 2})
	if err != nil {
		t.Fatalf("Search returned error on reader failure: %v", err)
	}
	for i, result := range resp.Results {
		if len(result.Evidence) != 0 {
			t.Fatalf("result %d evidence = %#v, want snippet-only", i, result.Evidence)
		}
		assertResultFetchGuidance(t, result, true, "web_fetch", "reader")
	}
	if !containsWarning(resp.Warnings, "extract") || !containsWarning(resp.Warnings, "fallback") {
		t.Fatalf("warnings = %#v, want extract fallback warning", resp.Warnings)
	}
}

func TestReaderProviderKeepsSingleURLFailureSnippetOnly(t *testing.T) {
	failedURL := "https://example.com/2"
	search := &fakeReaderSearchProvider{results: []Result{
		{Title: "one", URL: "https://example.com/1", Snippet: "snippet 1", Score: 0.9},
		{Title: "two", URL: failedURL, Snippet: "snippet 2", Score: 0.8},
	}}
	reader := &fakeExtractReader{failURL: failedURL}
	provider := NewReaderProvider(search, reader, ReaderConfig{Enabled: true, TopN: 2})

	resp, err := provider.Search(context.Background(), "phase 2", Options{MaxResults: 2})
	if err != nil {
		t.Fatalf("Search returned error on single URL failure: %v", err)
	}
	if len(resp.Results[0].Evidence) != 1 {
		t.Fatalf("first result evidence = %#v, want one chunk", resp.Results[0].Evidence)
	}
	if len(resp.Results[1].Evidence) != 0 {
		t.Fatalf("failed result evidence = %#v, want snippet-only", resp.Results[1].Evidence)
	}
	if len(resp.Results[1].Warnings) == 0 && len(resp.Warnings) == 0 {
		t.Fatalf("want per-result or response warning for single URL failure")
	}
}

func containsWarning(warnings []string, part string) bool {
	for _, warning := range warnings {
		if strings.Contains(strings.ToLower(warning), strings.ToLower(part)) {
			return true
		}
	}
	return false
}
