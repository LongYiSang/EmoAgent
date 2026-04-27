package work

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/longyisang/emoagent/internal/tool"
)

// FinishTaskPayload is the internal completion signal emitted by Work.
type FinishTaskPayload struct {
	Status        string   `json:"status"`
	Summary       string   `json:"summary"`
	Findings      []string `json:"findings,omitempty"`
	OpenQuestions []string `json:"open_questions,omitempty"`
}

const finishTaskDescription = `Submit the final task result to the runtime.

Rules:
- finish_task MUST be the sole tool call in the round.
- Provide only status, summary, findings, and open_questions.
- findings and open_questions must be arrays of strings, never arrays of objects.
- Use completed only when the acceptance criteria are satisfied.
- Use partial when useful work was completed but criteria remain unmet; use failed when no useful result can be produced.
- Include verification performed or the verification gap in the summary.
- Never paste raw tool output; summarize relevant facts.
- Do not include task_id, goal, created_at, or any raw tool dumps.`

var finishTaskSchema = json.RawMessage(`{
  "type":"object",
  "properties":{
    "status":{"type":"string","enum":["completed","partial","failed"]},
    "summary":{"type":"string"},
    "findings":{"type":"array","items":{"type":"string"}},
    "open_questions":{"type":"array","items":{"type":"string"}}
  },
  "required":["status","summary"],
  "additionalProperties":false
}`)

// NewFinishTaskTool returns the Work-side completion tool spec.
func NewFinishTaskTool() tool.Spec {
	return tool.Spec{
		Name:        "finish_task",
		Description: finishTaskDescription,
		Parameters:  finishTaskSchema,
		Scope:       tool.ScopeWork,
		Permission:  tool.PermReadOnly,
	}
}

// FinishTaskPlaceholderHandler should never execute when Runtime interception works.
func FinishTaskPlaceholderHandler(context.Context, json.RawMessage) (json.RawMessage, error) {
	return nil, fmt.Errorf("finish_task must be intercepted by runtime")
}

func ParseFinishTaskPayload(input json.RawMessage) (FinishTaskPayload, error) {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()

	var payload FinishTaskPayload
	if err := decoder.Decode(&payload); err != nil {
		return FinishTaskPayload{}, err
	}
	if err := validateFinishTaskPayload(payload); err != nil {
		return FinishTaskPayload{}, err
	}
	return payload, nil
}

func validateFinishTaskPayload(payload FinishTaskPayload) error {
	switch payload.Status {
	case "completed", "partial", "failed":
	default:
		return fmt.Errorf("status must be one of completed, partial, failed")
	}
	if strings.TrimSpace(payload.Summary) == "" {
		return fmt.Errorf("summary is required")
	}
	return nil
}
