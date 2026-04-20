package work

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/longyisang/emoagent/internal/tool"
)

var listPendingDecisionsSchema = json.RawMessage(`{
  "type":"object",
  "properties":{
    "status_filter":{
      "type":"array",
      "items":{"type":"string"}
    }
  },
  "additionalProperties":false
}`)

// NewListDecisionsTool builds the Emotion-facing list_pending_decisions tool.
func NewListDecisionsTool(pending *PendingRegistry) (tool.Spec, tool.Handler) {
	spec := tool.Spec{
		Name:        "list_pending_decisions",
		Description: "List persisted pending or expired Work decision objects for the current session.",
		Parameters:  listPendingDecisionsSchema,
		Scope:       tool.ScopeEmotion,
		Permission:  tool.PermReadOnly,
	}

	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var req struct {
			StatusFilter []string `json:"status_filter"`
		}
		if len(input) > 0 {
			if err := decodeStrictJSON(input, &req); err != nil {
				return nil, fmt.Errorf("list_pending_decisions: invalid input: %w", err)
			}
		}
		statuses := req.StatusFilter
		if len(statuses) == 0 {
			statuses = []string{statusPending, statusStale, statusExpiredOpen, statusAutoRejected}
		}
		sessionID := SessionIDFromContext(ctx)
		rows := pending.ListDecisions(sessionID, statuses)
		return json.Marshal(rows)
	}

	return spec, handler
}
