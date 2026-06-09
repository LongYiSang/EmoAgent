package agentaffect

import (
	"fmt"
	"math"

	"github.com/longyisang/emoagent/internal/config"
)

type ClampOptions struct {
	CommittedBy string
}

type ClampResult struct {
	ClampedDelta   MoodVector
	PredictedState MoodVector
	Notes          []string
}

func ClampMoodDelta(cfg config.AgentAffectConfig, before MoodVector, proposed MoodVector, opts ClampOptions) ClampResult {
	multiplier := 1.0
	if opts.CommittedBy == "plugin" && cfg.Limits.PluginDeltaMultiplier > 0 {
		multiplier = cfg.Limits.PluginDeltaMultiplier
	}
	proposed = multiplyVector(proposed, multiplier)

	var notes []string
	delta := MoodVector{
		Valence:     clampDimension("valence", proposed.Valence, cfg.Limits.PerRequestDelta.Valence, &notes),
		Arousal:     clampDimension("arousal", proposed.Arousal, cfg.Limits.PerRequestDelta.Arousal, &notes),
		Dominance:   clampDimension("dominance", proposed.Dominance, cfg.Limits.PerRequestDelta.Dominance, &notes),
		Energy:      clampDimension("energy", proposed.Energy, cfg.Limits.PerRequestDelta.Energy, &notes),
		Warmth:      clampDimension("warmth", proposed.Warmth, cfg.Limits.PerRequestDelta.Warmth, &notes),
		Concern:     clampDimension("concern", proposed.Concern, cfg.Limits.PerRequestDelta.Concern, &notes),
		Curiosity:   clampDimension("curiosity", proposed.Curiosity, cfg.Limits.PerRequestDelta.Curiosity, &notes),
		Playfulness: clampDimension("playfulness", proposed.Playfulness, cfg.Limits.PerRequestDelta.Playfulness, &notes),
		Attachment:  clampDimension("attachment", proposed.Attachment, cfg.Limits.PerRequestDelta.Attachment, &notes),
		Frustration: clampDimension("frustration", proposed.Frustration, cfg.Limits.PerRequestDelta.Frustration, &notes),
		Uncertainty: clampDimension("uncertainty", proposed.Uncertainty, cfg.Limits.PerRequestDelta.Uncertainty, &notes),
	}

	predicted := addVector(before, delta)
	predicted = clampState(cfg, predicted, &notes)
	return ClampResult{
		ClampedDelta:   roundVector(subVector(predicted, before)),
		PredictedState: roundVector(predicted),
		Notes:          notes,
	}
}

func clampDimension(name string, value, limit float64, notes *[]string) float64 {
	if limit <= 0 {
		if value != 0 {
			*notes = append(*notes, fmt.Sprintf("%s delta %.3f clamped to 0.000", name, value))
		}
		return 0
	}
	if value > limit {
		*notes = append(*notes, fmt.Sprintf("%s delta %.3f clamped to %.3f", name, value, limit))
		return limit
	}
	if value < -limit {
		*notes = append(*notes, fmt.Sprintf("%s delta %.3f clamped to %.3f", name, value, -limit))
		return -limit
	}
	return value
}

func clampState(cfg config.AgentAffectConfig, state MoodVector, notes *[]string) MoodVector {
	state.Valence = clampStateDimension("valence", state.Valence, -1, 1, notes)
	state.Arousal = clampStateDimension("arousal", state.Arousal, 0, 1, notes)
	state.Dominance = clampStateDimension("dominance", state.Dominance, -1, 1, notes)
	state.Energy = clampStateDimension("energy", state.Energy, 0, 1, notes)
	state.Warmth = clampStateDimension("warmth", state.Warmth, 0, 1, notes)
	state.Concern = clampStateDimension("concern", state.Concern, 0, 1, notes)
	state.Curiosity = clampStateDimension("curiosity", state.Curiosity, 0, 1, notes)
	state.Playfulness = clampStateDimension("playfulness", state.Playfulness, 0, 1, notes)
	attachmentMax := cfg.Limits.Absolute.AttachmentMax
	if attachmentMax <= 0 {
		attachmentMax = 1
	}
	frustrationMax := cfg.Limits.Absolute.FrustrationMax
	if frustrationMax <= 0 {
		frustrationMax = 1
	}
	state.Attachment = clampStateDimension("attachment", state.Attachment, 0, attachmentMax, notes)
	state.Frustration = clampStateDimension("frustration", state.Frustration, 0, frustrationMax, notes)
	state.Uncertainty = clampStateDimension("uncertainty", state.Uncertainty, 0, 1, notes)
	return state
}

func clampStateDimension(name string, value, minValue, maxValue float64, notes *[]string) float64 {
	if value < minValue {
		*notes = append(*notes, fmt.Sprintf("%s state %.3f clamped to %.3f", name, value, minValue))
		return minValue
	}
	if value > maxValue {
		*notes = append(*notes, fmt.Sprintf("%s state %.3f clamped to %.3f", name, value, maxValue))
		return maxValue
	}
	return value
}

func addVector(a, b MoodVector) MoodVector {
	return MoodVector{
		Valence:     a.Valence + b.Valence,
		Arousal:     a.Arousal + b.Arousal,
		Dominance:   a.Dominance + b.Dominance,
		Energy:      a.Energy + b.Energy,
		Warmth:      a.Warmth + b.Warmth,
		Concern:     a.Concern + b.Concern,
		Curiosity:   a.Curiosity + b.Curiosity,
		Playfulness: a.Playfulness + b.Playfulness,
		Attachment:  a.Attachment + b.Attachment,
		Frustration: a.Frustration + b.Frustration,
		Uncertainty: a.Uncertainty + b.Uncertainty,
	}
}

func subVector(a, b MoodVector) MoodVector {
	return MoodVector{
		Valence:     a.Valence - b.Valence,
		Arousal:     a.Arousal - b.Arousal,
		Dominance:   a.Dominance - b.Dominance,
		Energy:      a.Energy - b.Energy,
		Warmth:      a.Warmth - b.Warmth,
		Concern:     a.Concern - b.Concern,
		Curiosity:   a.Curiosity - b.Curiosity,
		Playfulness: a.Playfulness - b.Playfulness,
		Attachment:  a.Attachment - b.Attachment,
		Frustration: a.Frustration - b.Frustration,
		Uncertainty: a.Uncertainty - b.Uncertainty,
	}
}

func multiplyVector(v MoodVector, multiplier float64) MoodVector {
	return MoodVector{
		Valence:     v.Valence * multiplier,
		Arousal:     v.Arousal * multiplier,
		Dominance:   v.Dominance * multiplier,
		Energy:      v.Energy * multiplier,
		Warmth:      v.Warmth * multiplier,
		Concern:     v.Concern * multiplier,
		Curiosity:   v.Curiosity * multiplier,
		Playfulness: v.Playfulness * multiplier,
		Attachment:  v.Attachment * multiplier,
		Frustration: v.Frustration * multiplier,
		Uncertainty: v.Uncertainty * multiplier,
	}
}

func roundVector(v MoodVector) MoodVector {
	return MoodVector{
		Valence:     roundFloat(v.Valence),
		Arousal:     roundFloat(v.Arousal),
		Dominance:   roundFloat(v.Dominance),
		Energy:      roundFloat(v.Energy),
		Warmth:      roundFloat(v.Warmth),
		Concern:     roundFloat(v.Concern),
		Curiosity:   roundFloat(v.Curiosity),
		Playfulness: roundFloat(v.Playfulness),
		Attachment:  roundFloat(v.Attachment),
		Frustration: roundFloat(v.Frustration),
		Uncertainty: roundFloat(v.Uncertainty),
	}
}

func roundFloat(v float64) float64 {
	return math.Round(v*1_000_000) / 1_000_000
}
