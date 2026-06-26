package agentaffect

import (
	"context"
	"sort"
	"strings"
	"unicode/utf8"
)

func (r *Runtime) retrieveAffectiveEpisodes(ctx context.Context, mood MoodSnapshot, batch AffectEventBatch) []AffectEpisodeSummary {
	if !r.cfg.Context.AffectiveEpisodeEnabled || r.store == nil {
		return nil
	}
	limit := r.cfg.Context.AffectiveEpisodeCandidateLimit
	if limit <= 0 {
		limit = 100
	}
	candidates, err := r.store.ListAffectiveEpisodeCandidates(ctx, AffectEpisodeQuery{
		PersonaID:      mood.PersonaID,
		MoodOwnerScope: mood.MoodOwnerScope,
		MoodOwnerID:    mood.MoodOwnerID,
		Limit:          limit,
	})
	if err != nil {
		if r.logger != nil {
			r.logger.Warn("agent affect episode retrieval failed", "persona_id", mood.PersonaID, "mood_owner_scope", mood.MoodOwnerScope, "error", err)
		}
		return nil
	}
	active := map[string]struct{}{}
	for _, cause := range mood.CauseStack {
		if cause.Kind != "" {
			active[cause.Kind] = struct{}{}
		}
	}
	query := eventBatchText(batch)
	topK := r.cfg.Context.AffectiveEpisodeTopK
	if topK <= 0 {
		topK = 2
	}
	minScore := r.cfg.Context.AffectiveEpisodeMinScore
	if minScore <= 0 {
		minScore = 0.35
	}
	maxChars := r.cfg.Context.AffectiveEpisodeMaxChars
	if maxChars <= 0 {
		maxChars = 160
	}
	scored := make([]AffectEpisodeSummary, 0, len(candidates))
	for _, item := range candidates {
		if _, ok := active[item.CauseCode]; ok && item.CauseCode != "" {
			continue
		}
		text := item.Summary + " " + strings.Join(item.Tags, " ")
		similarity := jaccardBigrams(query, text)
		recency := recencyScore(item.AgeSeconds)
		item.Score = 0.55*similarity + 0.30*clamp01(item.Significance) + 0.15*recency
		if item.Score < minScore {
			continue
		}
		item.Summary = compactText(item.Summary, maxChars)
		scored = append(scored, item)
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return scored[i].Significance > scored[j].Significance
	})
	if len(scored) > topK {
		scored = scored[:topK]
	}
	return scored
}

func eventBatchText(batch AffectEventBatch) string {
	var b strings.Builder
	for _, turn := range batch.Turns {
		if turn.User != "" {
			b.WriteString(turn.User)
			b.WriteByte(' ')
		}
		if turn.Assistant != "" {
			b.WriteString(turn.Assistant)
			b.WriteByte(' ')
		}
	}
	return compactWhitespace(b.String())
}

func jaccardBigrams(a string, b string) float64 {
	left := bigramSet(a)
	right := bigramSet(b)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	intersection := 0
	for item := range left {
		if right[item] {
			intersection++
		}
	}
	union := len(left) + len(right) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func bigramSet(value string) map[string]bool {
	value = strings.ToLower(compactWhitespace(value))
	runes := []rune(value)
	out := map[string]bool{}
	for i := 0; i < len(runes); i++ {
		if runes[i] == ' ' {
			continue
		}
		if i+1 < len(runes) && runes[i+1] != ' ' {
			out[string(runes[i:i+2])] = true
		} else if utf8.RuneLen(runes[i]) > 0 {
			out[string(runes[i])] = true
		}
	}
	return out
}

func recencyScore(ageSeconds int64) float64 {
	if ageSeconds <= 0 {
		return 1
	}
	const week = int64(7 * 24 * 3600)
	if ageSeconds >= week {
		return 0
	}
	return float64(week-ageSeconds) / float64(week)
}
