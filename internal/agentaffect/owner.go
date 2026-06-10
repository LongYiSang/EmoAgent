package agentaffect

import (
	"strings"

	"github.com/longyisang/emoagent/internal/config"
)

type MoodOwner struct {
	Scope string `json:"scope"`
	ID    string `json:"id"`
}

func ResolveMoodOwner(cfg config.AgentAffectConfig, personaID, sessionID string) MoodOwner {
	switch strings.TrimSpace(cfg.State.Scope) {
	case "session":
		return MoodOwner{Scope: "session", ID: "session:" + strings.TrimSpace(sessionID)}
	default:
		return MoodOwner{Scope: "persona", ID: "persona:" + strings.TrimSpace(personaID)}
	}
}

func moodOwnerSessionID(owner MoodOwner, sessionID string) string {
	if owner.Scope == "session" {
		return sessionID
	}
	return ""
}
