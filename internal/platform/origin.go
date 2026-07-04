package platform

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/longyisang/emoagent/internal/conversation"
)

const maxOriginSegmentLength = 40

type OriginScope string

const (
	OriginScopePrivate         OriginScope = "private"
	OriginScopeGroupShared     OriginScope = "group_shared"
	OriginScopeGroupUserUnique OriginScope = "group_user_unique"
)

type OriginBuildRequest struct {
	SourceType             string
	AdapterInstanceID      string
	PlatformID             string
	ChannelType            string
	ExternalConversationID string
	ExternalActorID        string
	Scope                  OriginScope
}

func BuildOriginKey(req OriginBuildRequest) (string, error) {
	source, err := originSegment(req.SourceType, "source_type")
	if err != nil {
		return "", err
	}
	instance, err := originSegment(req.AdapterInstanceID, "adapter_instance_id")
	if err != nil {
		return "", err
	}
	scope := req.Scope
	if scope == "" {
		if strings.TrimSpace(req.ChannelType) == "group" {
			scope = OriginScopeGroupShared
		} else {
			scope = OriginScopePrivate
		}
	}
	switch scope {
	case OriginScopePrivate:
		externalID := firstNonEmptyPlatformValue(req.ExternalActorID, req.ExternalConversationID)
		actor, err := originSegment(externalID, "external_actor_id")
		if err != nil {
			return "", err
		}
		return strings.Join([]string{source, instance, "private", actor}, ":"), nil
	case OriginScopeGroupShared:
		conversationID, err := originSegment(req.ExternalConversationID, "external_conversation_id")
		if err != nil {
			return "", err
		}
		return strings.Join([]string{source, instance, "group", conversationID}, ":"), nil
	case OriginScopeGroupUserUnique:
		conversationID, err := originSegment(req.ExternalConversationID, "external_conversation_id")
		if err != nil {
			return "", err
		}
		actor, err := originSegment(req.ExternalActorID, "external_actor_id")
		if err != nil {
			return "", err
		}
		return strings.Join([]string{source, instance, "group_user", conversationID, actor}, ":"), nil
	default:
		return "", fmt.Errorf("unsupported origin scope %q", scope)
	}
}

func OriginFromInbound(in InboundMessage, scope OriginScope) (conversation.Origin, error) {
	originKey, err := BuildOriginKey(OriginBuildRequest{
		SourceType:             in.SourceType,
		AdapterInstanceID:      in.AdapterInstanceID,
		PlatformID:             in.PlatformID,
		ChannelType:            in.ChannelType,
		ExternalConversationID: in.ExternalConversationID,
		ExternalActorID:        firstNonEmptyPlatformValue(in.ExternalActorID, in.Actor.ID),
		Scope:                  scope,
	})
	if err != nil {
		return conversation.Origin{}, err
	}
	return conversation.ResolveOrigin(conversation.OriginRequest{
		OriginKey:              originKey,
		SourceType:             in.SourceType,
		AdapterInstanceID:      in.AdapterInstanceID,
		PlatformID:             in.PlatformID,
		ChannelType:            in.ChannelType,
		ExternalConversationID: in.ExternalConversationID,
		ExternalActorID:        firstNonEmptyPlatformValue(in.ExternalActorID, in.Actor.ID),
		DisplayName:            in.Actor.DisplayName,
	})
}

func originSegment(value string, field string) (string, error) {
	value = sanitizeOriginSegment(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	return value, nil
}

func sanitizeOriginSegment(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		ok := r == '.' || r == '-' || r == '_' ||
			(r >= 'A' && r <= 'Z') ||
			(r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9')
		if !ok {
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
			continue
		}
		b.WriteRune(r)
		lastUnderscore = r == '_'
	}
	value = strings.Trim(b.String(), "_")
	if len(value) <= maxOriginSegmentLength {
		return value
	}
	sum := sha1.Sum([]byte(value))
	suffix := "_" + hex.EncodeToString(sum[:4])
	prefixLen := maxOriginSegmentLength - len(suffix)
	return strings.TrimRight(value[:prefixLen], "._-") + suffix
}

func firstNonEmptyPlatformValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
