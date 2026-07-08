package logcenter

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestEventsFiltersAndCapacity(t *testing.T) {
	service := NewService()
	service.capacity = 3
	service.Add(Event{SourceType: SourceTypeMain, SourceID: "host", Level: LevelInfo, Message: "one"})
	second := service.Add(Event{SourceType: SourceTypePlugin, SourceID: "com.example.echo", Level: LevelWarn, Message: "two"})
	service.Add(Event{SourceType: SourceTypePlugin, SourceID: "com.example.echo", Level: LevelUnknown, Message: "three"})
	service.Add(Event{SourceType: SourceTypePlugin, SourceID: "com.example.echo", Level: LevelError, Message: "four"})

	events := service.Events(Query{SourceType: SourceTypePlugin, SourceID: "com.example.echo", MinLevel: LevelWarn})
	if len(events) != 2 || events[0].Message != "two" || events[1].Message != "four" {
		t.Fatalf("filtered events = %#v, want warn+error without unknown", events)
	}

	events = service.Events(Query{AfterID: second.ID})
	if len(events) != 2 || events[0].Message != "three" || events[1].Message != "four" {
		t.Fatalf("after_id events = %#v", events)
	}
}

func TestSubscriberDropsWhenFull(t *testing.T) {
	service := NewService()
	service.subBuffer = 1
	ch, _ := service.Subscribe()
	service.Add(Event{SourceType: SourceTypeMain, SourceID: "host", Level: LevelInfo, Message: "one"})
	service.Add(Event{SourceType: SourceTypeMain, SourceID: "host", Level: LevelInfo, Message: "two"})

	if got := <-ch; got.Message != "one" {
		t.Fatalf("first event = %#v", got)
	}
	if _, ok := <-ch; ok {
		t.Fatal("subscriber channel still open after overflow")
	}
}

func TestPollIngestsOnlyNewTail(t *testing.T) {
	service := NewService()
	provider := staticProvider{items: []SourceTail{{
		Source: Source{Type: SourceTypePlugin, ID: "com.example.echo", Label: "Echo", Status: SourceStatusActive},
		Tail:   "[info] boot\n",
	}}}
	service.SetProviders(provider)
	service.Poll(context.Background())
	service.Poll(context.Background())
	if events := service.Events(Query{}); len(events) != 1 || events[0].Message != "boot" {
		t.Fatalf("events after duplicate poll = %#v", events)
	}
}

func TestSlogHandlerAddsStructuredEvent(t *testing.T) {
	service := NewService()
	handler := service.Handler(Source{Type: SourceTypeMain, ID: "host", Label: "EmoAgent 主程序"})
	record := slog.NewRecord(time.Now(), slog.LevelWarn, "provider failed", 0)
	record.AddAttrs(slog.String("api_key", "secret"), slog.String("provider", "moonshot"))
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	events := service.Events(Query{})
	if len(events) != 1 || events[0].Level != LevelWarn || events[0].Attrs["api_key"] != "[redacted]" {
		t.Fatalf("event = %#v", events)
	}
}

func TestParseTailLine(t *testing.T) {
	tests := []struct {
		line        string
		wantLevel   Level
		wantMessage string
	}{
		{`{"level":"error","message":"failed"}`, LevelError, "failed"},
		{"[info] ready", LevelInfo, "ready"},
		{"WARN disk slow", LevelWarn, "WARN disk slow"},
		{"plain text", LevelUnknown, "plain text"},
	}
	for _, tt := range tests {
		level, message := parseTailLine(tt.line)
		if level != tt.wantLevel || message != tt.wantMessage {
			t.Fatalf("parseTailLine(%q) = %s %q, want %s %q", tt.line, level, message, tt.wantLevel, tt.wantMessage)
		}
	}
}

type staticProvider struct {
	items []SourceTail
}

func (p staticProvider) LogCenterSources(context.Context) []SourceTail {
	return p.items
}
