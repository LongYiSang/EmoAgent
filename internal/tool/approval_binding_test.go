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
