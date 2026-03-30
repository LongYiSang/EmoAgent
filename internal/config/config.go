package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	LLM      LLMConfig      `yaml:"llm"`
	DB       DBConfig       `yaml:"db"`
	Log      LogConfig      `yaml:"log"`
	Personas PersonasConfig `yaml:"personas"`
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
	return nil
}
