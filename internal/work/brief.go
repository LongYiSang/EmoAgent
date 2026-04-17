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

// ValidateAndComplete validates a TaskBrief for the minimal read-only phase
// and fills server-owned metadata when absent.
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
	if brief.PermissionScope != "read-only" {
		return fmt.Errorf("minimal phase only supports read-only permission scope")
	}
	if brief.TaskID == "" {
		brief.TaskID = uuid.NewString()
	}
	if brief.CreatedAt.IsZero() {
		brief.CreatedAt = time.Now().UTC()
	}
	return nil
}
