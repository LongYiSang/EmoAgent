package websearch

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

// NewProvider constructs the appropriate Provider based on the given config.
// It reads the API key from the environment variable named by cfg.APIKeyEnv.
func NewProvider(cfg config.WebSearchConfig, logger *slog.Logger) (Provider, error) {
	apiKey := os.Getenv(cfg.APIKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("%s not set", cfg.APIKeyEnv)
	}
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	switch strings.ToLower(cfg.Provider) {
	case "tavily":
		return NewTavilyProvider(TavilyConfig{
			APIKey:        apiKey,
			Timeout:       timeout,
			IncludeAnswer: cfg.IncludeAnswer,
		}, logger), nil
	default:
		return nil, fmt.Errorf("unsupported websearch provider %q", cfg.Provider)
	}
}
