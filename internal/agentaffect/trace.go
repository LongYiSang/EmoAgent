package agentaffect

import (
	"math"
	"sort"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

func updateCauseTrace(cfg config.AgentAffectConfig, old []CauseContributor, result LLMEvaluationResult, committedDelta MoodVector, now time.Time) []CauseContributor {
	minWeight := cfg.State.CauseMinWeight
	if minWeight <= 0 {
		minWeight = 0.05
	}
	maxItems := cfg.State.CauseStackMaxItems
	if maxItems <= 0 {
		maxItems = 5
	}
	next := decayCauseTrace(cfg, old, now, minWeight)
	if result.HasAppraisal {
		weight := clamp01(result.Appraisal.EventSignificance * result.Confidence)
		if result.Cause.Code != "" && weight >= minWeight {
			incoming := CauseContributor{
				Kind:       result.Cause.Code,
				Summary:    compactText(defaultString(result.Cause.VisibleSummary, result.Cause.Summary), defaultPositive(cfg.Context.CauseSummaryMaxChars, 120)),
				Weight:     weight,
				Confidence: result.Confidence,
				Delta:      sparseDelta(committedDelta),
				OccurredAt: now,
			}
			next = mergeCause(next, incoming)
		}
	}
	sortCauseTrace(next)
	if len(next) > maxItems {
		next = next[:maxItems]
	}
	return next
}

func decayCauseTrace(cfg config.AgentAffectConfig, old []CauseContributor, now time.Time, minWeight float64) []CauseContributor {
	out := make([]CauseContributor, 0, len(old))
	for _, item := range old {
		if cfg.State.CauseHalfLifeSeconds > 0 && !item.OccurredAt.IsZero() && now.After(item.OccurredAt) {
			factor := math.Pow(0.5, now.Sub(item.OccurredAt).Seconds()/float64(cfg.State.CauseHalfLifeSeconds))
			if !math.IsNaN(factor) && !math.IsInf(factor, 0) {
				item.Weight *= factor
			}
		}
		if item.Weight < minWeight {
			continue
		}
		out = append(out, item)
	}
	return out
}

func mergeCause(items []CauseContributor, incoming CauseContributor) []CauseContributor {
	for i := range items {
		if items[i].Kind != "" && items[i].Kind == incoming.Kind {
			items[i].Summary = incoming.Summary
			items[i].Confidence = incoming.Confidence
			items[i].Delta = incoming.Delta
			items[i].OccurredAt = incoming.OccurredAt
			items[i].Weight = clamp01(items[i].Weight*0.55 + incoming.Weight)
			return items
		}
	}
	return append(items, incoming)
}

func sortCauseTrace(items []CauseContributor) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Weight != items[j].Weight {
			return items[i].Weight > items[j].Weight
		}
		return items[i].OccurredAt.After(items[j].OccurredAt)
	})
}

func sparseDelta(delta MoodVector) map[string]float64 {
	out := map[string]float64{}
	add := func(key string, value float64) {
		if math.Abs(value) >= 0.001 {
			out[key] = round3(value)
		}
	}
	add("valence", delta.Valence)
	add("arousal", delta.Arousal)
	add("dominance", delta.Dominance)
	add("energy", delta.Energy)
	add("warmth", delta.Warmth)
	add("concern", delta.Concern)
	add("curiosity", delta.Curiosity)
	add("playfulness", delta.Playfulness)
	add("attachment", delta.Attachment)
	add("frustration", delta.Frustration)
	add("uncertainty", delta.Uncertainty)
	if len(out) == 0 {
		return nil
	}
	return out
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
