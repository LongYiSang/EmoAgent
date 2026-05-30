package builtin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func classifyWriteFileDestructive(projectRoot string) func(json.RawMessage) (bool, string) {
	return func(input json.RawMessage) (bool, string) {
		var in struct {
			Path       string `json:"path"`
			Content    string `json:"content"`
			CreateDirs bool   `json:"create_dirs"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return false, ""
		}
		if isSensitivePath(in.Path) {
			return true, "write_file targets sensitive path"
		}

		fullPath, err := safeJoin(projectRoot, in.Path)
		if err != nil {
			return false, ""
		}
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			return true, "write_file would overwrite existing file"
		}
		if in.CreateDirs && pathContainsCriticalDir(filepath.Dir(cleanWorkspacePath(in.Path))) {
			return true, "write_file would create or touch critical directory"
		}
		return false, ""
	}
}

func classifyEditFileDestructive(projectRoot string) func(json.RawMessage) (bool, string) {
	return func(input json.RawMessage) (bool, string) {
		var in struct {
			Path       string `json:"path"`
			OldString  string `json:"old_string"`
			NewString  string `json:"new_string"`
			ReplaceAll bool   `json:"replace_all"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return false, ""
		}
		if in.ReplaceAll {
			return true, "edit_file replace_all may modify multiple locations"
		}
		if isSensitivePath(in.Path) {
			return true, "edit_file targets sensitive path"
		}
		if in.OldString == "" {
			return true, "edit_file old_string must not be empty"
		}

		fullPath, err := safeJoin(projectRoot, in.Path)
		if err != nil {
			return false, ""
		}
		data, err := os.ReadFile(fullPath)
		if err != nil || !utf8.Valid(data) || len(data) == 0 {
			return false, ""
		}
		content := string(data)
		count := strings.Count(content, in.OldString)
		if count >= 5 {
			return true, "edit_file changes too many locations"
		}
		if count == 0 {
			return false, ""
		}
		if float64(len(in.OldString))/float64(len(content)) >= 0.25 {
			return true, "edit_file changes large portion of file"
		}
		newContent := strings.Replace(content, in.OldString, in.NewString, 1)
		if absInt(len(newContent)-len(content)) >= 8192 {
			return true, "edit_file changes large portion of file"
		}
		return false, ""
	}
}

func isSensitivePath(path string) bool {
	cleaned := cleanWorkspacePath(path)
	if cleaned == "." || cleaned == "" {
		return false
	}
	lower := strings.ToLower(cleaned)
	if strings.Contains(lower, ".github/workflows") {
		return true
	}
	segments := strings.Split(lower, "/")
	base := segments[len(segments)-1]
	if base == ".env" || strings.HasPrefix(base, ".env.") ||
		strings.HasSuffix(base, ".pem") ||
		strings.HasSuffix(base, ".key") ||
		strings.HasSuffix(base, ".p12") ||
		strings.HasSuffix(base, ".pfx") ||
		base == "id_rsa" ||
		base == "id_ed25519" ||
		base == "authorized_keys" ||
		base == "known_hosts" {
		return true
	}
	for _, segment := range segments {
		if segment == ".git" || segment == ".ssh" || segment == ".aws" || segment == ".kube" {
			return true
		}
		if segment == "credentials" || strings.HasPrefix(segment, "credentials.") ||
			segment == "secrets" || strings.HasPrefix(segment, "secrets.") ||
			segment == "secret" || strings.HasPrefix(segment, "secret.") ||
			segment == "token" || strings.HasPrefix(segment, "token.") {
			return true
		}
	}
	return strings.Contains(base, "credential") ||
		strings.Contains(base, "secret") ||
		strings.Contains(base, "token") ||
		strings.Contains(base, "key")
}

func pathContainsCriticalDir(path string) bool {
	cleaned := strings.ToLower(cleanWorkspacePath(path))
	if cleaned == "." || cleaned == "" {
		return false
	}
	if strings.Contains(cleaned, ".github/workflows") {
		return true
	}
	for _, segment := range strings.Split(cleaned, "/") {
		switch segment {
		case ".git", ".ssh", ".aws", ".kube", "node_modules", "vendor", "dist", "build", "target", "bin":
			return true
		}
	}
	return false
}

func cleanWorkspacePath(path string) string {
	return filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
