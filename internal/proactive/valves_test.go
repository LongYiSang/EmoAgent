package proactive

import (
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

func enabledConfig() config.ProactiveConfig {
	cfg := config.DefaultProactiveConfig()
	cfg.Enabled = true
	cfg.QuietHours = nil
	return cfg
}

// A quiet afternoon with one fresh candidate and no recent activity: the only
// state in which the valves let anything through.
func permissiveInput(now time.Time) ValveInput {
	return ValveInput{
		PersonaKey:     "default",
		Now:            now,
		CandidateCount: 1,
	}
}

func afternoon(t *testing.T) time.Time {
	t.Helper()
	return time.Date(2026, 8, 15, 14, 30, 0, 0, time.Local)
}

func TestValvesAllowWhenNothingBlocks(t *testing.T) {
	reason, ok := EvaluateValves(enabledConfig(), permissiveInput(afternoon(t)))
	if !ok {
		t.Fatalf("EvaluateValves blocked with reason %q, want pass", reason)
	}
}

func TestValvesBlockWhenDisabled(t *testing.T) {
	cfg := enabledConfig()
	cfg.Enabled = false
	reason, ok := EvaluateValves(cfg, permissiveInput(afternoon(t)))
	if ok || reason != ReasonDisabled {
		t.Fatalf("reason=%q ok=%v, want %q/false", reason, ok, ReasonDisabled)
	}
}

// The feature must be off until explicitly enabled: an operator who installs
// this and does nothing else must never get an unsolicited message.
func TestValvesBlockOnDefaultConfig(t *testing.T) {
	reason, ok := EvaluateValves(config.DefaultProactiveConfig(), permissiveInput(afternoon(t)))
	if ok || reason != ReasonDisabled {
		t.Fatalf("default config allowed proactive message (reason=%q ok=%v)", reason, ok)
	}
}

func TestValvesBlockWithoutCandidates(t *testing.T) {
	in := permissiveInput(afternoon(t))
	in.CandidateCount = 0
	reason, ok := EvaluateValves(enabledConfig(), in)
	if ok || reason != ReasonNoCandidates {
		t.Fatalf("reason=%q ok=%v, want %q/false", reason, ok, ReasonNoCandidates)
	}
}

func TestValvesBlockDuringQuietCommand(t *testing.T) {
	now := afternoon(t)
	in := permissiveInput(now)
	in.HasQuietUntil = true
	in.QuietUntil = now.Add(time.Hour)
	reason, ok := EvaluateValves(enabledConfig(), in)
	if ok || reason != ReasonQuietCommand {
		t.Fatalf("reason=%q ok=%v, want %q/false", reason, ok, ReasonQuietCommand)
	}
}

func TestValvesAllowAfterQuietCommandExpires(t *testing.T) {
	now := afternoon(t)
	in := permissiveInput(now)
	in.HasQuietUntil = true
	in.QuietUntil = now.Add(-time.Minute)
	if reason, ok := EvaluateValves(enabledConfig(), in); !ok {
		t.Fatalf("expired quiet window still blocking: %q", reason)
	}
}

func TestValvesBlockDuringQuietHoursIncludingMidnightWrap(t *testing.T) {
	cfg := enabledConfig()
	cfg.QuietHours = []string{"23:00-09:00"}

	for _, tc := range []struct {
		name    string
		at      time.Time
		blocked bool
	}{
		{"late night", time.Date(2026, 8, 15, 23, 30, 0, 0, time.Local), true},
		{"small hours", time.Date(2026, 8, 15, 3, 0, 0, 0, time.Local), true},
		{"just before waking", time.Date(2026, 8, 15, 8, 59, 0, 0, time.Local), true},
		{"morning", time.Date(2026, 8, 15, 9, 0, 0, 0, time.Local), false},
		{"afternoon", time.Date(2026, 8, 15, 14, 30, 0, 0, time.Local), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reason, ok := EvaluateValves(cfg, permissiveInput(tc.at))
			if tc.blocked && (ok || reason != ReasonQuietHours) {
				t.Fatalf("reason=%q ok=%v, want blocked by quiet hours", reason, ok)
			}
			if !tc.blocked && !ok {
				t.Fatalf("unexpectedly blocked: %q", reason)
			}
		})
	}
}

// The daily cap is evaluated before the model is ever consulted. A cap a model
// could argue past is not a cap.
func TestValvesBlockAtDailyCap(t *testing.T) {
	cfg := enabledConfig()
	cfg.Cooldown.MaxPerDay = 3
	in := permissiveInput(afternoon(t))
	in.SpeaksLast24h = 3
	reason, ok := EvaluateValves(cfg, in)
	if ok || reason != ReasonDailyCapReached {
		t.Fatalf("reason=%q ok=%v, want %q/false", reason, ok, ReasonDailyCapReached)
	}
}

func TestValvesBlockBeforeMinInterval(t *testing.T) {
	cfg := enabledConfig()
	cfg.Cooldown.MinIntervalMinutes = 45
	in := permissiveInput(afternoon(t))
	in.HasPriorSpeak = true
	in.MinutesSinceLast = 20
	reason, ok := EvaluateValves(cfg, in)
	if ok || reason != ReasonMinIntervalUnmet {
		t.Fatalf("reason=%q ok=%v, want %q/false", reason, ok, ReasonMinIntervalUnmet)
	}

	in.MinutesSinceLast = 46
	if reason, ok := EvaluateValves(cfg, in); !ok {
		t.Fatalf("blocked past min interval: %q", reason)
	}
}

// The min-interval check must not fire when the agent has never spoken, or the
// very first proactive message would be blocked forever.
func TestValvesAllowFirstEverMessage(t *testing.T) {
	cfg := enabledConfig()
	cfg.Cooldown.MinIntervalMinutes = 45
	in := permissiveInput(afternoon(t))
	in.HasPriorSpeak = false
	in.MinutesSinceLast = 0
	if reason, ok := EvaluateValves(cfg, in); !ok {
		t.Fatalf("first-ever proactive message blocked: %q", reason)
	}
}

func TestValvesBackOffAfterBeingIgnored(t *testing.T) {
	cfg := enabledConfig()
	cfg.Cooldown.BackoffAfterIgnored = 3
	in := permissiveInput(afternoon(t))
	in.ConsecutiveIgnored = 3
	reason, ok := EvaluateValves(cfg, in)
	if ok || reason != ReasonIgnoredBackoff {
		t.Fatalf("reason=%q ok=%v, want %q/false", reason, ok, ReasonIgnoredBackoff)
	}
}

// Interrupting an in-flight reply is the worst thing this feature can do, and
// it is a deterministic fact, so it must never reach the model.
func TestValvesBlockWhenConversationActive(t *testing.T) {
	in := permissiveInput(afternoon(t))
	in.ConversationActive = true
	reason, ok := EvaluateValves(enabledConfig(), in)
	if ok || reason != ReasonUserPresent {
		t.Fatalf("reason=%q ok=%v, want %q/false", reason, ok, ReasonUserPresent)
	}
}

// A malformed quiet window should silence the agent, not unleash it.
func TestValvesFailTowardSilenceOnMalformedQuietHours(t *testing.T) {
	cfg := enabledConfig()
	cfg.QuietHours = []string{"not-a-window"}
	reason, ok := EvaluateValves(cfg, permissiveInput(afternoon(t)))
	if ok || reason != ReasonQuietHours {
		t.Fatalf("reason=%q ok=%v, want silence on malformed window", reason, ok)
	}
}
