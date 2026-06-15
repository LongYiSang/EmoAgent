package websearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestTavilyExtractReaderPostsExtractRequestAndReturnsEvidence(t *testing.T) {
	var reqBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/extract" {
			t.Fatalf("path = %s, want /extract", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [
				{"url": "https://example.com/a", "raw_content": "abcdefghijklmnopqrstuvwxyz"}
			],
			"failed_results": []
		}`))
	}))
	defer srv.Close()

	reader := NewTavilyExtractReader(TavilyExtractConfig{
		APIKey:         "test-key",
		BaseURL:        srv.URL,
		ExtractDepth:   "basic",
		Format:         "markdown",
		Timeout:        3 * time.Second,
		MaxCharsPerDoc: 12,
		MaxChunkChars:  5,
	}, newTestLogger())

	resp, err := reader.Extract(context.Background(), []string{"https://example.com/a"}, ExtractOptions{})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if _, hasAPIKey := reqBody["api_key"]; hasAPIKey {
		t.Fatal("request body must not contain api_key")
	}
	assertRequestArray(t, reqBody, "urls", []string{"https://example.com/a"})
	if reqBody["extract_depth"] != "basic" {
		t.Fatalf("extract_depth = %#v, want basic", reqBody["extract_depth"])
	}
	if reqBody["format"] != "markdown" {
		t.Fatalf("format = %#v, want markdown", reqBody["format"])
	}
	if reqBody["include_usage"] != true {
		t.Fatalf("include_usage = %#v, want true", reqBody["include_usage"])
	}
	if _, ok := reqBody["timeout"]; !ok {
		t.Fatalf("request missing timeout: %#v", reqBody)
	}

	got := resp.ByURL["https://example.com/a"]
	if len(got.Chunks) == 0 {
		t.Fatalf("chunks = %#v, want evidence", got.Chunks)
	}
	chunk := got.Chunks[0]
	if chunk.Text != "abcde" {
		t.Fatalf("chunk text = %q, want first max_chunk_chars text", chunk.Text)
	}
	if chunk.Source != "tavily_extract" {
		t.Fatalf("chunk source = %q, want tavily_extract", chunk.Source)
	}
	if !chunk.Truncated {
		t.Fatalf("chunk truncated = false, want true")
	}
}

func TestTavilyExtractReaderTreatsFailedAndEmptyResultsAsPerURLFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [
				{"url": "https://example.com/empty", "raw_content": ""}
			],
			"failed_results": [
				{"url": "https://example.com/fail", "error": "contains secret page body"}
			]
		}`))
	}))
	defer srv.Close()

	reader := NewTavilyExtractReader(TavilyExtractConfig{
		APIKey:        "test-key",
		BaseURL:       srv.URL,
		ExtractDepth:  "basic",
		Format:        "markdown",
		MaxChunkChars: 100,
	}, newTestLogger())

	resp, err := reader.Extract(context.Background(), []string{"https://example.com/empty", "https://example.com/fail"}, ExtractOptions{})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	for _, url := range []string{"https://example.com/empty", "https://example.com/fail"} {
		got := resp.ByURL[url]
		if got.Error == "" {
			t.Fatalf("%s error is empty, want per-URL failure", url)
		}
		if strings.Contains(got.Error, "contains secret page body") {
			t.Fatalf("per-URL error leaked Tavily raw failure text: %q", got.Error)
		}
		if len(got.Chunks) != 0 {
			t.Fatalf("%s chunks = %#v, want none", url, got.Chunks)
		}
	}
}

func TestTavilyExtractReaderTruncatesEvidenceWithoutBreakingUTF8(t *testing.T) {
	text, truncated := truncateEvidenceText("你好世界", 1, 1)
	if !truncated {
		t.Fatal("truncated = false, want true")
	}
	if text != "你" {
		t.Fatalf("text = %q, want first complete rune", text)
	}
	if !utf8.ValidString(text) {
		t.Fatalf("text is not valid UTF-8: %q", text)
	}
}

func assertRequestArray(t *testing.T, body map[string]any, field string, want []string) {
	t.Helper()
	raw, ok := body[field].([]any)
	if !ok {
		t.Fatalf("%s = %#v, want array", field, body[field])
	}
	if len(raw) != len(want) {
		t.Fatalf("%s len = %d, want %d", field, len(raw), len(want))
	}
	for i, v := range raw {
		if v != want[i] {
			t.Fatalf("%s[%d] = %#v, want %q", field, i, v, want[i])
		}
	}
}
