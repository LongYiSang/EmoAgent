package work

import (
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/protocol"
)

func TestParseOrFallback_ParsesCleanJSON(t *testing.T) {
	brief := protocol.TaskBrief{
		TaskID:          "task-1",
		Goal:            "inspect file",
		PermissionScope: "read-only",
	}

	report := ParseOrFallback(`{"task_id":"ignored","goal":"ignored","status":"completed","summary":"ok","findings":["a","b"]}`, brief)
	if report.TaskID != "task-1" {
		t.Fatalf("TaskID = %q, want task-1", report.TaskID)
	}
	if report.Goal != "inspect file" {
		t.Fatalf("Goal = %q, want inspect file", report.Goal)
	}
	if report.Status != "completed" {
		t.Fatalf("Status = %q, want completed", report.Status)
	}
	if len(report.Findings) != 2 {
		t.Fatalf("Findings = %#v, want 2 entries", report.Findings)
	}
	if report.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be filled")
	}
}

func TestParseOrFallback_StripsCodeFence(t *testing.T) {
	brief := protocol.TaskBrief{
		TaskID:          "task-2",
		Goal:            "inspect file",
		PermissionScope: "read-only",
	}

	report := ParseOrFallback("```json\n{\"status\":\"partial\",\"summary\":\"s\"}\n```", brief)
	if report.Status != "partial" {
		t.Fatalf("Status = %q, want partial", report.Status)
	}
	if report.Summary != "s" {
		t.Fatalf("Summary = %q, want s", report.Summary)
	}
}

func TestParseOrFallback_DefaultsUnknownStatusToPartial(t *testing.T) {
	brief := protocol.TaskBrief{
		TaskID:          "task-3",
		Goal:            "inspect file",
		PermissionScope: "read-only",
	}

	report := ParseOrFallback(`{"summary":"x","status":"mystery"}`, brief)
	if report.Status != "partial" {
		t.Fatalf("Status = %q, want partial", report.Status)
	}
}

func TestParseOrFallback_FallsBackOnGarbage(t *testing.T) {
	brief := protocol.TaskBrief{
		TaskID:          "task-4",
		Goal:            "inspect file",
		PermissionScope: "read-only",
	}

	report := ParseOrFallback("sorry, I could not finish", brief)
	if report.Status != "partial" {
		t.Fatalf("Status = %q, want partial", report.Status)
	}
	if !strings.Contains(report.Summary, "sorry") {
		t.Fatalf("Summary = %q, want raw text included", report.Summary)
	}
	if report.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be filled")
	}
}

func TestParseOrFallback_TruncatesFallbackSummary(t *testing.T) {
	brief := protocol.TaskBrief{
		TaskID:          "task-5",
		Goal:            "inspect file",
		PermissionScope: "read-only",
	}

	report := ParseOrFallback(strings.Repeat("a", 2000), brief)
	if len([]rune(report.Summary)) > fallbackSummaryLimit+3 {
		t.Fatalf("Summary too long after truncation: %d", len([]rune(report.Summary)))
	}
}

func TestParseOrFallback_PreservesExistingCreatedAt(t *testing.T) {
	when := time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC)
	brief := protocol.TaskBrief{
		TaskID:          "task-6",
		Goal:            "inspect file",
		PermissionScope: "read-only",
	}

	report := ParseOrFallback(`{"status":"completed","summary":"ok","created_at":"2026-04-17T12:00:00Z"}`, brief)
	if !report.CreatedAt.Equal(when) {
		t.Fatalf("CreatedAt = %s, want %s", report.CreatedAt.Format(time.RFC3339), when.Format(time.RFC3339))
	}
}
