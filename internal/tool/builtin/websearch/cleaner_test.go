package websearch

import "testing"

func TestCleanResultsNormalizesURLAndDeduplicates(t *testing.T) {
	results := []Result{
		{
			Title:   "Go release",
			URL:     "https://Go.dev/doc/?utm_source=newsletter&ref=home#install",
			Snippet: "first",
			Score:   0.9,
		},
		{
			Title:   "Go release duplicate",
			URL:     "https://go.dev/doc/",
			Snippet: "duplicate",
			Score:   0.8,
		},
		{
			Title:   "Tracked",
			URL:     "https://Example.com/path?fbclid=abc&gclid=def&keep=1#top",
			Snippet: "tracked",
			Score:   0.7,
		},
	}

	got := CleanResults(results)
	if len(got) != 2 {
		t.Fatalf("len(CleanResults) = %d, want 2: %#v", len(got), got)
	}
	if got[0].URL != "https://go.dev/doc/" {
		t.Fatalf("first URL = %q, want normalized go.dev URL", got[0].URL)
	}
	if got[0].Title != "Go release" || got[0].Snippet != "first" || got[0].Score != 0.9 {
		t.Fatalf("first result fields changed: %#v", got[0])
	}
	if got[1].URL != "https://example.com/path?keep=1" {
		t.Fatalf("second URL = %q, want tracking params and fragment removed", got[1].URL)
	}
}
