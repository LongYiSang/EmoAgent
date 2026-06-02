package turn

import (
	"context"
	"sync"
	"time"
)

type BoundedOutboundOptions struct {
	FlushInterval        time.Duration
	MaxDeltaBytes        int
	WorkProgressInterval time.Duration
	QueueSize            int
}

type BoundedOutboundSink struct {
	next OutboundSink
	opts BoundedOutboundOptions

	queue chan queuedOutboundEvent
	done  chan struct{}

	mu       sync.Mutex
	firstErr error
	closing  bool
}

type queuedOutboundEvent struct {
	event OutboundEvent
	ack   chan error
	close bool
}

func NewBoundedOutboundSink(next OutboundSink, opts BoundedOutboundOptions) *BoundedOutboundSink {
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = 30 * time.Millisecond
	}
	if opts.MaxDeltaBytes <= 0 {
		opts.MaxDeltaBytes = 512
	}
	if opts.WorkProgressInterval <= 0 {
		opts.WorkProgressInterval = 500 * time.Millisecond
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = 64
	}
	sink := &BoundedOutboundSink{
		next:  next,
		opts:  opts,
		queue: make(chan queuedOutboundEvent, opts.QueueSize),
		done:  make(chan struct{}),
	}
	go sink.run()
	return sink
}

func (s *BoundedOutboundSink) Emit(ctx context.Context, event OutboundEvent) error {
	if s == nil || s.next == nil {
		return nil
	}
	if err := s.err(); err != nil {
		return err
	}
	item := queuedOutboundEvent{event: event}
	if event.Type != EventStreamDelta && event.Type != EventWorkProgress {
		item.ack = make(chan error, 1)
	}
	if event.Type == EventWorkProgress {
		select {
		case s.queue <- item:
			return nil
		default:
			return nil
		}
	}
	if err := s.enqueue(ctx, item); err != nil {
		return err
	}
	if item.ack == nil {
		return nil
	}
	select {
	case err := <-item.ack:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return s.err()
	}
}

func (s *BoundedOutboundSink) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	select {
	case <-s.done:
		return s.err()
	default:
	}
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		select {
		case <-s.done:
			return s.err()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.closing = true
	s.mu.Unlock()

	ack := make(chan error, 1)
	if err := s.enqueue(ctx, queuedOutboundEvent{ack: ack, close: true}); err != nil {
		s.mu.Lock()
		s.closing = false
		s.mu.Unlock()
		return err
	}
	select {
	case err := <-ack:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return s.err()
	}
}

func (s *BoundedOutboundSink) enqueue(ctx context.Context, item queuedOutboundEvent) error {
	select {
	case s.queue <- item:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return s.err()
	}
}

func (s *BoundedOutboundSink) run() {
	timer := time.NewTimer(s.opts.FlushInterval)
	if !timer.Stop() {
		<-timer.C
	}
	timerActive := false
	pendingDelta := ""
	pendingMeta := OutboundEvent{}
	lastProgressAt := time.Time{}

	resetTimer := func() {
		if timerActive {
			return
		}
		timer.Reset(s.opts.FlushInterval)
		timerActive = true
	}
	stopTimer := func() {
		if !timerActive {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timerActive = false
	}
	emit := func(event OutboundEvent) error {
		if err := s.err(); err != nil {
			return err
		}
		if err := s.next.Emit(context.Background(), event); err != nil {
			s.setErr(err)
			return err
		}
		return nil
	}
	flush := func() error {
		stopTimer()
		if pendingDelta == "" {
			return s.err()
		}
		event := pendingMeta
		event.Type = EventStreamDelta
		event.Content = pendingDelta
		pendingDelta = ""
		pendingMeta = OutboundEvent{}
		return emit(event)
	}

	for {
		select {
		case item := <-s.queue:
			var err error
			if item.close {
				err = flush()
				if item.ack != nil {
					item.ack <- err
				}
				close(s.done)
				return
			}
			switch item.event.Type {
			case EventStreamDelta:
				pendingDelta += item.event.Content
				if pendingMeta.Type == "" {
					pendingMeta = item.event
				}
				if len([]byte(pendingDelta)) >= s.opts.MaxDeltaBytes {
					err = flush()
				} else {
					resetTimer()
				}
			case EventWorkProgress:
				now := time.Now()
				if lastProgressAt.IsZero() || now.Sub(lastProgressAt) >= s.opts.WorkProgressInterval {
					lastProgressAt = now
					if err = flush(); err == nil {
						err = emit(item.event)
					}
				}
			default:
				if err = flush(); err == nil {
					err = emit(item.event)
				}
			}
			if item.ack != nil {
				item.ack <- err
			}
		case <-timer.C:
			timerActive = false
			_ = flush()
		}
	}
}

func (s *BoundedOutboundSink) err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.firstErr
}

func (s *BoundedOutboundSink) setErr(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.firstErr == nil {
		s.firstErr = err
	}
}
