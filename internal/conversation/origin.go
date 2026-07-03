package conversation

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	DefaultSourceType = "webui"
	DefaultOriginKey  = "webui:local:main"
	DefaultChannel    = "web"
)

var originKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:._-]{0,191}$`)

type Origin struct {
	OriginKey              string
	SourceType             string
	AdapterInstanceID      string
	PlatformID             string
	ChannelType            string
	ExternalConversationID string
	ExternalActorID        string
	DisplayName            string
}

type OriginRequest struct {
	OriginKey              string
	SourceType             string
	AdapterInstanceID      string
	PlatformID             string
	ChannelType            string
	ExternalConversationID string
	ExternalActorID        string
	DisplayName            string
}

func ResolveOrigin(req OriginRequest) (Origin, error) {
	origin := Origin{
		OriginKey:              firstNonEmpty(req.OriginKey, DefaultOriginKey),
		SourceType:             firstNonEmpty(req.SourceType, DefaultSourceType),
		AdapterInstanceID:      strings.TrimSpace(req.AdapterInstanceID),
		PlatformID:             strings.TrimSpace(req.PlatformID),
		ChannelType:            firstNonEmpty(req.ChannelType, DefaultChannel),
		ExternalConversationID: strings.TrimSpace(req.ExternalConversationID),
		ExternalActorID:        strings.TrimSpace(req.ExternalActorID),
		DisplayName:            strings.TrimSpace(req.DisplayName),
	}
	if err := ValidateOriginKey(origin.OriginKey); err != nil {
		return Origin{}, err
	}
	return origin, nil
}

func ValidateOriginKey(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("origin key is required")
	}
	if !originKeyPattern.MatchString(value) {
		return fmt.Errorf("origin key %q is invalid", value)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
