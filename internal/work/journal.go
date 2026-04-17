package work

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Event is one JSONL row in the Work audit journal.
type Event struct {
	Timestamp time.Time `json:"ts"`
	TaskID    string    `json:"task_id"`
	Kind      string    `json:"kind"`
	Round     int       `json:"round,omitempty"`
	Payload   any       `json:"payload,omitempty"`
}

// Journal writes one file per task. Nil receivers are valid no-ops so callers
// can keep logging even when journal initialization failed.
type Journal struct {
	mu     sync.Mutex
	taskID string
	file   *os.File
	closed bool
	logger *slog.Logger
}

// Open creates or reuses the daily journal file for a task.
func Open(rootDir, taskID string, now time.Time, logger *slog.Logger) (*Journal, error) {
	dayDir := filepath.Join(rootDir, now.Format("2006-01-02"))
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		return nil, fmt.Errorf("create journal directory: %w", err)
	}

	file, err := os.OpenFile(filepath.Join(dayDir, taskID+".jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open journal file: %w", err)
	}

	return &Journal{taskID: taskID, file: file, logger: logger}, nil
}

// Write appends a single JSONL event.
func (j *Journal) Write(kind string, round int, payload any) {
	if j == nil {
		return
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	if j.closed || j.file == nil {
		return
	}

	line, err := json.Marshal(Event{
		Timestamp: time.Now().UTC(),
		TaskID:    j.taskID,
		Kind:      kind,
		Round:     round,
		Payload:   payload,
	})
	if err != nil {
		if j.logger != nil {
			j.logger.Warn("journal marshal failed", "kind", kind, "error", err)
		}
		return
	}
	line = append(line, '\n')
	if _, err := j.file.Write(line); err != nil && j.logger != nil {
		j.logger.Warn("journal write failed", "kind", kind, "error", err)
	}
}

// Close closes the journal file. It is idempotent and nil-safe.
func (j *Journal) Close() error {
	if j == nil {
		return nil
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	if j.closed {
		return nil
	}
	j.closed = true
	if j.file == nil {
		return nil
	}

	err := j.file.Close()
	j.file = nil
	return err
}
