package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/longyisang/emoagent/internal/tool"
)

const (
	listDirDefaultMax = 200
	listDirHardMax    = 1000
)

type dirEntry struct {
	Name    string    `json:"name"`
	Type    string    `json:"type"` // "file", "dir", "symlink", "other"
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// NewListDirTool constructs the list_dir tool for Work.
func NewListDirTool(projectRoot string) (tool.Spec, tool.Handler) {
	spec := tool.Spec{
		Name:        "list_dir",
		Description: "List files and directories. With read_scope=workspace, use a workspace-relative path; with read_scope=all, absolute or workspace-relative paths are allowed. Use max_entries to keep results focused; output includes truncated when the listing hit the limit. Returns path, path_scope, name, type, size, and modification time.",
		Parameters: json.RawMessage(`{
			"type":"object",
			"properties":{
				"path":{"type":"string"},
				"recursive":{"type":"boolean"},
				"max_entries":{"type":"integer"}
			},
			"required":[],
			"additionalProperties":false
		}`),
		Scope:              tool.ScopeWork,
		Permission:         tool.PermReadOnly,
		ApprovalClassifier: classifySensitiveRead(projectRoot, "list_dir"),
	}

	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Path       string `json:"path"`
			Recursive  bool   `json:"recursive"`
			MaxEntries int    `json:"max_entries"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("list_dir: invalid input: %w", err)
		}

		rel := in.Path
		if rel == "" {
			rel = "."
		}
		resolved, err := resolveReadPath(ctx, projectRoot, rel)
		if err != nil {
			return nil, fmt.Errorf("list_dir: %w", err)
		}

		info, err := os.Lstat(resolved.FullPath)
		if err != nil {
			return nil, fmt.Errorf("list_dir: stat failed: %w", err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("list_dir: path is not a directory")
		}

		limit := in.MaxEntries
		if limit <= 0 {
			limit = listDirDefaultMax
		}
		if limit > listDirHardMax {
			limit = listDirHardMax
		}

		var entries []dirEntry
		truncated := false

		collect := func(_ string, d os.DirEntry) error {
			if len(entries) >= limit {
				truncated = true
				return fmt.Errorf("limit reached")
			}
			fi, statErr := d.Info()
			if statErr != nil {
				return nil // skip unreadable entries
			}
			e := dirEntry{
				Name:    d.Name(),
				ModTime: fi.ModTime().UTC(),
			}
			switch {
			case d.Type()&os.ModeSymlink != 0:
				e.Type = "symlink"
			case d.IsDir():
				e.Type = "dir"
			case d.Type().IsRegular():
				e.Type = "file"
				e.Size = fi.Size()
			default:
				e.Type = "other"
			}
			entries = append(entries, e)
			return nil
		}

		if in.Recursive {
			_ = filepath.WalkDir(resolved.FullPath, func(path string, d os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return nil
				}
				if path == resolved.FullPath {
					return nil // skip root itself
				}
				return collect(path, d)
			})
		} else {
			raw, readErr := os.ReadDir(resolved.FullPath)
			if readErr != nil {
				return nil, fmt.Errorf("list_dir: read failed: %w", readErr)
			}
			for _, d := range raw {
				if err := collect("", d); err != nil {
					break
				}
			}
		}

		return json.Marshal(map[string]any{
			"path":       resolved.DisplayPath,
			"path_scope": pathScopeForResolved(resolved),
			"entries":    entries,
			"truncated":  truncated,
		})
	}

	return spec, handler
}
