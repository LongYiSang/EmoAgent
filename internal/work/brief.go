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
	switch brief.PermissionScope {
	case "read-only", "workspace-write", "approved-destructive":
		// accepted
	default:
		return fmt.Errorf("unsupported permission scope %q (accepted: read-only, workspace-write, approved-destructive)", brief.PermissionScope)
	}
	if brief.TaskID == "" {
		brief.TaskID = uuid.NewString()
	}
	if brief.CreatedAt.IsZero() {
		brief.CreatedAt = time.Now().UTC()
	}
	return nil
}
