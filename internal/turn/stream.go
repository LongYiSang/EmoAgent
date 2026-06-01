package turn

import "context"

const (
	EventStreamStart      = "stream_start"
	EventStreamDelta      = "stream_delta"
	EventStreamEnd        = "stream_end"
	EventToolCallStart    = "tool_call_start"
	EventToolCallEnd      = "tool_call_end"
	EventReasoningStart   = "reasoning_start"
	EventReasoningDelta   = "reasoning_delta"
	EventReasoningEnd     = "reasoning_end"
	EventWorkProgress     = "work_progress"
	EventWorkProgressEnd  = "work_progress_end"
	EventApprovalRequired = "approval_required"
	EventApprovalUpdated  = "approval_updated"
	EventError            = "error"
)

type OutboundSink interface {
	Emit(ctx context.Context, event OutboundEvent) error
}

type SinkFunc func(ctx context.Context, event OutboundEvent) error

func (f SinkFunc) Emit(ctx context.Context, event OutboundEvent) error {
	return f(ctx, event)
}

type outboundSinkContextKey struct{}

func WithOutboundSink(ctx context.Context, sink OutboundSink) context.Context {
	return context.WithValue(ctx, outboundSinkContextKey{}, sink)
}

func OutboundSinkFromContext(ctx context.Context) OutboundSink {
	sink, _ := ctx.Value(outboundSinkContextKey{}).(OutboundSink)
	return sink
}
