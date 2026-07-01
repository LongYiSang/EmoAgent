package agentaffect

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode"
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
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(object), &raw); err != nil {
		return LLMEvaluationResult{}, fmt.Errorf("parse affect response json: %w", err)
	}
	for _, forbidden := range []string{"hidden_reasoning", "reasoning", "response_advice", "memory_permission", "memory_permissions"} {
		if _, ok := raw[forbidden]; ok {
			return LLMEvaluationResult{}, fmt.Errorf("affect response contains forbidden field %q", forbidden)
		}
	}
	var parsed struct {
		SchemaVersion string               `json:"schema_version"`
		Appraisal     *AffectAppraisal     `json:"appraisal"`
		Delta         *MoodVector          `json:"delta"`
		Label         string               `json:"label"`
		Cause         *AffectCauseProposal `json:"cause"`
		Confidence    *float64             `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(object), &parsed); err != nil {
		return LLMEvaluationResult{}, fmt.Errorf("parse affect response json: %w", err)
	}
	if parsed.SchemaVersion != agentAffectSchemaVersion {
		return LLMEvaluationResult{}, fmt.Errorf("affect response schema_version = %q, want %q", parsed.SchemaVersion, agentAffectSchemaVersion)
	}
	if parsed.Appraisal == nil {
		return LLMEvaluationResult{}, fmt.Errorf("affect response missing appraisal")
	}
	if err := validateAppraisal(*parsed.Appraisal); err != nil {
		return LLMEvaluationResult{}, err
	}
	if parsed.Delta == nil {
		return LLMEvaluationResult{}, fmt.Errorf("affect response missing delta")
	}
	delta := *parsed.Delta
	if err := validateMoodVectorFinite(delta); err != nil {
		return LLMEvaluationResult{}, err
	}
	if parsed.Cause == nil {
		return LLMEvaluationResult{}, fmt.Errorf("affect response missing cause")
	}
	cause := normalizeCauseProposal(*parsed.Cause)
	confidence := 0.5
	if parsed.Confidence != nil {
		confidence = *parsed.Confidence
	}
	if !finiteInRange(confidence, 0, 1) {
		return LLMEvaluationResult{}, fmt.Errorf("affect response confidence out of range")
	}
	label := strings.TrimSpace(parsed.Label)
	if label == "" {
		label = deriveLabel(delta)
	}
	moodDescription, moodReason, promptMoodText := naturalMoodFieldsFromCause(label, cause)
	return LLMEvaluationResult{
		Delta:               delta,
		Appraisal:           *parsed.Appraisal,
		HasAppraisal:        true,
		Cause:               cause,
		Label:               label,
		MoodDescription:     moodDescription,
		MoodReason:          moodReason,
		PromptMoodText:      promptMoodText,
		CauseSummary:        cause.Summary,
		VisibleCauseSummary: cause.VisibleSummary,
		AffectTags:          cause.Tags,
		Confidence:          confidence,
		RawResponseJSON:     object,
		Status:              EvaluationStatusPreview,
	}, nil
}

func validateAppraisal(appraisal AffectAppraisal) error {
	checks := map[string]struct {
		value float64
		min   float64
		max   float64
	}{
		"event_significance":  {value: appraisal.EventSignificance, min: 0, max: 1},
		"novelty":             {value: appraisal.Novelty, min: 0, max: 1},
		"goal_relevance":      {value: appraisal.GoalRelevance, min: 0, max: 1},
		"relationship_impact": {value: appraisal.RelationshipImpact, min: -1, max: 1},
		"boundary_impact":     {value: appraisal.BoundaryImpact, min: 0, max: 1},
		"uncertainty":         {value: appraisal.Uncertainty, min: 0, max: 1},
	}
	for name, check := range checks {
		if !finiteInRange(check.value, check.min, check.max) {
			return fmt.Errorf("affect response appraisal.%s out of range", name)
		}
	}
	return nil
}

func validateMoodVectorFinite(v MoodVector) error {
	values := map[string]float64{
		"valence":     v.Valence,
		"arousal":     v.Arousal,
		"dominance":   v.Dominance,
		"energy":      v.Energy,
		"warmth":      v.Warmth,
		"concern":     v.Concern,
		"curiosity":   v.Curiosity,
		"playfulness": v.Playfulness,
		"attachment":  v.Attachment,
		"frustration": v.Frustration,
		"uncertainty": v.Uncertainty,
	}
	for name, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("affect response delta.%s is not finite", name)
		}
	}
	return nil
}

func finiteInRange(value, min, max float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= min && value <= max
}

func normalizeCauseProposal(cause AffectCauseProposal) AffectCauseProposal {
	cause.Code = truncateRunes(slugifyCauseCode(cause.Code), 64)
	cause.Summary = truncateRunes(compactWhitespace(cause.Summary), 120)
	cause.VisibleSummary = truncateRunes(compactWhitespace(cause.VisibleSummary), 100)
	if cause.Code == "" {
		cause.Code = "affect_event"
	}
	tags := make([]string, 0, 4)
	for _, tag := range cause.Tags {
		tag = truncateRunes(slugifyCauseCode(tag), 32)
		if tag == "" {
			continue
		}
		tags = append(tags, tag)
		if len(tags) >= 4 {
			break
		}
	}
	cause.Tags = tags
	return cause
}

func slugifyCauseCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		allowed := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
		if allowed {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if r == '_' || unicode.IsSpace(r) {
			if !lastUnderscore && b.Len() > 0 {
				b.WriteRune('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_-")
}

func NoChangeResult(reason string, status string) LLMEvaluationResult {
	if status == "" {
		status = EvaluationStatusPreview
	}
	promptText := "平稳、接近基线。"
	return LLMEvaluationResult{
		Delta:               MoodVector{},
		Label:               "steady",
		MoodDescription:     promptText,
		PromptMoodText:      promptText,
		CauseSummary:        reason,
		VisibleCauseSummary: reason,
		Confidence:          0.5,
		Fallback:            true,
		Status:              status,
	}
}

func naturalPromptMoodTextFromCause(cause AffectCauseProposal) string {
	if text := strings.TrimSpace(cause.VisibleSummary); text != "" {
		return text
	}
	if text := strings.TrimSpace(cause.Summary); text != "" {
		return text
	}
	return ""
}

func stripPromptMoodLabelPrefix(label, text string) string {
	label = strings.TrimSpace(label)
	text = strings.TrimSpace(text)
	if label == "" || text == "" {
		return text
	}
	if text == label {
		return ""
	}
	for _, sep := range []string{"；", ";", "：", ":", " - "} {
		prefix := label + sep
		if strings.HasPrefix(text, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(text, prefix))
		}
	}
	return text
}

func naturalMoodFieldsFromCause(label string, cause AffectCauseProposal) (description string, reason string, promptText string) {
	promptText = stripPromptMoodLabelPrefix(label, naturalPromptMoodTextFromCause(cause))
	description = promptText

	summary := strings.TrimSpace(cause.Summary)
	visible := strings.TrimSpace(cause.VisibleSummary)
	cleanSummary := stripPromptMoodLabelPrefix(label, summary)
	if cleanSummary != "" && cleanSummary != visible && cleanSummary != promptText {
		reason = cleanSummary
	}
	return description, reason, promptText
}

func buildPromptMoodTextFallback(description string, reason string) string {
	description = strings.TrimSpace(description)
	reason = strings.TrimSpace(reason)
	switch {
	case description != "" && reason != "":
		return description + "；" + reason
	case description != "":
		return description
	default:
		return reason
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
