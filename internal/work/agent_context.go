package work

import "context"

type agentIDContextKey struct{}

func WithAgentID(ctx context.Context, agentID string) context.Context {
	if agentID == "" {
		return ctx
	}
	return context.WithValue(ctx, agentIDContextKey{}, agentID)
}

func AgentIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	agentID, _ := ctx.Value(agentIDContextKey{}).(string)
	return agentID
}
