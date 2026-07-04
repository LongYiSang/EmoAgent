package onebotv11

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/longyisang/emoagent/internal/conversation"
	"github.com/longyisang/emoagent/internal/platform"
)

const commandCoalesceWindow = 2 * time.Second

type Sink struct {
	client  ActionClient
	profile Profile
	cfg     OutboundConfig

	mu          sync.Mutex
	lastCommand commandEventFingerprint
}

type commandEventFingerprint struct {
	Type      string
	OriginKey string
	SessionID string
	Status    string
	Content   string
	At        time.Time
}

func NewSink(client ActionClient, profile Profile, cfg OutboundConfig) *Sink {
	if cfg.MaxMessageChars == 0 {
		cfg.MaxMessageChars = 1800
	}
	if cfg.OutputFormat == "" {
		cfg.OutputFormat = profile.DefaultOutputFormat
	}
	if cfg.OutputFormat == "" {
		cfg.OutputFormat = MessageFormatArray
	}
	return &Sink{client: client, profile: profile, cfg: cfg}
}

func (s *Sink) Emit(ctx context.Context, event platform.OutboundEvent) error {
	if s == nil || s.client == nil {
		return nil
	}
	content := strings.TrimSpace(event.Content)
	if content == "" {
		return nil
	}
	if s.shouldSuppress(event) {
		return nil
	}
	for _, part := range splitOutboundText(content, s.cfg) {
		req, err := s.requestForEvent(event, part)
		if err != nil {
			return err
		}
		if req.Action == "" {
			continue
		}
		resp, err := s.client.Call(ctx, req)
		if err != nil {
			return err
		}
		if !s.profile.RetcodeSuccess(resp) {
			return ActionRetcodeError{Action: req.Action, Status: resp.Status, Retcode: resp.Retcode, Wording: resp.Wording, Response: resp}
		}
	}
	s.rememberCommand(event)
	return nil
}

func (s *Sink) shouldSuppress(event platform.OutboundEvent) bool {
	if !s.cfg.CoalesceCommandEvents || event.Type != "command_result" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	last := s.lastCommand
	return last.Type == "context_switched" &&
		last.OriginKey == event.Origin.OriginKey &&
		last.SessionID == event.SessionID &&
		last.Status == event.Status &&
		last.Content == strings.TrimSpace(event.Content) &&
		time.Since(last.At) <= commandCoalesceWindow
}

func (s *Sink) rememberCommand(event platform.OutboundEvent) {
	if !s.cfg.CoalesceCommandEvents {
		return
	}
	if event.Type != "context_switched" && event.Type != "command_result" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastCommand = commandEventFingerprint{
		Type:      event.Type,
		OriginKey: event.Origin.OriginKey,
		SessionID: event.SessionID,
		Status:    event.Status,
		Content:   strings.TrimSpace(event.Content),
		At:        time.Now(),
	}
}

func (s *Sink) requestForEvent(event platform.OutboundEvent, text string) (ActionRequest, error) {
	message := outboundTextMessage(text, s.cfg.OutputFormat)
	switch event.Origin.ChannelType {
	case "private", "":
		userID := firstNonEmpty(event.Origin.ExternalActorID, event.Origin.ExternalConversationID)
		if userID == "" {
			return ActionRequest{}, fmt.Errorf("onebot private outbound requires external actor id")
		}
		return sendPrivateMsgRequest(userID, message, s.cfg.AutoEscape), nil
	case "group":
		if !s.cfg.GroupEnabled {
			return ActionRequest{}, fmt.Errorf("onebot group outbound is disabled")
		}
		groupID := event.Origin.ExternalConversationID
		if groupID == "" {
			return ActionRequest{}, fmt.Errorf("onebot group outbound requires external conversation id")
		}
		return sendGroupMsgRequest(groupID, message, s.cfg.AutoEscape), nil
	default:
		return ActionRequest{}, fmt.Errorf("unsupported onebot outbound channel %q", event.Origin.ChannelType)
	}
}

func splitOutboundText(content string, cfg OutboundConfig) []string {
	if !cfg.SplitLongMessages || cfg.MaxMessageChars <= 0 || utf8.RuneCountInString(content) <= cfg.MaxMessageChars {
		return []string{content}
	}
	var out []string
	for _, paragraph := range strings.SplitAfter(content, "\n") {
		runes := []rune(paragraph)
		for len(runes) > cfg.MaxMessageChars {
			out = append(out, string(runes[:cfg.MaxMessageChars]))
			runes = runes[cfg.MaxMessageChars:]
		}
		if len(runes) > 0 {
			out = append(out, string(runes))
		}
	}
	if len(out) == 0 {
		return []string{content}
	}
	return out
}

func originChannel(origin conversation.Origin) string {
	if origin.ChannelType == "" {
		return "private"
	}
	return origin.ChannelType
}
