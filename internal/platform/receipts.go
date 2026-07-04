package platform

import (
	"context"

	"github.com/longyisang/emoagent/internal/conversation"
)

type ReceiptStatus string

const (
	ReceiptStatusProcessing ReceiptStatus = "processing"
	ReceiptStatusHandled    ReceiptStatus = "handled"
	ReceiptStatusDuplicate  ReceiptStatus = "duplicate"
	ReceiptStatusFailed     ReceiptStatus = "failed"
	ReceiptStatusIgnored    ReceiptStatus = "ignored"
)

type ReceiptResult struct {
	ID        string
	Status    ReceiptStatus
	Duplicate bool
}

type ReceiptStore interface {
	BeginInbound(context.Context, InboundMessage, conversation.Origin) (ReceiptResult, error)
	CompleteInbound(context.Context, string, string, string) error
	FailInbound(context.Context, string, string, error) error
}
