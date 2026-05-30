package storage

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMemorySegmentLifecycle(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	if err := db.CreateSession(ctx, "chat-1", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	first, err := db.CreateMemorySegment(ctx, CreateMemorySegmentParams{
		ID:              "segment-1",
		ChatSessionID:   "chat-1",
		PersonaID:       "default",
		MemorySessionID: "memory-1",
	})
	if err != nil {
		t.Fatalf("CreateMemorySegment(first): %v", err)
	}
	if first.SegmentIndex != 1 {
		t.Fatalf("first.SegmentIndex = %d, want 1", first.SegmentIndex)
	}
	if first.FinalizedAt != "" {
		t.Fatalf("first.FinalizedAt = %q, want empty", first.FinalizedAt)
	}

	link, err := db.GetMemoryChatLink(ctx, "chat-1")
	if err != nil {
		t.Fatalf("GetMemoryChatLink: %v", err)
	}
	if link == nil || link.CurrentMemorySessionID != "memory-1" {
		t.Fatalf("link = %#v, want current memory-1", link)
	}

	if _, err := db.CreateMemorySegment(ctx, CreateMemorySegmentParams{
		ID:              "segment-active-conflict",
		ChatSessionID:   "chat-1",
		PersonaID:       "default",
		MemorySessionID: "memory-conflict",
	}); err == nil {
		t.Fatal("CreateMemorySegment succeeded with active segment, want unique constraint error")
	}

	if err := db.UpdateMemorySegmentEpisode(ctx, "segment-1", "user", "episode-user"); err != nil {
		t.Fatalf("UpdateMemorySegmentEpisode(user): %v", err)
	}
	updated, err := db.GetMemorySegment(ctx, "segment-1")
	if err != nil {
		t.Fatalf("GetMemorySegment(updated): %v", err)
	}
	if updated.LastUserEpisodeID != "episode-user" {
		t.Fatalf("LastUserEpisodeID = %q, want episode-user", updated.LastUserEpisodeID)
	}

	time.Sleep(time.Millisecond)
	if err := db.FinalizeMemorySegment(ctx, "segment-1", "session_resume", ""); err != nil {
		t.Fatalf("FinalizeMemorySegment: %v", err)
	}
	finalized, err := db.GetMemorySegment(ctx, "segment-1")
	if err != nil {
		t.Fatalf("GetMemorySegment(finalized): %v", err)
	}
	if finalized.FinalizedAt == "" {
		t.Fatal("FinalizedAt is empty, want timestamp")
	}
	if finalized.FinalizeReason != "session_resume" {
		t.Fatalf("FinalizeReason = %q, want session_resume", finalized.FinalizeReason)
	}
	if finalized.LastActivityAt <= updated.LastActivityAt {
		t.Fatalf("LastActivityAt = %q, want > %q", finalized.LastActivityAt, updated.LastActivityAt)
	}
	link, err = db.GetMemoryChatLink(ctx, "chat-1")
	if err != nil {
		t.Fatalf("GetMemoryChatLink(after finalize): %v", err)
	}
	if link == nil || link.CurrentMemorySessionID != "" {
		t.Fatalf("link current = %#v, want empty current memory session", link)
	}

	second, err := db.CreateMemorySegment(ctx, CreateMemorySegmentParams{
		ID:              "segment-2",
		ChatSessionID:   "chat-1",
		PersonaID:       "default",
		MemorySessionID: "memory-2",
	})
	if err != nil {
		t.Fatalf("CreateMemorySegment(second): %v", err)
	}
	if second.SegmentIndex != 2 {
		t.Fatalf("second.SegmentIndex = %d, want 2", second.SegmentIndex)
	}
}

func TestMemorySegmentsCascadeWithChatSession(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	if err := db.CreateSession(ctx, "chat-delete", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := db.CreateMemorySegment(ctx, CreateMemorySegmentParams{
		ID:              "segment-delete",
		ChatSessionID:   "chat-delete",
		PersonaID:       "default",
		MemorySessionID: "memory-delete",
	}); err != nil {
		t.Fatalf("CreateMemorySegment: %v", err)
	}

	if err := db.DeleteSession(ctx, "chat-delete"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	link, err := db.GetMemoryChatLink(ctx, "chat-delete")
	if err != nil {
		t.Fatalf("GetMemoryChatLink(after delete): %v", err)
	}
	if link != nil {
		t.Fatalf("link after delete = %#v, want nil", link)
	}
	segment, err := db.GetMemorySegment(ctx, "segment-delete")
	if err != nil {
		t.Fatalf("GetMemorySegment(after delete): %v", err)
	}
	if segment != nil {
		t.Fatalf("segment after delete = %#v, want nil", segment)
	}
}

func TestUpdateMemorySegmentEpisodeRejectsUnsupportedRole(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	if err := db.CreateSession(ctx, "chat-role", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := db.CreateMemorySegment(ctx, CreateMemorySegmentParams{
		ID:              "segment-role",
		ChatSessionID:   "chat-role",
		PersonaID:       "default",
		MemorySessionID: "memory-role",
	}); err != nil {
		t.Fatalf("CreateMemorySegment: %v", err)
	}

	err := db.UpdateMemorySegmentEpisode(ctx, "segment-role", "system", "episode-system")
	if err == nil {
		t.Fatal("UpdateMemorySegmentEpisode should reject unsupported role")
	}
	if !strings.Contains(err.Error(), "unsupported memory episode role") {
		t.Fatalf("error = %v, want unsupported role", err)
	}
}

func TestUpdateMemorySegmentEpisodeMarksSuccessfulExtractionStale(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)

	if err := db.CreateSession(ctx, "chat-stale", "default"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	segment, err := db.CreateMemorySegment(ctx, CreateMemorySegmentParams{
		ID:              "segment-stale",
		ChatSessionID:   "chat-stale",
		PersonaID:       "default",
		MemorySessionID: "memory-stale",
	})
	if err != nil {
		t.Fatalf("CreateMemorySegment: %v", err)
	}
	untilAt := segment.LastActivityAt
	if err := db.UpdateMemorySegmentExtractionCompleted(ctx, "segment-stale", MemorySegmentExtractionCompleted{
		JobID:            "job-stale",
		Status:           MemorySegmentExtractionStatusSucceeded,
		ExtractedUntilAt: untilAt,
	}); err != nil {
		t.Fatalf("UpdateMemorySegmentExtractionCompleted: %v", err)
	}

	time.Sleep(time.Millisecond)
	if err := db.UpdateMemorySegmentEpisode(ctx, "segment-stale", "user", "episode-new"); err != nil {
		t.Fatalf("UpdateMemorySegmentEpisode: %v", err)
	}

	got, err := db.GetMemorySegment(ctx, "segment-stale")
	if err != nil {
		t.Fatalf("GetMemorySegment: %v", err)
	}
	if got.ExtractionStatus != MemorySegmentExtractionStatusStale {
		t.Fatalf("ExtractionStatus = %q, want stale", got.ExtractionStatus)
	}
	if got.LastExtractedUntilAt != untilAt {
		t.Fatalf("LastExtractedUntilAt = %q, want %q", got.LastExtractedUntilAt, untilAt)
	}
}
