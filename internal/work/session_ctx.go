package work

import "context"

type sessionIDKeyType struct{}

var sessionIDCtxKey = sessionIDKeyType{}

// WithSessionID injects the chat session ID into context for tool handlers.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDCtxKey, sessionID)
}

// SessionIDFromContext reads the chat session ID from context.
func SessionIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(sessionIDCtxKey).(string)
	return v
}
