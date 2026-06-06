package app

import (
	"strings"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/llm"
)

func cloneActiveAgentRuntime(runtime *ActiveAgentRuntime) *ActiveAgentRuntime {
	if runtime == nil {
		return nil
	}
	cp := *runtime
	cp.EmotionMain = cloneModelRuntime(runtime.EmotionMain)
	cp.EmotionSummary = cloneModelRuntime(runtime.EmotionSummary)
	cp.WorkMain = cloneModelRuntime(runtime.WorkMain)
	cp.WorkSummary = cloneModelRuntime(runtime.WorkSummary)
	return &cp
}

func cloneModelRuntime(runtime ModelRuntime) ModelRuntime {
	runtime.Params = cloneRequestParams(runtime.Params)
	return runtime
}

func cloneRequestParams(params llm.RequestParams) llm.RequestParams {
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
		for key, value := range params.Extra {
			cp.Extra[key] = value
		}
	}
	return cp
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func derefFloat64(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}

func toolProviderName(protocol string) string {
	if protocol == "anthropic" {
		return "anthropic"
	}
	return "openai"
}

func providerDisplayName(provider config.LLMProvider) string {
	if strings.TrimSpace(provider.Name) != "" {
		return provider.Name
	}
	return provider.ID
}

func cloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func clonePersona(p *config.Persona) *config.Persona {
	if p == nil {
		return nil
	}
	cp := *p
	if p.Quirks != nil {
		cp.Quirks = append([]string(nil), p.Quirks...)
	}
	if p.WorkProgressPhrases != nil {
		cp.WorkProgressPhrases = cloneProgressPhrases(p.WorkProgressPhrases)
	}
	return &cp
}

func clonePersonaMap(src map[string]*config.Persona) map[string]*config.Persona {
	dst := make(map[string]*config.Persona, len(src))
	for key, persona := range src {
		dst[key] = clonePersona(persona)
	}
	return dst
}

func cloneProgressPhrases(src map[string][]string) map[string][]string {
	if src == nil {
		return nil
	}
	dst := make(map[string][]string, len(src))
	for key, phrases := range src {
		dst[key] = append([]string(nil), phrases...)
	}
	return dst
}
