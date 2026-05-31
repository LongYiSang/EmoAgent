package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/longyisang/emoagent/internal/tool"
)

// NewEditFileTool constructs the edit_file tool for Work.
func NewEditFileTool(projectRoot string) (tool.Spec, tool.Handler) {
	spec := tool.Spec{
		Name:        "edit_file",
		Description: "Replace old_string with new_string in a valid UTF-8 workspace file using a workspace-relative path. Absolute paths and path traversal are rejected. With replace_all=false (default), old_string must appear exactly once. Files larger than 1 MiB are rejected. Returns the number of replacements made.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"path":{"type":"string"},
				"old_string":{"type":"string"},
				"new_string":{"type":"string"},
				"replace_all":{"type":"boolean"}
			},
			"required":["path","old_string","new_string"],
			"additionalProperties":false
		}`),
		Scope:                 tool.ScopeWork,
		Permission:            tool.PermWorkspaceWrite,
		DestructiveClassifier: classifyEditFileDestructive(projectRoot),
	}

	handler := func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Path       string `json:"path"`
			OldString  string `json:"old_string"`
			NewString  string `json:"new_string"`
			ReplaceAll bool   `json:"replace_all"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("edit_file: invalid input: %w", err)
		}
		if in.Path == "" {
			return nil, fmt.Errorf("edit_file: path is required")
		}
		if in.OldString == "" {
			return nil, fmt.Errorf("edit_file: old_string must not be empty")
		}
		if in.OldString == in.NewString {
			return nil, fmt.Errorf("edit_file: old_string and new_string are identical")
		}

		fullPath, err := safeJoin(projectRoot, in.Path)
		if err != nil {
			return nil, fmt.Errorf("edit_file: %w", err)
		}

		info, err := os.Stat(fullPath)
		if err != nil {
			return nil, fmt.Errorf("edit_file: stat failed: %w", err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("edit_file: path is a directory")
		}
		if info.Size() > readFileMaxBytes {
			return nil, fmt.Errorf("edit_file: file too large (%d bytes)", info.Size())
		}

		data, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("edit_file: read failed: %w", err)
		}
		if !utf8.Valid(data) {
			return nil, fmt.Errorf("edit_file: file is not valid UTF-8")
		}

		content := string(data)
		count := strings.Count(content, in.OldString)
		if count == 0 {
			return nil, fmt.Errorf("edit_file: old_string not found in file")
		}
		if !in.ReplaceAll && count > 1 {
			return nil, fmt.Errorf("edit_file: old_string appears %d times; set replace_all=true or provide a unique string", count)
		}

		var newContent string
		var replacements int
		if in.ReplaceAll {
			newContent = strings.ReplaceAll(content, in.OldString, in.NewString)
			replacements = count
		} else {
			newContent = strings.Replace(content, in.OldString, in.NewString, 1)
			replacements = 1
		}

		if err := atomicWriteString(fullPath, newContent, ".edit_file_*"); err != nil {
			return nil, fmt.Errorf("edit_file: %w", err)
		}

		return json.Marshal(map[string]any{
			"path":          filepath.ToSlash(in.Path),
			"replacements":  replacements,
			"bytes_written": len(newContent),
		})
	}

	return spec, handler
}
