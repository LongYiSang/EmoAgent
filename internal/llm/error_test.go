package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestContextOverflowErrorClassification(t *testing.T) {
	err := wrapStatusError("openai", "chat", 400, "prompt too long: exceeds max input tokens")
	if !IsKind(err, ErrorKindContextOverflow) {
		t.Fatalf("IsKind(context_overflow) = false, err=%v", err)
	}

	var llmErr *Error
	if !errors.As(err, &llmErr) {
		t.Fatalf("errors.As(*Error) = false, err=%T", err)
	}
	if llmErr.StatusCode != 400 {
		t.Fatalf("StatusCode = %d, want 400", llmErr.StatusCode)
	}
}

func TestProviderResponseErrorClassification(t *testing.T) {
	err := wrapStatusError("anthropic", "messages", 500, "internal server error")
	if !IsKind(err, ErrorKindProviderResponse) {
		t.Fatalf("IsKind(provider_response) = false, err=%v", err)
	}
	if IsKind(err, ErrorKindContextOverflow) {
		t.Fatalf("IsKind(context_overflow) = true, err=%v", err)
	}
}

func TestProviderStatusErrorRedactsImageData(t *testing.T) {
	err := wrapStatusError("openai", "chat", 400, "bad request data:image/png;base64,iVBORw0KGgo=")
	if strings.Contains(err.Error(), "data:image") || strings.Contains(err.Error(), "base64") || strings.Contains(err.Error(), "iVBOR") {
		t.Fatalf("status error leaked image data: %v", err)
	}
}

func TestStreamTransportErrorClassification(t *testing.T) {
	err := wrapStreamDecodeError("openai", "chat_stream", context.DeadlineExceeded)
	if !IsKind(err, ErrorKindTransport) {
		t.Fatalf("IsKind(transport) = false, err=%v", err)
	}
}
