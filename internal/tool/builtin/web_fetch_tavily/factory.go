package web_fetch_tavily

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

// NewProvider creates the configured web_fetch provider.
// Tavily without an API key falls back to direct fetching so web_fetch remains available.
func NewProvider(cfg config.WebFetchConfig, logger *slog.Logger) (Provider, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = "tavily"
	}

	switch provider {
	case "direct":
		return NewDirectProvider(cfg, logger), nil
	case "tavily":
		apiKey := os.Getenv(cfg.APIKeyEnv)
		if apiKey == "" {
			if logger != nil {
				logger.Warn("web_fetch tavily api key missing; falling back to direct provider", "api_key_env", cfg.APIKeyEnv)
			}
			return NewDirectProvider(cfg, logger), nil
		}
		timeout := time.Duration(cfg.TimeoutSec) * time.Second
		if timeout <= 0 {
			timeout = 20 * time.Second
		}
		return NewTavilyProvider(TavilyConfig{
			APIKey:         apiKey,
			BaseURL:        cfg.BaseURL,
			Timeout:        timeout,
			TimeoutSec:     cfg.TimeoutSec,
			ExtractDepth:   cfg.ExtractDepth,
			Format:         cfg.Format,
			IncludeImages:  cfg.IncludeImages,
			IncludeFavicon: cfg.IncludeFavicon,
			IncludeUsage:   cfg.IncludeUsage,
		}, logger), nil
	default:
		return nil, fmt.Errorf("unsupported webfetch provider %q", cfg.Provider)
	}
}
