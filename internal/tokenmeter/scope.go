package tokenmeter

import "context"

type UsageScope struct {
	Component  string
	Operation  string
	SessionID  string
	TurnID     string
	RequestID  string
	AgentID    string
	PersonaKey string
	PluginID   string
	TaskID     string

	ProviderID   string
	ProviderName string
	Protocol     string
	Model        string
}

type usageScopeKey struct{}

func WithUsageScope(ctx context.Context, scope UsageScope) context.Context {
	return context.WithValue(ctx, usageScopeKey{}, scope)
}

func UsageScopeFromContext(ctx context.Context) (UsageScope, bool) {
	if ctx == nil {
		return UsageScope{}, false
	}
	scope, ok := ctx.Value(usageScopeKey{}).(UsageScope)
	return scope, ok
}

func MergeUsageScope(ctx context.Context, patch UsageScope) context.Context {
	base, _ := UsageScopeFromContext(ctx)
	base = mergeScope(base, patch)
	return WithUsageScope(ctx, base)
}

func mergeScope(base, patch UsageScope) UsageScope {
	if patch.Component != "" {
		base.Component = patch.Component
	}
	if patch.Operation != "" {
		base.Operation = patch.Operation
	}
	if patch.SessionID != "" {
		base.SessionID = patch.SessionID
	}
	if patch.TurnID != "" {
		base.TurnID = patch.TurnID
	}
	if patch.RequestID != "" {
		base.RequestID = patch.RequestID
	}
	if patch.AgentID != "" {
		base.AgentID = patch.AgentID
	}
	if patch.PersonaKey != "" {
		base.PersonaKey = patch.PersonaKey
	}
	if patch.PluginID != "" {
		base.PluginID = patch.PluginID
	}
	if patch.TaskID != "" {
		base.TaskID = patch.TaskID
	}
	if patch.ProviderID != "" {
		base.ProviderID = patch.ProviderID
	}
	if patch.ProviderName != "" {
		base.ProviderName = patch.ProviderName
	}
	if patch.Protocol != "" {
		base.Protocol = patch.Protocol
	}
	if patch.Model != "" {
		base.Model = patch.Model
	}
	return base
}
