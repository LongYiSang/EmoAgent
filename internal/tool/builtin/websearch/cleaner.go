package websearch

import (
	"net/url"
	"strings"
)

func CleanResults(results []Result) []Result {
	cleaned := make([]Result, 0, len(results))
	seen := map[string]struct{}{}
	for _, result := range results {
		result.URL = normalizeResultURL(result.URL)
		key := result.URL
		if key == "" {
			key = strings.TrimSpace(result.Title) + "\x00" + strings.TrimSpace(result.Snippet)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, result)
	}
	return cleaned
}

func normalizeResultURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	values := parsed.Query()
	for key := range values {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") || lower == "ref" || lower == "fbclid" || lower == "gclid" {
			values.Del(key)
		}
	}
	parsed.RawQuery = values.Encode()
	return parsed.String()
}
