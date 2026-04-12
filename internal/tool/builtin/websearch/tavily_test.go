package websearch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"log/slog"
	"os"
)

// newTestLogger returns a discard slog.Logger suitable for tests.
func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestTavilySearch_Success verifies that a valid Tavily response is parsed correctly
// and that the HTTP request uses Bearer auth (not api_key in body).
func TestTavilySearch_Success(t *testing.T) {
	const testAPIKey = "testkey"

	// Track what the server observed.
	var gotAuthHeader string
	var reqBodyMap map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")

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
		APIKey:        testAPIKey,
		BaseURL:       srv.URL,
		IncludeAnswer: true,
	}, newTestLogger())

	resp, err := provider.Search(context.Background(), "test query", Options{MaxResults: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert Authorization header uses Bearer scheme.
	wantAuth := "Bearer " + testAPIKey
	if gotAuthHeader != wantAuth {
		t.Errorf("Authorization header = %q, want %q", gotAuthHeader, wantAuth)
	}

	// Assert request body does NOT contain api_key field.
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

// TestTavilySearch_Non2xx verifies that a non-2xx HTTP response produces an error
// whose message contains the status code.
func TestTavilySearch_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	provider := NewTavilyProvider(TavilyConfig{
		APIKey:  "badkey",
		BaseURL: srv.URL,
	}, newTestLogger())

	_, err := provider.Search(context.Background(), "query", Options{})
	if err == nil {
		t.Fatal("expected error for 401 response, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error message %q does not contain \"401\"", err.Error())
	}
}

// TestTavilySearch_ContextTimeout verifies that a hanging server causes the
// request to fail with an error that wraps context.DeadlineExceeded.
func TestTavilySearch_ContextTimeout(t *testing.T) {
	// done is closed when the handler exits so srv.Close() does not block.
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		// Hang until either the client disconnects or a safety timeout elapses.
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer func() {
		<-done      // wait for handler to exit before closing
		srv.Close()
	}()

	provider := NewTavilyProvider(TavilyConfig{
		APIKey:  "anykey",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second, // larger than context deadline — context wins
	}, newTestLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	_, err := provider.Search(ctx, "query", Options{})
	if err == nil {
		t.Fatal("expected error due to context timeout, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected errors.Is(err, context.DeadlineExceeded) to be true, got err = %v", err)
	}
}
