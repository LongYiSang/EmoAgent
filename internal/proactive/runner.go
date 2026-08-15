package proactive

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/storage"
)

// Store is the persistence surface the runner needs. Satisfied by *storage.DB.
type Store interface {
	TargetStore
	ListProactiveCandidates(ctx context.Context, filter storage.ProactiveCandidateFilter) ([]storage.ProactiveCandidateRecord, error)
	MarkProactiveCandidates(ctx context.Context, ids []string, status string, decisionID string) error
	ExpireProactiveCandidates(ctx context.Context, now time.Time) (int, error)
	InsertProactiveDecision(ctx context.Context, record storage.ProactiveDecisionRecord) error
	ListProactiveDecisions(ctx context.Context, personaKey string, limit int) ([]storage.ProactiveDecisionRecord, error)
	CountProactiveSpeakSince(ctx context.Context, personaKey string, since time.Time) (int, error)
	CountProactiveConsecutiveIgnored(ctx context.Context, personaKey string, limit int) (int, error)
	LastMessageTimes(ctx context.Context, sessionID string) (string, string, error)
}

// ContextProvider supplies the relationship and mood blocks of the gate input.
// Both are optional: a missing block degrades the gate's judgement, it does not
// break it.
type ContextProvider interface {
	Relationship(ctx context.Context, personaKey, sessionID string) GateRelationship
	Mood(ctx context.Context, personaKey, sessionID string) GateMood
}

// Evaluation is the full outcome of one decision pass.
type Evaluation struct {
	PersonaKey string
	Decision   Decision
	DecisionID string
	Target     Target
	HasTarget  bool
	Candidates []Candidate
}

// Runner executes the decision chain: expire stale candidates → collect → run
// the deterministic valves → resolve a target → ask the gate. It stops at the
// first refusal and records every outcome, including refusals.
type Runner struct {
	store    Store
	gate     *Gate
	presence PresenceChecker
	quiet    QuietStore
	context  ContextProvider
	logger   *slog.Logger
	now      func() time.Time
}

func NewRunner(store Store, gate *Gate, presence PresenceChecker, quiet QuietStore, contextProvider ContextProvider, logger *slog.Logger) *Runner {
	return &Runner{
		store:    store,
		gate:     gate,
		presence: presence,
		quiet:    quiet,
		context:  contextProvider,
		logger:   logger,
		now:      time.Now,
	}
}

// SetClock overrides the runner's clock. Test hook.
func (r *Runner) SetClock(now func() time.Time) {
	if r != nil && now != nil {
		r.now = now
	}
}

// Evaluate decides whether the agent should speak, and where. It records the
// verdict either way — a skip that leaves no trace is indistinguishable from a
// broken pipeline, and this feature is silent most of the time by design.
func (r *Runner) Evaluate(ctx context.Context, cfg config.ProactiveConfig, personaKey string) (Evaluation, error) {
	if r == nil || r.store == nil {
		return Evaluation{Decision: Decision{Verdict: VerdictSkip, Reason: ReasonNotConfigured}}, nil
	}
	cfg = config.NormalizeProactiveConfig(cfg)
	now := r.now()

	if _, err := r.store.ExpireProactiveCandidates(ctx, now); err != nil {
		r.warn("expire proactive candidates failed", err)
	}

	candidates, err := r.collectCandidates(ctx, personaKey)
	if err != nil {
		return Evaluation{}, err
	}

	valveInput, err := r.buildValveInput(ctx, cfg, personaKey, now, len(candidates))
	if err != nil {
		return Evaluation{}, err
	}

	if reason, ok := EvaluateValves(cfg, valveInput); !ok {
		decision := Decision{Verdict: VerdictSkip, Reason: reason, CandidateIDs: candidateIDs(candidates)}
		return r.record(ctx, personaKey, decision, Target{}, false, candidates)
	}

	target, hasTarget, err := ResolveTarget(ctx, r.store, cfg, personaKey)
	if err != nil {
		return Evaluation{}, err
	}
	if !hasTarget {
		decision := Decision{Verdict: VerdictSkip, Reason: ReasonNoTarget, CandidateIDs: candidateIDs(candidates)}
		return r.record(ctx, personaKey, decision, Target{}, false, candidates)
	}

	gateInput, err := r.buildGateInput(ctx, cfg, personaKey, target, now, candidates, valveInput)
	if err != nil {
		return Evaluation{}, err
	}

	decision := r.gate.Decide(ctx, personaKey, gateInput)
	decision.CandidateIDs = candidateIDs(candidates)
	return r.record(ctx, personaKey, decision, target, true, candidates)
}

func (r *Runner) collectCandidates(ctx context.Context, personaKey string) ([]Candidate, error) {
	// Skipped candidates stay in play: the gate needs to see that it has already
	// declined this activity N times, and a persisting situation may still
	// escalate into something worth interrupting for.
	records, err := r.store.ListProactiveCandidates(ctx, storage.ProactiveCandidateFilter{
		PersonaKey: personaKey,
		Statuses:   []string{storage.ProactiveCandidateStatusPending, storage.ProactiveCandidateStatusSkipped},
		Limit:      20,
	})
	if err != nil {
		return nil, err
	}
	candidates := make([]Candidate, 0, len(records))
	for _, record := range records {
		candidates = append(candidates, candidateFromRecord(record))
	}
	return candidates, nil
}

func (r *Runner) buildValveInput(ctx context.Context, cfg config.ProactiveConfig, personaKey string, now time.Time, candidateCount int) (ValveInput, error) {
	in := ValveInput{
		PersonaKey:     personaKey,
		Now:            now,
		CandidateCount: candidateCount,
	}

	speaks, err := r.store.CountProactiveSpeakSince(ctx, personaKey, now.Add(-24*time.Hour))
	if err != nil {
		return in, err
	}
	in.SpeaksLast24h = speaks

	ignored, err := r.store.CountProactiveConsecutiveIgnored(ctx, personaKey, 10)
	if err != nil {
		return in, err
	}
	in.ConsecutiveIgnored = ignored

	decisions, err := r.store.ListProactiveDecisions(ctx, personaKey, 10)
	if err != nil {
		return in, err
	}
	for _, decision := range decisions {
		if decision.Decision != storage.ProactiveDecisionSpeak || decision.SilencedByEmotion {
			continue
		}
		if sentAt := parseTimeOrZero(decision.CreatedAt); !sentAt.IsZero() {
			in.HasPriorSpeak = true
			in.MinutesSinceLast = int(now.Sub(sentAt).Minutes())
		}
		break
	}

	if r.presence != nil {
		in.ConversationActive = r.presence.HasActiveRun(personaKey)
	}
	if r.quiet != nil {
		if until, ok := r.quiet.QuietUntil(ctx); ok {
			in.HasQuietUntil = true
			in.QuietUntil = until
		}
	}
	return in, nil
}

func (r *Runner) buildGateInput(ctx context.Context, cfg config.ProactiveConfig, personaKey string, target Target, now time.Time, candidates []Candidate, valves ValveInput) (GateInput, error) {
	in := GateInput{
		InterruptionCost: GateInterruptionCost{
			ConversationActive: valves.ConversationActive,
			LocalTime:          now.Format(time.RFC3339),
			IsWorkday:          now.Weekday() != time.Saturday && now.Weekday() != time.Sunday,
		},
		History: GateProactiveHistory{
			SentLast24h:        valves.SpeaksLast24h,
			ConsecutiveIgnored: valves.ConsecutiveIgnored,
		},
	}

	for _, candidate := range candidates {
		in.Candidates = append(in.Candidates, GateCandidate{
			EventType:    candidate.EventType,
			Summary:      candidate.Summary,
			ObservedFrom: formatOrEmpty(candidate.ObservedFrom),
			ObservedTo:   formatOrEmpty(candidate.ObservedTo),
			Importance:   candidate.Importance,
			SkipCount:    candidate.SkipCount,
		})
	}

	lastUser, lastAgent, err := r.store.LastMessageTimes(ctx, target.SessionID)
	if err != nil {
		return in, err
	}
	in.InterruptionCost.MinutesSinceUserMessage = minutesSince(now, lastUser)
	in.InterruptionCost.MinutesSinceAgentMessage = minutesSince(now, lastAgent)

	decisions, err := r.store.ListProactiveDecisions(ctx, personaKey, 10)
	if err != nil {
		return in, err
	}
	seenLastSpeak := false
	for _, decision := range decisions {
		if decision.Decision == storage.ProactiveDecisionSpeak && !decision.SilencedByEmotion && !seenLastSpeak {
			seenLastSpeak = true
			in.History.LastSentMinutesAgo = minutesSince(now, decision.CreatedAt)
			in.History.LastSentSummary = decision.Hint
			in.History.LastSentReplied = decision.UserRepliedAt != ""
			continue
		}
		if decision.Decision == storage.ProactiveDecisionSkip && len(in.History.RecentSkipReasons) < 3 {
			in.History.RecentSkipReasons = append(in.History.RecentSkipReasons, decision.Reason)
		}
	}

	if r.context != nil {
		in.Relationship = r.context.Relationship(ctx, personaKey, target.SessionID)
		in.Mood = r.context.Mood(ctx, personaKey, target.SessionID)
	}
	return in, nil
}

func (r *Runner) record(ctx context.Context, personaKey string, decision Decision, target Target, hasTarget bool, candidates []Candidate) (Evaluation, error) {
	decisionID := uuid.NewString()
	record := storage.ProactiveDecisionRecord{
		ID:            decisionID,
		PersonaKey:    personaKey,
		OriginKey:     target.Origin.OriginKey,
		CandidateIDs:  decision.CandidateIDs,
		Decision:      string(decision.Verdict),
		Reason:        decision.Reason,
		Urgency:       decision.Urgency,
		Hint:          decision.Hint,
		GateModel:     decision.GateModel,
		GateLatencyMS: decision.GateLatencyMS,
		GateTokens:    decision.GateTokens,
	}
	if err := r.store.InsertProactiveDecision(ctx, record); err != nil {
		return Evaluation{}, err
	}

	// A skip leaves the candidates in play with a bumped skip_count; only a
	// delivered message consumes them, and that happens after the turn runs.
	if decision.Verdict == VerdictSkip && len(decision.CandidateIDs) > 0 {
		if err := r.store.MarkProactiveCandidates(ctx, decision.CandidateIDs, storage.ProactiveCandidateStatusSkipped, decisionID); err != nil {
			r.warn("mark skipped candidates failed", err)
		}
	}

	return Evaluation{
		PersonaKey: personaKey,
		Decision:   decision,
		DecisionID: decisionID,
		Target:     target,
		HasTarget:  hasTarget,
		Candidates: candidates,
	}, nil
}

func (r *Runner) warn(msg string, err error) {
	if r != nil && r.logger != nil {
		r.logger.Warn(msg, "error", err)
	}
}

func formatOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func minutesSince(now time.Time, raw string) int {
	parsed := parseTimeOrZero(raw)
	if parsed.IsZero() {
		return 0
	}
	minutes := int(now.Sub(parsed).Minutes())
	if minutes < 0 {
		return 0
	}
	return minutes
}
