package proactive

import (
	"context"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

// PresenceChecker reports whether a reply is currently being produced for this
// persona. Backed by conversation.RunRegistry in production.
type PresenceChecker interface {
	HasActiveRun(personaKey string) bool
}

// QuietStore reports the /quiet mute deadline, if any.
type QuietStore interface {
	QuietUntil(ctx context.Context) (time.Time, bool)
}

// ValveInput is everything the deterministic valves need. Every field is a fact,
// not a judgement — no model is consulted at this layer.
type ValveInput struct {
	PersonaKey         string
	Now                time.Time
	CandidateCount     int
	SpeaksLast24h      int
	MinutesSinceLast   int
	HasPriorSpeak      bool
	ConsecutiveIgnored int
	ConversationActive bool
	QuietUntil         time.Time
	HasQuietUntil      bool
}

// EvaluateValves runs the deterministic gate chain. It must be called before the
// LLM gate: these checks are cheap, unambiguous, and — crucially — must not be
// overridable by a model. A daily cap that a model can argue its way past is not
// a cap.
//
// Returns ok=true when the chain permits proceeding to the LLM gate.
func EvaluateValves(cfg config.ProactiveConfig, in ValveInput) (reason string, ok bool) {
	cfg = config.NormalizeProactiveConfig(cfg)

	if !cfg.Enabled {
		return ReasonDisabled, false
	}
	if in.CandidateCount == 0 {
		return ReasonNoCandidates, false
	}
	if in.HasQuietUntil && in.Now.Before(in.QuietUntil) {
		return ReasonQuietCommand, false
	}
	if inQuietHours(cfg.QuietHours, in.Now) {
		return ReasonQuietHours, false
	}
	// The daily cap is evaluated before the model runs, on purpose.
	if in.SpeaksLast24h >= cfg.Cooldown.MaxPerDay {
		return ReasonDailyCapReached, false
	}
	if in.HasPriorSpeak && in.MinutesSinceLast < cfg.Cooldown.MinIntervalMinutes {
		return ReasonMinIntervalUnmet, false
	}
	if in.ConsecutiveIgnored >= cfg.Cooldown.BackoffAfterIgnored {
		return ReasonIgnoredBackoff, false
	}
	// Interrupting an in-flight reply is the single worst thing this feature can
	// do, and it is a deterministic fact, so it never reaches the model.
	if in.ConversationActive {
		return ReasonUserPresent, false
	}
	return "", true
}

func inQuietHours(windows []string, now time.Time) bool {
	for _, raw := range windows {
		window, err := config.ParseQuietHourWindow(raw)
		if err != nil {
			// A malformed window is rejected at config validation time. If one
			// slips through, treat it as quiet: fail toward silence.
			return true
		}
		if window.Contains(now) {
			return true
		}
	}
	return false
}
