package websearch

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/longyisang/emoagent/internal/config"
	"github.com/longyisang/emoagent/internal/rerank"
)

type rerankProvider struct {
	search   Provider
	reranker rerank.Provider
	cfg      config.WebSearchPipelineRerankConfig
}

func NewRerankProvider(search Provider, reranker rerank.Provider, cfg config.WebSearchPipelineRerankConfig) Provider {
	return &rerankProvider{search: search, reranker: reranker, cfg: cfg}
}

func (p *rerankProvider) Name() string {
	if p.search == nil {
		return "rerank"
	}
	return p.search.Name()
}

func (p *rerankProvider) Search(ctx context.Context, query string, opts Options) (*Response, error) {
	resp, err := p.search.Search(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	if p.reranker == nil || !p.cfg.Enabled || len(resp.Results) == 0 {
		fillFinalScores(resp.Results)
		return resp, nil
	}

	docs, docIndexes := rerankDocuments(resp.Results, p.cfg)
	if len(docs) == 0 {
		fillFinalScores(resp.Results)
		return resp, nil
	}

	providerName := p.reranker.Name()
	rr, err := p.reranker.Rerank(ctx, rerank.Request{
		Query:     query,
		Model:     p.cfg.Model,
		Documents: docs,
		TopK:      p.cfg.TopK,
	})
	if err != nil {
		resp.Warnings = append(resp.Warnings, rerankFallbackWarning(p.reranker.Name(), p.cfg.Fallback))
		if strings.EqualFold(p.cfg.Fallback, "heuristic") {
			heuristic := rerank.NewHeuristicProvider()
			rr, err = heuristic.Rerank(ctx, rerank.Request{
				Query:     query,
				Model:     "heuristic",
				Documents: docs,
				TopK:      p.cfg.TopK,
			})
			providerName = heuristic.Name()
		}
		if err != nil || len(rr.Results) == 0 {
			fillFinalScores(resp.Results)
			return resp, nil
		}
	}

	usage := ensureSearchUsage(resp)
	if rr.Usage.Documents > 0 {
		usage.RerankDocuments += rr.Usage.Documents
	} else {
		usage.RerankDocuments += len(docs)
	}
	applyRerankResults(resp, rr.Results, docIndexes, providerName)
	return resp, nil
}

func rerankDocuments(results []Result, cfg config.WebSearchPipelineRerankConfig) ([]rerank.Document, map[int]int) {
	limit := cfg.InputTopN
	if limit <= 0 || limit > len(results) {
		limit = len(results)
	}
	docs := make([]rerank.Document, 0, limit)
	docIndexes := make(map[int]int, limit)
	for i := 0; i < limit; i++ {
		text := resultDocumentText(results[i], cfg.MaxDocChars)
		if strings.TrimSpace(text) == "" {
			continue
		}
		docIndex := len(docs)
		docs = append(docs, rerank.Document{
			ID:    strconv.Itoa(i),
			Index: docIndex,
			Text:  text,
			Metadata: map[string]string{
				"title": results[i].Title,
				"url":   results[i].URL,
			},
		})
		docIndexes[docIndex] = i
	}
	return docs, docIndexes
}

func resultDocumentText(result Result, maxChars int) string {
	var b strings.Builder
	appendPart := func(label, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(label)
		b.WriteString(": ")
		b.WriteString(value)
	}
	appendPart("title", result.Title)
	appendPart("url", result.URL)
	appendPart("snippet", result.Snippet)
	for _, chunk := range result.Evidence {
		appendPart("evidence", chunk.Text)
	}
	return truncateString(b.String(), maxChars)
}

func applyRerankResults(resp *Response, ranked []rerank.Result, docIndexes map[int]int, providerName string) {
	seen := map[int]struct{}{}
	ordered := make([]Result, 0, len(resp.Results))
	for _, item := range ranked {
		resultIndex, ok := docIndexes[item.Index]
		if !ok {
			continue
		}
		result := resp.Results[resultIndex]
		result.RerankScore = item.Score
		result.TrustScore = trustScore(result)
		result.FinalScore = fuseScore(result.RerankScore, result.Score, result.TrustScore)
		result.Reasons = append(result.Reasons, fmt.Sprintf("rerank: %s", providerName))
		ordered = append(ordered, result)
		seen[resultIndex] = struct{}{}
	}
	for i, result := range resp.Results {
		if _, ok := seen[i]; ok {
			continue
		}
		result.TrustScore = trustScore(result)
		result.FinalScore = fuseScore(result.RerankScore, result.Score, result.TrustScore)
		if result.FinalScore > 0 && len(result.Reasons) == 0 {
			result.Reasons = append(result.Reasons, "score: original")
		}
		ordered = append(ordered, result)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].FinalScore > ordered[j].FinalScore
	})
	resp.Results = ordered
}

func fillFinalScores(results []Result) {
	for i := range results {
		results[i].TrustScore = trustScore(results[i])
		results[i].FinalScore = fuseScore(results[i].RerankScore, results[i].Score, results[i].TrustScore)
		if results[i].FinalScore > 0 && len(results[i].Reasons) == 0 {
			results[i].Reasons = append(results[i].Reasons, "score: original")
		}
	}
}

func rerankFallbackWarning(provider, fallback string) string {
	if fallback == "" || strings.EqualFold(fallback, "disabled") {
		return "rerank fallback: provider unavailable"
	}
	return "rerank fallback: " + fallback + " used after " + provider + " unavailable"
}

func truncateString(text string, maxChars int) string {
	if maxChars <= 0 || utf8.RuneCountInString(text) <= maxChars {
		return text
	}
	runes := []rune(text)
	return string(runes[:maxChars])
}
