package tavily

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// Config holds shared Tavily HTTP client settings.
type Config struct {
	APIKey  string
	BaseURL string
	Timeout time.Duration
}

// Client wraps common Tavily HTTP behavior shared by search and extract.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
}

// NewClient creates a Tavily client. Empty BaseURL defaults to Tavily's public API.
func NewClient(cfg Config, logger *slog.Logger) *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.tavily.com"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		apiKey:     cfg.APIKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: timeout},
		logger:     logger,
	}
}

// BaseURL returns the normalized Tavily API base URL.
func (c *Client) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

// PostJSON sends a JSON POST request to a Tavily endpoint and decodes the JSON response.
func (c *Client) PostJSON(ctx context.Context, endpoint string, request any, response any, operation string) error {
	if c == nil {
		return fmt.Errorf("tavily %s: client is nil", operation)
	}
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("tavily %s: marshal request: %w", operation, err)
	}

	url := c.baseURL + "/" + strings.TrimLeft(endpoint, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("tavily %s: create request: %w", operation, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	if c.logger != nil {
		c.logger.DebugContext(ctx, "tavily request", "operation", operation, "endpoint", endpoint)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("tavily %s: http request: %w", operation, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("tavily %s: read response: %w", operation, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("tavily %s: status %d: %s", operation, resp.StatusCode, truncateForError(string(respBody), 512))
	}
	if response == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, response); err != nil {
		return fmt.Errorf("tavily %s: decode response: %w", operation, err)
	}
	return nil
}

func truncateForError(text string, maxBytes int) string {
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	var b strings.Builder
	for _, r := range text {
		if b.Len()+utf8.RuneLen(r) > maxBytes {
			return b.String()
		}
		b.WriteRune(r)
	}
	return b.String()
}
