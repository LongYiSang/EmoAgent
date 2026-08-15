package llm

import (
	"maps"
	"strings"
)

// ExtractFirstJSONObject returns the first balanced top-level JSON object found
// in content, tolerating prose or code fences around it. Models asked for JSON
// routinely wrap it in explanation, so callers that parse structured verdicts
// need this rather than a bare json.Unmarshal.
func ExtractFirstJSONObject(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	for start := 0; start < len(trimmed); start++ {
		if trimmed[start] != '{' {
			continue
		}
		depth := 0
		inString := false
		escaped := false
		for i := start; i < len(trimmed); i++ {
			ch := trimmed[i]
			if inString {
				if escaped {
					escaped = false
					continue
				}
				switch ch {
				case '\\':
					escaped = true
				case '"':
					inString = false
				}
				continue
			}
			switch ch {
			case '"':
				inString = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return trimmed[start : i+1], true
				}
			}
		}
	}
	return "", false
}

// ResponseTextCandidates returns the plausible text payloads of a response, in
// preference order: plain content, joined content blocks, then reasoning
// content. Structured-output parsers should try each in turn.
func ResponseTextCandidates(resp *ChatResponse) []string {
	if resp == nil {
		return nil
	}
	var candidates []string
	if content := strings.TrimSpace(resp.Content); content != "" {
		candidates = append(candidates, content)
	}
	var blockText strings.Builder
	for _, block := range resp.ContentBlocks {
		if strings.TrimSpace(block.Text) != "" {
			if blockText.Len() > 0 {
				blockText.WriteByte('\n')
			}
			blockText.WriteString(block.Text)
		}
	}
	if content := strings.TrimSpace(blockText.String()); content != "" {
		candidates = append(candidates, content)
	}
	if content := strings.TrimSpace(resp.ReasoningContent); content != "" {
		candidates = append(candidates, content)
	}
	return candidates
}

// CloneRequestParams deep-copies the pointer and map fields of params so callers
// can override values (temperature, max tokens, stream) without mutating the
// shared configuration they were handed.
func CloneRequestParams(params RequestParams) RequestParams {
	cp := params
	cp.Temperature = cloneFloat64Ptr(params.Temperature)
	cp.TopP = cloneFloat64Ptr(params.TopP)
	cp.PresencePenalty = cloneFloat64Ptr(params.PresencePenalty)
	cp.FrequencyPenalty = cloneFloat64Ptr(params.FrequencyPenalty)
	cp.Stream = cloneBoolPtr(params.Stream)
	if params.Thinking != nil {
		thinking := *params.Thinking
		if params.Thinking.BudgetTokens != nil {
			budget := *params.Thinking.BudgetTokens
			thinking.BudgetTokens = &budget
		}
		cp.Thinking = &thinking
	}
	if params.Extra != nil {
		cp.Extra = make(map[string]any, len(params.Extra))
		maps.Copy(cp.Extra, params.Extra)
	}
	return cp
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
