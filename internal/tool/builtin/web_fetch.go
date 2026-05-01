package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/tool/builtin/webfetch"
)

// NewWebFetchTool constructs the web_fetch tool for Work.
func NewWebFetchTool(cfg config.WebFetchConfig, logger *slog.Logger) (tool.Spec, tool.Handler) {
	provider, err := webfetch.NewProvider(cfg, logger)
	if err != nil {
		provider = nil
	}
	return NewWebFetchToolWithProvider(provider, cfg, logger)
}

// NewWebFetchToolWithProvider constructs the web_fetch tool around an explicit provider.
func NewWebFetchToolWithProvider(provider webfetch.Provider, cfg config.WebFetchConfig, logger *slog.Logger) (tool.Spec, tool.Handler) {
	spec := tool.Spec{
		Name:        "web_fetch",
		Description: "Fetch a specific http or https source URL and return readable text. Returns url, final_url, text, truncated, and provider; direct fetch also returns status and content_type. Treat truncated=true as incomplete source content.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"url":{"type":"string"},
				"max_bytes":{"type":"integer"}
			},
			"required":["url"],
			"additionalProperties":false
		}`),
		Scope:      tool.ScopeWork,
		Permission: tool.PermReadOnly,
	}

	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			URL      string `json:"url"`
			MaxBytes int    `json:"max_bytes"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("web_fetch: invalid input: %w", err)
		}

		u := strings.TrimSpace(in.URL)
		if u == "" {
			return nil, fmt.Errorf("web_fetch: url is required")
		}
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return nil, fmt.Errorf("web_fetch: only http and https schemes are allowed")
		}
		if provider == nil {
			return nil, fmt.Errorf("web_fetch: provider is unavailable")
		}

		limit := in.MaxBytes
		if limit <= 0 || limit > cfg.MaxBytes {
			limit = cfg.MaxBytes
		}

		resp, err := provider.Fetch(ctx, u, webfetch.Options{MaxBytes: limit})
		if err != nil {
			return nil, err
		}

		if logger != nil {
			logger.DebugContext(ctx, "web_fetch done",
				"url", u, "provider", provider.Name(), "truncated", resp.Truncated,
			)
		}

		return json.Marshal(resp)
	}

	return spec, handler
}
