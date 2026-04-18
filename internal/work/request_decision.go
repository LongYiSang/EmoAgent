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
- choose the most specific escalation category and include clear options.`

var requestDecisionSchema = json.RawMessage(`{
  "type":"object",
  "properties":{
    "task_id":{"type":"string"},
    "category":{"type":"string","enum":["execution_only","preference_sensitive","emotion_sensitive","tone_sensitive","relationship_sensitive","ambiguous_goal","strategy_shift","high_risk","irreversible"]},
    "risk_level":{"type":"string","enum":["low","medium","high"]},
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
    "suggests_user_input":{"type":"boolean"},
    "created_at":{"type":"string","format":"date-time"}
  },
  "required":["task_id","category","risk_level","goal_summary","question","why_blocked","options","suggests_user_input"],
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
