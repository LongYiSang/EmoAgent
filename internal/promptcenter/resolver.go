package promptcenter

import (
	"context"
	"fmt"
)

type Resolver struct {
	catalog *Catalog
	store   Store
	warn    func(ResolveWarning)
}

type ResolveWarning struct {
	ComponentID string `json:"component_id"`
	Code        string `json:"code"`
	Message     string `json:"message"`
}

func NewResolver(catalog *Catalog, store Store) *Resolver {
	return &Resolver{catalog: catalog, store: store}
}

func NewResolverWithWarning(catalog *Catalog, store Store, warn func(ResolveWarning)) *Resolver {
	return &Resolver{catalog: catalog, store: store, warn: warn}
}

func (r *Resolver) ResolveText(ctx context.Context, componentID string, scope PromptScope) string {
	if r == nil || r.catalog == nil {
		return ""
	}
	resolved, err := r.Resolve(ctx, componentID, scope)
	if err != nil {
		component, ok := r.catalog.Get(componentID)
		if ok {
			return component.DefaultText
		}
		return ""
	}
	return resolved.Text
}

func (r *Resolver) Resolve(ctx context.Context, componentID string, scope PromptScope) (ResolvedPrompt, error) {
	if r == nil || r.catalog == nil {
		return ResolvedPrompt{}, fmt.Errorf("prompt catalog is required")
	}
	component, ok := r.catalog.Get(componentID)
	if !ok {
		return ResolvedPrompt{}, fmt.Errorf("prompt component %q not found", componentID)
	}
	if r.store == nil {
		return resolvedFromComponent(component, component.DefaultText, SourceEmbeddedDefault, "", "", nil), nil
	}

	if scope.AgentID != "" {
		record, err := r.store.GetOverride(ctx, componentID, ScopeAgent, scope.AgentID)
		if err != nil {
			r.emitWarning(ResolveWarning{
				ComponentID: componentID,
				Code:        "store_error",
				Message:     fmt.Sprintf("read agent override failed; falling back to embedded default: %v", err),
			})
			return resolvedFromComponent(component, component.DefaultText, SourceEmbeddedDefault, "", "", nil), nil
		}
		if record != nil && record.Enabled {
			switch record.Mode {
			case OverrideModeCustom:
				return resolvedFromComponent(component, record.OverrideText, SourceAgentOverride, ScopeAgent, scope.AgentID, record), nil
			case OverrideModeUseDefault:
				return resolvedFromComponent(component, component.DefaultText, SourceAgentDefault, ScopeAgent, scope.AgentID, record), nil
			}
		}
	}

	record, err := r.store.GetOverride(ctx, componentID, ScopeGlobal, "")
	if err != nil {
		r.emitWarning(ResolveWarning{
			ComponentID: componentID,
			Code:        "store_error",
			Message:     fmt.Sprintf("read global override failed; falling back to embedded default: %v", err),
		})
		return resolvedFromComponent(component, component.DefaultText, SourceEmbeddedDefault, "", "", nil), nil
	}
	if record != nil && record.Enabled && record.Mode == OverrideModeCustom {
		return resolvedFromComponent(component, record.OverrideText, SourceGlobalOverride, ScopeGlobal, "", record), nil
	}
	return resolvedFromComponent(component, component.DefaultText, SourceEmbeddedDefault, "", "", nil), nil
}

func (r *Resolver) emitWarning(warning ResolveWarning) {
	if r != nil && r.warn != nil {
		r.warn(warning)
	}
}

func resolvedFromComponent(component PromptComponent, text, source, scopeType, scopeID string, record *OverrideRecord) ResolvedPrompt {
	resolved := ResolvedPrompt{
		ComponentID:   component.ID,
		Name:          component.Name,
		Text:          text,
		Source:        source,
		ScopeType:     scopeType,
		ScopeID:       scopeID,
		DefaultHash:   component.DefaultHash,
		EffectiveHash: HashText(text),
		Kind:          component.Kind,
		Editable:      component.Editable,
		TextLength:    len([]rune(text)),
	}
	if record != nil {
		resolved.DefaultHashAtEdit = record.DefaultHashAtEdit
		resolved.StaleOverride = record.DefaultHashAtEdit != "" && record.DefaultHashAtEdit != component.DefaultHash
	}
	return resolved
}
