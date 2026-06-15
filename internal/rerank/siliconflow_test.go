package rerank

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSiliconFlowProviderUsesConfiguredEndpointAndMapsResults(t *testing.T) {
	const apiKey = "sf-secret"
	var gotPath, gotAuth string
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [
				{"index": 1, "document": {"text": "rerank query beta"}, "relevance_score": 0.93},
				{"index": 0, "document": {"text": "alpha"}, "relevance_score": 0.41}
			],
			"meta": {"tokens": {"input_tokens": 17, "output_tokens": 3, "image_tokens": 0}}
		}`))
	}))
	defer server.Close()

	provider := NewSiliconFlowProvider(SiliconFlowConfig{
		APIKey:  apiKey,
		BaseURL: server.URL,
		Path:    "/v1/rerank",
		Model:   "BAAI/bge-reranker-v2-m3",
		Timeout: time.Second,
	})

	resp, err := provider.Rerank(context.Background(), Request{
		Query: "rerank query",
		Documents: []Document{
			{ID: "doc-a", Index: 0, Text: "alpha"},
			{ID: "doc-b", Index: 1, Text: "rerank query beta"},
		},
	})
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}

	if provider.Name() != "siliconflow" {
		t.Fatalf("Name = %q, want siliconflow", provider.Name())
	}
	if gotPath != "/v1/rerank" {
		t.Fatalf("path = %q, want /v1/rerank", gotPath)
	}
	if gotAuth != "Bearer "+apiKey {
		t.Fatalf("Authorization = %q, want bearer header", gotAuth)
	}
	if _, ok := gotBody["api_key"]; ok {
		t.Fatalf("request body must not contain api_key: %#v", gotBody)
	}
	if gotBody["model"] != "BAAI/bge-reranker-v2-m3" || gotBody["query"] != "rerank query" {
		t.Fatalf("request body = %#v, want configured model/query", gotBody)
	}
	docs, ok := gotBody["documents"].([]any)
	if !ok || len(docs) != 2 || docs[0] != "alpha" || docs[1] != "rerank query beta" {
		t.Fatalf("documents = %#v, want SiliconFlow text array", gotBody["documents"])
	}
	if _, ok := docs[0].(map[string]any); ok {
		t.Fatalf("documents must not be object array: %#v", gotBody["documents"])
	}
	if _, ok := gotBody["top_k"]; ok {
		t.Fatalf("request body must use top_n, not top_k: %#v", gotBody)
	}
	if gotBody["top_n"] != nil {
		t.Fatalf("top_n should be omitted when TopK is not requested: %#v", gotBody)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("results = %#v, want 2", resp.Results)
	}
	if resp.Results[0].ID != "doc-b" || resp.Results[0].Index != 1 || resp.Results[0].Score != 0.93 {
		t.Fatalf("first result = %#v, want mapped ID/Index/Score", resp.Results[0])
	}
	if resp.Usage.Documents != 2 || resp.Usage.InputTokens != 17 || resp.Usage.TotalTokens != 20 {
		t.Fatalf("usage = %#v, want documents/input/total tokens", resp.Usage)
	}
}

func TestSiliconFlowProviderUsesTopNWhenRequested(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"index":0,"relevance_score":0.8}],"meta":{"tokens":{"input_tokens":5}}}`))
	}))
	defer server.Close()

	provider := NewSiliconFlowProvider(SiliconFlowConfig{
		APIKey:  "sf-secret",
		BaseURL: server.URL,
		Path:    "/rerank",
		Model:   "BAAI/bge-reranker-v2-m3",
	})

	_, err := provider.Rerank(context.Background(), Request{
		Query:     "query",
		TopK:      1,
		Documents: []Document{{ID: "doc-a", Index: 0, Text: "alpha"}},
	})
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if gotBody["top_n"] != float64(1) {
		t.Fatalf("request body = %#v, want top_n=1", gotBody)
	}
}

func TestSiliconFlowProviderErrorsDoNotLeakRawBodyOrAPIKey(t *testing.T) {
	const apiKey = "sf-secret"
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "non 2xx", status: http.StatusUnauthorized, body: `{"error":"bad key sf-secret"}`},
		{name: "parse error", status: http.StatusOK, body: `{"results":[`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			provider := NewSiliconFlowProvider(SiliconFlowConfig{
				APIKey:  apiKey,
				BaseURL: server.URL,
				Path:    "/rerank",
				Model:   "test-model",
				Timeout: time.Second,
			})
			_, err := provider.Rerank(context.Background(), Request{
				Query:     "q",
				Documents: []Document{{ID: "doc", Index: 0, Text: "text"}},
			})
			if err == nil {
				t.Fatal("Rerank error = nil, want error")
			}
			msg := err.Error()
			if strings.Contains(msg, apiKey) || strings.Contains(msg, tt.body) {
				t.Fatalf("error leaked sensitive raw response: %q", msg)
			}
		})
	}
}
