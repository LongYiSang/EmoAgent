package logger

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

// Init creates a configured slog.Logger and sets it as the default.
// level: "debug", "info", "warn", "error"
// format: "text", "json"
func Init(level, format string) *slog.Logger {
	return InitWithTimezone(level, format, "Asia/Shanghai")
}

func InitWithTimezone(level, format, timezone string) *slog.Logger {
	lvl := parseLevel(level)
	loc, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		loc = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	opts := &slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				if t, ok := a.Value.Any().(time.Time); ok {
					a.Value = slog.TimeValue(t.In(loc))
				}
			}
			return a
		},
	}

	var handler slog.Handler
	if strings.ToLower(format) == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
