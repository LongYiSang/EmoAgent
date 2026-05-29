package memoryhost

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	memconfig "github.com/longyisang/emoagent-memorycore/config"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

type Host struct {
	Service          memorycore.Service
	Source           string
	DBPath           string
	retrievalPolicy  memorycore.RetrievalPolicy
	extractionPolicy ExtractionHostPolicy
	logger           *slog.Logger
}

func OpenFromConfig(ctx context.Context, path string, logger *slog.Logger) (*Host, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("memorycore config path is required")
	}

	cfg, err := memconfig.LoadEffective(memconfig.LoadEffectiveOptions{
		ConfigPath: path,
	})
	if err != nil {
		return nil, fmt.Errorf("load memorycore config %q: %w", path, err)
	}
	if !cfg.Enabled {
		return nil, fmt.Errorf("memorycore config enabled must be true")
	}

	runtime, err := cfg.Runtime()
	if err != nil {
		return nil, fmt.Errorf("build memorycore runtime config: %w", err)
	}
	if !runtime.Options.AutoMigrate {
		return nil, fmt.Errorf("memorycore core.auto_migrate must be true")
	}

	return open(ctx, runtime.Options, runtime.RetrievalPolicy, logger, path)
}

func OpenWithOptions(ctx context.Context, opts memorycore.Options, logger *slog.Logger) (*Host, error) {
	return open(ctx, opts, memorycore.RetrievalPolicy{}, logger, "options")
}

func (h *Host) Close() error {
	if h == nil {
		return nil
	}
	var closeErr error
	if h.Service == nil {
		return closeErr
	}
	if err := h.Service.Close(); err != nil {
		if closeErr == nil {
			closeErr = err
		}
	}
	if h.logger != nil {
		h.logger.Info("memorycore stopped", "db_path", h.DBPath)
	}
	h.Service = nil
	return closeErr
}

func (h *Host) SetExtractionRunner(runner *ExtractionRunner) {
	if h == nil {
		return
	}
	if runner == nil {
		h.extractionPolicy.Enabled = false
		return
	}
	h.extractionPolicy = extractionHostPolicyFromConfig(runner.cfg)
}

func (h *Host) ConfigureExtractionPolicy(policy ExtractionHostPolicy) {
	if h == nil {
		return
	}
	memoryCoreEnabled := h.extractionPolicy.Enabled
	if policy.SemanticDedup == (memorycore.SemanticDedupOptions{}) {
		policy.SemanticDedup = h.extractionPolicy.SemanticDedup
	}
	policy = policy.normalized()
	policy.Enabled = memoryCoreEnabled && policy.Enabled
	h.extractionPolicy = policy
}

func (h *Host) ExtractionEnabled() bool {
	return h != nil && h.Service != nil && h.extractionPolicy.Enabled
}

func (h *Host) extractionTriggerOnFinalizeSegment() bool {
	return h != nil && h.ExtractionEnabled() && h.extractionPolicy.TriggerOnFinalizeSegment
}

func (h *Host) ExtractSessionEnd(ctx context.Context, personaID string, memorySessionID string) (*memorycore.ExtractionRunResult, error) {
	if !h.ExtractionEnabled() {
		return nil, nil
	}
	memorySessionID = strings.TrimSpace(memorySessionID)
	if memorySessionID == "" {
		return nil, sanitizedExtractionError("missing_session", "")
	}
	personaID = defaultPersonaID(personaID)
	result, err := h.Service.RunExtraction(ctx, memorycore.RunExtractionRequest{
		PersonaID: personaID,
		SessionID: &memorySessionID,
		Trigger:   memorycore.ExtractionTriggerSessionEnd,
		Timezone:  h.extractionPolicy.timezoneOrDefault(),
		Mode:      h.extractionPolicy.sessionEndModeOrDefault(),
		Build: &memorycore.ExtractionBuildSelector{
			SessionID: &memorySessionID,
			Limit:     h.extractionPolicy.limitOrDefault(),
		},
		SemanticDedup: h.extractionPolicy.SemanticDedup,
	})
	if err != nil {
		return result, sanitizedExtractionError(extractionErrorCode(result, err), "")
	}
	return result, nil
}

func open(ctx context.Context, opts memorycore.Options, retrievalPolicy memorycore.RetrievalPolicy, logger *slog.Logger, source string) (*Host, error) {
	if strings.TrimSpace(opts.DBPath) == "" {
		return nil, fmt.Errorf("memorycore DBPath is required")
	}
	if !opts.AutoMigrate {
		return nil, fmt.Errorf("memorycore AutoMigrate must be true")
	}

	svc, err := memorycore.Open(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("open memorycore service: %w", err)
	}

	host := &Host{
		Service:          svc,
		Source:           source,
		DBPath:           opts.DBPath,
		retrievalPolicy:  retrievalPolicy,
		extractionPolicy: extractionHostPolicyFromOptions(opts),
		logger:           logger,
	}
	if logger != nil {
		logger.Info("memorycore opened", "source", source, "db_path", opts.DBPath)
	}
	return host, nil
}
