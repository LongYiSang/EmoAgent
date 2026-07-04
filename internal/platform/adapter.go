package platform

import "context"

type Adapter interface {
	Start(context.Context, InboundHandler) error
	Stop(context.Context) error
}

type InboundHandler interface {
	HandleInbound(context.Context, InboundMessage, OutboundSink) (HandleResult, error)
}
