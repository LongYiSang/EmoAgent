package work

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/longyisang/emoagent/internal/tool"
)

const requestDecisionDescription = `Request a decision from the runtime when Work cannot proceed.

Rules:
- request_decision MUST be the sole tool call in the round.
- relevant_findings must contain summarized facts only. Never paste raw tool output.
- choose the most specific category (auto / emotion_judgment / human_confirmation).
- use emotion_judgment only when Emotion should decide using relationship, tone, preference, or emotional context.
- human_confirmation is for user choice, not tool permission escalation.
- for human_confirmation include relevant_findings or key_tradeoffs.
- for human_confirmation also include recommendation_reason and reject_option_id.
- never try to request destructive permission via request_decision; runtime will pause separately if scope escalation is needed.
- never use tool_approval; runtime sets that automatically.
- include clear options.`

var requestDecisionSchema = json.RawMessage(`{
  "type":"object",
  "properties":{
    "task_id":{"type":"string"},
    "category":{"type":"string","enum":["auto","emotion_judgment","human_confirmation"]},
    "goal_summary":{"type":"string"},
    "question":{"type":"string"},
    "why_blocked":{"type":"string"},
    "options":{
      "type":"array",
      "items":{
        "type":"object",
        "properties":{
          "id":{"type":"string"},
          "summary":{"type":"string"},
          "pros":{"type":"array","items":{"type":"string"}},
          "cons":{"type":"array","items":{"type":"string"}},
          "side_effects":{"type":"array","items":{"type":"string"}}
        },
        "required":["id","summary"],
        "additionalProperties":false
      }
    },
    "relevant_findings":{
      "type":"array",
      "items":{
        "type":"object",
        "properties":{
          "finding":{"type":"string","description":"Summarized fact only; never paste raw tool output."},
          "source":{"type":"string"}
        },
        "required":["finding"],
        "additionalProperties":false
      }
    },
    "key_tradeoffs":{
      "type":"array",
      "items":{
        "type":"object",
        "properties":{
          "dimension":{"type":"string"},
          "note":{"type":"string"}
        },
        "required":["dimension","note"],
        "additionalProperties":false
      }
    },
    "recommended_option":{"type":"string"},
    "recommendation_reason":{"type":"string"},
    "reject_option_id":{"type":"string"},
    "suggests_user_input":{"type":"boolean"},
    "created_at":{"type":"string","format":"date-time"}
  },
  "required":["task_id","category","goal_summary","question","why_blocked","options","suggests_user_input"],
  "additionalProperties":false
}`)

// NewRequestDecisionTool returns the Work-side escalation tool spec.
func NewRequestDecisionTool() tool.Spec {
	return tool.Spec{
		Name:        "request_decision",
		Description: requestDecisionDescription,
		Parameters:  requestDecisionSchema,
		Scope:       tool.ScopeWork,
		Permission:  tool.PermReadOnly,
	}
}

// RequestDecisionPlaceholderHandler should never execute when Runtime interception works.
func RequestDecisionPlaceholderHandler(context.Context, json.RawMessage) (json.RawMessage, error) {
	return nil, fmt.Errorf("request_decision must be intercepted by runtime")
}
