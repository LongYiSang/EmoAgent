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
		Description: "Write content to a file in the workspace. Creates the file if it does not exist; overwrites if it does. Use create_dirs to create missing parent directories.",
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
		Scope:      tool.ScopeWork,
		Permission: tool.PermWorkspaceWrite,
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

		// Atomic write: temp file in same directory → rename.
		tmp, err := os.CreateTemp(parent, ".write_file_*")
		if err != nil {
			return nil, fmt.Errorf("write_file: create temp: %w", err)
		}
		tmpName := tmp.Name()
		defer func() { _ = os.Remove(tmpName) }()

		if _, err := tmp.WriteString(in.Content); err != nil {
			_ = tmp.Close()
			return nil, fmt.Errorf("write_file: write temp: %w", err)
		}
		if err := tmp.Close(); err != nil {
			return nil, fmt.Errorf("write_file: close temp: %w", err)
		}
		if err := os.Rename(tmpName, fullPath); err != nil {
			return nil, fmt.Errorf("write_file: rename: %w", err)
		}

		return json.Marshal(map[string]any{
			"path":          filepath.ToSlash(in.Path),
			"bytes_written": len(in.Content),
			"existed":       existed,
		})
	}

	return spec, handler
}
