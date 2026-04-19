package progress

import (
	"strings"
	"sync"
	"time"
)

// Throttler de-duplicates high-frequency tool progress events.
type Throttler struct {
	mu          sync.Mutex
	minInterval time.Duration
	lastTool    string
	lastToolAt  time.Time
	now         func() time.Time
}

// NewThrottler returns a de-duplication throttler.
func NewThrottler(minInterval time.Duration) *Throttler {
	return newThrottlerWithClock(minInterval, time.Now)
}

func newThrottlerWithClock(minInterval time.Duration, now func() time.Time) *Throttler {
	if now == nil {
		now = time.Now
	}
	return &Throttler{
		minInterval: minInterval,
		now:         now,
	}
}

// ShouldEmit decides whether the event should be forwarded.
func (t *Throttler) ShouldEmit(event Event) bool {
	if t == nil {
		return true
	}

	switch event.Kind {
	case KindStart, KindHeartbeat, KindFinishing, KindPaused, KindEnd:
		return true
	case KindTool:
		return t.shouldEmitTool(event.ToolName)
	default:
		return true
	}
}

func (t *Throttler) shouldEmitTool(toolName string) bool {
	normalized := strings.TrimSpace(strings.ToLower(toolName))
	if normalized == "" {
		return true
	}

	now := t.now()

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.lastTool == normalized && !t.lastToolAt.IsZero() && now.Sub(t.lastToolAt) < t.minInterval {
		return false
	}

	t.lastTool = normalized
	t.lastToolAt = now
	return true
}
