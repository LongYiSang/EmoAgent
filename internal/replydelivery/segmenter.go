package replydelivery

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/longyisang/emoagent/internal/config"
)

type Segment struct {
	Index     int
	Text      string
	WordCount int
}

type Plan struct {
	Mode           string   `json:"mode"`
	Strategy       string   `json:"strategy"`
	Segments       []string `json:"segments"`
	SegmentCount   int      `json:"segment_count"`
	Suppressed     bool     `json:"suppressed"`
	SuppressReason string   `json:"suppress_reason,omitempty"`
}

func BuildPlan(cfg config.ReplyDeliveryConfig, promptMode string, realtimeStreaming bool, text string) Plan {
	cfg = config.NormalizeReplyDeliveryConfig(cfg)
	strategy := cfg.Segment.SplitMode
	if reason := suppressReason(cfg, promptMode, realtimeStreaming, text); reason != "" {
		return Plan{
			Mode:           promptMode,
			Strategy:       strategy,
			Segments:       []string{text},
			SegmentCount:   1,
			Suppressed:     true,
			SuppressReason: reason,
		}
	}
	segments := SplitText(cfg.Segment, text)
	if len(segments) == 0 {
		segments = []string{text}
	}
	return Plan{
		Mode:         promptMode,
		Strategy:     strategy,
		Segments:     segments,
		SegmentCount: len(segments),
	}
}

func (p Plan) ShouldEmitSegments() bool {
	return !p.Suppressed && len(p.Segments) > 1
}

func SplitText(cfg config.ReplySegmentConfig, text string) []string {
	wrapper := config.DefaultReplyDeliveryConfig()
	wrapper.Segment = cfg
	cfg = config.NormalizeReplyDeliveryConfig(wrapper).Segment
	if strings.TrimSpace(text) == "" {
		return []string{text}
	}

	ranges := protectedRanges(cfg, text)
	var raw []string
	if cfg.SplitMode == "regex" {
		if re, err := regexp.Compile(cfg.Regex); err == nil {
			raw = splitRegex(text, ranges, re)
		}
	}
	if raw == nil {
		raw = splitNatural(text, ranges, cfg.SplitWords)
	}
	segments := cleanupSegments(raw, cfg.CleanupRegex)
	if len(segments) == 0 || len(segments) > cfg.MaxSegments {
		return []string{text}
	}
	return segments
}

func splitNatural(text string, ranges []protectedRange, splitWords []string) []string {
	words := append([]string(nil), splitWords...)
	sort.SliceStable(words, func(i, j int) bool {
		return len(words[i]) > len(words[j])
	})
	var segments []string
	start := 0
	for idx, r := range text {
		end := idx + utf8.RuneLen(r)
		if isProtected(ranges, idx) {
			continue
		}
		current := text[start:end]
		for _, word := range words {
			if word == "" || !strings.HasSuffix(current, word) {
				continue
			}
			if strings.TrimSpace(current) != "" {
				segments = append(segments, current)
			}
			start = end
			break
		}
	}
	if start < len(text) {
		segments = append(segments, text[start:])
	}
	return segments
}

func splitRegex(text string, ranges []protectedRange, re *regexp.Regexp) []string {
	var segments []string
	cursor := 0
	for _, pr := range ranges {
		if cursor < pr.start {
			segments = appendRegexSegments(segments, text[cursor:pr.start], re)
		}
		if pr.start < pr.end {
			segments = append(segments, text[pr.start:pr.end])
		}
		cursor = pr.end
	}
	if cursor < len(text) {
		segments = appendRegexSegments(segments, text[cursor:], re)
	}
	return segments
}

func appendRegexSegments(segments []string, span string, re *regexp.Regexp) []string {
	matches := re.FindAllString(span, -1)
	if len(matches) == 0 {
		return append(segments, span)
	}
	return append(segments, matches...)
}

func cleanupSegments(raw []string, cleanupRegex string) []string {
	var cleanup *regexp.Regexp
	if strings.TrimSpace(cleanupRegex) != "" {
		cleanup, _ = regexp.Compile(cleanupRegex)
	}
	segments := make([]string, 0, len(raw))
	for _, segment := range raw {
		if cleanup != nil {
			segment = cleanup.ReplaceAllString(segment, "")
		}
		segment = strings.TrimSpace(segment)
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	return segments
}
