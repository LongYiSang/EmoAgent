package websearch

func fuseScore(rerankScore, originalScore, trustScore float64) float64 {
	if rerankScore <= 0 {
		if originalScore > 0 {
			return originalScore
		}
		return trustScore
	}
	return rerankScore*0.90 + originalScore*0.05 + trustScore*0.05
}

func trustScore(result Result) float64 {
	score := 0.35
	if result.URL != "" {
		score += 0.20
	}
	if result.Title != "" {
		score += 0.10
	}
	if result.Snippet != "" {
		score += 0.15
	}
	if len(result.Evidence) > 0 {
		score += 0.20
	}
	if score > 1 {
		return 1
	}
	return score
}
