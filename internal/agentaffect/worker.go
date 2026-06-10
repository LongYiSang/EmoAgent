package agentaffect

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type BatchWorker struct {
	runtime      *Runtime
	workerID     string
	pollInterval time.Duration
}

func NewBatchWorker(runtime *Runtime, workerID string, pollInterval time.Duration) *BatchWorker {
	if workerID == "" {
		workerID = "agent_affect_worker"
	}
	if pollInterval <= 0 {
		pollInterval = 800 * time.Millisecond
	}
	return &BatchWorker{runtime: runtime, workerID: workerID, pollInterval: pollInterval}
}

func (w *BatchWorker) ProcessOnce(ctx context.Context) (bool, error) {
	if w == nil || w.runtime == nil {
		return false, nil
	}
	return w.runtime.ProcessNextBatch(ctx, w.workerID)
}

func (w *BatchWorker) Run(ctx context.Context) {
	if w == nil || w.runtime == nil {
		return
	}
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		processed, err := w.ProcessOnce(ctx)
		if err != nil && w.runtime.logger != nil {
			w.runtime.logger.Warn("agent affect batch worker failed", "error", err)
		}
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Runtime) ProcessNextBatch(ctx context.Context, workerID string) (bool, error) {
	if !r.cfg.Enabled || !r.cfg.StorageEnabled || !r.cfg.Async.Enabled || !r.cfg.Async.QueueEnabled || r.store == nil {
		return false, nil
	}
	if workerID == "" {
		workerID = "agent_affect_worker"
	}
	opts := ClaimBatchOptions{
		MaxJobs:        r.cfg.Async.Batch.MaxJobs,
		ClaimTTL:       time.Duration(r.cfg.Async.QueueClaimTTLSeconds) * time.Second,
		MaxAge:         time.Duration(r.cfg.Async.Batch.MaxAgeSeconds) * time.Second,
		MinWait:        time.Duration(r.cfg.Async.Batch.MinWaitMS) * time.Millisecond,
		MaxInputTokens: r.cfg.Async.Batch.MaxInputTokens,
		SplitSessions:  !r.cfg.Async.Batch.MergeAcrossSessions,
	}
	if !r.cfg.Async.Batch.Enabled {
		opts.MaxJobs = 1
	}
	batch, err := r.store.ClaimNextBatch(ctx, workerID, r.now(), opts)
	if err != nil {
		return false, err
	}
	if batch == nil {
		return false, nil
	}
	jobs, err := r.store.ListJobsByBatch(ctx, batch.ID)
	if err != nil {
		return true, r.failBatch(ctx, batch.ID, nil, err, false)
	}
	if len(jobs) == 0 {
		err := fmt.Errorf("agent affect batch %s has no jobs", batch.ID)
		return true, r.failBatch(ctx, batch.ID, jobs, err, false)
	}
	if batch.JobType == AffectJobTypeBarrier {
		if err := r.store.MarkBatchDone(ctx, MarkBatchDoneRequest{
			BatchID:    batch.ID,
			FinishedAt: r.now(),
			ClearRaw:   r.cfg.Async.ClearRawAfterDone,
		}); err != nil {
			return true, err
		}
		return true, nil
	}

	sessionID := commonNonEmpty(jobs, func(job AffectJobRecord) string { return job.SessionID })
	turnID := commonNonEmpty(jobs, func(job AffectJobRecord) string { return job.TurnID })
	beforeSession := sessionID
	if beforeSession == "" {
		beforeSession = jobs[0].SessionID
	}
	before, err := r.currentMood(ctx, batch.PersonaID, beforeSession)
	if err != nil {
		return true, r.failBatch(ctx, batch.ID, jobs, err, true)
	}
	recentSession := sessionID
	if strings.TrimSpace(r.cfg.State.RecentContextScope) != "session" {
		recentSession = ""
	}
	recent, _ := r.recentEvaluations(ctx, batch.PersonaID, recentSession)
	summary := buildBatchEvaluationInput(*batch, jobs, before, recent, r.cfg.Limits)
	req := EvaluateMoodImpactRequest{
		PersonaID: batch.PersonaID,
		SessionID: sessionID,
		TurnID:    turnID,
		BatchID:   batch.ID,
		Trigger: TriggerDescriptor{
			TriggerType:    "customize",
			CustomType:     "turn_batch",
			CustomTypeDesc: "Coalesced chronological completed chat turns for one mood owner.",
			SourceKind:     "agent_affect_job_batch",
			SourceRefType:  "agent_affect_job_batch",
			SourceRefID:    batch.ID,
		},
		Input: MoodImpactInput{
			Mode:    "mixed",
			Summary: summary,
		},
	}
	profile := r.profileForPrompt(ctx, batch.PersonaID)
	result, err := r.evaluator.Evaluate(ctx, LLMEvaluationRequest{
		PersonaID:            req.PersonaID,
		SessionID:            req.SessionID,
		TurnID:               req.TurnID,
		PersonaAffectProfile: profile,
		CurrentMood:          before,
		Trigger:              req.Trigger,
		Input:                req.Input,
		Recent:               recent,
	})
	if err != nil {
		return true, r.failBatch(ctx, batch.ID, jobs, err, true)
	}
	if err := r.ensureBatchBaseStillCurrent(ctx, batch.PersonaID, beforeSession, before); err != nil {
		return true, r.failBatch(ctx, batch.ID, jobs, err, false)
	}
	_, after, evalRecord, eventRecord := r.buildBatchCommitRecords(req, before, result)
	if err := r.store.CommitBatchEvaluation(ctx, CommitBatchEvaluationRequest{
		BatchID:    batch.ID,
		Evaluation: evalRecord,
		State:      after,
		Event:      eventRecord,
		FinishedAt: r.now(),
		ClearRaw:   r.cfg.Async.ClearRawAfterDone,
	}); err != nil {
		return true, err
	}
	return true, nil
}

func (r *Runtime) buildBatchCommitRecords(req EvaluateMoodImpactRequest, before MoodSnapshot, result LLMEvaluationResult) (evaluationState, MoodSnapshot, AffectEvaluationRecord, AffectEventRecord) {
	clamped := ClampMoodDelta(r.cfg, before.Vector, result.Delta, ClampOptions{CommittedBy: "core"})
	predicted := before
	predicted.StateID = ""
	predicted.Vector = clamped.PredictedState
	predicted.Label = defaultMoodLabel(result.Label)
	predicted.Confidence = result.Confidence
	predicted.MoodDescription = result.MoodDescription
	predicted.MoodReason = result.MoodReason
	predicted.PromptMoodText = result.PromptMoodText
	predicted.CauseSummary = result.CauseSummary
	predicted.VisibleCauseSummary = result.VisibleCauseSummary
	predicted.UpdatedAt = r.now()
	evalID := uuid.NewString()
	record := AffectEvaluationRecord{
		ID:                      evalID,
		PersonaID:               req.PersonaID,
		SessionID:               req.SessionID,
		TurnID:                  req.TurnID,
		BatchID:                 req.BatchID,
		MoodOwnerScope:          before.MoodOwnerScope,
		MoodOwnerID:             before.MoodOwnerID,
		Trigger:                 normalizeTrigger(req.Trigger),
		Input:                   r.storageInput(req.Input),
		ContextWindowPolicyJSON: "{}",
		BeforeStateID:           before.StateID,
		BeforeStateJSON:         mustJSON(before),
		PromptVersion:           "agent_affect_v2.prompt.v1",
		ResponseJSON:            result.RawResponseJSON,
		ProposedDelta:           result.Delta,
		ClampedDelta:            clamped.ClampedDelta,
		PredictedState:          predicted.Vector,
		MoodDescription:         result.MoodDescription,
		MoodReason:              result.MoodReason,
		PromptMoodText:          result.PromptMoodText,
		CauseSummary:            result.CauseSummary,
		VisibleCauseSummary:     result.VisibleCauseSummary,
		Confidence:              result.Confidence,
		ClampNotes:              clamped.Notes,
		Status:                  EvaluationStatusCommitted,
		CreatedAt:               r.now(),
	}
	after := predicted
	after.StateID = uuid.NewString()
	if after.Confidence == 0 {
		after.Confidence = 0.5
	}
	event := AffectEventRecord{
		ID:              uuid.NewString(),
		PersonaID:       req.PersonaID,
		SessionID:       req.SessionID,
		TurnID:          req.TurnID,
		BatchID:         req.BatchID,
		MoodOwnerScope:  before.MoodOwnerScope,
		MoodOwnerID:     before.MoodOwnerID,
		EvaluationID:    evalID,
		Trigger:         normalizeTrigger(req.Trigger),
		BeforeStateID:   before.StateID,
		AfterStateID:    after.StateID,
		ProposedDelta:   result.Delta,
		ClampedDelta:    clamped.ClampedDelta,
		CommittedDelta:  clamped.ClampedDelta,
		LabelBefore:     before.Label,
		LabelAfter:      after.Label,
		MoodDescription: after.MoodDescription,
		MoodReason:      after.MoodReason,
		PromptMoodText:  after.PromptMoodText,
		CauseSummary:    after.CauseSummary,
		Significance:    significance(clamped.ClampedDelta),
		Confidence:      after.Confidence,
		CommittedBy:     "core",
		CreatedAt:       r.now(),
	}
	state := evaluationState{
		evaluationID: evalID,
		eventID:      event.ID,
		before:       before,
		predicted:    predicted,
		after:        after,
		proposed:     result.Delta,
		clamped:      clamped,
		noChange:     result.Fallback || clamped.ClampedDelta.IsZero(),
		status:       EvaluationStatusCommitted,
	}
	return state, after, record, event
}

func (r *Runtime) ensureBatchBaseStillCurrent(ctx context.Context, personaID string, sessionID string, before MoodSnapshot) error {
	latest, err := r.currentMood(ctx, personaID, sessionID)
	if err != nil {
		return err
	}
	if before.StateID == "" {
		if latest.StateID != "" {
			return fmt.Errorf("agent affect mood state changed during batch")
		}
		return nil
	}
	if latest.StateID != "" && latest.StateID != before.StateID {
		return fmt.Errorf("agent affect mood state changed during batch")
	}
	return nil
}

func (r *Runtime) failBatch(ctx context.Context, batchID string, jobs []AffectJobRecord, cause error, retryable bool) error {
	retry := retryable && shouldRetryJobs(jobs)
	req := MarkBatchFailedRequest{
		BatchID:      batchID,
		ErrorMessage: cause.Error(),
		FinishedAt:   r.now(),
		Retry:        retry,
	}
	if retry {
		req.RetryAt = r.now().Add(r.retryDelay(jobs))
	}
	if err := r.store.MarkBatchFailed(ctx, req); err != nil {
		return fmt.Errorf("%w; mark batch failed: %v", cause, err)
	}
	return cause
}

func (r *Runtime) retryDelay(jobs []AffectJobRecord) time.Duration {
	base := time.Duration(r.cfg.Async.RetryBaseDelaySeconds) * time.Second
	if base <= 0 {
		base = 30 * time.Second
	}
	maxDelay := time.Duration(r.cfg.Async.RetryMaxDelaySeconds) * time.Second
	if maxDelay <= 0 {
		maxDelay = 15 * time.Minute
	}
	attempts := 1
	for _, job := range jobs {
		if job.Attempts > attempts {
			attempts = job.Attempts
		}
	}
	delay := base
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= maxDelay {
			return maxDelay
		}
	}
	return delay
}

func shouldRetryJobs(jobs []AffectJobRecord) bool {
	for _, job := range jobs {
		if job.Attempts < job.MaxAttempts {
			return true
		}
	}
	return false
}

func commonNonEmpty(jobs []AffectJobRecord, value func(AffectJobRecord) string) string {
	if len(jobs) == 0 {
		return ""
	}
	first := value(jobs[0])
	if first == "" {
		return ""
	}
	for _, job := range jobs[1:] {
		if value(job) != first {
			return ""
		}
	}
	return first
}

type batchEvaluationPayload struct {
	Instructions           []string                    `json:"instructions"`
	Batch                  batchEvaluationPayloadBatch `json:"batch"`
	CurrentMoodBeforeBatch MoodSnapshot                `json:"current_mood_before_batch"`
	RecentEvaluations      []batchEvaluationRecent     `json:"recent_evaluations"`
	DimensionLimits        any                         `json:"dimension_limits"`
}

type batchEvaluationPayloadBatch struct {
	BatchID        string                `json:"batch_id"`
	JobCount       int                   `json:"job_count"`
	MoodOwnerScope string                `json:"mood_owner_scope"`
	MoodOwnerID    string                `json:"mood_owner_id"`
	Turns          []batchEvaluationTurn `json:"turns"`
}

type batchEvaluationTurn struct {
	TurnID                 string `json:"turn_id,omitempty"`
	SessionID              string `json:"session_id,omitempty"`
	UserTextOrSummary      string `json:"user_text_or_summary,omitempty"`
	AssistantTextOrSummary string `json:"assistant_text_or_summary,omitempty"`
	MemoryContextSummary   string `json:"memory_context_summary,omitempty"`
}

type batchEvaluationRecent struct {
	ID                  string     `json:"id,omitempty"`
	SessionID           string     `json:"session_id,omitempty"`
	TurnID              string     `json:"turn_id,omitempty"`
	BatchID             string     `json:"batch_id,omitempty"`
	MoodOwnerScope      string     `json:"mood_owner_scope,omitempty"`
	MoodOwnerID         string     `json:"mood_owner_id,omitempty"`
	ProposedDelta       MoodVector `json:"proposed_delta"`
	ClampedDelta        MoodVector `json:"clamped_delta"`
	MoodDescription     string     `json:"mood_description,omitempty"`
	MoodReason          string     `json:"mood_reason,omitempty"`
	PromptMoodText      string     `json:"prompt_mood_text,omitempty"`
	CauseSummary        string     `json:"cause_summary,omitempty"`
	VisibleCauseSummary string     `json:"visible_cause_summary,omitempty"`
	Confidence          float64    `json:"confidence"`
	CreatedAt           time.Time  `json:"created_at"`
}

func buildBatchEvaluationInput(batch AffectJobBatchRecord, jobs []AffectJobRecord, before MoodSnapshot, recent []AffectEvaluationRecord, limits any) string {
	payload := batchEvaluationPayload{
		Instructions: []string{
			"You are evaluating the combined mood impact of a chronological batch of completed turns.",
			"Do not output per-turn deltas.",
			"Output one consolidated mood transition for the Agent after absorbing the whole batch.",
		},
		Batch: batchEvaluationPayloadBatch{
			BatchID:        batch.ID,
			JobCount:       len(jobs),
			MoodOwnerScope: batch.MoodOwnerScope,
			MoodOwnerID:    batch.MoodOwnerID,
			Turns:          make([]batchEvaluationTurn, 0, len(jobs)),
		},
		CurrentMoodBeforeBatch: before,
		RecentEvaluations:      make([]batchEvaluationRecent, 0, len(recent)),
		DimensionLimits:        limits,
	}
	for _, job := range jobs {
		payload.Batch.Turns = append(payload.Batch.Turns, batchEvaluationTurn{
			TurnID:                 job.TurnID,
			SessionID:              job.SessionID,
			UserTextOrSummary:      compactForSummary(defaultString(job.UserText, job.InputSummary), 1200),
			AssistantTextOrSummary: compactForSummary(defaultString(job.AssistantText, job.InputSummary), 1600),
			MemoryContextSummary:   compactForSummary(job.MemoryPromptBlock, 800),
		})
	}
	for _, item := range recent {
		payload.RecentEvaluations = append(payload.RecentEvaluations, batchEvaluationRecent{
			ID:                  item.ID,
			SessionID:           item.SessionID,
			TurnID:              item.TurnID,
			BatchID:             item.BatchID,
			MoodOwnerScope:      item.MoodOwnerScope,
			MoodOwnerID:         item.MoodOwnerID,
			ProposedDelta:       item.ProposedDelta,
			ClampedDelta:        item.ClampedDelta,
			MoodDescription:     item.MoodDescription,
			MoodReason:          item.MoodReason,
			PromptMoodText:      item.PromptMoodText,
			CauseSummary:        item.CauseSummary,
			VisibleCauseSummary: item.VisibleCauseSummary,
			Confidence:          item.Confidence,
			CreatedAt:           item.CreatedAt,
		})
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}
