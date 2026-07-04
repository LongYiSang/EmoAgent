package platform

import "context"

type OutboundSink interface {
	Emit(context.Context, OutboundEvent) error
}

type BufferedPlatformSink struct {
	Events []OutboundEvent
}

func (s *BufferedPlatformSink) Emit(_ context.Context, event OutboundEvent) error {
	if s == nil {
		return nil
	}
	s.Events = append(s.Events, event)
	return nil
}
