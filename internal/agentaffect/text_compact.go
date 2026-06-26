package agentaffect

import (
	"context"
	"strings"

	"github.com/longyisang/emoagent/internal/tokenmeter"
)

func compactWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func compactText(value string, maxRunes int) string {
	value = compactWhitespace(value)
	if maxRunes <= 0 {
		return value
	}
	return truncateRunes(value, maxRunes)
}

func runeLen(value string) int {
	return len([]rune(value))
}

func estimateTokens(value string) int {
	return tokenmeter.DefaultCounter().CountText(context.Background(), "", "", value).InputTokens
}
