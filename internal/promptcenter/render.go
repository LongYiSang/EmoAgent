package promptcenter

import "encoding/json"

func BuildRenderSnapshot(input RenderSnapshot, options ...SnapshotRenderOptions) (RenderSnapshot, error) {
	renderedText := input.RenderedText
	input.FinalHash = HashText(renderedText)
	if len(options) > 0 {
		input = applySnapshotRenderOptions(input, renderedText, options[0])
	}
	if input.ComponentsJSON == "" {
		payload, err := json.Marshal(input.Components)
		if err != nil {
			return RenderSnapshot{}, err
		}
		input.ComponentsJSON = string(payload)
	}
	return input, nil
}

func DynamicComponent(id, sectionName, source, text string, metadata map[string]any) RenderComponent {
	component := RenderComponent{
		ComponentID:   id,
		Source:        source,
		SectionName:   sectionName,
		EffectiveHash: HashText(text),
		Editable:      false,
		Dynamic:       true,
		TextLength:    len([]rune(text)),
	}
	if len(metadata) > 0 {
		if payload, err := json.Marshal(metadata); err == nil {
			component.MetadataJSON = string(payload)
		}
	}
	return component
}

func RenderComponentFromResolved(resolved ResolvedPrompt) RenderComponent {
	return RenderComponent{
		ComponentID:   resolved.ComponentID,
		Name:          resolved.Name,
		Source:        resolved.Source,
		ScopeType:     resolved.ScopeType,
		ScopeID:       resolved.ScopeID,
		DefaultHash:   resolved.DefaultHash,
		EffectiveHash: resolved.EffectiveHash,
		Kind:          resolved.Kind,
		Editable:      resolved.Editable,
		Dynamic:       false,
		TextLength:    resolved.TextLength,
	}
}

func applySnapshotRenderOptions(input RenderSnapshot, renderedText string, options SnapshotRenderOptions) RenderSnapshot {
	if !options.StoreRenderedText {
		input.RenderedText = ""
		input.Truncated = false
		return input
	}
	if options.MaxRenderedTextChars <= 0 {
		input.RenderedText = renderedText
		return input
	}
	runes := []rune(renderedText)
	if len(runes) <= options.MaxRenderedTextChars {
		input.RenderedText = renderedText
		return input
	}
	input.RenderedText = string(runes[:options.MaxRenderedTextChars])
	input.Truncated = true
	return input
}
