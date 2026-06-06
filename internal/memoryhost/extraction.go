package memoryhost

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

type ExtractionRunner struct {
	host *Host
	cfg  ExtractionConfig
}

func NewExtractionRunner(ctx context.Context, host *Host, cfg ExtractionConfig, logger *slog.Logger) (*ExtractionRunner, error) {
	cfg = cfg.normalized()
	if !cfg.Enabled {
		return nil, nil
	}
	if !host.configured() {
		return nil, fmt.Errorf("memory host is not configured")
	}
	return &ExtractionRunner{host: host, cfg: cfg}, nil
}

func (r *ExtractionRunner) Close() error {
	return nil
}

func (r *ExtractionRunner) ExtractSessionEnd(ctx context.Context, personaID string, memorySessionID string) (*memorycore.ExtractionRunResult, error) {
	if r == nil || r.host == nil {
		return nil, nil
	}
	return r.host.ExtractSessionEnd(ctx, personaID, memorySessionID)
}

type sanitizedExtractionFailure struct {
	code    string
	message string
}

func (e *sanitizedExtractionFailure) Error() string {
	if e == nil {
		return ""
	}
	if e.message == "" {
		return "memory extraction failed code=" + e.code
	}
	return "memory extraction failed code=" + e.code + " message=" + e.message
}

func (e *sanitizedExtractionFailure) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.code
}

func successfulExtractionStatus(status memorycore.ExtractionRunStatus) bool {
	switch status {
	case memorycore.ExtractionRunStatusApplied,
		memorycore.ExtractionRunStatusNothingApplied,
		memorycore.ExtractionRunStatusSkipped,
		memorycore.ExtractionRunStatusDryRun,
		memorycore.ExtractionRunStatusValidated:
		return true
	default:
		return false
	}
}

func sanitizedExtractionError(code string, message string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		code = "unknown"
	}
	return &sanitizedExtractionFailure{
		code:    code,
		message: strings.TrimSpace(message),
	}
}

func extractionErrorCode(result *memorycore.ExtractionRunResult, err error) string {
	if result != nil && strings.TrimSpace(result.SanitizedErrorCode) != "" {
		return result.SanitizedErrorCode
	}
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) && strings.TrimSpace(coded.ErrorCode()) != "" {
		return coded.ErrorCode()
	}
	if err != nil {
		return "runner_failed"
	}
	return ""
}
