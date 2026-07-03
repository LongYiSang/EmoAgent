package context_test

import (
	"testing"

	ctxpkg "github.com/longyisang/emoagent/internal/context"
	"github.com/longyisang/emoagent/internal/storage"
)

func TestFilterHistoryAfterResetBarrierKeepsOnlyMessagesAfterBarrier(t *testing.T) {
	history := []storage.MessageRecord{
		{ID: "m1", Role: "user", Content: "before user"},
		{ID: "m2", Role: "assistant", Content: "before assistant"},
		{ID: "m3", Role: "user", Content: "after user"},
		{ID: "m4", Role: "assistant", Content: "after assistant"},
	}

	got := ctxpkg.FilterHistoryAfterResetBarrier(history, &ctxpkg.ContextResetBarrier{
		AfterMessageID: "m2",
	})

	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].ID != "m3" || got[1].ID != "m4" {
		t.Fatalf("got IDs = %q, %q; want m3, m4", got[0].ID, got[1].ID)
	}
}

func TestFilterHistoryAfterResetBarrierLeavesHistoryWhenBarrierEmpty(t *testing.T) {
	history := []storage.MessageRecord{
		{ID: "m1", Role: "user", Content: "before user"},
		{ID: "m2", Role: "assistant", Content: "before assistant"},
	}

	tests := []struct {
		name    string
		barrier *ctxpkg.ContextResetBarrier
	}{
		{name: "nil", barrier: nil},
		{name: "missing message id", barrier: &ctxpkg.ContextResetBarrier{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ctxpkg.FilterHistoryAfterResetBarrier(history, tt.barrier)
			if len(got) != len(history) {
				t.Fatalf("len(got) = %d, want %d", len(got), len(history))
			}
			for i := range history {
				if got[i].ID != history[i].ID {
					t.Fatalf("got[%d].ID = %q, want %q", i, got[i].ID, history[i].ID)
				}
			}
		})
	}
}
