package work

import (
	"encoding/json"
	"fmt"
	"strings"

	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/llm"
)

const (
	progressSummaryMaxTokens = 1024
	progressTemperature      = 0.1
)

// WorkProgress is the structured rolling summary of Work execution state.
// It replaces compressed older rounds in the message history.
type WorkProgress struct {
	TaskGoal          string   `json:"task_goal"`
	StepsCompleted    []string `json:"steps_completed"`
	KeyFindings       []string `json:"key_findings"`
	ErrorsEncountered []string `json:"errors_encountered"`
	CurrentApproach   string   `json:"current_approach"`
	DecisionsReceived []string `json:"decisions_received"`
}

func (p WorkProgress) IsZero() bool {
	return p.TaskGoal == "" &&
		len(p.StepsCompleted) == 0 &&
		len(p.KeyFindings) == 0 &&
		len(p.ErrorsEncountered) == 0 &&
		p.CurrentApproach == "" &&
		len(p.DecisionsReceived) == 0
}

var workProgressSystemPrompt = strings.TrimSpace(`
You maintain a structured rolling progress summary for a task execution agent.
Return exactly one JSON object with this shape:
{
  "work_progress": {
    "task_goal": "",
    "steps_completed": [],
    "key_findings": [],
    "errors_encountered": [],
    "current_approach": "",
    "decisions_received": []
  }
}

Update rules:
- Merge the existing work_progress with the new round messages; never summarize only the new round.
- Preserve task_goal unless the new round explicitly corrects it.
- steps_completed must include completed actions only, not plans, intentions, or attempted steps that failed.
- key_findings must include durable facts relevant to the delegated task, summarized in one sentence each.
- errors_encountered must include still-relevant tool errors, failed commands, permission blockers, or verification failures.
- current_approach should state the next immediate approach, blocker, or "ready_to_finish" when the task appears complete.
- decisions_received should preserve user, Emotion, runtime, and permission decisions that affect the task path.
- Drop superseded intermediate details, duplicate findings, and raw tool output.
- Do not include stack traces, long file excerpts, protocol JSON, or internal approval IDs unless an ID is required to identify the active pause.
- Use empty strings and empty arrays when unknown.
- JSON only. No markdown, prose, code fences, or explanations.
`)

var workProgressRepairSystemPrompt = strings.TrimSpace(`
Repair the work_progress response to the exact required JSON schema.
Do not add facts that are not present in the provided current progress or round messages.
Remove protocol leaks, raw tool output, stack traces, internal approval IDs, and any prose outside JSON.
Return JSON only. No markdown, code fences, or explanations.
`)

func buildProgressSummaryRequest(model string, current WorkProgress, delta []llm.Message) (llm.ChatRequest, error) {
	return buildProgressSummaryRequestWithParams(model, llm.RequestParams{}, current, delta)
}

func buildProgressSummaryRequestWithParams(model string, params llm.RequestParams, current WorkProgress, delta []llm.Message) (llm.ChatRequest, error) {
	currentPayload, err := json.Marshal(struct {
		WorkProgress WorkProgress `json:"work_progress"`
	}{
		WorkProgress: current,
	})
	if err != nil {
		return llm.ChatRequest{}, fmt.Errorf("marshal current work progress: %w", err)
	}

	type deltaMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	items := make([]deltaMessage, 0, len(delta))
	for _, msg := range delta {
		content := msg.Content
		if content == "" && len(msg.ContentBlocks) > 0 {
			var parts []string
			for _, block := range msg.ContentBlocks {
				switch block.Type {
				case "text":
					parts = append(parts, block.Text)
				case "tool_use":
					parts = append(parts, fmt.Sprintf("[tool_call: %s]", block.Name))
				case "tool_result":
					preview := block.Content
					runes := []rune(preview)
					if len(runes) > 300 {
						preview = string(runes[:300]) + "..."
					}
					parts = append(parts, preview)
				}
			}
			content = strings.Join(parts, "\n")
		}
		items = append(items, deltaMessage{
			Role:    string(msg.Role),
			Content: content,
		})
	}

	deltaPayload, err := json.Marshal(struct {
		RoundMessages []deltaMessage `json:"round_messages"`
	}{
		RoundMessages: items,
	})
	if err != nil {
		return llm.ChatRequest{}, fmt.Errorf("marshal progress delta: %w", err)
	}

	if params.MaxTokens <= 0 {
		params.MaxTokens = progressSummaryMaxTokens
	}
	if params.Temperature == nil {
		temp := progressTemperature
		params.Temperature = &temp
	}
	stream := false
	params.Stream = &stream

	return llm.ChatRequest{
		Model:       model,
		System:      workProgressSystemPrompt,
		Params:      params,
		MaxTokens:   params.MaxTokens,
		Temperature: *params.Temperature,
		Stream:      false,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: string(currentPayload)},
			{Role: llm.RoleUser, Content: string(deltaPayload)},
		},
	}, nil
}

func parseProgressSummaryResponse(resp *llm.ChatResponse) (WorkProgress, error) {
	if resp == nil {
		return WorkProgress{}, fmt.Errorf("progress summary response is nil")
	}

	content := strings.TrimSpace(resp.Content)
	if content == "" {
		for _, block := range resp.ContentBlocks {
			if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
				content = strings.TrimSpace(block.Text)
				break
			}
		}
	}
	if content == "" {
		return WorkProgress{}, fmt.Errorf("progress summary response content is empty")
	}

	progress, err := decodeAndValidateWorkProgress(stripCodeFence(content))
	if err != nil {
		return WorkProgress{}, fmt.Errorf("validate work progress response: %w", err)
	}
	return progress, nil
}

func buildProgressRepairRequest(req llm.ChatRequest, resp *llm.ChatResponse, parseErr error) (llm.ChatRequest, error) {
	payload, err := json.Marshal(struct {
		Error           string `json:"error"`
		InvalidResponse string `json:"invalid_response"`
	}{
		Error:           parseErr.Error(),
		InvalidResponse: truncateProgressRepairContent(progressResponseText(resp)),
	})
	if err != nil {
		return llm.ChatRequest{}, fmt.Errorf("marshal progress repair payload: %w", err)
	}
	repairReq := req
	repairReq.System = workProgressRepairSystemPrompt
	repairReq.Messages = append(append([]llm.Message(nil), req.Messages...), llm.Message{
		Role:    llm.RoleUser,
		Content: string(payload),
	})
	repairReq.Temperature = 0
	repairReq.Stream = false
	repairReq.Params = req.Params
	zero := 0.0
	repairReq.Params.Temperature = &zero
	stream := false
	repairReq.Params.Stream = &stream
	return repairReq, nil
}

func progressResponseText(resp *llm.ChatResponse) string {
	if resp == nil {
		return ""
	}
	if content := strings.TrimSpace(resp.Content); content != "" {
		return content
	}
	var b strings.Builder
	for _, block := range resp.ContentBlocks {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			b.WriteString(block.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

func truncateProgressRepairContent(content string) string {
	const max = 8000
	if len(content) <= max {
		return content
	}
	return content[:max]
}

func decodeAndValidateWorkProgress(content string) (WorkProgress, error) {
	var rawEnvelope struct {
		WorkProgress json.RawMessage `json:"work_progress"`
	}
	if err := decodeStrictJSON(json.RawMessage(content), &rawEnvelope); err != nil {
		return WorkProgress{}, fmt.Errorf("decode envelope: %w", err)
	}
	if len(rawEnvelope.WorkProgress) == 0 {
		return WorkProgress{}, fmt.Errorf("work_progress is required")
	}
	var progressFields map[string]json.RawMessage
	if err := json.Unmarshal(rawEnvelope.WorkProgress, &progressFields); err != nil {
		return WorkProgress{}, fmt.Errorf("work_progress must be an object: %w", err)
	}
	if err := validateWorkProgressJSONShape(progressFields); err != nil {
		return WorkProgress{}, err
	}
	var progress WorkProgress
	if err := decodeStrictJSON(rawEnvelope.WorkProgress, &progress); err != nil {
		return WorkProgress{}, fmt.Errorf("decode work_progress: %w", err)
	}
	progress = normalizeWorkProgress(progress)
	if err := validateWorkProgressContent(progress); err != nil {
		return WorkProgress{}, err
	}
	return progress, nil
}

func validateWorkProgressJSONShape(fields map[string]json.RawMessage) error {
	for _, spec := range []struct {
		name string
		kind string
		want byte
	}{
		{name: "task_goal", kind: "string", want: '"'},
		{name: "steps_completed", kind: "array", want: '['},
		{name: "key_findings", kind: "array", want: '['},
		{name: "errors_encountered", kind: "array", want: '['},
		{name: "current_approach", kind: "string", want: '"'},
		{name: "decisions_received", kind: "array", want: '['},
	} {
		if err := requireWorkJSONFieldKind(fields, spec.name, spec.kind, spec.want); err != nil {
			return err
		}
	}
	return nil
}

func requireWorkJSONFieldKind(fields map[string]json.RawMessage, name string, kind string, want byte) error {
	raw, ok := fields[name]
	if !ok {
		return fmt.Errorf("%s is required", name)
	}
	actual, ok := firstWorkJSONByte(raw)
	if !ok || actual != want {
		return fmt.Errorf("%s must be a JSON %s", name, kind)
	}
	return nil
}

func firstWorkJSONByte(raw json.RawMessage) (byte, bool) {
	for _, b := range raw {
		switch b {
		case ' ', '\n', '\r', '\t':
			continue
		default:
			return b, true
		}
	}
	return 0, false
}

func validateWorkProgressContent(progress WorkProgress) error {
	values := []string{progress.TaskGoal, progress.CurrentApproach}
	values = append(values, progress.StepsCompleted...)
	values = append(values, progress.KeyFindings...)
	values = append(values, progress.ErrorsEncountered...)
	values = append(values, progress.DecisionsReceived...)
	for _, value := range values {
		if len([]rune(value)) > 1000 {
			return fmt.Errorf("work_progress text item exceeds 1000 runes")
		}
		if containsWorkProtocolLeak(value) {
			return fmt.Errorf("work_progress contains internal protocol leakage")
		}
	}
	return nil
}

func containsWorkProtocolLeak(value string) bool {
	lower := strings.ToLower(value)
	for _, term := range []string{
		"decision_packet",
		"approval_request_id",
		"taskreport",
		"task_report",
		"tool_approval",
		"permission_escalation_required",
	} {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func normalizeWorkProgress(p WorkProgress) WorkProgress {
	if p.StepsCompleted == nil {
		p.StepsCompleted = []string{}
	}
	if p.KeyFindings == nil {
		p.KeyFindings = []string{}
	}
	if p.ErrorsEncountered == nil {
		p.ErrorsEncountered = []string{}
	}
	if p.DecisionsReceived == nil {
		p.DecisionsReceived = []string{}
	}
	return p
}

func buildWorkProgressMessage(progress WorkProgress) (llm.Message, error) {
	payload, err := json.Marshal(struct {
		WorkProgress WorkProgress `json:"work_progress"`
	}{
		WorkProgress: normalizeWorkProgress(progress),
	})
	if err != nil {
		return llm.Message{}, fmt.Errorf("marshal work progress message: %w", err)
	}

	return llm.Message{
		Role:    llm.RoleUser,
		Content: string(payload),
	}, nil
}

func estimateProgressTokens(p WorkProgress) int {
	payload, err := json.Marshal(p)
	if err != nil {
		return 0
	}
	return contextutil.EstimateTokens(string(payload))
}
