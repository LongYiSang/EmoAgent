package logger

import (
	"context"
	"log/slog"
)

type teeHandler struct {
	primary slog.Handler
	extra   slog.Handler
}

func (h teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.primary.Enabled(ctx, level)
}

func (h teeHandler) Handle(ctx context.Context, record slog.Record) error {
	err := h.primary.Handle(ctx, record)
	if h.extra != nil {
		_ = h.extra.Handle(ctx, record)
	}
	return err
}

func (h teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := teeHandler{primary: h.primary.WithAttrs(attrs)}
	if h.extra != nil {
		next.extra = h.extra.WithAttrs(attrs)
	}
	return next
}

func (h teeHandler) WithGroup(name string) slog.Handler {
	next := teeHandler{primary: h.primary.WithGroup(name)}
	if h.extra != nil {
		next.extra = h.extra.WithGroup(name)
	}
	return next
}
