package rerank

import (
	"context"
	"reflect"
	"testing"
)

type contractProvider struct {
	got Request
}

func (p *contractProvider) Name() string { return "contract" }

func (p *contractProvider) Rerank(_ context.Context, req Request) (Response, error) {
	p.got = req
	return Response{
		Results: []Result{
			{ID: "doc-b", Index: 1, Score: 0.91},
			{ID: "doc-a", Index: 0, Score: 0.42},
		},
		Usage: Usage{Documents: len(req.Documents)},
	}, nil
}

func TestProviderContractCarriesRequestDocumentsAndUsage(t *testing.T) {
	var provider Provider = &contractProvider{}

	resp, err := provider.Rerank(context.Background(), Request{
		Query: "phase 3 rerank",
		Model: "demo-reranker",
		TopK:  2,
		Documents: []Document{
			{ID: "doc-a", Index: 0, Text: "alpha text", Metadata: map[string]string{"url": "https://example.com/a"}},
			{ID: "doc-b", Index: 1, Text: "phase 3 rerank details", Metadata: map[string]string{"url": "https://example.com/b"}},
		},
	})
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}

	got := provider.(*contractProvider).got
	if got.Query != "phase 3 rerank" || got.Model != "demo-reranker" || got.TopK != 2 {
		t.Fatalf("request scalar fields = %#v, want query/model/top_k forwarded", got)
	}
	if len(got.Documents) != 2 {
		t.Fatalf("documents = %#v, want 2", got.Documents)
	}
	if got.Documents[1].ID != "doc-b" || got.Documents[1].Index != 1 || got.Documents[1].Text == "" {
		t.Fatalf("document contract = %#v, want ID/Index/Text", got.Documents[1])
	}
	if !reflect.DeepEqual(got.Documents[0].Metadata, map[string]string{"url": "https://example.com/a"}) {
		t.Fatalf("metadata = %#v, want preserved", got.Documents[0].Metadata)
	}
	if provider.Name() != "contract" {
		t.Fatalf("Name = %q, want contract", provider.Name())
	}
	if len(resp.Results) != 2 {
		t.Fatalf("results = %#v, want 2", resp.Results)
	}
	if resp.Results[0].ID != "doc-b" || resp.Results[0].Index != 1 || resp.Results[0].Score <= resp.Results[1].Score {
		t.Fatalf("result contract = %#v, want ID/Index/Score sorted by score", resp.Results)
	}
	if resp.Usage.Documents != 2 {
		t.Fatalf("usage.documents = %d, want 2", resp.Usage.Documents)
	}
}
