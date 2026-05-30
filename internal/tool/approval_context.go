package tool

import "context"

type ApprovalContext struct {
	RequestID           string
	ApprovalKind        string
	AllowToolCall       bool
	AllowDestructive    bool
	ToolName            string
	NormalizedInputHash string
	PathDigest          string
}

type approvalContextKey struct{}

// WithApproval attaches the active approval context for the current execution path.
func WithApproval(ctx context.Context, approval ApprovalContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, approvalContextKey{}, approval)
}

// ApprovalFromContext returns the active approval context when present.
func ApprovalFromContext(ctx context.Context) (ApprovalContext, bool) {
	if ctx == nil {
		return ApprovalContext{}, false
	}
	approval, ok := ctx.Value(approvalContextKey{}).(ApprovalContext)
	if !ok {
		return ApprovalContext{}, false
	}
	return approval, true
}
