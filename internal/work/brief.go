package work

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent/internal/protocol"
)

const maxGoalRunes = 500

func goalLikelyNeedsApprovedDestructive(goal string) bool {
	normalized := strings.ToLower(strings.TrimSpace(goal))
	if normalized == "" {
		return false
	}
	hints := []string{
		" delete ", " remove ", " rm ", " del ", " erase ",
		" move ", " rename ", " overwrite ", " replace ",
		"删除", "移除", "删掉", "改名", "重命名", "移动", "覆盖",
	}
	padded := " " + normalized + " "
	for _, hint := range hints {
		if strings.Contains(padded, hint) || strings.Contains(normalized, hint) {
			return true
		}
	}
	return false
}

// ValidateAndComplete validates a TaskBrief and fills server-owned metadata when absent.
// Accepted permission scopes: "read-only", "workspace-write", "approved-destructive".
func ValidateAndComplete(brief *protocol.TaskBrief) error {
	if brief == nil {
		return fmt.Errorf("task brief is required")
	}
	if strings.TrimSpace(brief.Goal) == "" {
		return fmt.Errorf("task brief goal is required")
	}
	if utf8.RuneCountInString(brief.Goal) > maxGoalRunes {
		return fmt.Errorf("task brief goal exceeds %d runes", maxGoalRunes)
	}
	if !hasNonEmptyAcceptanceCriterion(brief.AcceptanceCriteria) {
		return fmt.Errorf("task brief acceptance_criteria requires at least one non-empty item")
	}
	switch brief.PermissionScope {
	case "read-only", "workspace-write", "approved-destructive":
		// accepted
	default:
		return fmt.Errorf("unsupported permission scope %q (accepted: read-only, workspace-write, approved-destructive)", brief.PermissionScope)
	}
	if strings.TrimSpace(brief.ReadScope) == "" {
		brief.ReadScope = "workspace"
	}
	switch brief.ReadScope {
	case "workspace", "all":
		// accepted
	default:
		return fmt.Errorf("unsupported read_scope %q (accepted: workspace, all)", brief.ReadScope)
	}
	if goalLikelyNeedsApprovedDestructive(brief.Goal) && brief.PermissionScope != "approved-destructive" {
		return fmt.Errorf("task brief goal requires approved-destructive permission scope")
	}
	if brief.TaskID == "" {
		brief.TaskID = uuid.NewString()
	}
	if brief.CreatedAt.IsZero() {
		brief.CreatedAt = time.Now().UTC()
	}
	return nil
}

func hasNonEmptyAcceptanceCriterion(criteria []string) bool {
	for _, criterion := range criteria {
		if strings.TrimSpace(criterion) != "" {
			return true
		}
	}
	return false
}
