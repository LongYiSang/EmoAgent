package websearch

import (
	"strconv"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/config"
)

type Planner struct {
	cfg config.WebSearchPipelineConfig
}

func NewPlanner(cfg ...config.WebSearchPipelineConfig) *Planner {
	p := &Planner{}
	if len(cfg) > 0 {
		p.cfg = cfg[0]
	}
	p.cfg.Search = defaultSearchConfig(p.cfg.Search)
	return p
}

func (p *Planner) Plan(query string, opts Options) Options {
	planned := opts
	profile := strings.TrimSpace(planned.Profile)
	if profile == "" {
		profile = p.cfg.Search.DefaultProfile
	}
	if profile == "" {
		profile = "auto"
	}
	planned.Profile = profile

	switch profile {
	case "fast":
		planned.SearchDepth = p.cfg.Search.FastDepth
	case "news":
		planned.SearchDepth = p.cfg.Search.DefaultDepth
		planned.Topic = "news"
	case "official_docs", "deep":
		planned.SearchDepth = p.cfg.Search.DefaultDepth
	case "auto":
		planned.SearchDepth = p.cfg.Search.DefaultDepth
		if looksLikeNewsQuery(query) {
			planned.Topic = "news"
		}
	default:
		if planned.SearchDepth == "" {
			planned.SearchDepth = p.cfg.Search.DefaultDepth
		}
	}
	if planned.SearchDepth == "" {
		planned.SearchDepth = "advanced"
	}
	return planned
}

func defaultSearchConfig(cfg config.WebSearchPipelineSearch) config.WebSearchPipelineSearch {
	if cfg.DefaultProfile == "" {
		cfg.DefaultProfile = "auto"
	}
	if cfg.DefaultDepth == "" {
		cfg.DefaultDepth = "advanced"
	}
	if cfg.FastDepth == "" {
		cfg.FastDepth = "basic"
	}
	if cfg.MaxSubqueries == 0 {
		cfg.MaxSubqueries = 1
	}
	if cfg.CandidateCap == 0 {
		cfg.CandidateCap = 10
	}
	if cfg.PerQueryMaxResults == 0 {
		cfg.PerQueryMaxResults = 5
	}
	return cfg
}

func looksLikeNewsQuery(query string) bool {
	q := strings.ToLower(query)
	if strings.Contains(q, "最新") || strings.Contains(q, "今天") || strings.Contains(q, "today") || strings.Contains(q, "latest") || strings.Contains(q, "changelog") || strings.Contains(q, "release") {
		return true
	}
	year := time.Now().Year()
	return strings.Contains(q, strconv.Itoa(year)) || strings.Contains(q, strconv.Itoa(year+1))
}
