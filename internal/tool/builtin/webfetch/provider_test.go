package webfetch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/tool/builtin/tavily"
)

func defaultTestConfig() config.WebFetchConfig {
	return config.WebFetchConfig{
		Enabled:        true,
		Provider:       "direct",
		APIKeyEnv:      "TEST_TAVILY_KEY",
		BaseURL:        "https://api.tavily.com",
		TimeoutSec:     5,
		MaxBytes:       1 << 20,
		MaxRedirects:   5,
		UserAgent:      "TestAgent/1.0",
		ExtractDepth:   "basic",
		Format:         "markdown",
		IncludeFavicon: true,
	}
}

func TestNewProviderTavilyMissingKeyFallsBackToDirect(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Provider = "tavily"
	t.Setenv(cfg.APIKeyEnv, "")

	provider, err := NewProvider(cfg, nil)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if provider.Name() != "direct" {
		t.Fatalf("provider.Name() = %q, want direct", provider.Name())
	}
}

func TestNewProviderDirectDoesNotNeedKey(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Provider = "direct"
	t.Setenv(cfg.APIKeyEnv, "")

	provider, err := NewProvider(cfg, nil)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if provider.Name() != "direct" {
		t.Fatalf("provider.Name() = %q, want direct", provider.Name())
	}
}

func TestDirectFetchHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><p>Hello World</p></body></html>`))
	}))
	defer srv.Close()

	provider := NewDirectProvider(defaultTestConfig(), nil)
	resp, err := provider.Fetch(context.Background(), srv.URL, Options{MaxBytes: 1 << 20})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if resp.Provider != "direct" {
		t.Fatalf("Provider = %q, want direct", resp.Provider)
	}
	if resp.Status == nil || *resp.Status != http.StatusOK {
		t.Fatalf("Status = %#v, want 200", resp.Status)
	}
	if !strings.Contains(resp.Text, "Hello World") {
		t.Fatalf("Text = %q, want Hello World", resp.Text)
	}
}

func TestDirectFetchTruncation(t *testing.T) {
	big := strings.Repeat("x", 200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()

	provider := NewDirectProvider(defaultTestConfig(), nil)
	resp, err := provider.Fetch(context.Background(), srv.URL, Options{MaxBytes: 50})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !resp.Truncated {
		t.Fatal("expected truncated=true")
	}
	if len(resp.Text) != 50 {
		t.Fatalf("len(Text) = %d, want 50", len(resp.Text))
	}
}

func TestDirectFetchTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte("late"))
	}))
	defer srv.Close()

	cfg := defaultTestConfig()
	cfg.TimeoutSec = 1
	provider := NewDirectProvider(cfg, nil)
	_, err := provider.Fetch(context.Background(), srv.URL, Options{MaxBytes: cfg.MaxBytes})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestDirectFetchInvalidScheme(t *testing.T) {
	provider := NewDirectProvider(defaultTestConfig(), nil)
	if _, err := provider.Fetch(context.Background(), "ftp://example.com", Options{}); err == nil {
		t.Fatal("expected error for non-http scheme")
	}
}

func TestTavilyFetchSuccess(t *testing.T) {
	const apiKey = "test-key"

	var gotPath string
	var reqBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [
				{
					"url": "https://example.com/article",
					"raw_content": "# Article\nBody",
					"images": ["https://example.com/a.png"],
					"favicon": "https://example.com/favicon.ico"
				}
			],
			"failed_results": [],
			"response_time": 0.2,
			"usage": {"credits": 1},
			"request_id": "req-123"
		}`))
	}))
	defer srv.Close()

	cfg := defaultTestConfig()
	cfg.Provider = "tavily"
	cfg.BaseURL = srv.URL
	cfg.IncludeImages = true
	cfg.IncludeUsage = true
	provider := NewTavilyProvider(TavilyConfig{
		ExtractDepth:   cfg.ExtractDepth,
		Format:         cfg.Format,
		IncludeImages:  cfg.IncludeImages,
		IncludeFavicon: cfg.IncludeFavicon,
		IncludeUsage:   cfg.IncludeUsage,
		TimeoutSec:     cfg.TimeoutSec,
		Client: tavily.NewClient(tavily.Config{
			APIKey:  apiKey,
			BaseURL: cfg.BaseURL,
			Timeout: time.Duration(cfg.TimeoutSec) * time.Second,
		}, nil),
	}, nil)

	resp, err := provider.Fetch(context.Background(), "https://example.com/article", Options{MaxBytes: 1 << 20})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if gotPath != "/extract" {
		t.Fatalf("path = %q, want /extract", gotPath)
	}
	if reqBody["urls"] != "https://example.com/article" {
		t.Fatalf("urls = %#v, want requested URL", reqBody["urls"])
	}
	if reqBody["extract_depth"] != "basic" {
		t.Fatalf("extract_depth = %#v, want basic", reqBody["extract_depth"])
	}
	if reqBody["format"] != "markdown" {
		t.Fatalf("format = %#v, want markdown", reqBody["format"])
	}
	if reqBody["timeout"] != float64(5) {
		t.Fatalf("timeout = %#v, want 5", reqBody["timeout"])
	}
	if reqBody["include_images"] != true {
		t.Fatalf("include_images = %#v, want true", reqBody["include_images"])
	}
	if reqBody["include_favicon"] != true {
		t.Fatalf("include_favicon = %#v, want true", reqBody["include_favicon"])
	}
	if reqBody["include_usage"] != true {
		t.Fatalf("include_usage = %#v, want true", reqBody["include_usage"])
	}
	if resp.Provider != "tavily" {
		t.Fatalf("Provider = %q, want tavily", resp.Provider)
	}
	if resp.Text != "# Article\nBody" {
		t.Fatalf("Text = %q, want raw_content", resp.Text)
	}
	if resp.FinalURL != "https://example.com/article" {
		t.Fatalf("FinalURL = %q, want result URL", resp.FinalURL)
	}
	if resp.RequestID != "req-123" {
		t.Fatalf("RequestID = %q, want req-123", resp.RequestID)
	}
	if len(resp.Images) != 1 || resp.Images[0] != "https://example.com/a.png" {
		t.Fatalf("Images = %#v", resp.Images)
	}
	if resp.Favicon != "https://example.com/favicon.ico" {
		t.Fatalf("Favicon = %q", resp.Favicon)
	}
}

func TestTavilyFetchFailedOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [],
			"failed_results": [
				{"url": "https://example.com/missing", "error": "blocked"}
			],
			"request_id": "req-failed"
		}`))
	}))
	defer srv.Close()

	provider := NewTavilyProvider(TavilyConfig{
		ExtractDepth: "basic",
		Format:       "markdown",
		Client: tavily.NewClient(tavily.Config{
			APIKey:  "key",
			BaseURL: srv.URL,
			Timeout: 5 * time.Second,
		}, nil),
	}, nil)

	_, err := provider.Fetch(context.Background(), "https://example.com/missing", Options{MaxBytes: 1 << 20})
	if err == nil {
		t.Fatal("expected error for failed-only response")
	}
	if !strings.Contains(err.Error(), "blocked") || !strings.Contains(err.Error(), "req-failed") {
		t.Fatalf("error = %q, want failed reason and request_id", err.Error())
	}
}
