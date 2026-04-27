package work

import (
	"strings"
	"testing"

	"github.com/longyisang/emoagent/internal/protocol"
)

func TestValidateAndComplete_HappyPath(t *testing.T) {
	b := &protocol.TaskBrief{
		Goal:               "read go.mod and list dependencies",
		AcceptanceCriteria: []string{"List dependency names"},
		PermissionScope:    "read-only",
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
		Goal:               "",
		AcceptanceCriteria: []string{"Summarize findings"},
		PermissionScope:    "read-only",
	}

	if err := ValidateAndComplete(b); err == nil {
		t.Fatal("ValidateAndComplete should reject an empty goal")
	}
}

func TestValidateAndComplete_GoalTooLongRejected(t *testing.T) {
	b := &protocol.TaskBrief{
		Goal:               strings.Repeat("中", 501),
		AcceptanceCriteria: []string{"Summarize findings"},
		PermissionScope:    "read-only",
	}

	if err := ValidateAndComplete(b); err == nil {
		t.Fatal("ValidateAndComplete should reject goals longer than 500 runes")
	}
}

func TestValidateAndComplete_WorkspaceWriteAccepted(t *testing.T) {
	b := &protocol.TaskBrief{
		Goal:               "write a summary file",
		AcceptanceCriteria: []string{"Summary file is written"},
		PermissionScope:    "workspace-write",
	}
	if err := ValidateAndComplete(b); err != nil {
		t.Fatalf("ValidateAndComplete should accept workspace-write, got: %v", err)
	}
}

func TestValidateAndComplete_ApprovedDestructiveAccepted(t *testing.T) {
	b := &protocol.TaskBrief{
		Goal:               "delete generated cache files after approval",
		AcceptanceCriteria: []string{"Approved cache files are deleted"},
		PermissionScope:    "approved-destructive",
	}
	if err := ValidateAndComplete(b); err != nil {
		t.Fatalf("ValidateAndComplete should accept approved-destructive, got: %v", err)
	}
}

func TestValidateAndComplete_RejectsDestructiveGoalWithoutApprovedScope(t *testing.T) {
	tests := []protocol.TaskBrief{
		{Goal: "删除 hi.txt", AcceptanceCriteria: []string{"hi.txt is removed"}, PermissionScope: "workspace-write"},
		{Goal: "delete tmp directory", AcceptanceCriteria: []string{"tmp directory is removed"}, PermissionScope: "workspace-write"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.Goal, func(t *testing.T) {
			if err := ValidateAndComplete(&tc); err == nil {
				t.Fatalf("expected destructive goal to require approved-destructive: %#v", tc)
			}
		})
	}
}

func TestValidateAndComplete_AllowsDestructiveGoalWithApprovedScope(t *testing.T) {
	b := &protocol.TaskBrief{
		Goal:               "删除 hi.txt",
		AcceptanceCriteria: []string{"hi.txt is removed"},
		PermissionScope:    "approved-destructive",
	}

	if err := ValidateAndComplete(b); err != nil {
		t.Fatalf("approved-destructive should accept destructive goal, got: %v", err)
	}
}

func TestValidateAndComplete_UnsupportedScopeRejected(t *testing.T) {
	for _, scope := range []string{"superuser", ""} {
		t.Run(scope, func(t *testing.T) {
			b := &protocol.TaskBrief{
				Goal:               "edit config",
				AcceptanceCriteria: []string{"Config is edited"},
				PermissionScope:    scope,
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
		TaskID:             "fixed-task-id",
		Goal:               "read file",
		AcceptanceCriteria: []string{"File contents are summarized"},
		PermissionScope:    "read-only",
	}

	if err := ValidateAndComplete(b); err != nil {
		t.Fatalf("ValidateAndComplete returned error: %v", err)
	}
	if b.TaskID != "fixed-task-id" {
		t.Fatalf("TaskID = %q, want fixed-task-id", b.TaskID)
	}
}

func TestValidateAndComplete_RequiresAcceptanceCriteria(t *testing.T) {
	tests := []struct {
		name  string
		brief protocol.TaskBrief
	}{
		{
			name: "missing",
			brief: protocol.TaskBrief{
				Goal:            "inspect config",
				PermissionScope: "read-only",
			},
		},
		{
			name: "empty",
			brief: protocol.TaskBrief{
				Goal:               "inspect config",
				AcceptanceCriteria: []string{},
				PermissionScope:    "read-only",
			},
		},
		{
			name: "blank only",
			brief: protocol.TaskBrief{
				Goal:               "inspect config",
				AcceptanceCriteria: []string{"  "},
				PermissionScope:    "read-only",
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateAndComplete(&tc.brief); err == nil {
				t.Fatal("ValidateAndComplete should reject missing or blank acceptance criteria")
			}
		})
	}
}
