package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/longyisang/emoagent/internal/resource"
	"github.com/longyisang/emoagent/internal/tool"
)

func NewHostStageResourceTool(manager *resource.ChangeSetManager) (tool.Spec, tool.Handler) {
	spec := hostToolSpec("host_stage_resource", "Stage text content for a later host resource ChangeSet. Returns staging_id, content hash, and byte count.", `{"type":"object","properties":{"content":{"type":"string"}},"required":["content"],"additionalProperties":false}`)
	return spec, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		if manager == nil {
			return nil, fmt.Errorf("host_stage_resource: host resource changeset manager is unavailable")
		}
		var in struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("host_stage_resource: invalid input: %w", err)
		}
		staged, err := manager.StageResource(ctx, in.Content)
		if err != nil {
			return nil, fmt.Errorf("host_stage_resource: %w", err)
		}
		return json.Marshal(staged)
	}
}

func NewHostPrepareChangeTool(manager *resource.ChangeSetManager) (tool.Spec, tool.Handler) {
	spec := hostToolSpec("host_prepare_change", "Prepare a previewable host resource ChangeSet for create_file, overwrite_file, move, delete, mkdir, or rmdir. Does not apply changes.", `{"type":"object","properties":{"operation":{"type":"string"},"path":{"type":"string"},"resource_id":{"type":"string"},"target_path":{"type":"string"},"content":{"type":"string"},"staging_id":{"type":"string"},"permanent_delete":{"type":"boolean"},"recursive":{"type":"boolean"}},"required":["operation"],"additionalProperties":false}`)
	return spec, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		if manager == nil {
			return nil, fmt.Errorf("host_prepare_change: host resource changeset manager is unavailable")
		}
		var in struct {
			Operation       resource.ChangeOperation `json:"operation"`
			Path            string                   `json:"path"`
			ResourceID      string                   `json:"resource_id"`
			TargetPath      string                   `json:"target_path"`
			Content         string                   `json:"content"`
			StagingID       string                   `json:"staging_id"`
			PermanentDelete bool                     `json:"permanent_delete"`
			Recursive       bool                     `json:"recursive"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("host_prepare_change: invalid input: %w", err)
		}
		cs, err := manager.PrepareChange(ctx, resource.ChangeSetRequest{
			Operation:       in.Operation,
			Path:            in.Path,
			ResourceID:      in.ResourceID,
			TargetPath:      in.TargetPath,
			Content:         in.Content,
			StagingID:       in.StagingID,
			PermanentDelete: in.PermanentDelete,
			Recursive:       in.Recursive,
		})
		if err != nil {
			return nil, fmt.Errorf("host_prepare_change: %w", err)
		}
		return json.Marshal(cs)
	}
}

func NewHostPreviewChangeTool(manager *resource.ChangeSetManager) (tool.Spec, tool.Handler) {
	spec := hostToolSpec("host_preview_change", "Return a prepared host resource ChangeSet preview and plan hash by changeset_id.", `{"type":"object","properties":{"changeset_id":{"type":"string"}},"required":["changeset_id"],"additionalProperties":false}`)
	return spec, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		if manager == nil {
			return nil, fmt.Errorf("host_preview_change: host resource changeset manager is unavailable")
		}
		var in struct {
			ChangeSetID string `json:"changeset_id"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("host_preview_change: invalid input: %w", err)
		}
		cs, err := manager.PreviewChange(ctx, in.ChangeSetID)
		if err != nil {
			return nil, fmt.Errorf("host_preview_change: %w", err)
		}
		return json.Marshal(cs)
	}
}

func NewHostApplyChangeTool(manager *resource.ChangeSetManager) (tool.Spec, tool.Handler) {
	spec := hostToolSpec("host_apply_change", "Apply a previously prepared host resource ChangeSet after approval. Requires exact changeset_id, plan_hash, resource snapshot fields, and explicit delete/recursive flags when applicable.", `{"type":"object","properties":{"changeset_id":{"type":"string"},"plan_hash":{"type":"string"},"resource_id":{"type":"string"},"canonical_path_hash":{"type":"string"},"baseline_hash":{"type":"string"},"baseline_file_id":{"type":"string"},"delete_mode":{"type":"string"},"recursive":{"type":"boolean"}},"required":["changeset_id","plan_hash"],"additionalProperties":false}`)
	spec.Permission = tool.PermApprovedDestructive
	return spec, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		if manager == nil {
			return nil, fmt.Errorf("host_apply_change: host resource changeset manager is unavailable")
		}
		var in struct {
			ChangeSetID       string `json:"changeset_id"`
			PlanHash          string `json:"plan_hash"`
			ResourceID        string `json:"resource_id"`
			CanonicalPathHash string `json:"canonical_path_hash"`
			BaselineHash      string `json:"baseline_hash"`
			BaselineFileID    string `json:"baseline_file_id"`
			DeleteMode        string `json:"delete_mode"`
			Recursive         bool   `json:"recursive"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("host_apply_change: invalid input: %w", err)
		}
		cs, err := manager.PreviewChange(ctx, in.ChangeSetID)
		if err != nil {
			return nil, fmt.Errorf("host_apply_change: %w", err)
		}
		if err := validateApplyBindingInput(cs, resource.ChangeApplyOptions{
			DeleteMode:        in.DeleteMode,
			Recursive:         in.Recursive,
			ResourceID:        in.ResourceID,
			CanonicalPathHash: in.CanonicalPathHash,
			BaselineHash:      in.BaselineHash,
			BaselineFileID:    in.BaselineFileID,
		}); err != nil {
			return nil, fmt.Errorf("host_apply_change: %w", err)
		}
		cs, err = manager.ApplyChangeWithOptions(ctx, in.ChangeSetID, in.PlanHash, resource.ChangeApplyOptions{
			DeleteMode:        in.DeleteMode,
			Recursive:         in.Recursive,
			ResourceID:        in.ResourceID,
			CanonicalPathHash: in.CanonicalPathHash,
			BaselineHash:      in.BaselineHash,
			BaselineFileID:    in.BaselineFileID,
		})
		if err != nil {
			return nil, fmt.Errorf("host_apply_change: %w", err)
		}
		if cs.Status != resource.ChangeSetStatusApplied {
			return nil, fmt.Errorf("host_apply_change: changeset %s ended with status %s: %s", cs.ID, cs.Status, cs.ErrorMessage)
		}
		return json.Marshal(cs)
	}
}

func NewHostCancelChangeTool(manager *resource.ChangeSetManager) (tool.Spec, tool.Handler) {
	spec := hostToolSpec("host_cancel_change", "Cancel a prepared host resource ChangeSet that has not been applied.", `{"type":"object","properties":{"changeset_id":{"type":"string"}},"required":["changeset_id"],"additionalProperties":false}`)
	return spec, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		if manager == nil {
			return nil, fmt.Errorf("host_cancel_change: host resource changeset manager is unavailable")
		}
		var in struct {
			ChangeSetID string `json:"changeset_id"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("host_cancel_change: invalid input: %w", err)
		}
		cs, err := manager.CancelChange(ctx, in.ChangeSetID)
		if err != nil {
			return nil, fmt.Errorf("host_cancel_change: %w", err)
		}
		return json.Marshal(cs)
	}
}

func NewHostRestoreQuarantineTool(manager *resource.ChangeSetManager) (tool.Spec, tool.Handler) {
	spec := hostToolSpec("host_restore_quarantine", "Restore a quarantined host resource from an applied delete/rmdir ChangeSet after approval.", `{"type":"object","properties":{"changeset_id":{"type":"string"},"plan_hash":{"type":"string"}},"required":["changeset_id","plan_hash"],"additionalProperties":false}`)
	spec.Permission = tool.PermApprovedDestructive
	return spec, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		if manager == nil {
			return nil, fmt.Errorf("host_restore_quarantine: host resource changeset manager is unavailable")
		}
		var in struct {
			ChangeSetID string `json:"changeset_id"`
			PlanHash    string `json:"plan_hash"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("host_restore_quarantine: invalid input: %w", err)
		}
		cs, err := manager.RestoreQuarantine(ctx, in.ChangeSetID, in.PlanHash)
		if err != nil {
			return nil, fmt.Errorf("host_restore_quarantine: %w", err)
		}
		return json.Marshal(cs)
	}
}

func validateApplyBindingInput(cs resource.ChangeSet, opts resource.ChangeApplyOptions) error {
	ref := cs.Source
	if ref.ID == "" {
		ref = cs.Target
	}
	if ref.ID != "" && opts.ResourceID != ref.ID {
		return fmt.Errorf("resource_id mismatch")
	}
	if ref.CanonicalPathHash != "" && opts.CanonicalPathHash != ref.CanonicalPathHash {
		return fmt.Errorf("canonical_path_hash mismatch")
	}
	if cs.BaselineHash != "" && opts.BaselineHash != cs.BaselineHash {
		return fmt.Errorf("baseline_hash mismatch")
	}
	if cs.BaselineFileID != "" && opts.BaselineFileID != cs.BaselineFileID {
		return fmt.Errorf("baseline_file_id mismatch")
	}
	if cs.Operation == resource.ChangeOpDelete || cs.Operation == resource.ChangeOpRmdir {
		if cs.PermanentDelete && opts.DeleteMode != resource.DeleteModePermanent {
			return fmt.Errorf("permanent delete requires explicit delete_mode=permanent approval")
		}
		if !cs.PermanentDelete && opts.DeleteMode != "" && opts.DeleteMode != resource.DeleteModeQuarantine {
			return fmt.Errorf("delete_mode mismatch")
		}
	}
	if cs.Recursive && !opts.Recursive {
		return fmt.Errorf("recursive changeset requires explicit recursive approval")
	}
	if !cs.Recursive && opts.Recursive {
		return fmt.Errorf("recursive approval does not match changeset")
	}
	return nil
}
