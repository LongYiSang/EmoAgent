package tool

import "context"

type ReadScope string

const (
	ReadScopeWorkspace ReadScope = "workspace"
	ReadScopeAll       ReadScope = "all"
)

type readScopeKey struct{}

func WithReadScope(ctx context.Context, scope ReadScope) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if scope == "" {
		scope = ReadScopeWorkspace
	}
	return context.WithValue(ctx, readScopeKey{}, scope)
}

func ReadScopeFromContext(ctx context.Context) ReadScope {
	if ctx == nil {
		return ReadScopeWorkspace
	}
	scope, ok := ctx.Value(readScopeKey{}).(ReadScope)
	if !ok || scope == "" {
		return ReadScopeWorkspace
	}
	return scope
}
