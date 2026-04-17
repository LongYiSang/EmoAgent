package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/tool"
)

// NewWebFetchTool constructs the web_fetch tool for Work.
func NewWebFetchTool(cfg config.WebFetchConfig, logger *slog.Logger) (tool.Spec, tool.Handler) {
	spec := tool.Spec{
		Name:        "web_fetch",
		Description: "Fetch a URL and return its text content. HTML pages are stripped to readable plain text. Returns status, content type, and text with an optional truncation flag.",
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

	client := &http.Client{
		Timeout: time.Duration(cfg.TimeoutSec) * time.Second,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= cfg.MaxRedirects {
				return fmt.Errorf("too many redirects (max %d)", cfg.MaxRedirects)
			}
			return nil
		},
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

		limit := in.MaxBytes
		if limit <= 0 || limit > cfg.MaxBytes {
			limit = cfg.MaxBytes
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, fmt.Errorf("web_fetch: build request: %w", err)
		}
		req.Header.Set("User-Agent", cfg.UserAgent)

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("web_fetch: request failed: %w", err)
		}
		defer resp.Body.Close()

		limitedBody := io.LimitReader(resp.Body, int64(limit)+1)
		bodyBytes, err := io.ReadAll(limitedBody)
		if err != nil {
			return nil, fmt.Errorf("web_fetch: read body: %w", err)
		}

		truncated := len(bodyBytes) > limit
		if truncated {
			bodyBytes = bodyBytes[:limit]
		}

		contentType := resp.Header.Get("Content-Type")
		finalURL := resp.Request.URL.String()

		var text string
		if strings.Contains(contentType, "text/html") {
			text = htmlToText(string(bodyBytes))
		} else {
			if !utf8.Valid(bodyBytes) {
				text = "[binary content]"
			} else {
				text = string(bodyBytes)
			}
		}

		if logger != nil {
			logger.DebugContext(ctx, "web_fetch done",
				"url", u, "status", resp.StatusCode,
				"content_type", contentType, "bytes", len(bodyBytes), "truncated", truncated,
			)
		}

		return json.Marshal(map[string]any{
			"url":          u,
			"final_url":    finalURL,
			"status":       resp.StatusCode,
			"content_type": contentType,
			"text":         text,
			"truncated":    truncated,
		})
	}

	return spec, handler
}

// htmlToText extracts readable text from HTML, skipping script/style tags
// and collapsing whitespace into single-spaced paragraphs.
func htmlToText(src string) string {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return src // fall back to raw on parse failure
	}

	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			tag := strings.ToLower(n.Data)
			if tag == "script" || tag == "style" || tag == "head" {
				return
			}
			// Insert newlines around block-level elements.
			switch tag {
			case "p", "div", "br", "h1", "h2", "h3", "h4", "h5", "h6",
				"li", "tr", "blockquote", "pre", "article", "section",
				"header", "footer", "main", "nav":
				b.WriteByte('\n')
			}
		}
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				b.WriteString(text)
				b.WriteByte(' ')
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	// Collapse multiple blank lines.
	lines := strings.Split(b.String(), "\n")
	var out []string
	prev := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" && prev == "" {
			continue
		}
		out = append(out, trimmed)
		prev = trimmed
	}
	return strings.Join(out, "\n")
}
