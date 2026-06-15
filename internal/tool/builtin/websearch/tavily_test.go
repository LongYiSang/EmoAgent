package websearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"log/slog"
	"os"
)

// newTestLogger returns a discard slog.Logger suitable for tests.
func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestTavilySearch_Success verifies that a valid Tavily response is parsed correctly.
func TestTavilySearch_Success(t *testing.T) {
	var reqBodyMap map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&reqBodyMap); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"query": "test query",
			"answer": "test answer",
			"results": [
				{"title": "Title1", "url": "https://example.com", "content": "Snippet1", "score": 0.9}
			]
		}`))
	}))
	defer srv.Close()

	provider := NewTavilyProvider(TavilyConfig{
		APIKey:        "testkey",
		BaseURL:       srv.URL,
		IncludeAnswer: true,
	}, newTestLogger())

	resp, err := provider.Search(context.Background(), "test query", Options{MaxResults: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, hasAPIKey := reqBodyMap["api_key"]; hasAPIKey {
		t.Error("request body must NOT contain api_key field, but it does")
	}
	if reqBodyMap["search_depth"] != "basic" {
		t.Errorf("search_depth = %v, want basic", reqBodyMap["search_depth"])
	}
	if reqBodyMap["max_results"] != float64(5) {
		t.Errorf("max_results = %v, want 5", reqBodyMap["max_results"])
	}
	if reqBodyMap["include_answer"] != true {
		t.Errorf("include_answer = %v, want true", reqBodyMap["include_answer"])
	}

	// Assert result fields.
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	r := resp.Results[0]
	if r.Title != "Title1" {
		t.Errorf("Title = %q, want %q", r.Title, "Title1")
	}
	if r.URL != "https://example.com" {
		t.Errorf("URL = %q, want %q", r.URL, "https://example.com")
	}
	if r.Snippet != "Snippet1" {
		t.Errorf("Snippet = %q, want %q", r.Snippet, "Snippet1")
	}
	if r.Score != 0.9 {
		t.Errorf("Score = %v, want 0.9", r.Score)
	}
	if resp.Answer != "test answer" {
		t.Errorf("Answer = %q, want %q", resp.Answer, "test answer")
	}
	if resp.Query != "test query" {
		t.Errorf("Query = %q, want %q", resp.Query, "test query")
	}
}

func TestTavilySearch_ForwardsOptions(t *testing.T) {
	var reqBodyMap map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&reqBodyMap); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"query":"test query","results":[]}`))
	}))
	defer srv.Close()

	provider := NewTavilyProvider(TavilyConfig{
		APIKey:  "testkey",
		BaseURL: srv.URL,
	}, newTestLogger())

	opts := Options{
		MaxResults:     7,
		SearchDepth:    "advanced",
		IncludeDomains: []string{"example.com", "docs.example.com"},
		ExcludeDomains: []string{"old.example.com"},
	}
	setOptionString(t, &opts, "Topic", "news")
	setOptionString(t, &opts, "TimeRange", "week")
	setOptionString(t, &opts, "StartDate", "2026-01-01")
	setOptionString(t, &opts, "EndDate", "2026-01-31")
	setOptionBool(t, &opts, "ExactMatch", true)

	_, err := provider.Search(context.Background(), "test query", opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, hasAPIKey := reqBodyMap["api_key"]; hasAPIKey {
		t.Error("request body must NOT contain api_key field, but it does")
	}
	if reqBodyMap["search_depth"] != "advanced" {
		t.Errorf("search_depth = %v, want advanced", reqBodyMap["search_depth"])
	}
	if reqBodyMap["max_results"] != float64(7) {
		t.Errorf("max_results = %v, want 7", reqBodyMap["max_results"])
	}
	assertStringSliceField(t, reqBodyMap, "include_domains", []string{"example.com", "docs.example.com"})
	assertStringSliceField(t, reqBodyMap, "exclude_domains", []string{"old.example.com"})
	if reqBodyMap["topic"] != "news" {
		t.Fatalf("topic = %#v, want news", reqBodyMap["topic"])
	}
	if reqBodyMap["time_range"] != "week" {
		t.Fatalf("time_range = %#v, want week", reqBodyMap["time_range"])
	}
	if reqBodyMap["start_date"] != "2026-01-01" {
		t.Fatalf("start_date = %#v, want 2026-01-01", reqBodyMap["start_date"])
	}
	if reqBodyMap["end_date"] != "2026-01-31" {
		t.Fatalf("end_date = %#v, want 2026-01-31", reqBodyMap["end_date"])
	}
	if reqBodyMap["exact_match"] != true {
		t.Fatalf("exact_match = %#v, want true", reqBodyMap["exact_match"])
	}
}

func assertStringSliceField(t *testing.T, body map[string]any, field string, want []string) {
	t.Helper()

	raw, ok := body[field].([]any)
	if !ok {
		t.Fatalf("%s = %#v, want string array", field, body[field])
	}
	if len(raw) != len(want) {
		t.Fatalf("%s length = %d, want %d", field, len(raw), len(want))
	}
	for i, v := range raw {
		got, ok := v.(string)
		if !ok {
			t.Fatalf("%s[%d] = %#v, want string", field, i, v)
		}
		if got != want[i] {
			t.Fatalf("%s[%d] = %q, want %q", field, i, got, want[i])
		}
	}
}

func setOptionString(t *testing.T, opts *Options, name, value string) {
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

func setOptionBool(t *testing.T, opts *Options, name string, value bool) {
	t.Helper()
	field := reflect.ValueOf(opts).Elem().FieldByName(name)
	if !field.IsValid() {
		return
	}
	if field.Kind() != reflect.Bool {
		t.Fatalf("Options.%s kind = %s, want bool", name, field.Kind())
	}
	field.SetBool(value)
}
