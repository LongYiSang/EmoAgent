package memoryhost

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore/extractionruntime"
	_ "modernc.org/sqlite"
)

type ExtractionRunner struct {
	db     *sql.DB
	runner *extractionruntime.Runner
	cfg    ExtractionConfig
	logger *slog.Logger
}

func NewExtractionRunner(ctx context.Context, host *Host, cfg ExtractionConfig, logger *slog.Logger) (*ExtractionRunner, error) {
	return NewExtractionRunnerWithLLM(ctx, host, cfg, logger, nil)
}

func NewExtractionRunnerWithLLM(ctx context.Context, host *Host, cfg ExtractionConfig, logger *slog.Logger, llm memorycore.ExtractionLLM) (*ExtractionRunner, error) {
	cfg = cfg.normalized()
	if !cfg.Enabled {
		return nil, nil
	}
	if host == nil || host.Service == nil {
		return nil, fmt.Errorf("memory host is not configured")
	}
	if strings.TrimSpace(host.DBPath) == "" {
		return nil, fmt.Errorf("memorycore DBPath is required")
	}
	if err := cfg.validateForRunner(llm); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", host.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open extraction sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping extraction sqlite: %w", err)
	}

	if llm == nil {
		llm = extractionruntime.NewOpenAICompatibleLLM(extractionruntime.OpenAICompatibleOptions{
			BaseURL:     cfg.Provider.BaseURL,
			APIKeyEnv:   cfg.Provider.APIKeyEnv,
			Model:       cfg.Provider.Model,
			Timeout:     cfg.Provider.Timeout,
			Temperature: cfg.Provider.Temperature,
			MaxTokens:   cfg.Provider.MaxTokens,
			Thinking:    openAICompatibleThinkingOptions(cfg.Provider.Thinking),
		})
	}

	var audit extractionruntime.AuditStore
	if cfg.AuditEnabled {
		audit = extractionruntime.NewSQLiteAuditStore(db)
	}

	return &ExtractionRunner{
		db: db,
		runner: extractionruntime.NewRunner(extractionruntime.RunnerOptions{
			DB:         db,
			Service:    host.Service,
			LLM:        llm,
			AuditStore: audit,
		}),
		cfg:    cfg,
		logger: logger,
	}, nil
}

func (r *ExtractionRunner) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	err := r.db.Close()
	r.db = nil
	return err
}

func (r *ExtractionRunner) enabled() bool {
	return r != nil && r.runner != nil && r.cfg.Enabled
}

func (r *ExtractionRunner) triggerOnFinalizeSegment() bool {
	return r != nil && r.cfg.TriggerOnFinalizeSegment
}

func (r *ExtractionRunner) ExtractSessionEnd(ctx context.Context, personaID string, memorySessionID string) (*memorycore.ExtractionRunResult, error) {
	if !r.enabled() {
		return nil, nil
	}
	personaID = defaultPersonaID(personaID)
	memorySessionID = strings.TrimSpace(memorySessionID)
	if memorySessionID == "" {
		return nil, sanitizedExtractionError("missing_session", "")
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if r.cfg.Provider.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, r.cfg.Provider.Timeout)
		defer cancel()
	}

	req, err := extractionruntime.BuildRequest(runCtx, r.db, extractionruntime.BuildRequestOptions{
		PersonaID:                personaID,
		SessionID:                &memorySessionID,
		Trigger:                  memorycore.ExtractionTriggerSessionEnd,
		Limit:                    r.cfg.Limit,
		Timezone:                 r.cfg.Timezone,
		AllowInference:           r.cfg.AllowInference,
		AllowSensitiveExtraction: r.cfg.AllowSensitiveExtraction,
		MaxFacts:                 r.cfg.MaxFacts,
		MaxLinks:                 r.cfg.MaxLinks,
		Now:                      time.Now().UTC(),
	})
	if err != nil {
		return nil, sanitizedExtractionError("build_request_failed", "")
	}

	result, err := r.runner.Run(runCtx, memorycore.ExtractionRunRequest{
		Request:       req,
		Mode:          r.cfg.Mode,
		ProviderID:    r.cfg.Provider.ID,
		ProviderKind:  r.cfg.Provider.Kind,
		Model:         r.cfg.Provider.Model,
		Temperature:   r.cfg.Provider.Temperature,
		MaxTokens:     r.cfg.Provider.MaxTokens,
		Timeout:       r.cfg.Provider.Timeout,
		RepairEnabled: r.cfg.RepairEnabled,
		Audit:         r.cfg.auditMode(),
		RawLog: memorycore.ExtractionRawLogOptions{
			Enabled:   r.cfg.RawLog.Enabled,
			Directory: r.cfg.RawLog.Directory,
		},
		Window: memorycore.ExtractionRunWindow{
			Limit: r.cfg.Limit,
		},
	})
	if err != nil {
		return &result, sanitizedExtractionError(firstNonEmptyString(result.SanitizedErrorCode, "runner_failed"), result.SanitizedErrorMessage)
	}
	if !successfulExtractionStatus(result.Status) {
		return &result, sanitizedExtractionError(firstNonEmptyString(result.SanitizedErrorCode, "unexpected_status"), "")
	}
	return &result, nil
}

func openAICompatibleThinkingOptions(cfg ExtractionThinkingConfig) *extractionruntime.OpenAICompatibleThinkingOptions {
	if strings.TrimSpace(cfg.Type) == "" {
		return nil
	}
	return &extractionruntime.OpenAICompatibleThinkingOptions{Type: cfg.Type}
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
	message = strings.TrimSpace(message)
	if message == "" {
		return fmt.Errorf("memory extraction failed code=%s", code)
	}
	return errors.New("memory extraction failed code=" + code + " message=" + message)
}
