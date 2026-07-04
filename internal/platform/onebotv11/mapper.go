package onebotv11

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/platform"
)

func MapEvent(event Event, cfg Config, profile Profile) (platform.InboundMessage, bool, error) {
	if profile.NormalizeEvent != nil {
		profile.NormalizeEvent(&event)
	}
	if event.PostType != "message" {
		return platform.InboundMessage{}, false, nil
	}
	if cfg.Routing.IgnoreSelfMessages && event.SelfID != 0 && event.UserID == event.SelfID {
		return platform.InboundMessage{}, false, nil
	}
	switch event.MessageType {
	case "private":
		if !cfg.Routing.PrivateEnabled {
			return platform.InboundMessage{}, false, nil
		}
	case "group":
		return platform.InboundMessage{}, false, nil
	default:
		return platform.InboundMessage{}, false, nil
	}
	rendered := strings.TrimSpace(RenderInboundMessage(event.Message, cfg.Message))
	if rendered == "" && strings.TrimSpace(event.RawMessage) != "" {
		rendered = strings.TrimSpace(renderCQString(event.RawMessage, cfg.Message))
	}
	if rendered == "" {
		return platform.InboundMessage{}, false, nil
	}
	conversationID := strconv.FormatInt(event.UserID, 10)
	originScope := cfg.Routing.PrivateScope
	channelType := "private"
	if event.MessageType == "group" {
		conversationID = strconv.FormatInt(event.GroupID, 10)
		originScope = cfg.Routing.GroupScope
		channelType = "group"
	}
	actorID := strconv.FormatInt(event.UserID, 10)
	in := platform.InboundMessage{
		ExternalMessageID:      compositeExternalMessageID(event, conversationID),
		SourceType:             cfg.SourceType,
		AdapterInstanceID:      cfg.InstanceID,
		PlatformID:             cfg.PlatformID,
		ChannelType:            channelType,
		ExternalConversationID: conversationID,
		ExternalActorID:        actorID,
		Text:                   rendered,
		Actor: platform.Actor{
			ID:          actorID,
			DisplayName: firstNonEmpty(event.Sender.Nickname, event.Sender.Card),
			Role:        MapActorRole(event.Sender.Role),
			IsBot:       false,
		},
		Timestamp:      eventTimestamp(event),
		RawEventHash:   rawEventHash(event),
		Raw:            event.Raw,
		OriginScope:    originScope,
		AcceptedReason: "private",
	}
	if event.MessageType == "group" {
		in.AcceptedReason = "group"
	}
	return in, true, nil
}

func compositeExternalMessageID(event Event, conversationID string) string {
	return strings.Join([]string{
		strconv.FormatInt(event.SelfID, 10),
		event.MessageType,
		conversationID,
		event.messageIDString(),
	}, ":")
}

func MapActorRole(role string) platform.ActorRole {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "owner":
		return platform.ActorRoleOwner
	case "admin":
		return platform.ActorRoleAdmin
	default:
		return platform.ActorRoleMember
	}
}

func eventTimestamp(event Event) time.Time {
	if event.Time <= 0 {
		return time.Time{}
	}
	return time.Unix(event.Time, 0)
}

func rawEventHash(event Event) string {
	if event.Raw == nil {
		return ""
	}
	data, err := json.Marshal(event.Raw)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
