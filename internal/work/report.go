package work

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/longyisang/emoagent/internal/protocol"
)

const fallbackSummaryLimit = 800

// ParseOrFallback parses the final Work output into a TaskReport, falling back
// to a partial report when the LLM does not return valid JSON.
func ParseOrFallback(llmOutput string, brief protocol.TaskBrief) protocol.TaskReport {
	var report protocol.TaskReport
	if err := json.Unmarshal([]byte(stripCodeFence(llmOutput)), &report); err == nil {
		report.TaskID = brief.TaskID
		report.Goal = brief.Goal
		switch report.Status {
		case "completed", "failed", "partial":
		default:
			report.Status = "partial"
		}
		if report.CreatedAt.IsZero() {
			report.CreatedAt = time.Now().UTC()
		}
		return report
	}

	return protocol.TaskReport{
		TaskID:    brief.TaskID,
		Status:    "partial",
		Goal:      brief.Goal,
		Summary:   truncateRunes(strings.TrimSpace(llmOutput), fallbackSummaryLimit),
		CreatedAt: time.Now().UTC(),
	}
}

func stripCodeFence(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	if idx := strings.IndexByte(trimmed, '\n'); idx >= 0 {
		trimmed = trimmed[idx+1:]
	}
	if idx := strings.LastIndex(trimmed, "```"); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	return strings.TrimSpace(trimmed)
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}
