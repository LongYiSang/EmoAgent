package tavily

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientPostJSONSendsBearerAuthAndJSON(t *testing.T) {
	var gotAuth string
	var gotContentType string
	var gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	client := NewClient(Config{
		APIKey:  "test-key",
		BaseURL: srv.URL + "/",
		Timeout: 5 * time.Second,
	}, nil)

	var out struct {
		OK bool `json:"ok"`
	}
	err := client.PostJSON(context.Background(), "/extract", map[string]string{"url": "https://example.com"}, &out, "extract")
	if err != nil {
		t.Fatalf("PostJSON: %v", err)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("Authorization = %q, want Bearer test-key", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotPath != "/extract" {
		t.Fatalf("path = %q, want /extract", gotPath)
	}
	if gotBody["url"] != "https://example.com" {
		t.Fatalf("body url = %#v, want requested URL", gotBody["url"])
	}
	if !out.OK {
		t.Fatal("decoded output OK = false, want true")
	}
}

func TestClientDefaultsBaseURL(t *testing.T) {
	client := NewClient(Config{APIKey: "key"}, nil)
	if client.BaseURL() != "https://api.tavily.com" {
		t.Fatalf("BaseURL = %q, want https://api.tavily.com", client.BaseURL())
	}
}

func TestClientPostJSONNon2xxIncludesOperationStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := NewClient(Config{
		APIKey:  "key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	}, nil)

	var out struct{}
	err := client.PostJSON(context.Background(), "/search", map[string]string{"query": "x"}, &out, "search")
	if err == nil {
		t.Fatal("expected non-2xx error")
	}
	if !strings.Contains(err.Error(), "search") || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("error = %q, want operation, status, and response body", err.Error())
	}
}

func TestClientPostJSONContextTimeout(t *testing.T) {
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer func() {
		<-done
		srv.Close()
	}()

	client := NewClient(Config{
		APIKey:  "key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	var out struct{}
	err := client.PostJSON(ctx, "/extract", map[string]string{"url": "https://example.com"}, &out, "extract")
	if err == nil {
		t.Fatal("expected context timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(context.DeadlineExceeded) = false for %v", err)
	}
}
