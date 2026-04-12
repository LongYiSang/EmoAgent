package websearch

import "context"

// Options holds optional search parameters that callers can set per-query.
type Options struct {
	MaxResults     int
	SearchDepth    string // "basic" | "advanced"; providers that don't support it ignore it
	IncludeDomains []string
	ExcludeDomains []string
}

// Result holds a single search result item.
type Result struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score,omitempty"`
}

// Response is the unified response returned by all providers.
type Response struct {
	Query   string   `json:"query"`
	Answer  string   `json:"answer,omitempty"`
	Results []Result `json:"results"`
}

// Provider is the interface that every search backend must implement.
type Provider interface {
	Name() string
	Search(ctx context.Context, query string, opts Options) (*Response, error)
}
