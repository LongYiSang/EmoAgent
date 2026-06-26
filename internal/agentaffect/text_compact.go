package agentaffect

import "strings"

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
	if value == "" {
		return 0
	}
	ascii := 0
	nonASCII := 0
	for _, r := range value {
		if r <= 0x7f {
			ascii++
		} else {
			nonASCII++
		}
	}
	base := (ascii + 3) / 4
	base += nonASCII
	margin := (base + 9) / 10
	if margin < 1 {
		margin = 1
	}
	return base + margin
}
