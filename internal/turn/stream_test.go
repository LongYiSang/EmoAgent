package turn

import (
	"context"
	"testing"
	"time"
)

func TestSinkFuncAndContext(t *testing.T) {
	var got []OutboundEvent
	sink := SinkFunc(func(ctx context.Context, event OutboundEvent) error {
		got = append(got, event)
		return nil
	})

	ctx := WithOutboundSink(context.Background(), sink)
	fromCtx := OutboundSinkFromContext(ctx)
	if fromCtx == nil {
		t.Fatal("OutboundSinkFromContext returned nil")
	}
	if err := fromCtx.Emit(ctx, OutboundEvent{Type: EventStreamStart}); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if len(got) != 1 || got[0].Type != EventStreamStart {
		t.Fatalf("got = %#v, want stream_start", got)
	}
}

func TestBoundedOutboundSinkCoalescesDeltasAndPreservesTerminalOrder(t *testing.T) {
	var got []OutboundEvent
	next := SinkFunc(func(ctx context.Context, event OutboundEvent) error {
		got = append(got, event)
		return nil
	})
	sink := NewBoundedOutboundSink(next, BoundedOutboundOptions{
		FlushInterval:        10 * time.Millisecond,
		MaxDeltaBytes:        512,
		WorkProgressInterval: time.Minute,
		QueueSize:            4,
	})

	ctx := context.Background()
	if err := sink.Emit(ctx, OutboundEvent{Type: EventStreamStart}); err != nil {
		t.Fatalf("Emit start: %v", err)
	}
	if err := sink.Emit(ctx, OutboundEvent{Type: EventStreamDelta, Content: "hello "}); err != nil {
		t.Fatalf("Emit delta 1: %v", err)
	}
	if err := sink.Emit(ctx, OutboundEvent{Type: EventStreamDelta, Content: "world"}); err != nil {
		t.Fatalf("Emit delta 2: %v", err)
	}
	if err := sink.Emit(ctx, OutboundEvent{Type: EventApprovalRequired}); err != nil {
		t.Fatalf("Emit approval: %v", err)
	}
	if err := sink.Emit(ctx, OutboundEvent{Type: EventStreamEnd}); err != nil {
		t.Fatalf("Emit end: %v", err)
	}
	if err := sink.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	wantTypes := []string{EventStreamStart, EventStreamDelta, EventApprovalRequired, EventStreamEnd}
	if len(got) != len(wantTypes) {
		t.Fatalf("events = %#v, want %d events", got, len(wantTypes))
	}
	for i, want := range wantTypes {
		if got[i].Type != want {
			t.Fatalf("events[%d].Type = %q, want %q (all=%#v)", i, got[i].Type, want, got)
		}
	}
	if got[1].Content != "hello world" {
		t.Fatalf("coalesced delta = %q, want hello world", got[1].Content)
	}
}

func TestBoundedOutboundSinkDownsamplesWorkProgress(t *testing.T) {
	var got []OutboundEvent
	sink := NewBoundedOutboundSink(SinkFunc(func(ctx context.Context, event OutboundEvent) error {
		got = append(got, event)
		return nil
	}), BoundedOutboundOptions{
		FlushInterval:        time.Millisecond,
		MaxDeltaBytes:        512,
		WorkProgressInterval: time.Hour,
		QueueSize:            4,
	})

	ctx := context.Background()
	if err := sink.Emit(ctx, OutboundEvent{Type: EventWorkProgress, Content: "first"}); err != nil {
		t.Fatalf("Emit first progress: %v", err)
	}
	if err := sink.Emit(ctx, OutboundEvent{Type: EventWorkProgress, Content: "second"}); err != nil {
		t.Fatalf("Emit second progress: %v", err)
	}
	if err := sink.Emit(ctx, OutboundEvent{Type: EventWorkProgressEnd}); err != nil {
		t.Fatalf("Emit progress end: %v", err)
	}
	if err := sink.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if len(got) != 2 || got[0].Type != EventWorkProgress || got[0].Content != "first" || got[1].Type != EventWorkProgressEnd {
		t.Fatalf("events = %#v, want first progress and progress_end only", got)
	}
}
