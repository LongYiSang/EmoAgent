package builtin

import (
	"fmt"
	"path/filepath"
	"strings"
)

// safeJoin resolves a relative path against projectRoot and returns the
// absolute path. It rejects empty strings, absolute paths, and any path that
// would escape projectRoot (e.g. containing "..").
func safeJoin(projectRoot, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	cleaned := filepath.Clean(rel)
	if cleaned == "." {
		return projectRoot, nil
	}
	if strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("path escapes workspace")
	}
	full := filepath.Join(projectRoot, cleaned)
	r, err := filepath.Rel(projectRoot, full)
	if err != nil || strings.HasPrefix(r, "..") {
		return "", fmt.Errorf("path escapes workspace")
	}
	return full, nil
}
