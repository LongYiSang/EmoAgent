package agentaffect

import (
	"math"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

func applyStateDecay(cfg config.AgentAffectConfig, stored MoodSnapshot, baseline MoodVector, now time.Time) MoodSnapshot {
	if !cfg.State.DecayEnabled || cfg.State.DecayHalfLifeSeconds <= 0 || stored.UpdatedAt.IsZero() || !now.After(stored.UpdatedAt) {
		return stored
	}
	factor := math.Pow(0.5, now.Sub(stored.UpdatedAt).Seconds()/float64(cfg.State.DecayHalfLifeSeconds))
	if math.IsNaN(factor) || math.IsInf(factor, 0) {
		return stored
	}
	stored.Vector = decayVector(stored.Vector, baseline, factor)
	return stored
}

func decayVector(stored MoodVector, baseline MoodVector, factor float64) MoodVector {
	return MoodVector{
		Valence:     baseline.Valence + (stored.Valence-baseline.Valence)*factor,
		Arousal:     baseline.Arousal + (stored.Arousal-baseline.Arousal)*factor,
		Dominance:   baseline.Dominance + (stored.Dominance-baseline.Dominance)*factor,
		Energy:      baseline.Energy + (stored.Energy-baseline.Energy)*factor,
		Warmth:      baseline.Warmth + (stored.Warmth-baseline.Warmth)*factor,
		Concern:     baseline.Concern + (stored.Concern-baseline.Concern)*factor,
		Curiosity:   baseline.Curiosity + (stored.Curiosity-baseline.Curiosity)*factor,
		Playfulness: baseline.Playfulness + (stored.Playfulness-baseline.Playfulness)*factor,
		Attachment:  baseline.Attachment + (stored.Attachment-baseline.Attachment)*factor,
		Frustration: baseline.Frustration + (stored.Frustration-baseline.Frustration)*factor,
		Uncertainty: baseline.Uncertainty + (stored.Uncertainty-baseline.Uncertainty)*factor,
	}
}
