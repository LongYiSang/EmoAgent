package replydelivery

import (
	"strings"
	"unicode/utf8"

	"github.com/longyisang/emoagent/internal/config"
	contextutil "github.com/longyisang/emoagent/internal/context"
)

type Mode string

const (
	ModeCasualChat Mode = "casual_chat"
	ModeWorkMode   Mode = "work_mode"
)

func ShouldSegment(cfg config.ReplyDeliveryConfig, promptMode string, realtimeStreaming bool, text string) bool {
	return suppressReason(cfg, promptMode, realtimeStreaming, text) == ""
}

func suppressReason(cfg config.ReplyDeliveryConfig, promptMode string, realtimeStreaming bool, text string) string {
	cfg = config.NormalizeReplyDeliveryConfig(cfg)
	if !cfg.Enabled {
		return "disabled"
	}
	if strings.TrimSpace(text) == "" {
		return "empty_text"
	}
	if cfg.DisableWhenRealtimeStreaming && realtimeStreaming {
		return "realtime_streaming"
	}
	mode := string(contextutil.NormalizePromptMode(contextutil.PromptMode(strings.TrimSpace(promptMode))))
	if mode == string(contextutil.PromptModeWorkMode) {
		return "work_mode"
	}
	if !modeAllowed(cfg.ApplyPromptModes, mode) {
		return "prompt_mode_not_segmentable"
	}
	if utf8.RuneCountInString(text) > cfg.Segment.LongTextThreshold {
		return "long_text"
	}
	return ""
}

func modeAllowed(allowed []string, mode string) bool {
	for _, item := range allowed {
		if strings.TrimSpace(item) == mode {
			return true
		}
	}
	return false
}
