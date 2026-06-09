package agentaffect

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent/internal/config"
)

type Service interface {
	GetCurrentMood(ctx context.Context, req GetCurrentMoodRequest) (GetCurrentMoodResponse, error)
	EvaluateMoodImpact(ctx context.Context, req EvaluateMoodImpactRequest) (EvaluateMoodImpactResponse, error)
	SubmitMoodImpact(ctx context.Context, req SubmitMoodImpactRequest) (SubmitMoodImpactResponse, error)
	ApplyMoodDelta(ctx context.Context, req ApplyMoodDeltaRequest) (ApplyMoodDeltaResponse, error)
	BuildPromptAffectBlock(ctx context.Context, req BuildPromptAffectBlockRequest) (string, error)
}

type Runtime struct {
	cfg       config.AgentAffectConfig
	store     Store
	evaluator Evaluator
	logger    *slog.Logger
	now       func() time.Time
}

type RuntimeOptions struct {
	Config    config.AgentAffectConfig
	Store     Store
	Evaluator Evaluator
	Logger    *slog.Logger
	Now       func() time.Time
}

func NewRuntime(opts RuntimeOptions) *Runtime {
	evaluator := opts.Evaluator
	if evaluator == nil {
		evaluator = DisabledEvaluator{}
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Runtime{
		cfg:       opts.Config,
		store:     opts.Store,
		evaluator: evaluator,
		logger:    logger,
		now:       now,
	}
}

func (r *Runtime) GetCurrentMood(ctx context.Context, req GetCurrentMoodRequest) (GetCurrentMoodResponse, error) {
	mood, err := r.currentMood(ctx, req.PersonaID, req.SessionID)
	if err != nil {
		return GetCurrentMoodResponse{}, err
	}
	return GetCurrentMoodResponse{Enabled: r.cfg.Enabled, Mood: mood}, nil
}

func (r *Runtime) EvaluateMoodImpact(ctx context.Context, req EvaluateMoodImpactRequest) (EvaluateMoodImpactResponse, error) {
	eval, err := r.evaluate(ctx, req, false)
	if err != nil {
		return EvaluateMoodImpactResponse{}, err
	}
	return EvaluateMoodImpactResponse{
		Enabled:       r.cfg.Enabled,
		EvaluationID:  eval.evaluationID,
		Mood:          eval.before,
		ProposedDelta: eval.proposed,
		ClampedDelta:  eval.clamped.ClampedDelta,
		PredictedMood: eval.predicted,
		ClampNotes:    eval.clamped.Notes,
		NoChange:      eval.noChange,
		Status:        eval.status,
	}, nil
}

func (r *Runtime) SubmitMoodImpact(ctx context.Context, req SubmitMoodImpactRequest) (SubmitMoodImpactResponse, error) {
	if !r.cfg.Enabled {
		mood := baselineSnapshot(req.PersonaID, req.SessionID, r.now())
		return SubmitMoodImpactResponse{Enabled: false, Mood: mood, NoChange: true, Status: EvaluationStatusPreview}, nil
	}
	commit := req.CommitMode == "" || req.CommitMode == CommitModeCommitIfAllowed
	eval, err := r.evaluate(ctx, EvaluateMoodImpactRequest(req), commit)
	if err != nil {
		return SubmitMoodImpactResponse{}, err
	}
	resp := SubmitMoodImpactResponse{
		Enabled:       true,
		EvaluationID:  eval.evaluationID,
		EventID:       eval.eventID,
		Mood:          eval.after,
		ProposedDelta: eval.proposed,
		ClampedDelta:  eval.clamped.ClampedDelta,
		ClampNotes:    eval.clamped.Notes,
		NoChange:      eval.noChange,
		Status:        eval.status,
	}
	if !commit {
		resp.Mood = eval.predicted
	}
	return resp, nil
}

func (r *Runtime) ApplyMoodDelta(ctx context.Context, req ApplyMoodDeltaRequest) (ApplyMoodDeltaResponse, error) {
	if !r.cfg.Enabled {
		return ApplyMoodDeltaResponse{Mood: baselineSnapshot(req.PersonaID, req.SessionID, r.now())}, nil
	}
	committedBy, err := normalizeCommittedBy(req.CommittedBy)
	if err != nil {
		return ApplyMoodDeltaResponse{}, err
	}
	before, err := r.currentMood(ctx, req.PersonaID, req.SessionID)
	if err != nil {
		return ApplyMoodDeltaResponse{}, err
	}
	clamped := ClampMoodDelta(r.cfg, before.Vector, req.Delta, ClampOptions{CommittedBy: committedBy})
	after := before
	after.StateID = uuid.NewString()
	after.Vector = clamped.PredictedState
	after.UpdatedAt = r.now()
	if r.cfg.StorageEnabled && r.store != nil {
		event := AffectEventRecord{
			ID:             uuid.NewString(),
			PersonaID:      req.PersonaID,
			SessionID:      req.SessionID,
			TurnID:         req.TurnID,
			Trigger:        req.Trigger,
			BeforeStateID:  before.StateID,
			AfterStateID:   after.StateID,
			ProposedDelta:  req.Delta,
			ClampedDelta:   clamped.ClampedDelta,
			CommittedDelta: clamped.ClampedDelta,
			LabelBefore:    before.Label,
			LabelAfter:     after.Label,
			CauseSummary:   after.CauseSummary,
			Significance:   significance(clamped.ClampedDelta),
			Confidence:     after.Confidence,
			CommittedBy:    committedBy,
			CreatedAt:      r.now(),
		}
		if err := r.store.CommitStateEvent(ctx, after, event); err != nil {
			return ApplyMoodDeltaResponse{}, err
		}
		return ApplyMoodDeltaResponse{EventID: event.ID, Mood: after, ClampedDelta: clamped.ClampedDelta, ClampNotes: clamped.Notes}, nil
	}
	return ApplyMoodDeltaResponse{Mood: after, ClampedDelta: clamped.ClampedDelta, ClampNotes: clamped.Notes}, nil
}

func (r *Runtime) BuildPromptAffectBlock(ctx context.Context, req BuildPromptAffectBlockRequest) (string, error) {
	if !r.cfg.Enabled || !r.cfg.Prompt.IncludeMoodBlock {
		return "", nil
	}
	mood := req.Mood
	if mood.PersonaID == "" {
		var err error
		mood, err = r.currentMood(ctx, req.PersonaID, req.SessionID)
		if err != nil {
			return "", err
		}
	}
	return FormatPromptAffectBlock(r.cfg, mood), nil
}

type evaluationState struct {
	evaluationID string
	eventID      string
	before       MoodSnapshot
	predicted    MoodSnapshot
	after        MoodSnapshot
	proposed     MoodVector
	clamped      ClampResult
	noChange     bool
	status       string
}

func (r *Runtime) evaluate(ctx context.Context, req EvaluateMoodImpactRequest, commit bool) (evaluationState, error) {
	if !r.cfg.Enabled {
		mood := baselineSnapshot(req.PersonaID, req.SessionID, r.now())
		return evaluationState{before: mood, predicted: mood, after: mood, noChange: true, status: EvaluationStatusPreview}, nil
	}
	before, err := r.currentMood(ctx, req.PersonaID, req.SessionID)
	if err != nil {
		return evaluationState{}, err
	}
	profile := r.profileForPrompt(ctx, req.PersonaID)
	recent, _ := r.recentEvaluations(ctx, req.PersonaID, req.SessionID)
	result, err := r.evaluator.Evaluate(ctx, LLMEvaluationRequest{
		PersonaID:            req.PersonaID,
		SessionID:            req.SessionID,
		TurnID:               req.TurnID,
		PersonaAffectProfile: profile,
		CurrentMood:          before,
		Trigger:              req.Trigger,
		Input:                req.Input,
		MemoryPromptBlock:    req.MemoryPromptBlock,
		Recent:               recent,
	})
	if err != nil {
		return evaluationState{}, err
	}
	status := result.Status
	if status == "" {
		status = EvaluationStatusPreview
	}
	clamped := ClampMoodDelta(r.cfg, before.Vector, result.Delta, ClampOptions{CommittedBy: "core"})
	predicted := before
	predicted.StateID = ""
	predicted.Vector = clamped.PredictedState
	predicted.Label = defaultMoodLabel(result.Label)
	predicted.Confidence = result.Confidence
	predicted.CauseSummary = result.CauseSummary
	predicted.VisibleCauseSummary = result.VisibleCauseSummary
	predicted.UpdatedAt = r.now()
	evalID := uuid.NewString()
	if r.cfg.StorageEnabled && r.store != nil {
		record := AffectEvaluationRecord{
			ID:                      evalID,
			PersonaID:               req.PersonaID,
			SessionID:               req.SessionID,
			TurnID:                  req.TurnID,
			Trigger:                 normalizeTrigger(req.Trigger),
			Input:                   normalizeInput(req.Input),
			ContextWindowPolicyJSON: "{}",
			BeforeStateID:           before.StateID,
			BeforeStateJSON:         mustJSON(before),
			PromptVersion:           "agent_affect_v2.prompt.v1",
			ResponseJSON:            result.RawResponseJSON,
			ProposedDelta:           result.Delta,
			ClampedDelta:            clamped.ClampedDelta,
			PredictedState:          predicted.Vector,
			CauseSummary:            result.CauseSummary,
			VisibleCauseSummary:     result.VisibleCauseSummary,
			Confidence:              result.Confidence,
			ClampNotes:              clamped.Notes,
			Status:                  status,
			CreatedAt:               r.now(),
		}
		if err := r.store.InsertEvaluation(ctx, record); err != nil {
			return evaluationState{}, err
		}
	}
	state := evaluationState{
		evaluationID: evalID,
		before:       before,
		predicted:    predicted,
		after:        predicted,
		proposed:     result.Delta,
		clamped:      clamped,
		noChange:     result.Fallback || clamped.ClampedDelta.IsZero(),
		status:       status,
	}
	if !commit {
		return state, nil
	}
	after := predicted
	after.StateID = uuid.NewString()
	if after.Confidence == 0 {
		after.Confidence = 0.5
	}
	if r.cfg.StorageEnabled && r.store != nil {
		if err := r.store.InsertState(ctx, after); err != nil {
			return evaluationState{}, err
		}
		event := AffectEventRecord{
			ID:             uuid.NewString(),
			PersonaID:      req.PersonaID,
			SessionID:      req.SessionID,
			TurnID:         req.TurnID,
			EvaluationID:   evalID,
			Trigger:        normalizeTrigger(req.Trigger),
			BeforeStateID:  before.StateID,
			AfterStateID:   after.StateID,
			ProposedDelta:  result.Delta,
			ClampedDelta:   clamped.ClampedDelta,
			CommittedDelta: clamped.ClampedDelta,
			LabelBefore:    before.Label,
			LabelAfter:     after.Label,
			CauseSummary:   after.CauseSummary,
			Significance:   significance(clamped.ClampedDelta),
			Confidence:     after.Confidence,
			CommittedBy:    "core",
			CreatedAt:      r.now(),
		}
		if err := r.store.InsertEvent(ctx, event); err != nil {
			return evaluationState{}, err
		}
		if err := r.store.MarkEvaluationCommitted(ctx, evalID, after.StateID); err != nil {
			return evaluationState{}, err
		}
		state.eventID = event.ID
	}
	state.after = after
	state.status = EvaluationStatusCommitted
	return state, nil
}

func (r *Runtime) currentMood(ctx context.Context, personaID string, sessionID string) (MoodSnapshot, error) {
	if strings.TrimSpace(personaID) == "" {
		return MoodSnapshot{}, fmt.Errorf("persona_id is required")
	}
	if !r.cfg.Enabled || !r.cfg.StorageEnabled || r.store == nil {
		return baselineSnapshot(personaID, sessionID, r.now()), nil
	}
	profile, err := r.store.EnsureProfile(ctx, personaID)
	if err != nil {
		return MoodSnapshot{}, err
	}
	state, err := r.store.GetLatestState(ctx, personaID, sessionID)
	if err != nil {
		return MoodSnapshot{}, err
	}
	if state != nil {
		return *state, nil
	}
	return MoodSnapshot{
		PersonaID:    personaID,
		SessionID:    sessionID,
		Vector:       profile.Baseline,
		Label:        "baseline",
		Confidence:   0.5,
		CauseSummary: "Baseline mood.",
		UpdatedAt:    r.now(),
	}, nil
}

func (r *Runtime) recentEvaluations(ctx context.Context, personaID string, sessionID string) ([]AffectEvaluationRecord, error) {
	if !r.cfg.Context.IncludePreviousEvaluations || r.store == nil {
		return nil, nil
	}
	limit := r.cfg.Context.PreviousEvaluationKeepLast
	if limit <= 0 {
		limit = 30
	}
	return r.store.ListRecentEvaluations(ctx, RecentEvaluationsQuery{PersonaID: personaID, SessionID: sessionID, Limit: limit})
}

func (r *Runtime) profileForPrompt(ctx context.Context, personaID string) AffectProfile {
	if r.cfg.Enabled && r.cfg.StorageEnabled && r.store != nil {
		if profile, err := r.store.EnsureProfile(ctx, personaID); err == nil {
			return profile
		}
	}
	return AffectProfile{
		PersonaID:   personaID,
		ProfileName: "default",
		Baseline:    baselineSnapshot(personaID, "", r.now()).Vector,
	}
}

func baselineSnapshot(personaID string, sessionID string, now time.Time) MoodSnapshot {
	return MoodSnapshot{
		PersonaID:  personaID,
		SessionID:  sessionID,
		Label:      "baseline",
		Confidence: 0.5,
		Vector: MoodVector{
			Arousal:     0.2,
			Energy:      0.5,
			Warmth:      0.6,
			Concern:     0.3,
			Curiosity:   0.3,
			Playfulness: 0.2,
			Uncertainty: 0.1,
		},
		CauseSummary: "Baseline mood.",
		UpdatedAt:    now,
	}
}

func normalizeTrigger(trigger TriggerDescriptor) TriggerDescriptor {
	if trigger.TriggerType == "" {
		trigger.TriggerType = "debug"
	}
	return trigger
}

func normalizeInput(input MoodImpactInput) MoodImpactInput {
	if input.Mode == "" {
		input.Mode = "raw"
	}
	return input
}

func normalizeCommittedBy(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "user_debug", nil
	}
	switch value {
	case "core", "plugin", "user_debug", "system":
		return value, nil
	default:
		return "", fmt.Errorf("committed_by must be one of core, plugin, user_debug, system")
	}
}

func defaultMoodLabel(label string) string {
	if strings.TrimSpace(label) == "" {
		return "steady"
	}
	return strings.TrimSpace(label)
}

func significance(delta MoodVector) float64 {
	values := []float64{
		abs(delta.Valence), abs(delta.Arousal), abs(delta.Dominance), abs(delta.Energy),
		abs(delta.Warmth), abs(delta.Concern), abs(delta.Curiosity), abs(delta.Playfulness),
		abs(delta.Attachment), abs(delta.Frustration), abs(delta.Uncertainty),
	}
	var max float64
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	if max > 1 {
		return 1
	}
	if max == 0 {
		return 0.5
	}
	return max
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
