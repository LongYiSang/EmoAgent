package sidecar

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ProviderBinding struct {
	Provider    string
	BaseURL     string
	EndpointURL string
	APIKeyEnv   string
	Model       string
	Dimensions  int
	TopK        int
}

type Spec struct {
	Enabled              bool
	Managed              bool
	Adapter              string
	Host                 string
	Port                 int
	URL                  string
	WorkingDir           string
	Command              []string
	ConfigPath           string
	StartupTimeout       time.Duration
	ShutdownTimeout      time.Duration
	FailOpen             bool
	LogPath              string
	TriviumDir           string
	EmbeddingCacheDBPath string
	Embedding            ProviderBinding
	Rerank               ProviderBinding
	QueryAnalysis        ProviderBinding
}

func DefaultSpec() Spec {
	return Spec{
		Enabled:              false,
		Managed:              false,
		Adapter:              "trivium",
		Host:                 "127.0.0.1",
		Port:                 8765,
		URL:                  "http://127.0.0.1:8765",
		WorkingDir:           "../EmoAgent-MemoryCore/sidecar",
		Command:              []string{"uv", "run", "python", "-m", "memorycore_sidecar.server"},
		ConfigPath:           "./data/runtime/sidecar.generated.toml",
		StartupTimeout:       15 * time.Second,
		ShutdownTimeout:      5 * time.Second,
		FailOpen:             true,
		LogPath:              "./logs/sidecar.log",
		TriviumDir:           "./data/trivium",
		EmbeddingCacheDBPath: "./data/embedding_cache.sqlite3",
		Embedding: ProviderBinding{
			Provider:   "none",
			Dimensions: 1024,
		},
		Rerank: ProviderBinding{
			Provider: "none",
		},
		QueryAnalysis: ProviderBinding{
			Provider: "none",
		},
	}
}

func (s Spec) Validate() error {
	if !isLoopbackHost(s.Host) {
		return fmt.Errorf("sidecar host must be loopback, got %q", s.Host)
	}
	if strings.TrimSpace(s.URL) != "" {
		if err := validateLoopbackURL(s.URL); err != nil {
			return err
		}
	}
	switch s.Adapter {
	case "", "fake", "trivium":
	default:
		return fmt.Errorf("sidecar adapter must be fake or trivium, got %q", s.Adapter)
	}
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("sidecar port must be 1-65535, got %d", s.Port)
	}
	return nil
}

func (s Spec) CommandArgs() []string {
	command := append([]string(nil), s.Command...)
	if len(command) == 0 {
		command = append([]string(nil), DefaultSpec().Command...)
	}
	adapter := s.Adapter
	if adapter == "" {
		adapter = "trivium"
	}
	host := s.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := s.Port
	if port == 0 {
		port = 8765
	}
	return append(command,
		"--adapter", adapter,
		"--config", s.ConfigPath,
		"--host", host,
		"--port", strconv.Itoa(port),
	)
}

func (s Spec) EffectiveURL() string {
	if strings.TrimSpace(s.URL) != "" {
		return strings.TrimSpace(s.URL)
	}
	host := strings.TrimSpace(s.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	port := s.Port
	if port == 0 {
		port = 8765
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateLoopbackURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("sidecar.url must be a loopback HTTP URL: %w", err)
	}
	if parsed.Scheme != "http" {
		return fmt.Errorf("sidecar.url must be a loopback HTTP URL")
	}
	host := parsed.Hostname()
	if !isLoopbackHost(host) {
		return fmt.Errorf("sidecar.url must be a loopback HTTP URL, got host %q", host)
	}
	return nil
}
