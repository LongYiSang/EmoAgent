package llm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// ErrorKind is the normalized provider error category.
type ErrorKind string

const (
	ErrorKindContextOverflow  ErrorKind = "context_overflow"
	ErrorKindTransport        ErrorKind = "transport"
	ErrorKindProviderResponse ErrorKind = "provider_response"
)

// Error is the normalized provider/runtime error used by chat.Engine retry logic.
type Error struct {
	Kind       ErrorKind
	Provider   string
	Operation  string
	StatusCode int
	Message    string
	Cause      error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	base := string(e.Kind)
	if e.Provider != "" || e.Operation != "" {
		base = fmt.Sprintf("%s %s %s", e.Provider, e.Operation, e.Kind)
	}
	if e.StatusCode > 0 {
		base = fmt.Sprintf("%s status=%d", base, e.StatusCode)
	}
	if e.Message != "" {
		return fmt.Sprintf("%s: %s", base, e.Message)
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", base, e.Cause)
	}
	return base
}

// Unwrap exposes the underlying provider/network error.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// IsKind reports whether err or any wrapped error is a normalized llm error of the given kind.
func IsKind(err error, kind ErrorKind) bool {
	var llmErr *Error
	if !errors.As(err, &llmErr) {
		return false
	}
	return llmErr.Kind == kind
}

func wrapTransportError(provider, operation, message string, cause error) error {
	return &Error{
		Kind:      ErrorKindTransport,
		Provider:  provider,
		Operation: operation,
		Message:   message,
		Cause:     cause,
	}
}

func wrapDecodeError(provider, operation string, cause error) error {
	return &Error{
		Kind:      ErrorKindProviderResponse,
		Provider:  provider,
		Operation: operation,
		Message:   "decode response",
		Cause:     cause,
	}
}

func wrapStreamDecodeError(provider, operation string, cause error) error {
	if isTransportError(cause) {
		return wrapTransportError(provider, operation, "stream read", cause)
	}
	return &Error{
		Kind:      ErrorKindProviderResponse,
		Provider:  provider,
		Operation: operation,
		Message:   "sse decode",
		Cause:     cause,
	}
}

func wrapStatusError(provider, operation string, statusCode int, body string) error {
	kind := ErrorKindProviderResponse
	if isContextOverflowStatus(statusCode, body) {
		kind = ErrorKindContextOverflow
	}
	return &Error{
		Kind:       kind,
		Provider:   provider,
		Operation:  operation,
		StatusCode: statusCode,
		Message:    SanitizeImageDataForDiagnostics(body),
	}
}

func wrapRequestError(provider, operation string, err error) error {
	if err == nil {
		return nil
	}
	if isTransportError(err) {
		return wrapTransportError(provider, operation, "http request", err)
	}
	return &Error{
		Kind:      ErrorKindProviderResponse,
		Provider:  provider,
		Operation: operation,
		Message:   "request failed",
		Cause:     err,
	}
}

func isTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func isContextOverflowStatus(statusCode int, body string) bool {
	if statusCode == 413 {
		return true
	}
	if statusCode != 400 && statusCode != 422 {
		return false
	}
	lower := strings.ToLower(body)
	for _, token := range []string{
		"context length",
		"maximum context",
		"prompt too long",
		"too many tokens",
		"input is too long",
		"exceeds max input tokens",
	} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}
