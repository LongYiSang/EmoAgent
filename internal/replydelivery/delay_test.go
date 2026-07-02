package replydelivery

import (
	"math/rand"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

func TestDelayIncreasesWithWordCountWithoutRandomness(t *testing.T) {
	cfg := config.DefaultReplyDeliveryConfig().Timing
	cfg.RandomIntervalMinMS = 0
	cfg.RandomIntervalMaxMS = 0
	cfg.MinDelayMS = 0
	cfg.MaxDelayMS = 10000
	calc := NewDelayCalculator(cfg, rand.New(rand.NewSource(1)))

	short := calc.Delay("hi")
	long := calc.Delay("hello world this is a longer message")
	if long <= short {
		t.Fatalf("long delay = %s, short = %s; want long > short", long, short)
	}
}

func TestDelayUsesDeterministicRandomRangeAndClamp(t *testing.T) {
	cfg := config.DefaultReplyDeliveryConfig().Timing
	cfg.LogScaleMS = 0
	cfg.RandomIntervalMinMS = 250
	cfg.RandomIntervalMaxMS = 250
	cfg.MinDelayMS = 0
	cfg.MaxDelayMS = 1000
	calc := NewDelayCalculator(cfg, rand.New(rand.NewSource(1)))

	if got := calc.Delay("hello"); got != 250*time.Millisecond {
		t.Fatalf("delay = %s, want 250ms", got)
	}

	cfg.MinDelayMS = 300
	cfg.MaxDelayMS = 300
	calc = NewDelayCalculator(cfg, rand.New(rand.NewSource(1)))
	if got := calc.Delay("hello"); got != 300*time.Millisecond {
		t.Fatalf("clamped delay = %s, want 300ms", got)
	}
}

func TestDelayDisabledReturnsZeroAndLogBaseClamps(t *testing.T) {
	cfg := config.DefaultReplyDeliveryConfig().Timing
	cfg.Enabled = false
	calc := NewDelayCalculator(cfg, rand.New(rand.NewSource(1)))
	if got := calc.Delay("hello"); got != 0 {
		t.Fatalf("disabled delay = %s, want 0", got)
	}

	cfg.Enabled = true
	cfg.LogBase = 1
	cfg.RandomIntervalMinMS = 0
	cfg.RandomIntervalMaxMS = 0
	cfg.MinDelayMS = 0
	cfg.MaxDelayMS = 10000
	calc = NewDelayCalculator(cfg, rand.New(rand.NewSource(1)))
	if got := calc.Delay("hello"); got <= 0 {
		t.Fatalf("log_base clamp delay = %s, want positive", got)
	}
}
