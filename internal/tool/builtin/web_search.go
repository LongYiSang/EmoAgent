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
	Name:        "web_search",
	Description: "Search the web for up-to-date or external facts. Returns results with title, source URLs, and snippets. Use web_fetch on a specific result URL when you need source content beyond the snippet.",
	Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"max_results":{"type":"integer"}},"required":["query"],"additionalProperties":false}`),
	Scope:       tool.ScopeBoth,
	Permission:  tool.PermReadOnly,
}

// webSearchMaxResultsHardCap is the maximum number of results the handler will request.
const webSearchMaxResultsHardCap = 10

// NewWebSearchHandler returns a tool.Handler that executes web searches via provider.
// defaultMax is used when the caller omits max_results or supplies 0.
func NewWebSearchHandler(provider websearch.Provider, defaultMax int, logger *slog.Logger) tool.Handler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var args struct {
			Query      string `json:"query"`
			MaxResults int    `json:"max_results,omitempty"`
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

		logger.DebugContext(ctx, "web_search executing", "query", query, "max_results", n)

		resp, err := provider.Search(ctx, query, websearch.Options{MaxResults: n})
		if err != nil {
			return nil, err
		}

		return json.Marshal(resp)
	}
}
