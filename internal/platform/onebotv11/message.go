package onebotv11

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

type Segment struct {
	Type string            `json:"type"`
	Data map[string]string `json:"data"`
}

func (s *Segment) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type string         `json:"type"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.Type = raw.Type
	s.Data = map[string]string{}
	for key, value := range raw.Data {
		switch typed := value.(type) {
		case string:
			s.Data[key] = typed
		case nil:
		default:
			s.Data[key] = fmt.Sprint(typed)
		}
	}
	return nil
}

type Message []Segment

type RawMessageValue struct {
	Segments Message
	String   string
	IsString bool
	Raw      json.RawMessage
}

func (v *RawMessageValue) UnmarshalJSON(data []byte) error {
	v.Raw = append(v.Raw[:0], data...)
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		v.String = s
		v.IsString = true
		return nil
	}
	var segments Message
	if err := json.Unmarshal(data, &segments); err == nil {
		v.Segments = segments
		v.IsString = false
		return nil
	}
	return nil
}

func RenderInboundMessage(value RawMessageValue, cfg MessageConfig) string {
	var text string
	if value.IsString {
		text = renderCQString(value.String, cfg)
	} else {
		text = renderSegments(value.Segments, cfg)
	}
	if cfg.MaxTextChars > 0 && utf8.RuneCountInString(text) > cfg.MaxTextChars {
		return string([]rune(text)[:cfg.MaxTextChars])
	}
	return text
}

func renderSegments(segments Message, cfg MessageConfig) string {
	var b strings.Builder
	for _, segment := range segments {
		switch segment.Type {
		case "text":
			b.WriteString(segment.Data["text"])
		default:
			b.WriteString(segmentPlaceholder(segment.Type, cfg))
		}
	}
	return b.String()
}

var cqPattern = regexp.MustCompile(`\[CQ:([^,\]]+)(?:,[^\]]*)?\]`)

func renderCQString(input string, cfg MessageConfig) string {
	return cqPattern.ReplaceAllStringFunc(input, func(match string) string {
		parts := cqPattern.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		return segmentPlaceholder(parts[1], cfg)
	})
}

func segmentPlaceholder(kind string, cfg MessageConfig) string {
	if cfg.UnsupportedSegmentPolicy != "" && cfg.UnsupportedSegmentPolicy != UnsupportedSegmentPlaceholder {
		return ""
	}
	switch strings.TrimSpace(kind) {
	case "image":
		return "[图片]"
	case "record":
		return "[语音]"
	case "video":
		return "[视频]"
	case "":
		return ""
	default:
		return "[" + kind + "]"
	}
}

func outboundTextMessage(text string, format string) any {
	if format == MessageFormatString {
		return text
	}
	return []Segment{{Type: "text", Data: map[string]string{"text": text}}}
}
