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
Return JSON only in the form {"work_progress":{...}}.
Merge the new round information with the existing progress.
Keep steps_completed and key_findings concise - one sentence per item.
Drop intermediate detail that has been superseded by later findings.
Do not emit prose, markdown, or explanations outside the JSON object.
`)

func buildProgressSummaryRequest(model string, current WorkProgress, delta []llm.Message) (llm.ChatRequest, error) {
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

	return llm.ChatRequest{
		Model:       model,
		System:      workProgressSystemPrompt,
		MaxTokens:   progressSummaryMaxTokens,
		Temperature: progressTemperature,
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

	var envelope struct {
		WorkProgress WorkProgress `json:"work_progress"`
	}
	if err := json.Unmarshal([]byte(content), &envelope); err != nil {
		return WorkProgress{}, fmt.Errorf("unmarshal work progress response: %w", err)
	}
	return normalizeWorkProgress(envelope.WorkProgress), nil
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
