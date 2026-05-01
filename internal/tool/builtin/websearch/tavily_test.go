package websearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
