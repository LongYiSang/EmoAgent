package logger

import (
	"context"
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

func TestTeeHandlerDoesNotBypassPrimaryLevel(t *testing.T) {
	extra := &captureHandler{}
	l := InitWithTimezoneAndHandler("warn", "text", "Asia/Shanghai", extra)
	l.Info("ignored")
	l.Warn("captured")
	if len(extra.records) != 1 || extra.records[0].Message != "captured" {
		t.Fatalf("extra records = %#v", extra.records)
	}
}

type captureHandler struct {
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, record slog.Record) error {
	h.records = append(h.records, record.Clone())
	return nil
}

func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }
