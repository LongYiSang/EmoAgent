package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/config"
)

func TestRunFailsWithoutLLM(t *testing.T) {
	a := &App{
		Config: &config.Config{
			Server: config.ServerConfig{Host: "127.0.0.1", Port: 0},
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	err := a.Run(context.Background())
	if err == nil {
		t.Fatal("Run should fail when LLM is nil")
	}
	if !strings.Contains(err.Error(), "LLM client not initialized") {
		t.Fatalf("Run error = %v, want LLM initialization failure", err)
	}
}

func TestGetDefaultPersonaName(t *testing.T) {
	a := &App{
		Config: &config.Config{
			Personas: config.PersonasConfig{Default: "default"},
		},
	}

	if got := a.GetDefaultPersonaName(); got != "default" {
		t.Fatalf("GetDefaultPersonaName = %q, want default", got)
	}
}
