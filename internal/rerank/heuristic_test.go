package rerank

import (
	"context"
	"testing"
)

func TestHeuristicRerankerRanksByQueryTextAndReturnsStableResultFields(t *testing.T) {
	provider := NewHeuristicProvider()

	resp, err := provider.Rerank(context.Background(), Request{
		Query: "siliconflow rerank adapter",
		TopK:  3,
		Documents: []Document{
			{ID: "one", Index: 0, Text: "unrelated weather forecast"},
			{ID: "two", Index: 1, Text: "SiliconFlow rerank API adapter guide"},
			{ID: "three", Index: 2, Text: "rerank adapter without provider name"},
		},
	})
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	if provider.Name() != "heuristic" {
		t.Fatalf("Name = %q, want heuristic", provider.Name())
	}
	if len(resp.Results) != 3 {
		t.Fatalf("results = %#v, want 3", resp.Results)
	}
	if resp.Results[0].ID != "two" || resp.Results[0].Index != 1 {
		t.Fatalf("top result = %#v, want matching document two/index 1", resp.Results[0])
	}
	if resp.Results[0].Score <= resp.Results[1].Score || resp.Results[1].Score <= resp.Results[2].Score {
		t.Fatalf("scores = %#v, want descending stable order", resp.Results)
	}
	if resp.Usage.Documents != 3 {
		t.Fatalf("usage.documents = %d, want 3", resp.Usage.Documents)
	}
}

func TestHeuristicRerankerPreservesOriginalOrderForTies(t *testing.T) {
	provider := NewHeuristicProvider()

	resp, err := provider.Rerank(context.Background(), Request{
		Query: "same",
		Documents: []Document{
			{ID: "a", Index: 0, Text: "same"},
			{ID: "b", Index: 1, Text: "same"},
			{ID: "c", Index: 2, Text: "same"},
		},
	})
	if err != nil {
		t.Fatalf("Rerank: %v", err)
	}
	for i, want := range []string{"a", "b", "c"} {
		if resp.Results[i].ID != want || resp.Results[i].Index != i {
			t.Fatalf("result %d = %#v, want id %s/index %d", i, resp.Results[i], want, i)
		}
	}
}
