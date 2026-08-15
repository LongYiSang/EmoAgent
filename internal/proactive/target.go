package proactive

import (
	"context"
	"slices"
	"strings"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/conversation"
	"github.com/longyisang/emoagent/internal/storage"
)

// TargetStore supplies the origins a persona could be reached on.
type TargetStore interface {
	ListProactiveTargetCandidates(ctx context.Context, personaKey string) ([]storage.ProactiveTargetCandidate, error)
}

// Target is a resolved delivery destination.
type Target struct {
	Origin            conversation.Origin
	SessionID         string
	AdapterInstanceID string
}

// ResolveTarget picks where to deliver a proactive message.
//
// Candidates never carry a target: "the user is at their computer" has nothing
// to do with which channel to reach them on, so the choice is made here, at
// dispatch time.
//
// Group channels are excluded unless explicitly allowed. A bot speaking up
// unprompted in a group is a categorically different social risk from a private
// message, and it should never happen by default.
func ResolveTarget(ctx context.Context, store TargetStore, cfg config.ProactiveConfig, personaKey string) (Target, bool, error) {
	if store == nil {
		return Target{}, false, nil
	}
	candidates, err := store.ListProactiveTargetCandidates(ctx, personaKey)
	if err != nil {
		return Target{}, false, err
	}
	for _, candidate := range candidates {
		if !strings.EqualFold(strings.TrimSpace(candidate.SourceType), "platform") {
			continue
		}
		if !channelAllowed(candidate.ChannelType, cfg.Targets.AllowGroupChannels) {
			continue
		}
		if len(cfg.Targets.AllowOrigins) > 0 && !slices.Contains(cfg.Targets.AllowOrigins, candidate.OriginKey) {
			continue
		}
		return Target{
			Origin: conversation.Origin{
				OriginKey:              candidate.OriginKey,
				SourceType:             candidate.SourceType,
				AdapterInstanceID:      candidate.AdapterInstanceID,
				PlatformID:             candidate.PlatformID,
				ChannelType:            candidate.ChannelType,
				ExternalConversationID: candidate.ExternalConversationID,
				ExternalActorID:        candidate.ExternalActorID,
				DisplayName:            candidate.DisplayName,
			},
			SessionID:         candidate.SessionID,
			AdapterInstanceID: candidate.AdapterInstanceID,
		}, true, nil
	}
	return Target{}, false, nil
}

func channelAllowed(channelType string, allowGroups bool) bool {
	switch strings.ToLower(strings.TrimSpace(channelType)) {
	case "private", "":
		return true
	default:
		return allowGroups
	}
}
