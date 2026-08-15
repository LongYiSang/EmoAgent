package chat

import (
	"fmt"
	"strings"

	"github.com/longyisang/emoagent/internal/turn"
)

// SilentMarker is what Emotion emits to decline a proactive turn it was allowed
// to take. The gate judges whether interrupting is appropriate; this is the
// second gate, where Emotion judges whether it actually has anything to say.
const SilentMarker = "<silent/>"

// buildProactiveTriggerBlock renders the reason the host woke Emotion up.
//
// This is injected as a one-shot system block via the same extraSystem path as
// the memory and agent-affect blocks. It is deliberately NOT persisted to the
// messages table: writing it there would make the next turn read it back as
// something the user said.
func buildProactiveTriggerBlock(trigger *turn.ProactiveTrigger, allowSilent bool) string {
	if trigger == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("[主动开口]\n")
	b.WriteString("用户现在没有跟你说话。系统观察到下面的情况，认为也许值得你主动说点什么。\n")
	if activity := strings.TrimSpace(trigger.Activity); activity != "" {
		b.WriteString("\n观察到的情况：")
		b.WriteString(activity)
		b.WriteString("\n")
	}
	if hint := strings.TrimSpace(trigger.Hint); hint != "" {
		b.WriteString("值得关心的点：")
		b.WriteString(hint)
		b.WriteString("\n")
	}
	b.WriteString("\n请用你自己的语气、像平常那样开口，不要复述上面这段话，也不要提到「系统」「观察」「检测」之类的字眼。")
	if allowSilent {
		fmt.Fprintf(&b, "\n如果你觉得现在没什么真正值得说的，就只输出 %s，不要输出别的任何内容。", SilentMarker)
	}
	return b.String()
}

// isProactiveSilence reports whether Emotion declined to speak. The reply is
// treated as silence only when the marker is essentially the whole output, so a
// reply that merely mentions the marker still gets delivered.
func isProactiveSilence(reply string) bool {
	trimmed := strings.TrimSpace(reply)
	if trimmed == "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(strings.ReplaceAll(trimmed, SilentMarker, "")), "")
}
