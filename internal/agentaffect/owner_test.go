package agentaffect

import (
	"testing"

	"github.com/longyisang/emoagent/internal/config"
)

func TestResolveMoodOwnerPersonaAndSessionScope(t *testing.T) {
	cfg := config.DefaultConfig().AgentAffect
	owner := ResolveMoodOwner(cfg, "default", "session-1")
	if owner.Scope != "persona" || owner.ID != "persona:default" {
		t.Fatalf("default owner = %#v, want persona owner", owner)
	}

	cfg.State.Scope = "session"
	owner = ResolveMoodOwner(cfg, "default", "session-1")
	if owner.Scope != "session" || owner.ID != "session:session-1" {
		t.Fatalf("session owner = %#v, want session owner", owner)
	}
}
