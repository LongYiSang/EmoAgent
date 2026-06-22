package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/longyisang/emoagent/internal/resource"
	"github.com/longyisang/emoagent/internal/tool"
)

func NewHostReadTool(broker *resource.Broker) (tool.Spec, tool.Handler) {
	spec := hostToolSpec("host_read", "Read a configured host resource by alias, absolute path inside a configured root, or resource_id. Returns display path, resource_id, root_id, content, and size.", `{"type":"object","properties":{"path":{"type":"string"},"resource_id":{"type":"string"},"max_bytes":{"type":"integer"}},"additionalProperties":false}`)
	return spec, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		if broker == nil {
			return nil, fmt.Errorf("host_read: host resource broker is unavailable")
		}
		var in struct {
			Path       string `json:"path"`
			ResourceID string `json:"resource_id"`
			MaxBytes   int64  `json:"max_bytes"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("host_read: invalid input: %w", err)
		}
		data, ref, err := broker.Read(ctx, selectorFromInput(in.Path, in.ResourceID), resource.ReadOptions{MaxBytes: in.MaxBytes})
		if err != nil {
			return nil, fmt.Errorf("host_read: %w", err)
		}
		if !utf8.Valid(data) {
			return nil, fmt.Errorf("host_read: resource is not valid UTF-8")
		}
		return json.Marshal(map[string]any{
			"path":        ref.DisplayPath,
			"resource_id": ref.ID,
			"root_id":     ref.RootID,
			"content":     string(data),
			"size":        len(data),
		})
	}
}

func NewHostListTool(broker *resource.Broker) (tool.Spec, tool.Handler) {
	spec := hostToolSpec("host_list", "List a configured host directory by alias, absolute path inside a configured root, or resource_id.", `{"type":"object","properties":{"path":{"type":"string"},"resource_id":{"type":"string"},"recursive":{"type":"boolean"},"max_entries":{"type":"integer"}},"additionalProperties":false}`)
	return spec, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		if broker == nil {
			return nil, fmt.Errorf("host_list: host resource broker is unavailable")
		}
		var in struct {
			Path       string `json:"path"`
			ResourceID string `json:"resource_id"`
			Recursive  bool   `json:"recursive"`
			MaxEntries int    `json:"max_entries"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("host_list: invalid input: %w", err)
		}
		entries, ref, truncated, err := broker.List(ctx, selectorFromInput(in.Path, in.ResourceID), resource.ListOptions{Recursive: in.Recursive, MaxEntries: in.MaxEntries})
		if err != nil {
			return nil, fmt.Errorf("host_list: %w", err)
		}
		return json.Marshal(map[string]any{
			"path":        ref.DisplayPath,
			"resource_id": ref.ID,
			"root_id":     ref.RootID,
			"entries":     hostEntries(entries),
			"truncated":   truncated,
		})
	}
}

func NewHostStatTool(broker *resource.Broker) (tool.Spec, tool.Handler) {
	spec := hostToolSpec("host_stat", "Return metadata for a configured host resource.", `{"type":"object","properties":{"path":{"type":"string"},"resource_id":{"type":"string"}},"additionalProperties":false}`)
	return spec, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		if broker == nil {
			return nil, fmt.Errorf("host_stat: host resource broker is unavailable")
		}
		var in struct {
			Path       string `json:"path"`
			ResourceID string `json:"resource_id"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("host_stat: invalid input: %w", err)
		}
		stat, err := broker.Stat(ctx, selectorFromInput(in.Path, in.ResourceID))
		if err != nil {
			return nil, fmt.Errorf("host_stat: %w", err)
		}
		return json.Marshal(map[string]any{
			"path":          stat.Ref.DisplayPath,
			"resource_id":   stat.Ref.ID,
			"root_id":       stat.Ref.RootID,
			"type":          stat.Ref.ResourceType,
			"size":          stat.Size,
			"mode":          stat.Mode,
			"modified_time": stat.ModTime,
		})
	}
}

func NewHostSearchTool(broker *resource.Broker) (tool.Spec, tool.Handler) {
	spec := hostToolSpec("host_search", "Search configured host resource filenames under a configured directory.", `{"type":"object","properties":{"path":{"type":"string"},"resource_id":{"type":"string"},"query":{"type":"string"},"max_results":{"type":"integer"}},"required":["query"],"additionalProperties":false}`)
	return spec, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		if broker == nil {
			return nil, fmt.Errorf("host_search: host resource broker is unavailable")
		}
		var in struct {
			Path       string `json:"path"`
			ResourceID string `json:"resource_id"`
			Query      string `json:"query"`
			MaxResults int    `json:"max_results"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("host_search: invalid input: %w", err)
		}
		entries, ref, truncated, err := broker.Search(ctx, selectorFromInput(in.Path, in.ResourceID), resource.SearchOptions{Query: in.Query, MaxResults: in.MaxResults})
		if err != nil {
			return nil, fmt.Errorf("host_search: %w", err)
		}
		return json.Marshal(map[string]any{
			"path":        ref.DisplayPath,
			"resource_id": ref.ID,
			"root_id":     ref.RootID,
			"results":     hostEntries(entries),
			"truncated":   truncated,
		})
	}
}

func NewHostCopyToWorkspaceTool(broker *resource.Broker, workspaceRoot string) (tool.Spec, tool.Handler) {
	spec := hostToolSpec("host_copy_to_workspace", "Copy a configured host file into the workspace using a workspace-relative target path.", `{"type":"object","properties":{"path":{"type":"string"},"resource_id":{"type":"string"},"target_path":{"type":"string"},"max_bytes":{"type":"integer"}},"required":["target_path"],"additionalProperties":false}`)
	spec.Permission = tool.PermWorkspaceWrite
	return spec, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		if broker == nil {
			return nil, fmt.Errorf("host_copy_to_workspace: host resource broker is unavailable")
		}
		var in struct {
			Path       string `json:"path"`
			ResourceID string `json:"resource_id"`
			TargetPath string `json:"target_path"`
			MaxBytes   int64  `json:"max_bytes"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("host_copy_to_workspace: invalid input: %w", err)
		}
		ref, copiedPath, err := broker.CopyToWorkspace(ctx, selectorFromInput(in.Path, in.ResourceID), workspaceRoot, in.TargetPath, resource.CopyOptions{MaxBytes: in.MaxBytes})
		if err != nil {
			return nil, fmt.Errorf("host_copy_to_workspace: %w", err)
		}
		info, _ := os.Stat(filepath.Join(workspaceRoot, copiedPath))
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		return json.Marshal(map[string]any{
			"source_path":  ref.DisplayPath,
			"resource_id":  ref.ID,
			"target_path":  filepath.ToSlash(copiedPath),
			"bytes_copied": size,
		})
	}
}

func hostToolSpec(name, description, schema string) tool.Spec {
	return tool.Spec{
		Name:        name,
		Description: description,
		Parameters:  json.RawMessage(schema),
		Scope:       tool.ScopeWork,
		Permission:  tool.PermReadOnly,
		Source:      hostFileSource(),
	}
}

func selectorFromInput(path, resourceID string) resource.ResourceSelector {
	if resourceID != "" {
		return resource.ResourceSelector{Kind: resource.ResourceSelectorResourceID, ID: resourceID}
	}
	return hostResourceSelector(path)
}

func hostEntries(entries []resource.DirEntry) []map[string]any {
	out := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		out = append(out, map[string]any{
			"name":        entry.Name,
			"type":        entry.Type,
			"size":        entry.Size,
			"modified_at": entry.ModTime,
			"resource_id": entry.Ref.ID,
			"path":        entry.Ref.DisplayPath,
		})
	}
	return out
}
