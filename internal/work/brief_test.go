package work

import (
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/protocol"
)

func TestValidateAndComplete_HappyPath(t *testing.T) {
	b := &protocol.TaskBrief{
		Goal:            "read go.mod and list dependencies",
		PermissionScope: "read-only",
	}

	if err := ValidateAndComplete(b); err != nil {
		t.Fatalf("ValidateAndComplete returned error: %v", err)
	}
	if b.TaskID == "" {
		t.Fatal("TaskID should be auto-filled")
	}
	if b.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be auto-filled")
	}
}

func TestValidateAndComplete_EmptyGoalRejected(t *testing.T) {
	b := &protocol.TaskBrief{
		Goal:            "",
		PermissionScope: "read-only",
	}

	if err := ValidateAndComplete(b); err == nil {
		t.Fatal("ValidateAndComplete should reject an empty goal")
	}
}

func TestValidateAndComplete_GoalTooLongRejected(t *testing.T) {
	b := &protocol.TaskBrief{
		Goal:            strings.Repeat("中", 501),
		PermissionScope: "read-only",
	}

	if err := ValidateAndComplete(b); err == nil {
		t.Fatal("ValidateAndComplete should reject goals longer than 500 runes")
	}
}

func TestValidateAndComplete_WorkspaceWriteAccepted(t *testing.T) {
	b := &protocol.TaskBrief{
		Goal:            "write a summary file",
		PermissionScope: "workspace-write",
	}
	if err := ValidateAndComplete(b); err != nil {
		t.Fatalf("ValidateAndComplete should accept workspace-write, got: %v", err)
	}
}

func TestValidateAndComplete_ApprovedDestructiveAccepted(t *testing.T) {
	b := &protocol.TaskBrief{
		Goal:            "delete generated cache files after approval",
		PermissionScope: "approved-destructive",
	}
	if err := ValidateAndComplete(b); err != nil {
		t.Fatalf("ValidateAndComplete should accept approved-destructive, got: %v", err)
	}
}

func TestValidateAndComplete_UnsupportedScopeRejected(t *testing.T) {
	for _, scope := range []string{"superuser", ""} {
		t.Run(scope, func(t *testing.T) {
			b := &protocol.TaskBrief{
				Goal:            "edit config",
				PermissionScope: scope,
			}
			err := ValidateAndComplete(b)
			if err == nil {
				t.Fatalf("ValidateAndComplete should reject scope %q", scope)
			}
		})
	}
}

func TestValidateAndComplete_PreservesExistingTaskID(t *testing.T) {
	b := &protocol.TaskBrief{
		TaskID:          "fixed-task-id",
		Goal:            "read file",
		PermissionScope: "read-only",
	}

	if err := ValidateAndComplete(b); err != nil {
		t.Fatalf("ValidateAndComplete returned error: %v", err)
	}
	if b.TaskID != "fixed-task-id" {
		t.Fatalf("TaskID = %q, want fixed-task-id", b.TaskID)
	}
}
