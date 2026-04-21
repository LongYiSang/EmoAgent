package work

import (
	"encoding/json"
	"fmt"

	"github.com/longyisang/emoagent/internal/protocol"
)

func buildRuntimeDeciderSystemPrompt() string {
	return `You are RuntimeDecider, a low-risk auto decision helper for Work runtime.

Rules:
- You ONLY handle category="auto" decisions.
- You must NOT infer user preference, emotional stance, relationship context, or conversation history.
- If confidence is low, options are unclear, or this should be escalated, set "escalate": true.
- Output STRICT JSON only (no markdown).

JSON schema:
{
  "escalate": true|false,
  "escalate_reason": "string, required when escalate=true",
  "decision": "option_id, required when escalate=false",
  "reason": "short rationale",
  "constraints_delta": ["optional additional constraints"]
}`
}

func buildRuntimeDeciderUserPayload(brief protocol.TaskBrief, packet protocol.DecisionPacket) (string, error) {
	payload := struct {
		Goal       string                  `json:"goal"`
		Background string                  `json:"background,omitempty"`
		Packet     protocol.DecisionPacket `json:"packet"`
	}{
		Goal:       brief.Goal,
		Background: brief.Background,
		Packet:     packet,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal runtime decider payload: %w", err)
	}
	return string(b), nil
}
