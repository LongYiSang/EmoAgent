package replydelivery

import (
	"context"

	contextutil "github.com/longyisang/emoagent/internal/context"
)

type PromptModeRecorder func(contextutil.PromptMode)

type promptModeRecorderKey struct{}

func WithPromptModeRecorder(ctx context.Context, recorder PromptModeRecorder) context.Context {
	if ctx == nil || recorder == nil {
		return ctx
	}
	return context.WithValue(ctx, promptModeRecorderKey{}, recorder)
}

func RecordPromptMode(ctx context.Context, mode contextutil.PromptMode) {
	if ctx == nil {
		return
	}
	recorder, _ := ctx.Value(promptModeRecorderKey{}).(PromptModeRecorder)
	if recorder != nil {
		recorder(mode)
	}
}
