// Package proactive owns the only path in EmoAgent that can produce an outbound
// message without the user having said anything. Everything else the agent
// sends is a response to user input.
//
// The pipeline is: a trigger source proposes a candidate → deterministic valves
// decide whether interrupting is permissible at all → an LLM gate decides
// whether now is a good moment → the dispatcher runs a full Emotion turn and
// delivers it. Plugins may only propose; they can never decide to speak.
package proactive

import (
	"time"

	"github.com/longyisang/emoagent/internal/storage"
)

// Verdict is the outcome of the whole decision chain, valves and gate alike.
type Verdict string

const (
	VerdictSpeak Verdict = "speak"
	VerdictSkip  Verdict = "skip"
)

// Skip reasons produced by the deterministic valves. These are stable strings:
// they land in proactive_decisions.reason and are the primary signal for tuning
// the feature, so they must stay greppable.
const (
	ReasonDisabled         = "disabled"
	ReasonQuietCommand     = "quiet_command"
	ReasonQuietHours       = "quiet_hours"
	ReasonDailyCapReached  = "daily_cap_reached"
	ReasonMinIntervalUnmet = "min_interval_unmet"
	ReasonIgnoredBackoff   = "ignored_backoff"
	ReasonUserPresent      = "user_present"
	ReasonNoCandidates     = "no_candidates"
	ReasonNoTarget         = "no_target"
	ReasonGateError        = "gate_error"
	ReasonGateDeclined     = "gate_declined"
	ReasonNotConfigured    = "not_configured"
)

// Decision is what the chain concluded, before any turn has run.
type Decision struct {
	Verdict      Verdict
	Reason       string
	Urgency      float64
	Hint         string
	CandidateIDs []string

	// GateConsulted distinguishes a valve short-circuit from a model verdict.
	// Valve skips are free; gate skips cost a call. Tracking this separately is
	// what makes the skip rate interpretable.
	GateConsulted bool
	GateModel     string
	GateLatencyMS int64
	GateTokens    int
}

// Candidate is a proposed reason to speak up, as seen by the gate.
type Candidate struct {
	ID           string
	EventType    string
	Summary      string
	ObservedFrom time.Time
	ObservedTo   time.Time
	Importance   float64
	SkipCount    int
}

// GateInput is the complete picture handed to the gate model. It deliberately
// excludes raw conversation text: the gate runs on a different (cheaper) model
// than Emotion, and this is an intimate companion agent, so shipping the user's
// actual messages to a second model is not acceptable. The relationship block,
// derived from the host's own running summary, stands in for it.
type GateInput struct {
	Candidates       []GateCandidate      `json:"candidates"`
	InterruptionCost GateInterruptionCost `json:"interruption_cost"`
	History          GateProactiveHistory `json:"proactive_history"`
	Relationship     GateRelationship     `json:"relationship"`
	Mood             GateMood             `json:"mood"`
}

type GateCandidate struct {
	EventType    string  `json:"event_type"`
	Summary      string  `json:"summary"`
	ObservedFrom string  `json:"observed_from"`
	ObservedTo   string  `json:"observed_to"`
	Importance   float64 `json:"importance"`
	SkipCount    int     `json:"skip_count"`
}

type GateInterruptionCost struct {
	MinutesSinceUserMessage  int    `json:"minutes_since_user_message"`
	MinutesSinceAgentMessage int    `json:"minutes_since_agent_message"`
	ConversationActive       bool   `json:"conversation_active"`
	LocalTime                string `json:"local_time"`
	IsWorkday                bool   `json:"is_workday"`
}

type GateProactiveHistory struct {
	SentLast24h        int      `json:"sent_last_24h"`
	LastSentMinutesAgo int      `json:"last_sent_minutes_ago,omitempty"`
	LastSentSummary    string   `json:"last_sent_summary,omitempty"`
	LastSentReplied    bool     `json:"last_sent_replied"`
	ConsecutiveIgnored int      `json:"consecutive_ignored"`
	RecentSkipReasons  []string `json:"recent_skip_reasons,omitempty"`
}

type GateRelationship struct {
	SessionGoal   string   `json:"session_goal,omitempty"`
	OpenLoops     []string `json:"open_loops,omitempty"`
	PromisesMade  []string `json:"promises_made,omitempty"`
	Tone          string   `json:"tone,omitempty"`
	RecentEmotion string   `json:"recent_emotion,omitempty"`
}

type GateMood struct {
	Concern     float64 `json:"concern"`
	Attachment  float64 `json:"attachment"`
	Playfulness float64 `json:"playfulness"`
	Energy      float64 `json:"energy"`
	Description string  `json:"description,omitempty"`
}

// GateOutput is what the gate model must return. Hint is the only thing that
// flows downstream: a note for Emotion, never a scripted line. Emotion writes
// the actual words with its full context, so the persona is not diluted by the
// cheaper model.
type GateOutput struct {
	Decision string  `json:"decision"`
	Reason   string  `json:"reason"`
	Urgency  float64 `json:"urgency"`
	Hint     string  `json:"hint"`
}

func candidateFromRecord(record storage.ProactiveCandidateRecord) Candidate {
	return Candidate{
		ID:           record.ID,
		EventType:    record.EventType,
		Summary:      record.Summary,
		ObservedFrom: parseTimeOrZero(record.ObservedFrom),
		ObservedTo:   parseTimeOrZero(record.ObservedTo),
		Importance:   record.Importance,
		SkipCount:    record.SkipCount,
	}
}

func parseTimeOrZero(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func candidateIDs(candidates []Candidate) []string {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.ID)
	}
	return ids
}
