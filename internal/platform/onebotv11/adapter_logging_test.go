package onebotv11

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/platform"
)

func TestAdapterLogsIgnoredMetaEventsAtDebug(t *testing.T) {
	cfg := testConfig()
	adapter, err := NewAdapterWithTransport("qq-main", cfg, NewReverseTransport(cfg.Transport))
	if err != nil {
		t.Fatalf("NewAdapterWithTransport: %v", err)
	}

	var logs bytes.Buffer
	adapter.SetLogger(slog.New(slog.NewTextHandler(
		&logs,
		&slog.HandlerOptions{Level: slog.LevelDebug},
	)))
	if err := adapter.Start(context.Background(), ignoreInboundHandler{}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	_, err = adapter.HandleEvent(context.Background(), Event{
		PostType: "meta_event",
		SelfID:   123456,
	})
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	got := logs.String()
	if !strings.Contains(got, `level=DEBUG`) || !strings.Contains(got, `msg="onebot inbound ignored"`) {
		t.Fatalf("ignored meta event log = %q, want debug ignored log", got)
	}
	if strings.Contains(got, `level=INFO`) {
		t.Fatalf("ignored meta event log = %q, want no info log", got)
	}
}

type ignoreInboundHandler struct{}

func (ignoreInboundHandler) HandleInbound(context.Context, platform.InboundMessage, platform.OutboundSink) (platform.HandleResult, error) {
	return platform.HandleResult{}, nil
}
