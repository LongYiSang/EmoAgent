package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ProactiveConfig governs the only path in the system that can produce outbound
// messages on its own initiative. Every other outbound is a response to user
// input, so the defaults here are deliberately conservative: the feature is off
// until the user turns it on, and group channels stay excluded even then.
type ProactiveConfig struct {
	Enabled           bool                      `yaml:"enabled" json:"enabled"`
	Gate              ProactiveGateConfig       `yaml:"gate" json:"gate"`
	SilentTermination ProactiveSilentConfig     `yaml:"silent_termination" json:"silent_termination"`
	Cooldown          ProactiveCooldownConfig   `yaml:"cooldown" json:"cooldown"`
	QuietHours        []string                  `yaml:"quiet_hours" json:"quiet_hours"`
	Targets           ProactiveTargetsConfig    `yaml:"targets" json:"targets"`
	Candidates        ProactiveCandidatesConfig `yaml:"candidates" json:"candidates"`
}

type ProactiveGateConfig struct {
	// ProviderID and Model empty means reuse the host's main provider/model.
	ProviderID      string `yaml:"provider_id" json:"provider_id"`
	Model           string `yaml:"model" json:"model"`
	TimeoutMS       int    `yaml:"timeout_ms" json:"timeout_ms"`
	MaxOutputTokens int    `yaml:"max_output_tokens" json:"max_output_tokens"`
}

type ProactiveSilentConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

type ProactiveCooldownConfig struct {
	MinIntervalMinutes            int `yaml:"min_interval_minutes" json:"min_interval_minutes"`
	MaxPerDay                     int `yaml:"max_per_day" json:"max_per_day"`
	BackoffAfterIgnored           int `yaml:"backoff_after_ignored" json:"backoff_after_ignored"`
	ReplyAttributionWindowMinutes int `yaml:"reply_attribution_window_minutes" json:"reply_attribution_window_minutes"`
}

type ProactiveTargetsConfig struct {
	AllowGroupChannels bool     `yaml:"allow_group_channels" json:"allow_group_channels"`
	AllowOrigins       []string `yaml:"allow_origins" json:"allow_origins"`
}

type ProactiveCandidatesConfig struct {
	TTLHours   int `yaml:"ttl_hours" json:"ttl_hours"`
	MaxPending int `yaml:"max_pending" json:"max_pending"`
}

func DefaultProactiveConfig() ProactiveConfig {
	return ProactiveConfig{
		Enabled: false,
		Gate: ProactiveGateConfig{
			TimeoutMS:       5000,
			MaxOutputTokens: 256,
		},
		SilentTermination: ProactiveSilentConfig{Enabled: true},
		Cooldown: ProactiveCooldownConfig{
			MinIntervalMinutes:            45,
			MaxPerDay:                     8,
			BackoffAfterIgnored:           3,
			ReplyAttributionWindowMinutes: 30,
		},
		QuietHours: []string{"23:00-09:00"},
		Targets:    ProactiveTargetsConfig{AllowGroupChannels: false},
		Candidates: ProactiveCandidatesConfig{TTLHours: 6, MaxPending: 50},
	}
}

func NormalizeProactiveConfig(cfg ProactiveConfig) ProactiveConfig {
	defaults := DefaultProactiveConfig()
	if cfg.Gate.TimeoutMS <= 0 {
		cfg.Gate.TimeoutMS = defaults.Gate.TimeoutMS
	}
	if cfg.Gate.MaxOutputTokens <= 0 {
		cfg.Gate.MaxOutputTokens = defaults.Gate.MaxOutputTokens
	}
	if cfg.Cooldown.MinIntervalMinutes < 0 {
		cfg.Cooldown.MinIntervalMinutes = defaults.Cooldown.MinIntervalMinutes
	}
	if cfg.Cooldown.MaxPerDay <= 0 {
		cfg.Cooldown.MaxPerDay = defaults.Cooldown.MaxPerDay
	}
	if cfg.Cooldown.BackoffAfterIgnored <= 0 {
		cfg.Cooldown.BackoffAfterIgnored = defaults.Cooldown.BackoffAfterIgnored
	}
	if cfg.Cooldown.ReplyAttributionWindowMinutes <= 0 {
		cfg.Cooldown.ReplyAttributionWindowMinutes = defaults.Cooldown.ReplyAttributionWindowMinutes
	}
	if cfg.Candidates.TTLHours <= 0 {
		cfg.Candidates.TTLHours = defaults.Candidates.TTLHours
	}
	if cfg.Candidates.MaxPending <= 0 {
		cfg.Candidates.MaxPending = defaults.Candidates.MaxPending
	}
	return cfg
}

func ValidateProactiveConfig(cfg ProactiveConfig) error {
	for _, window := range cfg.QuietHours {
		if _, err := ParseQuietHourWindow(window); err != nil {
			return fmt.Errorf("proactive.quiet_hours: %w", err)
		}
	}
	return nil
}

// QuietHourWindow is a wall-clock range that may wrap past midnight.
type QuietHourWindow struct {
	StartMinutes int
	EndMinutes   int
}

// ParseQuietHourWindow parses "HH:MM-HH:MM". A window whose end is at or before
// its start wraps to the next day, which is the common case ("23:00-09:00").
func ParseQuietHourWindow(raw string) (QuietHourWindow, error) {
	parts := strings.Split(strings.TrimSpace(raw), "-")
	if len(parts) != 2 {
		return QuietHourWindow{}, fmt.Errorf("quiet hour window %q must be HH:MM-HH:MM", raw)
	}
	start, err := parseClockMinutes(parts[0])
	if err != nil {
		return QuietHourWindow{}, err
	}
	end, err := parseClockMinutes(parts[1])
	if err != nil {
		return QuietHourWindow{}, err
	}
	return QuietHourWindow{StartMinutes: start, EndMinutes: end}, nil
}

// Contains reports whether t falls inside the window, handling midnight wrap.
func (w QuietHourWindow) Contains(t time.Time) bool {
	minutes := t.Hour()*60 + t.Minute()
	if w.StartMinutes == w.EndMinutes {
		return false
	}
	if w.StartMinutes < w.EndMinutes {
		return minutes >= w.StartMinutes && minutes < w.EndMinutes
	}
	return minutes >= w.StartMinutes || minutes < w.EndMinutes
}

func parseClockMinutes(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	pieces := strings.Split(trimmed, ":")
	if len(pieces) != 2 {
		return 0, fmt.Errorf("clock time %q must be HH:MM", trimmed)
	}
	hour, err := strconv.Atoi(pieces[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, fmt.Errorf("clock time %q has invalid hour", trimmed)
	}
	minute, err := strconv.Atoi(pieces[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("clock time %q has invalid minute", trimmed)
	}
	return hour*60 + minute, nil
}
