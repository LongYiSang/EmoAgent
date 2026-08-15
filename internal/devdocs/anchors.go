// Package devdocs guards docs/dev/ against silent rot: when a referenced
// repo path is renamed or removed, the anchor test fails and names the doc.
package devdocs

import (
	"regexp"
	"strings"
)

// repoRoots are the only prefixes treated as repo path anchors. Anything else
// inside backticks (URLs, API routes, identifiers) is ignored.
//
// data/ is deliberately absent: it is gitignored runtime state, so its paths
// exist only on a machine that has produced them and cannot be verified here.
var repoRoots = []string{
	"cmd/", "internal/", "web/", "docs/", "sdk/",
	"config/", "personas/", "scripts/",
}

var backtickPattern = regexp.MustCompile("`([^`\n]+)`")

// lineSuffixPattern strips a trailing :123 or :123-145 from an anchor.
var lineSuffixPattern = regexp.MustCompile(`:\d+(-\d+)?$`)

// ExtractAnchors returns the repo-relative paths referenced inside backticks.
func ExtractAnchors(content string) []string {
	var anchors []string
	seen := make(map[string]bool)

	for _, m := range backtickPattern.FindAllStringSubmatch(content, -1) {
		token := strings.TrimSpace(m[1])
		if token == "" || strings.ContainsAny(token, " {}*<>") {
			continue
		}
		if strings.Contains(token, "://") {
			continue
		}
		if !hasRepoRoot(token) {
			continue
		}
		token = lineSuffixPattern.ReplaceAllString(token, "")
		token = strings.TrimSuffix(token, "/")
		if !seen[token] {
			seen[token] = true
			anchors = append(anchors, token)
		}
	}
	return anchors
}

func hasRepoRoot(token string) bool {
	for _, root := range repoRoots {
		if strings.HasPrefix(token, root) {
			return true
		}
	}
	return false
}
