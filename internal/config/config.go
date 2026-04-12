package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server      ServerConfig   `yaml:"server"`
	LLM         LLMConfig      `yaml:"llm"`
	LLMProfiles []LLMProfile   `yaml:"llm_profiles"`
	DB          DBConfig       `yaml:"db"`
	Log         LogConfig      `yaml:"log"`
	Personas    PersonasConfig  `yaml:"personas"`
	WebSearch   WebSearchConfig `yaml:"websearch"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type LLMConfig struct {
	Provider     string  `yaml:"provider"`
	BaseURL      string  `yaml:"base_url"`
	Model        string  `yaml:"model"`
	SummaryModel string  `yaml:"summary_model"`
	MaxTokens    int     `yaml:"max_tokens"`
	Temperature  float64 `yaml:"temperature"`
	APIKeyEnv    string  `yaml:"api_key_env"`
}

type LLMProfile struct {
	Name         string  `yaml:"name"`
	Provider     string  `yaml:"provider"`
	BaseURL      string  `yaml:"base_url"`
	Model        string  `yaml:"model"`
	SummaryModel string  `yaml:"summary_model"`
	MaxTokens    int     `yaml:"max_tokens"`
	Temperature  float64 `yaml:"temperature"`
	APIKeyEnv    string  `yaml:"api_key_env"`
}

type DBConfig struct {
	Path string `yaml:"path"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type PersonasConfig struct {
	Dir     string `yaml:"dir"`
	Default string `yaml:"default"`
}

type WebSearchConfig struct {
	Enabled       bool   `yaml:"enabled"`
	Provider      string `yaml:"provider"`       // "tavily"
	APIKeyEnv     string `yaml:"api_key_env"`    // "TAVILY_API_KEY"
	MaxResults    int    `yaml:"max_results"`    // handler default cap, default 5
	TimeoutSec    int    `yaml:"timeout_sec"`    // HTTP timeout seconds, default 30
	IncludeAnswer bool   `yaml:"include_answer"` // default false
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "127.0.0.1",
			Port: 8080,
		},
		LLM: LLMConfig{
			Provider:    "openai",
			BaseURL:     "https://api.openai.com",
			Model:       "gpt-4o",
			MaxTokens:   4096,
			Temperature: 0.7,
		},
		LLMProfiles: []LLMProfile{},
		DB: DBConfig{
			Path: "./data/emo.db",
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
		Personas: PersonasConfig{
			Dir:     "./personas",
			Default: "default",
		},
		WebSearch: WebSearchConfig{
			Enabled:    false,
			Provider:   "tavily",
			APIKeyEnv:  "TAVILY_API_KEY",
			MaxResults: 5,
			TimeoutSec: 30,
		},
	}
}

// Load reads a YAML config file and returns a Config.
// Missing fields retain their default values.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

// Validate checks that required fields are set.
func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be 1-65535, got %d", c.Server.Port)
	}
	if c.LLM.Provider == "" {
		return fmt.Errorf("llm.provider is required")
	}
	if c.LLM.Model == "" {
		return fmt.Errorf("llm.model is required")
	}
	for i, profile := range c.LLMProfiles {
		if err := profile.Validate(); err != nil {
			return fmt.Errorf("llm_profiles[%d]: %w", i, err)
		}
	}
	if c.WebSearch.Enabled {
		if c.WebSearch.Provider == "" {
			return fmt.Errorf("websearch.provider is required when websearch is enabled")
		}
		if c.WebSearch.APIKeyEnv == "" {
			return fmt.Errorf("websearch.api_key_env is required when websearch is enabled")
		}
	}
	return nil
}

// Validate checks that required fields are set.
func (p LLMProfile) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("name is required")
	}
	if p.Provider == "" {
		return fmt.Errorf("provider is required")
	}
	if p.Model == "" {
		return fmt.Errorf("model is required")
	}
	return nil
}
