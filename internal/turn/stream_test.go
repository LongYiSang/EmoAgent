package turn

import (
	"context"
	"testing"
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
