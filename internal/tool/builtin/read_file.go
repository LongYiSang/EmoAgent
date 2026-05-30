package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"unicode/utf8"

	"github.com/longyisang/emoagent/internal/tool"
)

const readFileMaxBytes = 1 << 20

// NewReadFileTool constructs the read_file tool for Work.
func NewReadFileTool(projectRoot string) (tool.Spec, tool.Handler) {
	spec := tool.Spec{
		Name:        "read_file",
		Description: "Read a valid UTF-8 text file. With read_scope=workspace, use a workspace-relative path. Absolute paths and path traversal are rejected in workspace scope. With read_scope=all, absolute or workspace-relative paths are allowed. Files larger than 1 MiB are rejected. Returns path, path_scope, content, and size.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{"path":{"type":"string"}},
			"required":["path"],
			"additionalProperties":false
		}`),
		Scope:              tool.ScopeWork,
		Permission:         tool.PermReadOnly,
		ApprovalClassifier: classifySensitiveRead(projectRoot, "read_file"),
	}

	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("read_file: invalid input: %w", err)
		}
		resolved, err := resolveReadPath(ctx, projectRoot, in.Path)
		if err != nil {
			return nil, fmt.Errorf("read_file: %w", err)
		}

		info, err := os.Stat(resolved.FullPath)
		if err != nil {
			return nil, fmt.Errorf("read_file: stat failed: %w", err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("read_file: path is a directory")
		}
		if info.Size() > readFileMaxBytes {
			return nil, fmt.Errorf("read_file: file too large (%d bytes)", info.Size())
		}

		data, err := os.ReadFile(resolved.FullPath)
		if err != nil {
			return nil, fmt.Errorf("read_file: read failed: %w", err)
		}
		if !utf8.Valid(data) {
			return nil, fmt.Errorf("read_file: file is not valid UTF-8")
		}

		return json.Marshal(map[string]any{
			"path":       resolved.DisplayPath,
			"path_scope": pathScopeForResolved(resolved),
			"content":    string(data),
			"size":       len(data),
		})
	}

	return spec, handler
}
