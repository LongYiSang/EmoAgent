package websearch

import "context"

// Options holds optional search parameters that callers can set per-query.
type Options struct {
	MaxResults     int
	SearchDepth    string // "basic" | "advanced"; providers that don't support it ignore it
	IncludeDomains []string
	ExcludeDomains []string
	Profile        string
	Topic          string
	TimeRange      string
	StartDate      string
	EndDate        string
	ExactMatch     bool
}

// Result holds a single search result item.
type Result struct {
	Title       string          `json:"title"`
	URL         string          `json:"url"`
	Snippet     string          `json:"snippet"`
	Score       float64         `json:"score,omitempty"`
	RerankScore float64         `json:"rerank_score,omitempty"`
	FinalScore  float64         `json:"final_score,omitempty"`
	TrustScore  float64         `json:"trust_score,omitempty"`
	Reasons     []string        `json:"reasons,omitempty"`
	Evidence    []EvidenceChunk `json:"evidence,omitempty"`
	NeedsFetch  bool            `json:"needs_fetch"`
	FetchHint   string          `json:"fetch_hint,omitempty"`
	Warnings    []string        `json:"warnings,omitempty"`
}

// Response is the unified response returned by all providers.
type Response struct {
	Query      string       `json:"query"`
	Answer     string       `json:"answer,omitempty"`
	Results    []Result     `json:"results"`
	Provider   string       `json:"provider,omitempty"`
	SearchMode string       `json:"search_mode,omitempty"`
	Warnings   []string     `json:"warnings,omitempty"`
	Usage      *SearchUsage `json:"usage,omitempty"`
}

type SearchUsage struct {
	SearchQueries   int `json:"search_queries,omitempty"`
	ExtractURLs     int `json:"extract_urls,omitempty"`
	RerankDocuments int `json:"rerank_documents,omitempty"`
}

type EvidenceChunk struct {
	Text      string  `json:"text"`
	URL       string  `json:"url,omitempty"`
	Source    string  `json:"source,omitempty"`
	Score     float64 `json:"score,omitempty"`
	Truncated bool    `json:"truncated,omitempty"`
}

// Provider is the interface that every search backend must implement.
type Provider interface {
	Name() string
	Search(ctx context.Context, query string, opts Options) (*Response, error)
}
