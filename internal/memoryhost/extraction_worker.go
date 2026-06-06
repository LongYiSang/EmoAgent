package memoryhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent/internal/storage"
)

type ExtractionWorkerConfig struct {
	WorkerID                  string
	ClaimLimit                int
	ClaimTTL                  time.Duration
	RetryBaseDelay            time.Duration
	RetryMaxDelay             time.Duration
	MirrorSyncAfterApply      bool
	MirrorSyncLimit           int
	FailExtractionOnSyncError bool
}

type ExtractionWorker struct {
	host   *Host
	db     *storage.DB
	logger *slog.Logger
	cfg    ExtractionWorkerConfig
}

func NewExtractionWorker(host *Host, db *storage.DB, logger *slog.Logger, cfg ExtractionWorkerConfig) *ExtractionWorker {
	if cfg.WorkerID == "" {
		cfg.WorkerID = "memory-extraction-worker"
	}
	if cfg.ClaimLimit <= 0 {
		cfg.ClaimLimit = 1
	}
	if cfg.ClaimTTL <= 0 {
		cfg.ClaimTTL = 5 * time.Minute
	}
	if cfg.RetryBaseDelay <= 0 {
		cfg.RetryBaseDelay = 30 * time.Second
	}
	if cfg.RetryMaxDelay <= 0 {
		cfg.RetryMaxDelay = 15 * time.Minute
	}
	if cfg.MirrorSyncLimit <= 0 {
		cfg.MirrorSyncLimit = 100
	}
	return &ExtractionWorker{host: host, db: db, logger: logger, cfg: cfg}
}

func (w *ExtractionWorker) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if _, err := w.RunOnce(ctx); err != nil && w.logger != nil {
			w.logger.Warn("memory extraction worker tick failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *ExtractionWorker) RunOnce(ctx context.Context) (int, error) {
	if w == nil || !w.host.configured() || w.db == nil {
		return 0, nil
	}
	jobs, err := w.db.ClaimMemoryExtractionJobs(ctx, w.cfg.WorkerID, w.cfg.ClaimLimit, w.cfg.ClaimTTL, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	for _, job := range jobs {
		if err := w.runJob(ctx, job); err != nil && w.logger != nil {
			w.logger.Warn("memory extraction job failed", "job_id", job.ID, "error_code", safeErrorCode(nil, err))
		}
	}
	return len(jobs), nil
}

func (w *ExtractionWorker) runJob(ctx context.Context, job storage.MemoryExtractionJob) error {
	req, err := w.buildRunExtractionRequest(job)
	if err != nil {
		_ = w.db.FailMemoryExtractionJob(ctx, job.ID, storage.FailMemoryExtractionJobParams{
			ErrorCode:    "invalid_job",
			ErrorMessage: "",
			Retry:        false,
		})
		return err
	}
	result, runErr := w.host.Core.RunExtraction(ctx, req)
	if runErr != nil || result == nil || !successfulExtractionStatus(result.Status) {
		return w.recordJobFailure(ctx, job, result, runErr)
	}

	status := storage.MemoryExtractionJobStatusSucceeded
	if result.SkippedByFingerprint || result.Status == memorycore.ExtractionRunStatusSkipped {
		status = storage.MemoryExtractionJobStatusSkipped
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return err
	}
	mirrorJSON, mirrorErr := w.runMirrorSyncAfterApply(ctx, job, status)
	if mirrorErr != nil {
		return w.recordJobFailure(ctx, job, result, mirrorErr)
	}
	return w.db.CompleteMemoryExtractionJob(ctx, job.ID, storage.CompleteMemoryExtractionJobParams{
		Status:               status,
		ResultJSON:           string(resultJSON),
		MirrorSyncResultJSON: mirrorJSON,
		ExtractedUntilAt:     job.UntilAt,
		ExpectedClaimedBy:    w.cfg.WorkerID,
	})
}

func (w *ExtractionWorker) buildRunExtractionRequest(job storage.MemoryExtractionJob) (memorycore.RunExtractionRequest, error) {
	memorySessionID := strings.TrimSpace(job.MemorySessionID)
	if memorySessionID == "" {
		return memorycore.RunExtractionRequest{}, fmt.Errorf("memory session id is required")
	}
	policy := w.host.extractionPolicy.normalized()
	req := memorycore.RunExtractionRequest{
		PersonaID:     defaultPersonaID(job.PersonaID),
		SessionID:     &memorySessionID,
		Trigger:       mapExtractionTrigger(job.Trigger),
		Timezone:      policy.Timezone,
		Mode:          memorycore.ExtractionRunMode(job.Mode),
		SemanticDedup: policy.SemanticDedup,
		Force:         job.Force,
		Build:         &memorycore.ExtractionBuildSelector{SessionID: &memorySessionID, EpisodeIDs: job.EpisodeIDs, Limit: job.EpisodeLimit},
		Policy: memorycore.ExtractionPolicyOverride{
			AllowInference:           boolPtr(policy.AllowInference),
			AllowSensitiveExtraction: boolPtr(policy.AllowSensitiveExtraction),
			MaxFacts:                 intPtr(policy.MaxFacts),
			MaxLinks:                 intPtr(policy.MaxLinks),
		},
	}
	if req.Mode == "" {
		req.Mode = policy.SessionEndMode
	}
	if req.Build.Limit == 0 {
		req.Build.Limit = policy.Limit
	}
	if req.Trigger == memorycore.ExtractionTriggerManualPin {
		req.Policy.ManualPin = boolPtr(true)
		req.Policy.AllowInference = boolPtr(true)
	}
	if since, ok := parseJobTime(job.SinceAt); ok {
		req.Build.Since = &since
	}
	if until, ok := parseJobTime(job.UntilAt); ok {
		req.Build.Until = &until
	}
	return req, nil
}

func (w *ExtractionWorker) recordJobFailure(ctx context.Context, job storage.MemoryExtractionJob, result *memorycore.ExtractionRunResult, err error) error {
	code := extractionErrorCode(result, err)
	message := ""
	if result != nil {
		message = result.SanitizedErrorMessage
	}
	if message == "" {
		var coded interface{ ErrorCode() string }
		if errors.As(err, &coded) && strings.TrimSpace(coded.ErrorCode()) != "" {
			message = err.Error()
		}
	}
	retry := job.Attempts < job.MaxAttempts
	nextRun := time.Now().UTC().Add(w.retryDelay(job.Attempts))
	if failErr := w.db.FailMemoryExtractionJob(ctx, job.ID, storage.FailMemoryExtractionJobParams{
		ErrorCode:         code,
		ErrorMessage:      message,
		Retry:             retry,
		NextRunAfter:      nextRun,
		ExpectedClaimedBy: w.cfg.WorkerID,
	}); failErr != nil {
		return failErr
	}
	if err != nil {
		return sanitizedExtractionError(code, "")
	}
	return sanitizedExtractionError(code, message)
}

func (w *ExtractionWorker) retryDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return w.cfg.RetryBaseDelay
	}
	multiplier := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(w.cfg.RetryBaseDelay) * multiplier)
	if delay > w.cfg.RetryMaxDelay {
		return w.cfg.RetryMaxDelay
	}
	return delay
}

func (w *ExtractionWorker) runMirrorSyncAfterApply(ctx context.Context, job storage.MemoryExtractionJob, status string) (string, error) {
	if !w.cfg.MirrorSyncAfterApply || status == storage.MemoryExtractionJobStatusSkipped || memorycore.ExtractionRunMode(job.Mode) != memorycore.ExtractionRunModeApply {
		return "", nil
	}
	mirror, err := w.host.Core.RunMirrorSync(ctx, memorycore.RunMirrorSyncRequest{
		PersonaID: defaultPersonaID(job.PersonaID),
		Limit:     w.cfg.MirrorSyncLimit,
	})
	if err != nil {
		degraded := map[string]any{"status": "degraded", "error_code": "mirror_sync_failed"}
		payload, _ := json.Marshal(degraded)
		if w.cfg.FailExtractionOnSyncError {
			return string(payload), sanitizedExtractionError("mirror_sync_failed", "")
		}
		return string(payload), nil
	}
	payload, err := json.Marshal(mirror)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func mapExtractionTrigger(trigger string) string {
	switch strings.TrimSpace(trigger) {
	case storage.MemoryExtractionTriggerIdleDetect:
		return memorycore.ExtractionTriggerIdleDetect
	case storage.MemoryExtractionTriggerSessionEnd:
		return memorycore.ExtractionTriggerSessionEnd
	case storage.MemoryExtractionTriggerManualPin:
		return memorycore.ExtractionTriggerManualPin
	case storage.MemoryExtractionTriggerReprocess, storage.MemoryExtractionTriggerManualScan, storage.MemoryExtractionTriggerManualSegmentScan, storage.MemoryExtractionTriggerPeriodicSweep:
		return memorycore.ExtractionTriggerReprocess
	default:
		return trigger
	}
}

func parseJobTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func intPtr(value int) *int {
	return &value
}
