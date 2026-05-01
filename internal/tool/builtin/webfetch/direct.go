package webfetch

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"

	"github.com/longyisang/emoagent/internal/config"
)

type directProvider struct {
	cfg        config.WebFetchConfig
	httpClient *http.Client
	logger     *slog.Logger
}

// NewDirectProvider creates the direct HTTP provider used as the local fallback.
func NewDirectProvider(cfg config.WebFetchConfig, logger *slog.Logger) Provider {
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	maxRedirects := cfg.MaxRedirects
	if maxRedirects <= 0 {
		maxRedirects = 5
	}
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("too many redirects (max %d)", maxRedirects)
			}
			return nil
		},
	}
	return &directProvider{cfg: cfg, httpClient: client, logger: logger}
}

func (p *directProvider) Name() string { return "direct" }

func (p *directProvider) Fetch(ctx context.Context, sourceURL string, opts Options) (*Response, error) {
	if !isHTTPURL(sourceURL) {
		return nil, fmt.Errorf("web_fetch: only http and https schemes are allowed")
	}

	limit := opts.MaxBytes
	if limit <= 0 || (p.cfg.MaxBytes > 0 && limit > p.cfg.MaxBytes) {
		limit = p.cfg.MaxBytes
	}
	if limit <= 0 {
		limit = 1 << 20
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("web_fetch: build request: %w", err)
	}
	userAgent := p.cfg.UserAgent
	if userAgent == "" {
		userAgent = "EmoAgent/0.1"
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := p.httpClient.Do(req)
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
	var text string
	if strings.Contains(contentType, "text/html") {
		text = htmlToText(string(bodyBytes))
	} else if !utf8.Valid(bodyBytes) {
		text = "[binary content]"
	} else {
		text = string(bodyBytes)
	}

	if p.logger != nil {
		p.logger.DebugContext(ctx, "web_fetch direct done",
			"url", sourceURL, "status", resp.StatusCode,
			"content_type", contentType, "bytes", len(bodyBytes), "truncated", truncated,
		)
	}

	status := resp.StatusCode
	return &Response{
		URL:         sourceURL,
		FinalURL:    resp.Request.URL.String(),
		Status:      &status,
		ContentType: contentType,
		Text:        text,
		Truncated:   truncated,
		Provider:    p.Name(),
	}, nil
}

func isHTTPURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

// htmlToText extracts readable text from HTML, skipping script/style tags
// and collapsing whitespace into single-spaced paragraphs.
func htmlToText(src string) string {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return src
	}

	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			tag := strings.ToLower(n.Data)
			if tag == "script" || tag == "style" || tag == "head" {
				return
			}
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
