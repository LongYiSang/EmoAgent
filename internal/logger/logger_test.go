package logger

import (
	"log/slog"
	"testing"
)

func TestInit(t *testing.T) {
	l := Init("debug", "text")
	if l == nil {
		t.Fatal("Init returned nil")
	}
	// Verify it was set as default.
	if slog.Default().Handler() != l.Handler() {
		t.Error("logger was not set as default")
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"unknown", slog.LevelInfo},
	}
	for _, tt := range tests {
		if got := parseLevel(tt.input); got != tt.want {
			t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
