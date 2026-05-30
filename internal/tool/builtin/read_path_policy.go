package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/longyisang/emoagent/internal/tool"
)

type resolvedReadPath struct {
	InputPath         string
	FullPath          string
	DisplayPath       string
	WorkspaceRelative string
	InWorkspace       bool
	External          bool
	Sensitive         bool
	SensitiveReason   string
}

func resolveReadPath(ctx context.Context, projectRoot string, rawPath string) (resolvedReadPath, error) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return resolvedReadPath{}, fmt.Errorf("path is required")
	}

	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return resolvedReadPath{}, fmt.Errorf("resolve workspace root: %w", err)
	}
	root = filepath.Clean(root)
	realRoot := evalPathIfExists(root)

	scope := tool.ReadScopeFromContext(ctx)
	if scope == tool.ReadScopeAll {
		return resolveReadPathAll(realRoot, rawPath)
	}
	return resolveReadPathWorkspace(realRoot, rawPath)
}

func resolveReadPathWorkspace(realRoot string, rawPath string) (resolvedReadPath, error) {
	if filepath.IsAbs(rawPath) {
		return resolvedReadPath{}, fmt.Errorf("absolute paths are not allowed")
	}
	cleaned := filepath.Clean(rawPath)
	if pathEscapes(cleaned) {
		return resolvedReadPath{}, fmt.Errorf("path escapes workspace")
	}
	fullPath := filepath.Join(realRoot, cleaned)
	realFull := evalPathIfExists(fullPath)
	if !isPathInWorkspace(realRoot, realFull) {
		return resolvedReadPath{}, fmt.Errorf("path escapes workspace")
	}
	rel := workspaceRel(realRoot, realFull)
	if rel == "" {
		rel = filepath.ToSlash(cleaned)
	}
	out := resolvedReadPath{
		InputPath:         rawPath,
		FullPath:          realFull,
		DisplayPath:       rel,
		WorkspaceRelative: rel,
		InWorkspace:       true,
	}
	applySensitivePath(&out)
	return out, nil
}

func resolveReadPathAll(realRoot string, rawPath string) (resolvedReadPath, error) {
	var fullPath string
	if filepath.IsAbs(rawPath) {
		fullPath = filepath.Clean(rawPath)
	} else {
		fullPath = filepath.Clean(filepath.Join(realRoot, rawPath))
	}
	realFull := evalPathIfExists(fullPath)
	inWorkspace := isPathInWorkspace(realRoot, realFull)
	out := resolvedReadPath{
		InputPath:   rawPath,
		FullPath:    realFull,
		InWorkspace: inWorkspace,
		External:    !inWorkspace,
	}
	if inWorkspace {
		out.WorkspaceRelative = workspaceRel(realRoot, realFull)
		out.DisplayPath = out.WorkspaceRelative
	} else {
		out.DisplayPath = filepath.ToSlash(filepath.Clean(realFull))
	}
	applySensitivePath(&out)
	return out, nil
}

func evalPathIfExists(path string) string {
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(realPath)
}

func pathEscapes(cleaned string) bool {
	slash := filepath.ToSlash(cleaned)
	return slash == ".." || strings.HasPrefix(slash, "../")
}

func isPathInWorkspace(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!filepath.IsAbs(rel) && !pathEscapes(rel))
}

func workspaceRel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return "."
	}
	return filepath.ToSlash(rel)
}

func pathScopeForResolved(resolved resolvedReadPath) string {
	if resolved.External {
		return "external"
	}
	return "workspace"
}

func applySensitivePath(resolved *resolvedReadPath) {
	if resolved == nil {
		return
	}
	for _, candidate := range []string{
		resolved.InputPath,
		resolved.WorkspaceRelative,
		resolved.DisplayPath,
		resolved.FullPath,
	} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if reason := sensitivePathReason(candidate); reason != "" {
			resolved.Sensitive = true
			resolved.SensitiveReason = reason
			return
		}
	}
}

func sensitivePathReason(path string) string {
	slashPath := filepath.ToSlash(filepath.Clean(path))
	lowerPath := strings.ToLower(slashPath)
	for _, segment := range strings.Split(lowerPath, "/") {
		if sensitiveDirSegments[segment] {
			return "sensitive_path"
		}
	}
	base := strings.ToLower(filepath.Base(path))
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return "sensitive_path"
	}
	if strings.HasSuffix(base, ".pem") ||
		strings.HasSuffix(base, ".key") ||
		strings.HasSuffix(base, ".p12") ||
		strings.HasSuffix(base, ".pfx") {
		return "sensitive_path"
	}
	if sensitiveFileNames[base] {
		return "sensitive_path"
	}
	for _, prefix := range []string{"credentials", "secrets", "secret", "token"} {
		if strings.HasPrefix(base, prefix) {
			return "sensitive_path"
		}
	}
	if runtime.GOOS != "windows" {
		if lowerPath == "/proc" || strings.HasPrefix(lowerPath, "/proc/") ||
			lowerPath == "/sys" || strings.HasPrefix(lowerPath, "/sys/") ||
			lowerPath == "/dev" || strings.HasPrefix(lowerPath, "/dev/") ||
			lowerPath == "/etc/ssh" || strings.HasPrefix(lowerPath, "/etc/ssh/") ||
			lowerPath == "/etc/sudoers" {
			return "sensitive_path"
		}
	}
	if runtime.GOOS == "windows" {
		if len(lowerPath) >= len("c:/windows") && (lowerPath == "c:/windows" || strings.HasPrefix(lowerPath, "c:/windows/")) {
			return "sensitive_path"
		}
		if strings.Contains(lowerPath, "/appdata/roaming/microsoft/credentials") {
			return "sensitive_path"
		}
	}
	return ""
}

var sensitiveDirSegments = map[string]bool{
	".ssh":            true,
	".aws":            true,
	".gcloud":         true,
	".azure":          true,
	".kube":           true,
	".gnupg":          true,
	".password-store": true,
	".keychain":       true,
	".git":            true,
}

var sensitiveFileNames = map[string]bool{
	"id_rsa":          true,
	"id_ed25519":      true,
	"authorized_keys": true,
	"known_hosts":     true,
	"credentials":     true,
	"secrets":         true,
	"secret":          true,
	"token":           true,
	".netrc":          true,
	".npmrc":          true,
	".pypirc":         true,
}

func classifySensitiveRead(projectRoot, toolName string) tool.ApprovalClassifier {
	return func(ctx context.Context, input json.RawMessage) (tool.ApprovalRequirement, bool) {
		var payload struct {
			Path      string `json:"path"`
			Recursive bool   `json:"recursive"`
		}
		if err := json.Unmarshal(input, &payload); err != nil {
			return tool.ApprovalRequirement{}, false
		}
		path := payload.Path
		if strings.TrimSpace(path) == "" && toolName == "list_dir" {
			path = "."
		}
		resolved, err := resolveReadPath(ctx, projectRoot, path)
		if err != nil {
			return tool.ApprovalRequirement{}, false
		}
		if resolved.Sensitive {
			return tool.ApprovalRequirement{Kind: tool.ApprovalKindSensitiveRead, Reason: resolved.SensitiveReason}, true
		}
		if toolName == "list_dir" && payload.Recursive && resolved.External {
			return tool.ApprovalRequirement{Kind: tool.ApprovalKindSensitiveRead, Reason: "external_recursive_list"}, true
		}
		return tool.ApprovalRequirement{}, false
	}
}
