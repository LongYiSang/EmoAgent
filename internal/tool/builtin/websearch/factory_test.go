package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
)

func TestNewProviderRejectsUnsupportedProvider(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "test-key")
	_, err := NewProvider(config.WebSearchConfig{
		Enabled:   true,
		Provider:  "unknown",
		APIKeyEnv: "TAVILY_API_KEY",
	}, slog.Default())
	if err == nil || !strings.Contains(err.Error(), "unsupported websearch provider") {
		t.Fatalf("NewProvider error = %v, want unsupported provider", err)
	}
}

func TestNewProviderSupportsTavilyAndPipeline(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "test-key")

	tavily, err := NewProvider(config.WebSearchConfig{
		Enabled:   true,
		Provider:  "tavily",
		APIKeyEnv: "TAVILY_API_KEY",
	}, slog.Default())
	if err != nil {
		t.Fatalf("NewProvider tavily: %v", err)
	}
	if tavily.Name() != "tavily" {
		t.Fatalf("tavily provider name = %q, want tavily", tavily.Name())
	}

	pipeline, err := NewProvider(config.WebSearchConfig{
		Enabled:   true,
		Provider:  "pipeline",
		APIKeyEnv: "TAVILY_API_KEY",
		Pipeline:  config.WebSearchPipelineConfig{Enabled: true},
	}, slog.Default())
	if err != nil {
		t.Fatalf("NewProvider pipeline: %v", err)
	}
	if pipeline.Name() != "pipeline" {
		t.Fatalf("pipeline provider name = %q, want pipeline", pipeline.Name())
	}
}

func TestNewProviderPipelineAssemblesReaderWithTavilyExtract(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "test-key")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search":
			_, _ = w.Write([]byte(`{
				"query": "reader factory",
				"results": [
					{"title": "Title", "url": "https://example.com/a", "content": "Snippet", "score": 0.9}
				]
			}`))
		case "/extract":
			if _, hasAPIKey := body["api_key"]; hasAPIKey {
				t.Fatal("/extract request body must not contain api_key")
			}
			_, _ = w.Write([]byte(`{
				"results": [
					{"url": "https://example.com/a", "raw_content": "Factory evidence body."}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	provider, err := NewProvider(config.WebSearchConfig{
		Enabled:    true,
		Provider:   "pipeline",
		APIKeyEnv:  "TAVILY_API_KEY",
		BaseURL:    srv.URL,
		TimeoutSec: 5,
		Pipeline: config.WebSearchPipelineConfig{
			Enabled: true,
			Reader:  config.WebSearchPipelineReaderConfig{Enabled: true, TopN: 1},
		},
	}, slog.Default())
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	resp, err := provider.Search(context.Background(), "reader factory", Options{MaxResults: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 1 || len(resp.Results[0].Evidence) == 0 {
		t.Fatalf("results = %#v, want evidence from /extract", resp.Results)
	}
}

func TestNewProviderTavilyWithoutPipelineDoesNotUseReader(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "test-key")

	var extractCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search":
			_, _ = w.Write([]byte(`{
				"query": "legacy tavily",
				"results": [
					{"title": "Title", "url": "https://example.com/a", "content": "Snippet", "score": 0.9}
				]
			}`))
		case "/extract":
			extractCalled = true
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	provider, err := NewProvider(config.WebSearchConfig{
		Enabled:   true,
		Provider:  "tavily",
		APIKeyEnv: "TAVILY_API_KEY",
		BaseURL:   srv.URL,
		Pipeline: config.WebSearchPipelineConfig{
			Enabled: true,
			Reader:  config.WebSearchPipelineReaderConfig{Enabled: true, TopN: 1},
		},
	}, slog.Default())
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	resp, err := provider.Search(context.Background(), "legacy tavily", Options{MaxResults: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if extractCalled {
		t.Fatal("provider=tavily with pipeline disabled must not call /extract")
	}
	if len(resp.Results) != 1 || len(resp.Results[0].Evidence) != 0 {
		t.Fatalf("results = %#v, want legacy snippet-only result", resp.Results)
	}
}

func TestNewProviderPipelineDisabledFallsBackToTavily(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "test-key")

	var extractCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search":
			_, _ = w.Write([]byte(`{
				"query": "pipeline disabled",
				"results": [
					{"title": "Title", "url": "https://example.com/a", "content": "Snippet", "score": 0.9}
				]
			}`))
		case "/extract":
			extractCalled = true
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	provider, err := NewProvider(config.WebSearchConfig{
		Enabled:   true,
		Provider:  "pipeline",
		APIKeyEnv: "TAVILY_API_KEY",
		BaseURL:   srv.URL,
		Pipeline: config.WebSearchPipelineConfig{
			Enabled: false,
			Reader:  config.WebSearchPipelineReaderConfig{Enabled: true, TopN: 1},
			Rerank:  config.WebSearchPipelineRerankConfig{Enabled: true, Provider: "heuristic", InputTopN: 1},
		},
	}, slog.Default())
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if provider.Name() != "tavily" {
		t.Fatalf("provider name = %q, want tavily rollback path", provider.Name())
	}

	resp, err := provider.Search(context.Background(), "pipeline disabled", Options{MaxResults: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if extractCalled {
		t.Fatal("pipeline.enabled=false must not call /extract")
	}
	if len(resp.Results) != 1 || len(resp.Results[0].Evidence) != 0 || resp.Results[0].FinalScore != 0 {
		t.Fatalf("results = %#v, want legacy tavily output without pipeline fields", resp.Results)
	}
}

func TestNewProviderPipelineReaderDisabledMarksSnippetOnlyNeedsFetch(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "test-key")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/search" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{
			"query": "reader disabled",
			"results": [
				{"title": "Snippet", "url": "https://example.com/snippet", "content": "snippet only", "score": 0.8}
			]
		}`))
	}))
	defer srv.Close()

	provider, err := NewProvider(config.WebSearchConfig{
		Enabled:    true,
		Provider:   "pipeline",
		APIKeyEnv:  "TAVILY_API_KEY",
		BaseURL:    srv.URL,
		TimeoutSec: 5,
		Pipeline: config.WebSearchPipelineConfig{
			Enabled: true,
			Reader:  config.WebSearchPipelineReaderConfig{Enabled: false},
			Rerank:  config.WebSearchPipelineRerankConfig{Enabled: false, Provider: "disabled"},
		},
	}, slog.Default())
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	resp, err := provider.Search(context.Background(), "reader disabled", Options{MaxResults: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("results = %#v, want 1", resp.Results)
	}
	assertResultFetchGuidance(t, resp.Results[0], true, "web_fetch", "evidence")
}

func TestNewProviderPipelineReaderTopZeroDoesNotExtractAndMarksNeedsFetch(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "test-key")

	var extractCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search":
			_, _ = w.Write([]byte(`{
				"query": "top zero",
				"results": [
					{"title": "Snippet", "url": "https://example.com/snippet", "content": "snippet only", "score": 0.8}
				]
			}`))
		case "/extract":
			extractCalled = true
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	provider, err := NewProvider(config.WebSearchConfig{
		Enabled:    true,
		Provider:   "pipeline",
		APIKeyEnv:  "TAVILY_API_KEY",
		BaseURL:    srv.URL,
		TimeoutSec: 5,
		Pipeline: config.WebSearchPipelineConfig{
			Enabled: true,
			Reader:  config.WebSearchPipelineReaderConfig{Enabled: true, TopN: 0},
			Rerank:  config.WebSearchPipelineRerankConfig{Enabled: false, Provider: "disabled"},
		},
	}, slog.Default())
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	resp, err := provider.Search(context.Background(), "top zero", Options{MaxResults: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if extractCalled {
		t.Fatal("reader.top_n=0 must not call /extract")
	}
	assertResultFetchGuidance(t, resp.Results[0], true, "web_fetch", "evidence")
}

func TestNewProviderRerankDisabledStillFillsFinalScores(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "test-key")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/search" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{
			"query": "rerank disabled",
			"results": [
				{"title": "Snippet", "url": "https://example.com/snippet", "content": "snippet only", "score": 0.8}
			]
		}`))
	}))
	defer srv.Close()

	provider, err := NewProvider(config.WebSearchConfig{
		Enabled:    true,
		Provider:   "pipeline",
		APIKeyEnv:  "TAVILY_API_KEY",
		BaseURL:    srv.URL,
		TimeoutSec: 5,
		Pipeline: config.WebSearchPipelineConfig{
			Enabled: true,
			Reader:  config.WebSearchPipelineReaderConfig{Enabled: false},
			Rerank:  config.WebSearchPipelineRerankConfig{Enabled: false, Provider: "disabled"},
		},
	}, slog.Default())
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	resp, err := provider.Search(context.Background(), "rerank disabled", Options{MaxResults: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	got := resp.Results[0]
	if got.FinalScore == 0 || got.TrustScore == 0 || len(got.Reasons) == 0 {
		t.Fatalf("result = %#v, want final/trust score and reason when rerank disabled", got)
	}
}

func TestNewProviderPipelineAssemblesHeuristicRerank(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "test-key")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/search" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{
			"query": "phase 3 rerank",
			"results": [
				{"title": "High lexical", "url": "https://example.com/high", "content": "phase 3 rerank exact", "score": 0.2},
				{"title": "Old score", "url": "https://example.com/old", "content": "unrelated", "score": 0.9}
			]
		}`))
	}))
	defer srv.Close()

	provider, err := NewProvider(config.WebSearchConfig{
		Enabled:    true,
		Provider:   "pipeline",
		APIKeyEnv:  "TAVILY_API_KEY",
		BaseURL:    srv.URL,
		TimeoutSec: 5,
		Pipeline: config.WebSearchPipelineConfig{
			Enabled: true,
			Reader:  config.WebSearchPipelineReaderConfig{Enabled: false},
			Rerank:  config.WebSearchPipelineRerankConfig{Enabled: true, Provider: "heuristic", InputTopN: 2, TopK: 2},
		},
	}, slog.Default())
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	resp, err := provider.Search(context.Background(), "phase 3 rerank", Options{MaxResults: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("results = %#v, want 2", resp.Results)
	}
	if resp.Results[0].Title != "High lexical" {
		t.Fatalf("top result = %#v, want heuristic rerank to reorder", resp.Results[0])
	}
	if resp.Results[0].RerankScore == 0 || resp.Results[0].FinalScore == 0 || len(resp.Results[0].Reasons) == 0 {
		t.Fatalf("enhanced fields = %#v, want rerank_score/final_score/reasons", resp.Results[0])
	}
}

func TestNewProviderSiliconFlowRerankMissingEnvFallsBackToHeuristic(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "test-key")
	t.Setenv("SILICONFLOW_RERANK_API_KEY", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/search" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{
			"query": "siliconflow fallback",
			"results": [
				{"title": "Match", "url": "https://example.com/match", "content": "siliconflow fallback rerank", "score": 0.1},
				{"title": "No match", "url": "https://example.com/nomatch", "content": "other text", "score": 0.8}
			]
		}`))
	}))
	defer srv.Close()

	provider, err := NewProvider(config.WebSearchConfig{
		Enabled:    true,
		Provider:   "pipeline",
		APIKeyEnv:  "TAVILY_API_KEY",
		BaseURL:    srv.URL,
		TimeoutSec: 5,
		Pipeline: config.WebSearchPipelineConfig{
			Enabled: true,
			Reader:  config.WebSearchPipelineReaderConfig{Enabled: false},
			Rerank: config.WebSearchPipelineRerankConfig{
				Enabled:   true,
				Provider:  "siliconflow",
				Model:     "BAAI/bge-reranker-v2-m3",
				BaseURL:   "http://127.0.0.1:1",
				Path:      "/v1/rerank",
				APIKeyEnv: "SILICONFLOW_RERANK_API_KEY",
				Fallback:  "heuristic",
				InputTopN: 2,
				TopK:      2,
			},
		},
	}, slog.Default())
	if err != nil {
		t.Fatalf("NewProvider should not fail when siliconflow rerank env is missing: %v", err)
	}

	resp, err := provider.Search(context.Background(), "siliconflow fallback", Options{MaxResults: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 2 || resp.Results[0].Title != "Match" {
		t.Fatalf("results = %#v, want heuristic fallback without real network", resp.Results)
	}
	if !containsWarning(resp.Warnings, "rerank") || !containsWarning(resp.Warnings, "heuristic") {
		t.Fatalf("warnings = %#v, want rerank heuristic fallback warning", resp.Warnings)
	}
}

func TestNewProviderSiliconFlowMissingEnvWithFallbackDisabledDoesNotUseHeuristic(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "test-key")
	t.Setenv("SILICONFLOW_RERANK_API_KEY", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/search" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{
			"query": "fallback disabled",
			"results": [
				{"title": "No match high score", "url": "https://example.com/high", "content": "other text", "score": 0.9},
				{"title": "Match low score", "url": "https://example.com/low", "content": "fallback disabled query", "score": 0.1}
			]
		}`))
	}))
	defer srv.Close()

	provider, err := NewProvider(config.WebSearchConfig{
		Enabled:    true,
		Provider:   "pipeline",
		APIKeyEnv:  "TAVILY_API_KEY",
		BaseURL:    srv.URL,
		TimeoutSec: 5,
		Pipeline: config.WebSearchPipelineConfig{
			Enabled: true,
			Reader:  config.WebSearchPipelineReaderConfig{Enabled: false},
			Rerank: config.WebSearchPipelineRerankConfig{
				Enabled:   true,
				Provider:  "siliconflow",
				Model:     "BAAI/bge-reranker-v2-m3",
				APIKeyEnv: "SILICONFLOW_RERANK_API_KEY",
				Fallback:  "disabled",
				InputTopN: 2,
				TopK:      2,
			},
		},
	}, slog.Default())
	if err != nil {
		t.Fatalf("NewProvider should not fail when siliconflow key is missing: %v", err)
	}

	resp, err := provider.Search(context.Background(), "fallback disabled query", Options{MaxResults: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := resp.Results[0].Title; got != "No match high score" {
		t.Fatalf("top result = %q, want original order without heuristic fallback", got)
	}
	if containsWarning(resp.Warnings, "heuristic") {
		t.Fatalf("warnings = %#v, fallback disabled must not report heuristic", resp.Warnings)
	}
}

func TestPipelineProviderLogsStageUsageWithoutQuery(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "test-key")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search":
			_, _ = w.Write([]byte(`{
				"query": "private query text",
				"results": [
					{"title": "Match", "url": "https://example.com/match", "content": "private query text", "score": 0.8}
				]
			}`))
		case "/extract":
			_, _ = w.Write([]byte(`{
				"results": [
					{"url": "https://example.com/match", "raw_content": "reader evidence"}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	handler := &recordingSlogHandler{}
	provider, err := NewProvider(config.WebSearchConfig{
		Enabled:    true,
		Provider:   "pipeline",
		APIKeyEnv:  "TAVILY_API_KEY",
		BaseURL:    srv.URL,
		TimeoutSec: 5,
		Pipeline: config.WebSearchPipelineConfig{
			Enabled: true,
			Reader:  config.WebSearchPipelineReaderConfig{Enabled: true, TopN: 1},
			Rerank:  config.WebSearchPipelineRerankConfig{Enabled: true, Provider: "heuristic", InputTopN: 1, TopK: 1},
		},
	}, slog.New(handler))
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	_, err = provider.Search(context.Background(), "private query text", Options{MaxResults: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	attrs, ok := handler.attrsForMessage("websearch pipeline completed")
	if !ok {
		t.Fatalf("logs = %#v, want websearch pipeline completed", handler.records)
	}
	for _, key := range []string{"results", "search_queries", "extract_urls", "rerank_documents", "duration_ms"} {
		if !slices.Contains(attrs.keys(), key) {
			t.Fatalf("log attrs = %#v, missing %s", attrs, key)
		}
	}
	if handler.containsValue("private query text") || handler.containsKey("query") {
		t.Fatalf("logs leaked query: %#v", handler.records)
	}
}

func TestPipelineProviderLogsStageUsageWhenRerankDisabled(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "test-key")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/search" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{
			"query": "private no rerank",
			"results": [
				{"title": "Match", "url": "https://example.com/match", "content": "snippet", "score": 0.8}
			]
		}`))
	}))
	defer srv.Close()

	handler := &recordingSlogHandler{}
	provider, err := NewProvider(config.WebSearchConfig{
		Enabled:    true,
		Provider:   "pipeline",
		APIKeyEnv:  "TAVILY_API_KEY",
		BaseURL:    srv.URL,
		TimeoutSec: 5,
		Pipeline: config.WebSearchPipelineConfig{
			Enabled: true,
			Reader:  config.WebSearchPipelineReaderConfig{Enabled: false},
			Rerank:  config.WebSearchPipelineRerankConfig{Enabled: false, Provider: "disabled"},
		},
	}, slog.New(handler))
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	_, err = provider.Search(context.Background(), "private no rerank", Options{MaxResults: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if _, ok := handler.attrsForMessage("websearch pipeline completed"); !ok {
		t.Fatalf("logs = %#v, want websearch pipeline completed without rerank", handler.records)
	}
	if handler.containsValue("private no rerank") || handler.containsKey("query") {
		t.Fatalf("logs leaked query: %#v", handler.records)
	}
}

type recordingSlogHandler struct {
	records []recordedLog
	attrs   []slog.Attr
}

type recordedLog struct {
	Message string
	Attrs   logAttrs
}

type logAttrs map[string]any

func (h *recordingSlogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingSlogHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := logAttrs{}
	for _, attr := range h.attrs {
		attrs[attr.Key] = attr.Value.Any()
	}
	r.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.Any()
		return true
	})
	h.records = append(h.records, recordedLog{Message: r.Message, Attrs: attrs})
	return nil
}

func (h *recordingSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &next
}

func (h *recordingSlogHandler) WithGroup(string) slog.Handler { return h }

func (h *recordingSlogHandler) attrsForMessage(message string) (logAttrs, bool) {
	for _, record := range h.records {
		if record.Message == message {
			return record.Attrs, true
		}
	}
	return nil, false
}

func (h *recordingSlogHandler) containsValue(value string) bool {
	for _, record := range h.records {
		if strings.Contains(record.Message, value) {
			return true
		}
		for _, got := range record.Attrs {
			if strings.Contains(strings.TrimSpace(toLogString(got)), value) {
				return true
			}
		}
	}
	return false
}

func (h *recordingSlogHandler) containsKey(key string) bool {
	for _, record := range h.records {
		if _, ok := record.Attrs[key]; ok {
			return true
		}
	}
	return false
}

func (a logAttrs) keys() []string {
	keys := make([]string, 0, len(a))
	for key := range a {
		keys = append(keys, key)
	}
	return keys
}

func toLogString(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
