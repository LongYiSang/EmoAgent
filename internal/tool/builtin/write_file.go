package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/longyisang/emoagent/internal/tool"
)

const writeFileMaxBytes = 1 << 20 // 1 MiB

// NewWriteFileTool constructs the write_file tool for Work.
func NewWriteFileTool(projectRoot string) (tool.Spec, tool.Handler) {
	spec := tool.Spec{
		Name:        "write_file",
		Description: "Write content to a workspace file using a workspace-relative path. Absolute paths and path traversal are rejected. Creates the file if it does not exist; overwrites existing content if it does. Content larger than 1 MiB is rejected. Use create_dirs to create missing parent directories.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"path":{"type":"string"},
				"content":{"type":"string"},
				"create_dirs":{"type":"boolean"}
			},
			"required":["path","content"],
			"additionalProperties":false
		}`),
		Scope:                 tool.ScopeWork,
		Permission:            tool.PermWorkspaceWrite,
		DestructiveClassifier: classifyWriteFileDestructive(projectRoot),
	}

	handler := func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Path       string `json:"path"`
			Content    string `json:"content"`
			CreateDirs bool   `json:"create_dirs"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("write_file: invalid input: %w", err)
		}
		if in.Path == "" {
			return nil, fmt.Errorf("write_file: path is required")
		}
		if len(in.Content) > writeFileMaxBytes {
			return nil, fmt.Errorf("write_file: content too large (%d bytes)", len(in.Content))
		}

		fullPath, err := safeJoin(projectRoot, in.Path)
		if err != nil {
			return nil, fmt.Errorf("write_file: %w", err)
		}

		existed := false
		if _, statErr := os.Stat(fullPath); statErr == nil {
			existed = true
		}

		parent := filepath.Dir(fullPath)
		if in.CreateDirs {
			if err := os.MkdirAll(parent, 0755); err != nil {
				return nil, fmt.Errorf("write_file: create dirs: %w", err)
			}
		}

		if err := atomicWriteString(fullPath, in.Content, ".write_file_*"); err != nil {
			return nil, fmt.Errorf("write_file: %w", err)
		}

		return json.Marshal(map[string]any{
			"path":          filepath.ToSlash(in.Path),
			"bytes_written": len(in.Content),
			"existed":       existed,
		})
	}

	return spec, handler
}
