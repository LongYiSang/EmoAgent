package context

import (
	"fmt"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/llm"
	"github.com/longyisang/emoagent/internal/runtimeenv"
	"github.com/longyisang/emoagent/internal/storage"
)

// resolveEnvLocation picks the location the user's clock actually runs in,
// falling back to the host's local zone when the environment says nothing.
func resolveEnvLocation(env runtimeenv.Facts) *time.Location {
	if timezone := strings.TrimSpace(env.Timezone); timezone != "" {
		if loaded, err := time.LoadLocation(timezone); err == nil {
			return loaded
		}
	}
	return time.Local
}

// timeGapAnnotationThreshold is how far apart two consecutive messages must be
// before the gap is spelled out in context. Below it the conversation reads as
// one continuous scene, which is what it actually is.
const timeGapAnnotationThreshold = time.Hour

// buildTimeAnchors returns, per message ID, the text to prepend to that message
// before it reaches the model.
//
// History messages otherwise carry no time at all, so anything the assistant
// once said about the clock ("都快四点半了") keeps reading as current no matter
// how much later it is replayed. User messages get an absolute timestamp, and
// any message that follows a long silence gets the gap spelled out. Assistant
// messages are left untimestamped so the persona's own voice stays clean.
func buildTimeAnchors(records []storage.MessageRecord, loc *time.Location) map[string]string {
	if loc == nil {
		loc = time.Local
	}
	anchors := make(map[string]string)
	var previous time.Time
	for _, record := range records {
		at, ok := parseMessageTime(record.CreatedAt)
		if !ok {
			continue
		}
		at = at.In(loc)

		var parts []string
		if !previous.IsZero() {
			if gap := at.Sub(previous); gap >= timeGapAnnotationThreshold {
				parts = append(parts, formatTimeGapNote(gap))
			}
		}
		previous = at

		if record.Role == "user" {
			parts = append(parts, formatMessageTimePrefix(at))
		}
		if len(parts) == 0 {
			continue
		}
		if record.ID == "" {
			continue
		}
		anchors[record.ID] = strings.Join(parts, "\n")
	}
	return anchors
}

// deltaLocation reports the zone the given messages were written in.
//
// Message timestamps carry the offset the storage layer was configured with
// (config.Time.Timezone), which need not be the host's. Rendering a clock for
// the summariser in any other zone would leave it dating facts against a
// different offset than the messages it is reading.
func deltaLocation(records []storage.MessageRecord) *time.Location {
	for i := len(records) - 1; i >= 0; i-- {
		if at, ok := parseMessageTime(records[i].CreatedAt); ok {
			return at.Location()
		}
	}
	return time.Local
}

func parseMessageTime(text string) (time.Time, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func formatMessageTimePrefix(at time.Time) string {
	return fmt.Sprintf(
		"[%04d-%02d-%02d %02d:%02d %s]",
		at.Year(),
		int(at.Month()),
		at.Day(),
		at.Hour(),
		at.Minute(),
		formatShortChineseWeekday(at.Weekday()),
	)
}

func formatTimeGapNote(gap time.Duration) string {
	return "（距上一条 " + humanizeDurationZH(gap) + "）"
}

func formatShortChineseWeekday(day time.Weekday) string {
	return strings.Replace(formatChineseWeekday(day), "星期", "周", 1)
}

// ApplyTimeAnchors prepends each anchor to the message it belongs to.
//
// This runs on the final message slice rather than at assembly time on purpose:
// llm.RenderMessage rebuilds Content from ContentBlocks whenever a message
// carries media, so a prefix written any earlier is silently dropped for
// exactly the messages it would be hardest to notice it missing on.
//
// Messages without an ID — the running_summary and tool_digest slots — are
// skipped, which is why the empty key is never consulted.
func ApplyTimeAnchors(messages []llm.Message, anchors map[string]string) []llm.Message {
	if len(anchors) == 0 {
		return messages
	}
	out := append([]llm.Message(nil), messages...)
	for i := range out {
		if out[i].ID == "" {
			continue
		}
		anchor := anchors[out[i].ID]
		if anchor == "" {
			continue
		}
		out[i].Content = anchor + "\n" + out[i].Content
	}
	return out
}
