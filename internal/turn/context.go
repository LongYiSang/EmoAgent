package turn

import "context"

type CorrelationContext struct {
	TurnID     string
	SessionID  string
	PersonaKey string
	RequestID  string
	Kind       InboundKind
	Stage      StageName
}

type correlationContextKey struct{}

func WithCorrelationContext(ctx context.Context, value CorrelationContext) context.Context {
	return context.WithValue(ctx, correlationContextKey{}, value)
}

func CorrelationContextFromContext(ctx context.Context) (CorrelationContext, bool) {
	value, ok := ctx.Value(correlationContextKey{}).(CorrelationContext)
	return value, ok
}

func WithCorrelationStage(ctx context.Context, stage StageName) context.Context {
	value, _ := CorrelationContextFromContext(ctx)
	value.Stage = stage
	return WithCorrelationContext(ctx, value)
}
