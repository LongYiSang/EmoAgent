package web_fetch_tavily

import "context"

// Options holds per-call fetch settings.
type Options struct {
	MaxBytes int
}

// FailedResult is a provider-reported extraction failure for a URL.
type FailedResult struct {
	URL   string `json:"url"`
	Error string `json:"error,omitempty"`
}

// Response is the unified web_fetch response returned by providers.
type Response struct {
	URL           string         `json:"url"`
	FinalURL      string         `json:"final_url"`
	Status        *int           `json:"status,omitempty"`
	ContentType   string         `json:"content_type,omitempty"`
	Text          string         `json:"text"`
	Truncated     bool           `json:"truncated"`
	Provider      string         `json:"provider"`
	RequestID     string         `json:"request_id,omitempty"`
	Images        []string       `json:"images,omitempty"`
	Favicon       string         `json:"favicon,omitempty"`
	Usage         map[string]any `json:"usage,omitempty"`
	FailedResults []FailedResult `json:"failed_results,omitempty"`
}

// Provider fetches readable content for a single URL.
type Provider interface {
	Name() string
	Fetch(ctx context.Context, url string, opts Options) (*Response, error)
}
