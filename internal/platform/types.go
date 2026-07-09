package platform

import (
	"time"

	"github.com/longyisang/emoagent/internal/conversation"
	"github.com/longyisang/emoagent/internal/llm"
)

type InboundMessage struct {
	ID                     string
	RequestID              string
	ExternalMessageID      string
	SourceType             string
	AdapterInstanceID      string
	PlatformID             string
	ChannelType            string
	ExternalConversationID string
	ExternalActorID        string
	PersonaKey             string
	Text                   string
	Parts                  []llm.ContentBlock
	Actor                  Actor
	Timestamp              time.Time
	RawEventHash           string
	Raw                    map[string]any
	OriginScope            OriginScope
	AcceptedReason         string
}

type OutboundEvent struct {
	Type                     string
	Origin                   conversation.Origin
	SessionID                string
	PersonaKey               string
	Content                  string
	Status                   string
	ErrorKind                string
	Payload                  map[string]any
	ReplyToExternalMessageID string
}

type HandleResult struct {
	Handled   bool
	Duplicate bool
	Ignored   bool
	SessionID string
}
