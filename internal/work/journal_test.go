package work

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestJournal_WritesExpectedEventKinds(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)

	journal, err := Open(root, "task-abc", now, testLogger())
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	journal.Write("task_start", 0, map[string]string{"goal": "g"})
	journal.Write("tool_call", 1, map[string]string{"name": "read_file"})
	journal.Write("tool_result", 1, map[string]string{"preview": "ok"})
	journal.Write("task_end", 2, map[string]string{"status": "completed"})
	journal.Write("task_error", 2, map[string]string{"error": "boom"})
	if err := journal.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	file, err := os.Open(filepath.Join(root, "2026-04-17", "task-abc.jsonl"))
	if err != nil {
		t.Fatalf("expected journal file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var got []string
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("journal line should be valid JSON: %v", err)
		}
		got = append(got, event["kind"].(string))
	}
	want := "task_start,tool_call,tool_result,task_end,task_error"
	if strings.Join(got, ",") != want {
		t.Fatalf("journal kinds = %q, want %q", strings.Join(got, ","), want)
	}
}

func TestJournal_NilIsNoop(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil journal should not panic: %v", r)
		}
	}()

	var journal *Journal
	journal.Write("task_start", 0, nil)
	if err := journal.Close(); err != nil {
		t.Fatalf("Close on nil journal returned error: %v", err)
	}
}

func TestJournal_CloseIsIdempotent(t *testing.T) {
	journal, err := Open(t.TempDir(), "task-id", time.Now(), testLogger())
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatalf("first Close returned error: %v", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatalf("second Close should be a no-op, got %v", err)
	}
}

func TestJournal_CreatesDailyDirectory(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC)

	journal, err := Open(root, "task-id", now, testLogger())
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "2030-01-02")); err != nil {
		t.Fatalf("expected daily directory: %v", err)
	}
}
