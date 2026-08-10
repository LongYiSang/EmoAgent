// Package badsample freezes the scene around a reply the user flagged as bad.
//
// The scene is copied, never referenced: prompt snapshots are pruned by
// retention (30 days / 1000 rows by default), while samples are meant to stay
// readable for as long as it takes to sit down and analyse them.
package badsample

import (
	"encoding/json"

	"github.com/longyisang/emoagent/internal/storage"
)

// ContextSchemaVersion identifies the shape of FrozenContext. Readers should
// check it before parsing so that older samples stay readable after the shape
// evolves.
const ContextSchemaVersion = 1

const (
	// MaxMessages is how far back the conversation is frozen. Messages have no
	// turn_id, so they are taken by session recency rather than sliced per turn.
	MaxMessages = 12
	// MaxPromptSnapshots / MaxTurns / MaxAffectEvaluations bound the per-turn
	// views. More than the flagged turn is kept because the root cause often
	// sits a few turns earlier than where the user noticed it.
	MaxPromptSnapshots = 3
	MaxTurns           = 3
	MaxAffectEvals     = 3
	// MaxContextBytes caps a single frozen scene. A partial scene is acceptable;
	// a sample large enough to bloat the database is not.
	MaxContextBytes = 256 * 1024
)

type FrozenMeta struct {
	CapturedAt   string `json:"captured_at"`
	SessionID    string `json:"session_id"`
	PersonaKey   string `json:"persona_key"`
	OriginKey    string `json:"origin_key"`
	TargetTurnID string `json:"target_turn_id"`
	Model        string `json:"model"`
}

type FrozenMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
	// Metadata is the raw messages.metadata string. For assistant rows it holds
	// memory_pipeline.prompt_block — the memory block actually injected that
	// turn — which is the primary material for attribution.
	Metadata string `json:"metadata"`
}

type FrozenPromptSnapshot struct {
	TurnID         string `json:"turn_id"`
	Purpose        string `json:"purpose"`
	Model          string `json:"model"`
	CreatedAt      string `json:"created_at"`
	ComponentsJSON string `json:"components_json"`
	RenderedText   string `json:"rendered_text"`
	Truncated      bool   `json:"truncated,omitempty"`
}

type FrozenAffectState struct {
	Label           string  `json:"label"`
	Confidence      float64 `json:"confidence"`
	StateVectorJSON string  `json:"state_vector_json"`
	CauseSummary    string  `json:"cause_summary"`
	MoodDescription string  `json:"mood_description"`
	MoodReason      string  `json:"mood_reason"`
	PromptMoodText  string  `json:"prompt_mood_text"`
	UpdatedAt       string  `json:"updated_at"`
}

type FrozenAffectEvaluation struct {
	TurnID           string  `json:"turn_id"`
	TriggerType      string  `json:"trigger_type"`
	Status           string  `json:"status"`
	LLMModel         string  `json:"llm_model"`
	ClampedDeltaJSON string  `json:"clamped_delta_json"`
	CauseSummary     string  `json:"cause_summary"`
	MoodDescription  string  `json:"mood_description"`
	MoodReason       string  `json:"mood_reason"`
	Confidence       float64 `json:"confidence"`
	CreatedAt        string  `json:"created_at"`
}

type FrozenAffect struct {
	State             *FrozenAffectState       `json:"state"`
	RecentEvaluations []FrozenAffectEvaluation `json:"recent_evaluations"`
}

type FrozenTurn struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	State        string `json:"state"`
	Status       string `json:"status"`
	ErrorKind    string `json:"error_kind"`
	ErrorMessage string `json:"error_message"`
	StartedAt    string `json:"started_at"`
	CompletedAt  string `json:"completed_at"`
}

// BlockError records that one part of the scene could not be collected. Blocks
// are gathered independently so a single failure degrades the sample instead of
// losing it.
type BlockError struct {
	Block   string `json:"block"`
	Message string `json:"message"`
}

type FrozenContext struct {
	ContextSchemaVersion int                    `json:"context_schema_version"`
	Meta                 FrozenMeta             `json:"meta"`
	Messages             []FrozenMessage        `json:"messages"`
	PromptSnapshots      []FrozenPromptSnapshot `json:"prompt_snapshots"`
	Affect               FrozenAffect           `json:"affect"`
	Turns                []FrozenTurn           `json:"turns"`
	Errors               []BlockError           `json:"errors,omitempty"`
	Truncated            bool                   `json:"truncated"`
}

func (c *FrozenContext) addError(block string, err error) {
	if err == nil {
		return
	}
	c.Errors = append(c.Errors, BlockError{Block: block, Message: err.Error()})
}

func convertMessages(in []storage.BadSampleMessage) []FrozenMessage {
	out := make([]FrozenMessage, 0, len(in))
	for _, m := range in {
		out = append(out, FrozenMessage{
			Role:      m.Role,
			Content:   m.Content,
			CreatedAt: m.CreatedAt,
			Metadata:  m.Metadata,
		})
	}
	return out
}

func convertSnapshots(in []storage.BadSamplePromptSnapshot) []FrozenPromptSnapshot {
	out := make([]FrozenPromptSnapshot, 0, len(in))
	for _, s := range in {
		out = append(out, FrozenPromptSnapshot{
			TurnID:         s.TurnID,
			Purpose:        s.Purpose,
			Model:          s.Model,
			CreatedAt:      s.CreatedAt,
			ComponentsJSON: s.ComponentsJSON,
			RenderedText:   s.RenderedText,
		})
	}
	return out
}

func convertAffect(in storage.BadSampleAffect) FrozenAffect {
	out := FrozenAffect{RecentEvaluations: make([]FrozenAffectEvaluation, 0, len(in.RecentEvaluations))}
	if in.State != nil {
		out.State = &FrozenAffectState{
			Label:           in.State.Label,
			Confidence:      in.State.Confidence,
			StateVectorJSON: in.State.StateVectorJSON,
			CauseSummary:    in.State.CauseSummary,
			MoodDescription: in.State.MoodDescription,
			MoodReason:      in.State.MoodReason,
			PromptMoodText:  in.State.PromptMoodText,
			UpdatedAt:       in.State.UpdatedAt,
		}
	}
	for _, e := range in.RecentEvaluations {
		out.RecentEvaluations = append(out.RecentEvaluations, FrozenAffectEvaluation{
			TurnID:           e.TurnID,
			TriggerType:      e.TriggerType,
			Status:           e.Status,
			LLMModel:         e.LLMModel,
			ClampedDeltaJSON: e.ClampedDeltaJSON,
			CauseSummary:     e.CauseSummary,
			MoodDescription:  e.MoodDescription,
			MoodReason:       e.MoodReason,
			Confidence:       e.Confidence,
			CreatedAt:        e.CreatedAt,
		})
	}
	return out
}

func convertTurns(in []storage.BadSampleTurn) []FrozenTurn {
	out := make([]FrozenTurn, 0, len(in))
	for _, t := range in {
		out = append(out, FrozenTurn{
			ID:           t.ID,
			Kind:         t.Kind,
			State:        t.State,
			Status:       t.Status,
			ErrorKind:    t.ErrorKind,
			ErrorMessage: t.ErrorMessage,
			StartedAt:    t.StartedAt,
			CompletedAt:  t.CompletedAt,
		})
	}
	return out
}

// encodeWithinLimit serialises the scene, shrinking it if it exceeds
// MaxContextBytes. Rendered prompt text is dropped oldest-first because it is
// both the bulkiest field and the most redundant: the newest turn is the one
// being complained about, and the memory block survives in message metadata
// regardless.
func encodeWithinLimit(ctx *FrozenContext) ([]byte, error) {
	encoded, err := json.Marshal(ctx)
	if err != nil {
		return nil, err
	}
	if len(encoded) <= MaxContextBytes {
		return encoded, nil
	}
	for i := range ctx.PromptSnapshots {
		if ctx.PromptSnapshots[i].RenderedText == "" {
			continue
		}
		ctx.PromptSnapshots[i].RenderedText = ""
		ctx.PromptSnapshots[i].Truncated = true
		ctx.Truncated = true
		encoded, err = json.Marshal(ctx)
		if err != nil {
			return nil, err
		}
		if len(encoded) <= MaxContextBytes {
			return encoded, nil
		}
	}
	// Still oversized: message content is the last thing to go, oldest first,
	// and always leaving the newest message intact.
	for i := 0; i < len(ctx.Messages)-1; i++ {
		if ctx.Messages[i].Content == "" && ctx.Messages[i].Metadata == "" {
			continue
		}
		ctx.Messages[i].Content = ""
		ctx.Messages[i].Metadata = ""
		ctx.Truncated = true
		encoded, err = json.Marshal(ctx)
		if err != nil {
			return nil, err
		}
		if len(encoded) <= MaxContextBytes {
			return encoded, nil
		}
	}
	return encoded, nil
}
