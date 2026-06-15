package rerank

import (
	"context"
	"sort"
	"strings"
	"unicode"
)

type heuristicProvider struct{}

func NewHeuristicProvider() Provider {
	return &heuristicProvider{}
}

func (p *heuristicProvider) Name() string { return "heuristic" }

func (p *heuristicProvider) Rerank(_ context.Context, req Request) (Response, error) {
	queryTerms := terms(req.Query)
	results := make([]Result, len(req.Documents))
	for i, doc := range req.Documents {
		results[i] = Result{
			ID:    doc.ID,
			Index: doc.Index,
			Score: overlapScore(queryTerms, terms(doc.Text)),
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if req.TopK > 0 && req.TopK < len(results) {
		results = results[:req.TopK]
	}
	return Response{
		Results: results,
		Usage:   Usage{Documents: len(req.Documents)},
	}, nil
}

func terms(text string) map[string]int {
	out := map[string]int{}
	for _, field := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if field == "" {
			continue
		}
		out[field]++
	}
	return out
}

func overlapScore(queryTerms, docTerms map[string]int) float64 {
	if len(queryTerms) == 0 || len(docTerms) == 0 {
		return 0
	}
	var matches int
	for term := range queryTerms {
		if docTerms[term] > 0 {
			matches++
		}
	}
	return float64(matches) / float64(len(queryTerms))
}
