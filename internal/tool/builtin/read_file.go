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

const readFileMaxBytes = 1 << 20

// NewReadFileTool constructs the read_file tool for Work.
func NewReadFileTool(projectRoot string) (tool.Spec, tool.Handler) {
	spec := tool.Spec{
		Name:        "read_file",
		Description: "Read a UTF-8 text file from the workspace using a relative path.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{"path":{"type":"string"}},
			"required":["path"],
			"additionalProperties":false
		}`),
		Scope:      tool.ScopeWork,
		Permission: tool.PermReadOnly,
	}

	handler := func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("read_file: invalid input: %w", err)
		}
		if in.Path == "" {
			return nil, fmt.Errorf("read_file: path is required")
		}
		if filepath.IsAbs(in.Path) {
			return nil, fmt.Errorf("read_file: absolute paths are not allowed")
		}

		cleaned := filepath.Clean(in.Path)
		if cleaned == "." || strings.HasPrefix(cleaned, "..") {
			return nil, fmt.Errorf("read_file: path escapes workspace")
		}

		fullPath := filepath.Join(projectRoot, cleaned)
		rel, err := filepath.Rel(projectRoot, fullPath)
		if err != nil || strings.HasPrefix(rel, "..") {
			return nil, fmt.Errorf("read_file: path escapes workspace")
		}

		info, err := os.Stat(fullPath)
		if err != nil {
			return nil, fmt.Errorf("read_file: stat failed: %w", err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("read_file: path is a directory")
		}
		if info.Size() > readFileMaxBytes {
			return nil, fmt.Errorf("read_file: file too large (%d bytes)", info.Size())
		}

		data, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("read_file: read failed: %w", err)
		}
		if !utf8.Valid(data) {
			return nil, fmt.Errorf("read_file: file is not valid UTF-8")
		}

		return json.Marshal(map[string]any{
			"path":    filepath.ToSlash(cleaned),
			"content": string(data),
			"size":    len(data),
		})
	}

	return spec, handler
}
