package agentaffect

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Evaluator interface {
	Evaluate(ctx context.Context, req LLMEvaluationRequest) (LLMEvaluationResult, error)
}

type DisabledEvaluator struct {
	Reason string
}

func (e DisabledEvaluator) Evaluate(context.Context, LLMEvaluationRequest) (LLMEvaluationResult, error) {
	reason := strings.TrimSpace(e.Reason)
	if reason == "" {
		reason = "Agent Affect evaluator disabled."
	}
	return NoChangeResult(reason, EvaluationStatusPreview), nil
}

func ParseLLMResponse(content string) (LLMEvaluationResult, error) {
	object, err := extractJSONObject(content)
	if err != nil {
		return LLMEvaluationResult{}, err
	}
	var parsed struct {
		Delta               *MoodVector `json:"delta"`
		ProposedDelta       *MoodVector `json:"proposed_delta"`
		Label               string      `json:"label"`
		CauseSummary        string      `json:"cause_summary"`
		VisibleCauseSummary string      `json:"visible_cause_summary"`
		Confidence          *float64    `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(object), &parsed); err != nil {
		return LLMEvaluationResult{}, fmt.Errorf("parse affect response json: %w", err)
	}
	if parsed.Delta == nil && parsed.ProposedDelta == nil {
		return LLMEvaluationResult{}, fmt.Errorf("affect response missing delta")
	}
	delta := MoodVector{}
	if parsed.Delta != nil {
		delta = *parsed.Delta
	} else if parsed.ProposedDelta != nil {
		delta = *parsed.ProposedDelta
	}
	confidence := 0.5
	if parsed.Confidence != nil {
		confidence = *parsed.Confidence
	}
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}
	label := strings.TrimSpace(parsed.Label)
	if label == "" {
		label = deriveLabel(delta)
	}
	return LLMEvaluationResult{
		Delta:               delta,
		Label:               label,
		CauseSummary:        strings.TrimSpace(parsed.CauseSummary),
		VisibleCauseSummary: strings.TrimSpace(parsed.VisibleCauseSummary),
		Confidence:          confidence,
		RawResponseJSON:     object,
		Status:              EvaluationStatusPreview,
	}, nil
}

func NoChangeResult(reason string, status string) LLMEvaluationResult {
	if status == "" {
		status = EvaluationStatusPreview
	}
	return LLMEvaluationResult{
		Delta:               MoodVector{},
		Label:               "steady",
		CauseSummary:        reason,
		VisibleCauseSummary: reason,
		Confidence:          0.5,
		Fallback:            true,
		Status:              status,
	}
}

func extractJSONObject(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("empty affect response")
	}
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end < start {
		return "", fmt.Errorf("affect response missing json object")
	}
	return content[start : end+1], nil
}

func deriveLabel(delta MoodVector) string {
	if delta.Valence > 0.05 || delta.Warmth > 0.05 || delta.Attachment > 0.03 {
		return "warmer"
	}
	if delta.Valence < -0.05 || delta.Frustration > 0.03 {
		return "strained"
	}
	if delta.Curiosity > 0.05 {
		return "curious"
	}
	return "steady"
}
