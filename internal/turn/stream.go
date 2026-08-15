package turn

import "context"

const (
	EventStreamStart      = "stream_start"
	EventStreamDelta      = "stream_delta"
	EventStreamEnd        = "stream_end"
	EventAssistantSegment = "assistant_segment"
	EventToolCallStart    = "tool_call_start"
	EventToolCallEnd      = "tool_call_end"
	EventReasoningStart   = "reasoning_start"
	EventReasoningDelta   = "reasoning_delta"
	EventReasoningEnd     = "reasoning_end"
	EventWorkProgress     = "work_progress"
	EventWorkProgressEnd  = "work_progress_end"
	EventApprovalRequired = "approval_required"
	EventApprovalUpdated  = "approval_updated"
	EventTurnStatus       = "turn_status"
	EventError            = "error"
)

// IsUserVisibleEvent reports whether an outbound event can produce something the
// user actually perceives, as opposed to diagnostics (context_stats, turn_status)
// that only feed the UI and journals.
//
// Proactive turns depend on this distinction: a turn Emotion declined must emit
// nothing visible, while still being free to journal what it cost.
func IsUserVisibleEvent(event OutboundEvent) bool {
	switch event.Type {
	case EventStreamStart, EventStreamDelta, EventStreamEnd,
		EventAssistantSegment, EventApprovalRequired, EventError:
		return true
	default:
		return false
	}
}

// HasUserVisibleContent reports whether any of the events would actually show
// something to the user. Used to tell a delivered proactive message apart from
// one that ended in silence.
func HasUserVisibleContent(events []OutboundEvent) bool {
	for _, event := range events {
		if event.Type == EventAssistantSegment || event.Type == EventStreamDelta {
			if event.Content != "" {
				return true
			}
		}
	}
	return false
}

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
