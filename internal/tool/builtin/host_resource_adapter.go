package builtin

import (
	"strings"

	"github.com/longyisang/emoagent/internal/resource"
)

func hostResourceSelector(path string) resource.ResourceSelector {
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, "@") {
		return resource.ResourceSelector{Kind: resource.ResourceSelectorAlias, Path: path}
	}
	return resource.ResourceSelector{Kind: resource.ResourceSelectorPath, Path: path}
}
