package builtin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

func defaultWebFetchCfg() config.WebFetchConfig {
	return config.WebFetchConfig{
		Enabled:      true,
		TimeoutSec:   5,
		MaxBytes:     1 << 20,
		MaxRedirects: 5,
		UserAgent:    "TestAgent/1.0",
	}
}

func TestWebFetch_HappyPathHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><p>Hello World</p></body></html>`))
	}))
	defer srv.Close()

	_, handler := NewWebFetchTool(defaultWebFetchCfg(), nil)
	input, _ := json.Marshal(map[string]string{"url": srv.URL})
	raw, err := handler(context.Background(), input)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var out struct {
		Status int    `json:"status"`
		Text   string `json:"text"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Status != 200 {
		t.Fatalf("status = %d, want 200", out.Status)
	}
	if !strings.Contains(out.Text, "Hello World") {
		t.Fatalf("text = %q, want to contain 'Hello World'", out.Text)
	}
}

func TestWebFetch_NonHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("plain text content"))
	}))
	defer srv.Close()

	_, handler := NewWebFetchTool(defaultWebFetchCfg(), nil)
	input, _ := json.Marshal(map[string]string{"url": srv.URL})
	raw, err := handler(context.Background(), input)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(out.Text, "plain text content") {
		t.Fatalf("text = %q", out.Text)
	}
}

func TestWebFetch_Truncation(t *testing.T) {
	big := strings.Repeat("x", 200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()

	cfg := defaultWebFetchCfg()
	cfg.MaxBytes = 50
	_, handler := NewWebFetchTool(cfg, nil)
	input, _ := json.Marshal(map[string]any{"url": srv.URL, "max_bytes": 50})
	raw, err := handler(context.Background(), input)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var out struct {
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Truncated {
		t.Fatal("expected truncated=true")
	}
}

func TestWebFetch_Redirect(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redir" {
			http.Redirect(w, r, srv.URL+"/dest", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("destination"))
	}))
	defer srv.Close()

	_, handler := NewWebFetchTool(defaultWebFetchCfg(), nil)
	input, _ := json.Marshal(map[string]string{"url": srv.URL + "/redir"})
	raw, err := handler(context.Background(), input)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var out struct {
		Text     string `json:"text"`
		FinalURL string `json:"final_url"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(out.Text, "destination") {
		t.Fatalf("text = %q, want 'destination'", out.Text)
	}
	if !strings.HasSuffix(out.FinalURL, "/dest") {
		t.Fatalf("final_url = %q, want to end with /dest", out.FinalURL)
	}
}

func TestWebFetch_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte("late"))
	}))
	defer srv.Close()

	cfg := defaultWebFetchCfg()
	cfg.TimeoutSec = 1
	_, handler := NewWebFetchTool(cfg, nil)
	input, _ := json.Marshal(map[string]string{"url": srv.URL})
	_, err := handler(context.Background(), input)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestWebFetch_InvalidScheme(t *testing.T) {
	_, handler := NewWebFetchTool(defaultWebFetchCfg(), nil)
	input, _ := json.Marshal(map[string]string{"url": "ftp://example.com"})
	if _, err := handler(context.Background(), input); err == nil {
		t.Fatal("expected error for non-http scheme")
	}
}

func TestHTMLToText_ScriptStripped(t *testing.T) {
	src := `<html><head><title>T</title></head><body><script>alert(1)</script><p>Visible</p></body></html>`
	result := htmlToText(src)
	if strings.Contains(result, "alert") {
		t.Fatalf("script content leaked into text: %q", result)
	}
	if !strings.Contains(result, "Visible") {
		t.Fatalf("visible text missing: %q", result)
	}
}
