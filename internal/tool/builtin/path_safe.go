package builtin

import (
	"fmt"
	"os"
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
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	root = filepath.Clean(root)
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	realRoot = filepath.Clean(realRoot)

	cleaned := filepath.Clean(rel)
	if cleaned == "." {
		return realRoot, nil
	}
	if strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("path escapes workspace")
	}
	full := filepath.Join(realRoot, cleaned)
	r, err := filepath.Rel(realRoot, full)
	if err != nil || strings.HasPrefix(r, "..") {
		return "", fmt.Errorf("path escapes workspace")
	}
	return resolveWritablePath(realRoot, full)
}

func resolveWritablePath(realRoot, full string) (string, error) {
	if realFull, err := filepath.EvalSymlinks(full); err == nil {
		realFull = filepath.Clean(realFull)
		if !isPathInWorkspace(realRoot, realFull) {
			return "", fmt.Errorf("path escapes workspace")
		}
		return realFull, nil
	}

	ancestor := filepath.Clean(full)
	var missing []string
	for {
		if info, err := os.Stat(ancestor); err == nil {
			if !info.IsDir() {
				return "", fmt.Errorf("path parent is not a directory")
			}
			realAncestor, err := filepath.EvalSymlinks(ancestor)
			if err != nil {
				return "", fmt.Errorf("resolve path parent: %w", err)
			}
			realAncestor = filepath.Clean(realAncestor)
			if !isPathInWorkspace(realRoot, realAncestor) {
				return "", fmt.Errorf("path escapes workspace")
			}
			for i := len(missing) - 1; i >= 0; i-- {
				realAncestor = filepath.Join(realAncestor, missing[i])
			}
			return realAncestor, nil
		}

		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", fmt.Errorf("path parent does not exist")
		}
		missing = append(missing, filepath.Base(ancestor))
		ancestor = parent
	}
}
