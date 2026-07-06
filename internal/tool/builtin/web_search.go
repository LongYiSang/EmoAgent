package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/tool/builtin/websearch"
)

// WebSearchSpec defines the tool specification for web_search.
var WebSearchSpec = tool.Spec{
	Name:         "web_search",
	Description:  "Search the web for current or external facts. Returns curated, ranked source URLs with title, URL, snippet, score, evidence, reasons, needs_fetch/fetch_hint, warnings, and usage. Use web_fetch only for a specific top 1-2 result URL when needs_fetch is true or you need full tables, code blocks, or exact source wording beyond returned evidence.",
	Parameters:   json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"max_results":{"type":"integer"},"profile":{"type":"string"},"include_domains":{"type":"array","items":{"type":"string"}},"exclude_domains":{"type":"array","items":{"type":"string"}},"time_range":{"type":"string"},"start_date":{"type":"string"},"end_date":{"type":"string"},"exact_match":{"type":"boolean"}},"required":["query"],"additionalProperties":false}`),
	Scope:        tool.ScopeBoth,
	Permission:   tool.PermReadOnly,
	RoutingClass: tool.RoutingClassCasual,
	Source:       externalWebSource(),
}

// webSearchMaxResultsHardCap is the maximum number of results the handler will request.
const webSearchMaxResultsHardCap = 10

// NewWebSearchHandler returns a tool.Handler that executes web searches via provider.
// defaultMax is used when the caller omits max_results or supplies 0.
func NewWebSearchHandler(provider websearch.Provider, defaultMax int, logger *slog.Logger) tool.Handler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var args struct {
			Query          string   `json:"query"`
			MaxResults     int      `json:"max_results,omitempty"`
			Profile        string   `json:"profile,omitempty"`
			IncludeDomains []string `json:"include_domains,omitempty"`
			ExcludeDomains []string `json:"exclude_domains,omitempty"`
			TimeRange      string   `json:"time_range,omitempty"`
			StartDate      string   `json:"start_date,omitempty"`
			EndDate        string   `json:"end_date,omitempty"`
			ExactMatch     bool     `json:"exact_match,omitempty"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, fmt.Errorf("web_search: invalid input: %w", err)
		}

		query := strings.TrimSpace(args.Query)
		if query == "" {
			return nil, fmt.Errorf("query must be a non-empty string")
		}

		n := args.MaxResults
		if n <= 0 {
			n = defaultMax
		}
		if n > webSearchMaxResultsHardCap {
			n = webSearchMaxResultsHardCap
		}

		logger.DebugContext(ctx, "web_search executing", "max_results", n)

		resp, err := provider.Search(ctx, query, websearch.Options{
			MaxResults:     n,
			Profile:        args.Profile,
			IncludeDomains: args.IncludeDomains,
			ExcludeDomains: args.ExcludeDomains,
			TimeRange:      args.TimeRange,
			StartDate:      args.StartDate,
			EndDate:        args.EndDate,
			ExactMatch:     args.ExactMatch,
		})
		if err != nil {
			return nil, err
		}

		return json.Marshal(resp)
	}
}
