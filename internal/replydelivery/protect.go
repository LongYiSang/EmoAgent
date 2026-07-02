package replydelivery

import (
	"regexp"
	"sort"
	"strings"

	"github.com/longyisang/emoagent/internal/config"
)

type protectedRange struct {
	start int
	end   int
}

var urlPattern = regexp.MustCompile(`https?://[^\s。！？~～…]+`)

func protectedRanges(cfg config.ReplySegmentConfig, text string) []protectedRange {
	var ranges []protectedRange
	if cfg.ProtectCodeBlocks {
		ranges = append(ranges, codeBlockRanges(text)...)
	}
	if cfg.ProtectMarkdownTables {
		ranges = append(ranges, markdownTableRanges(text)...)
	}
	if cfg.ProtectURLs {
		for _, loc := range urlPattern.FindAllStringIndex(text, -1) {
			ranges = append(ranges, protectedRange{start: loc[0], end: loc[1]})
		}
	}
	return mergeRanges(ranges)
}

func codeBlockRanges(text string) []protectedRange {
	var ranges []protectedRange
	cursor := 0
	for cursor < len(text) {
		tick := strings.Index(text[cursor:], "```")
		tilde := strings.Index(text[cursor:], "~~~")
		offset, fence := firstFence(tick, tilde)
		if offset < 0 {
			break
		}
		start := cursor + offset
		searchFrom := start + len(fence)
		endOffset := strings.Index(text[searchFrom:], fence)
		if endOffset < 0 {
			ranges = append(ranges, protectedRange{start: start, end: len(text)})
			break
		}
		end := searchFrom + endOffset + len(fence)
		ranges = append(ranges, protectedRange{start: start, end: end})
		cursor = end
	}
	return ranges
}

func firstFence(tick, tilde int) (int, string) {
	switch {
	case tick < 0 && tilde < 0:
		return -1, ""
	case tick < 0:
		return tilde, "~~~"
	case tilde < 0:
		return tick, "```"
	case tick <= tilde:
		return tick, "```"
	default:
		return tilde, "~~~"
	}
}

func markdownTableRanges(text string) []protectedRange {
	type lineRange struct {
		start int
		end   int
		text  string
	}
	var lines []lineRange
	start := 0
	for start <= len(text) {
		next := strings.IndexByte(text[start:], '\n')
		end := len(text)
		if next >= 0 {
			end = start + next
		}
		lines = append(lines, lineRange{start: start, end: end, text: text[start:end]})
		if next < 0 {
			break
		}
		start = end + 1
	}

	var ranges []protectedRange
	for i := 0; i < len(lines); {
		if !strings.Contains(lines[i].text, "|") {
			i++
			continue
		}
		j := i
		for j < len(lines) && strings.Contains(lines[j].text, "|") {
			j++
		}
		if j-i >= 2 {
			ranges = append(ranges, protectedRange{start: lines[i].start, end: lines[j-1].end})
		}
		i = j
	}
	return ranges
}

func mergeRanges(ranges []protectedRange) []protectedRange {
	if len(ranges) == 0 {
		return nil
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].start == ranges[j].start {
			return ranges[i].end < ranges[j].end
		}
		return ranges[i].start < ranges[j].start
	})
	merged := []protectedRange{ranges[0]}
	for _, current := range ranges[1:] {
		last := &merged[len(merged)-1]
		if current.start <= last.end {
			if current.end > last.end {
				last.end = current.end
			}
			continue
		}
		merged = append(merged, current)
	}
	return merged
}

func isProtected(ranges []protectedRange, index int) bool {
	for _, pr := range ranges {
		if index < pr.start {
			return false
		}
		if index >= pr.start && index < pr.end {
			return true
		}
	}
	return false
}
