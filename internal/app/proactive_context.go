package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/longyisang/emoagent/internal/agentaffect"
	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/proactive"
	"github.com/longyisang/emoagent/internal/storage"
)

// hostContextProvider supplies the relationship and mood blocks of the gate
// input from state the host already keeps.
//
// The gate deliberately never sees raw conversation text: it runs on a cheaper,
// separate model, and this agent holds intimate conversations. The running
// summary — already a compressed artefact the host generated itself — stands in
// for it. Its open loops and promises are also the best reasons to speak up at
// all, since they turn a proactive message from small talk into following
// through on something.
type hostContextProvider struct {
	db          *storage.DB
	agentAffect *AgentAffectService
}

func (p *hostContextProvider) Relationship(ctx context.Context, personaKey, sessionID string) proactive.GateRelationship {
	if p == nil || p.db == nil || strings.TrimSpace(sessionID) == "" {
		return proactive.GateRelationship{}
	}
	summary, ok := p.runningSummary(ctx, sessionID)
	if !ok {
		return proactive.GateRelationship{}
	}
	return proactive.GateRelationship{
		SessionGoal:   summary.SessionGoal,
		OpenLoops:     summary.OpenLoops,
		PromisesMade:  summary.RelationshipState.PromisesMade,
		Tone:          summary.RelationshipState.Tone,
		RecentEmotion: summary.RelationshipState.RecentEmotion,
	}
}

func (p *hostContextProvider) runningSummary(ctx context.Context, sessionID string) (contextutil.RunningSummary, bool) {
	session, err := p.db.GetSession(ctx, sessionID)
	if err != nil || session == nil || strings.TrimSpace(session.Metadata) == "" {
		return contextutil.RunningSummary{}, false
	}
	var metadata struct {
		ContextState *struct {
			RunningSummary contextutil.RunningSummary `json:"running_summary"`
		} `json:"context_state"`
	}
	if err := json.Unmarshal([]byte(session.Metadata), &metadata); err != nil || metadata.ContextState == nil {
		return contextutil.RunningSummary{}, false
	}
	if metadata.ContextState.RunningSummary.IsZero() {
		return contextutil.RunningSummary{}, false
	}
	return metadata.ContextState.RunningSummary, true
}

// Mood returns four of the eleven affect dimensions. The full vector is noise
// for a cheap classifier; these are the ones that plausibly bear on whether to
// reach out.
func (p *hostContextProvider) Mood(ctx context.Context, personaKey, sessionID string) proactive.GateMood {
	if p == nil || p.agentAffect == nil {
		return proactive.GateMood{}
	}
	runtime := p.agentAffect.Runtime()
	if runtime == nil {
		return proactive.GateMood{}
	}
	resp, err := runtime.GetCurrentMood(ctx, agentaffect.GetCurrentMoodRequest{
		PersonaID: personaKey,
		SessionID: sessionID,
		View:      "plugin_safe",
	})
	if err != nil || !resp.Enabled {
		return proactive.GateMood{}
	}
	mood := proactive.GateMood{
		Concern:     resp.Mood.Vector.Concern,
		Attachment:  resp.Mood.Vector.Attachment,
		Playfulness: resp.Mood.Vector.Playfulness,
		Energy:      resp.Mood.Vector.Energy,
	}
	mood.Description = describeMood(mood)
	return mood
}

func describeMood(mood proactive.GateMood) string {
	parts := make([]string, 0, 4)
	if mood.Concern > 0.6 {
		parts = append(parts, "有点担心")
	}
	if mood.Attachment > 0.6 {
		parts = append(parts, "亲近感强")
	}
	if mood.Playfulness > 0.6 {
		parts = append(parts, "想玩闹")
	}
	switch {
	case mood.Energy > 0.7:
		parts = append(parts, "精神不错")
	case mood.Energy < 0.3:
		parts = append(parts, "有点没劲")
	}
	if len(parts) == 0 {
		return "状态平稳"
	}
	return strings.Join(parts, "，")
}
