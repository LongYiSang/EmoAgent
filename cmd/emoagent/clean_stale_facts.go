package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/longyisang/emoagent/internal/config"
	contextutil "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/storage"
)

// maxCleanupSessions bounds the sweep. Sessions are listed newest-first and a
// personal deployment has nowhere near this many.
const maxCleanupSessions = 10000

// runCleanStaleSummaryFacts strips running_summary facts whose wording misdates
// itself — "用户刚切了西瓜在吃" written weeks ago still claims to be happening now.
//
// It opens the database directly instead of booting the app: this is offline
// maintenance and must not start servers, sidecars, or platform adapters.
func runCleanStaleSummaryFacts(ctx context.Context, configPath string, logger *slog.Logger, out io.Writer, apply bool) int {
	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		return 1
	}

	db, err := storage.OpenWithOptions(cfg.DB.Path, logger, storage.StorageOptions{Timezone: cfg.Time.Timezone})
	if err != nil {
		logger.Error("failed to open database", "path", cfg.DB.Path, "error", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	sessions, err := db.ListSessions(ctx, "", maxCleanupSessions)
	if err != nil {
		logger.Error("failed to list sessions", "error", err)
		return 1
	}
	sessionIDs := make([]string, 0, len(sessions))
	for _, session := range sessions {
		sessionIDs = append(sessionIDs, session.ID)
	}

	results, err := contextutil.CleanStaleTimeFacts(ctx, db, sessionIDs, cfg.Context, apply)
	if err != nil {
		logger.Error("failed to clean stale summary facts", "error", err)
		return 1
	}

	if len(results) == 0 {
		_, _ = fmt.Fprintf(out, "scanned %d sessions: no self-misdating facts found\n", len(sessionIDs))
		return 0
	}

	mode := "DRY RUN — nothing was written"
	if apply {
		mode = "APPLIED — these facts were removed"
	}
	_, _ = fmt.Fprintf(out, "%s\nscanned %d sessions, %d affected:\n\n", mode, len(sessionIDs), len(results))
	total := 0
	for _, result := range results {
		_, _ = fmt.Fprintf(out, "session %s\n", result.SessionID)
		for _, fact := range result.Removed {
			_, _ = fmt.Fprintf(out, "  - %s\n", fact)
			total++
		}
	}
	_, _ = fmt.Fprintf(out, "\n%d facts total\n", total)
	if !apply {
		_, _ = fmt.Fprintf(out, "\nback up %s and stop the server, then re-run with --clean-stale-summary-facts-apply\n", cfg.DB.Path)
	}
	return 0
}
