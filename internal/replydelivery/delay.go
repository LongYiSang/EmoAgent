package replydelivery

import (
	"math"
	"math/rand"
	"strings"
	"time"
	"unicode"

	"github.com/longyisang/emoagent/internal/config"
)

type DelayCalculator struct {
	cfg config.ReplyTimingConfig
	rng *rand.Rand
}

func NewDelayCalculator(cfg config.ReplyTimingConfig, rng *rand.Rand) *DelayCalculator {
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	cfg = normalizeTiming(cfg)
	return &DelayCalculator{cfg: cfg, rng: rng}
}

func (d *DelayCalculator) Delay(segmentText string) time.Duration {
	if d == nil || !d.cfg.Enabled {
		return 0
	}
	wc := WordCount(segmentText)
	base := math.Max(d.cfg.LogBase, 1.01)
	logComponent := float64(d.cfg.LogScaleMS) * math.Log(float64(wc)+1) / math.Log(base)
	randomComponent := d.randomInt(d.cfg.RandomIntervalMinMS, d.cfg.RandomIntervalMaxMS)
	delay := int(logComponent) + randomComponent
	delay = clampInt(delay, d.cfg.MinDelayMS, d.cfg.MaxDelayMS)
	return time.Duration(delay) * time.Millisecond
}

func (d *DelayCalculator) randomInt(minValue, maxValue int) int {
	if maxValue <= minValue {
		return minValue
	}
	return minValue + d.rng.Intn(maxValue-minValue+1)
}

func WordCount(text string) int {
	ascii := true
	for _, r := range text {
		if r > 127 {
			ascii = false
			break
		}
	}
	if ascii {
		return len(strings.Fields(text))
	}
	count := 0
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			count++
		}
	}
	return count
}

func normalizeTiming(cfg config.ReplyTimingConfig) config.ReplyTimingConfig {
	defaults := config.DefaultReplyDeliveryConfig().Timing
	if cfg.LogBase <= 1.0 {
		cfg.LogBase = defaults.LogBase
	}
	if cfg.LogScaleMS < 0 {
		cfg.LogScaleMS = defaults.LogScaleMS
	}
	if cfg.RandomIntervalMinMS < 0 {
		cfg.RandomIntervalMinMS = 0
	}
	if cfg.RandomIntervalMaxMS < 0 {
		cfg.RandomIntervalMaxMS = 0
	}
	if cfg.RandomIntervalMaxMS < cfg.RandomIntervalMinMS {
		cfg.RandomIntervalMinMS, cfg.RandomIntervalMaxMS = cfg.RandomIntervalMaxMS, cfg.RandomIntervalMinMS
	}
	if cfg.MinDelayMS < 0 {
		cfg.MinDelayMS = 0
	}
	if cfg.MaxDelayMS < 0 {
		cfg.MaxDelayMS = 0
	}
	if cfg.MaxDelayMS < cfg.MinDelayMS {
		cfg.MaxDelayMS = cfg.MinDelayMS
	}
	return cfg
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
