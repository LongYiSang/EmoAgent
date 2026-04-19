package progress

import "context"

// EventKind indicates the stage of delegated Work progress.
type EventKind string

const (
	KindStart     EventKind = "start"
	KindTool      EventKind = "tool"
	KindHeartbeat EventKind = "heartbeat"
	KindFinishing EventKind = "finishing"
	KindPaused    EventKind = "paused"
	KindEnd       EventKind = "end"
)

// Event is the runtime progress signal emitted by Work.
type Event struct {
	Kind     EventKind
	ToolName string
	Round    int
	TaskID   string
}

// Callback consumes progress events.
type Callback func(Event)

type callbackKeyType struct{}

var callbackCtxKey = callbackKeyType{}

// WithCallback stores a progress callback into context.
func WithCallback(ctx context.Context, cb Callback) context.Context {
	if ctx == nil || cb == nil {
		return ctx
	}
	return context.WithValue(ctx, callbackCtxKey, cb)
}

// CallbackFromContext reads the progress callback from context.
func CallbackFromContext(ctx context.Context) Callback {
	if ctx == nil {
		return nil
	}
	cb, _ := ctx.Value(callbackCtxKey).(Callback)
	return cb
}
