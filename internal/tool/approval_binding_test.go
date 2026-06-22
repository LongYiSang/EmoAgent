package tool

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizedInputHashCanonicalizesObjectKeyOrder(t *testing.T) {
	left, err := NormalizedInputHash(json.RawMessage(`{"path":"a.txt","content":"hello","create_dirs":true}`))
	if err != nil {
		t.Fatalf("NormalizedInputHash(left): %v", err)
	}
	right, err := NormalizedInputHash(json.RawMessage(`{"create_dirs":true,"content":"hello","path":"a.txt"}`))
	if err != nil {
		t.Fatalf("NormalizedInputHash(right): %v", err)
	}
	if left != right {
		t.Fatalf("hashes differ for same semantic input: %q vs %q", left, right)
	}
	if !strings.HasPrefix(left, "sha256:") {
		t.Fatalf("hash = %q, want sha256 prefix", left)
	}
}

func TestBuildApprovalBindingIncludesFullInputInHashButPreviewOmitsWriteContent(t *testing.T) {
	call := Call{
		ID:    "write-1",
		Name:  "write_file",
		Input: json.RawMessage(`{"path":"docs/a.txt","content":"very secret body","create_dirs":false}`),
	}

	binding, err := BuildApprovalBinding(call, "approval-1", ApprovalKindDestructiveWrite)
	if err != nil {
		t.Fatalf("BuildApprovalBinding: %v", err)
	}
	if binding.RequestID != "approval-1" {
		t.Fatalf("RequestID = %q, want approval-1", binding.RequestID)
	}
	if binding.ApprovalKind != string(ApprovalKindDestructiveWrite) {
		t.Fatalf("ApprovalKind = %q, want %q", binding.ApprovalKind, ApprovalKindDestructiveWrite)
	}
	if binding.ToolName != "write_file" {
		t.Fatalf("ToolName = %q, want write_file", binding.ToolName)
	}
	if binding.NormalizedInputHash == "" || !strings.HasPrefix(binding.NormalizedInputHash, "sha256:") {
		t.Fatalf("NormalizedInputHash = %q, want sha256 digest", binding.NormalizedInputHash)
	}
	if binding.PathDigest == "" || !strings.HasPrefix(binding.PathDigest, "sha256:") {
		t.Fatalf("PathDigest = %q, want sha256 digest", binding.PathDigest)
	}
	if strings.Contains(binding.InputPreview, "very secret body") {
		t.Fatalf("InputPreview leaks write_file content: %q", binding.InputPreview)
	}

	mutated := call
	mutated.Input = json.RawMessage(`{"path":"docs/a.txt","content":"changed","create_dirs":false}`)
	mutatedBinding, err := BuildApprovalBinding(mutated, "approval-1", ApprovalKindDestructiveWrite)
	if err != nil {
		t.Fatalf("BuildApprovalBinding(mutated): %v", err)
	}
	if binding.NormalizedInputHash == mutatedBinding.NormalizedInputHash {
		t.Fatal("hash should change when write_file content changes")
	}
	if binding.PathDigest != mutatedBinding.PathDigest {
		t.Fatal("path digest should stay stable when only content changes")
	}
}

func TestBuildApprovalBindingIncludesChangeSetPlanFields(t *testing.T) {
	call := Call{
		ID:    "apply-1",
		Name:  "host_apply_change",
		Input: json.RawMessage(`{"changeset_id":"cs-1","plan_hash":"sha256:plan","resource_id":"local:abc","canonical_path_hash":"sha256:path","baseline_hash":"sha256:old","baseline_file_id":"file-id","delete_mode":"quarantine","recursive":true}`),
	}

	binding, err := BuildApprovalBinding(call, "approval-1", ApprovalKindDestructiveWrite)
	if err != nil {
		t.Fatalf("BuildApprovalBinding: %v", err)
	}
	if binding.ChangeSetID != "cs-1" || binding.PlanHash != "sha256:plan" || binding.ResourceID != "local:abc" ||
		binding.CanonicalPathHash != "sha256:path" || binding.BaselineHash != "sha256:old" ||
		binding.BaselineFileID != "file-id" || binding.DeleteMode != "quarantine" {
		t.Fatalf("binding = %#v", binding)
	}
	if binding.PathDigest == "" ||
		!strings.Contains(binding.InputPreview, "changeset_id=cs-1") ||
		!strings.Contains(binding.InputPreview, "recursive=true") ||
		strings.Contains(binding.InputPreview, "file-id") {
		t.Fatalf("binding preview/path digest = %#v", binding)
	}

	mutated := call
	mutated.Input = json.RawMessage(`{"changeset_id":"cs-1","plan_hash":"sha256:changed"}`)
	mutatedBinding, err := BuildApprovalBinding(mutated, "approval-1", ApprovalKindDestructiveWrite)
	if err != nil {
		t.Fatalf("BuildApprovalBinding(mutated): %v", err)
	}
	if binding.NormalizedInputHash == mutatedBinding.NormalizedInputHash {
		t.Fatal("normalized input hash should change when plan_hash changes")
	}
	if binding.PathDigest == mutatedBinding.PathDigest {
		t.Fatal("path digest should bind changeset_id and plan_hash for host_apply_change")
	}
}
