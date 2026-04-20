package work

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/storage"
)

func samplePaused(taskID string) *PausedWork {
	packet := validDecisionPacket(taskID)
	packet.GoalSummary = "sample goal"
	packet.Question = "sample question"
	return &PausedWork{
		TaskID: taskID,
		Brief: protocol.TaskBrief{
			TaskID:          taskID,
			Goal:            "sample",
			PermissionScope: "read-only",
		},
		PendingCallID: "call-" + taskID,
		Packet:        packet,
		CreatedAt:     time.Now().UTC(),
	}
}

func sampleHighRiskPaused(taskID string) *PausedWork {
	paused := samplePaused(taskID)
	paused.Packet.Category = protocol.CatHighRisk
	paused.Packet.RiskLevel = "high"
	return paused
}

func newSQLitePendingRegistry(t *testing.T) *PendingRegistry {
	t.Helper()
	return newSQLitePendingRegistryWithTTLs(t, 5*time.Minute, time.Hour, 24*time.Hour, 2*time.Minute)
}

func newSQLitePendingRegistryWithTTLs(t *testing.T, softTTL, hardTTL, archiveTTL, claimTTL time.Duration) *PendingRegistry {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "pending.db"), testLogger())
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewPendingRegistry(db.SqlDB(), testLogger(), PendingRegistryConfig{
		SoftTTL:        softTTL,
		HardTTL:        hardTTL,
		ArchiveTTL:     archiveTTL,
		ResumeClaimTTL: claimTTL,
	})
}

func TestPendingRegistry_PutClaimAndFinalize(t *testing.T) {
	reg := newSQLitePendingRegistry(t)
	reg.Put("s1", "t1", samplePaused("t1"))

	claim := reg.ClaimForResume("s1", "t1")
	if claim.PausedWork == nil || claim.ClaimID == "" {
		t.Fatalf("claim = %#v, want paused work + claim id", claim)
	}

	report := &protocol.TaskReport{
		TaskID:    "t1",
		Status:    "completed",
		Goal:      "sample",
		Summary:   "done",
		CreatedAt: time.Now().UTC(),
	}
	resp := protocol.DecisionResponse{TaskID: "t1", Decision: "keep", Reason: "best"}
	if err := reg.FinalizeResolved("s1", "t1", claim.ClaimID, resp, report); err != nil {
		t.Fatalf("FinalizeResolved: %v", err)
	}

	claimedAgain := reg.ClaimForResume("s1", "t1")
	if claimedAgain.PausedWork != nil || claimedAgain.FinalState != "resolved" {
		t.Fatalf("ClaimForResume after finalize = %#v, want final_state=resolved", claimedAgain)
	}
}

func TestPendingRegistry_ClaimForResume_IsExclusive(t *testing.T) {
	reg := newSQLitePendingRegistry(t)
	reg.Put("s1", "t1", samplePaused("t1"))

	first := reg.ClaimForResume("s1", "t1")
	if first.PausedWork == nil || first.ClaimID == "" {
		t.Fatalf("first claim = %#v, want claim id + paused work", first)
	}

	second := reg.ClaimForResume("s1", "t1")
	if second.PausedWork != nil || second.ClaimID != "" || second.FinalState != "claimed" {
		t.Fatalf("second claim = %#v, want blocked active claim", second)
	}
}

func TestPendingRegistry_ListInjectable_OnlyReturnsPendingAndStale(t *testing.T) {
	reg := newSQLitePendingRegistry(t)
	reg.Put("s1", "pending", samplePaused("pending"))
	reg.Put("s1", "other", samplePaused("other"))

	claim := reg.ClaimForResume("s1", "other")
	if err := reg.FinalizeResolved("s1", "other", claim.ClaimID, protocol.DecisionResponse{
		TaskID:   "other",
		Decision: "keep",
	}, &protocol.TaskReport{
		TaskID:    "other",
		Status:    "completed",
		Goal:      "sample",
		Summary:   "done",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("FinalizeResolved: %v", err)
	}

	list := reg.ListInjectable("s1")
	if len(list) != 1 || list[0].TaskID != "pending" {
		t.Fatalf("ListInjectable = %#v, want only pending row", list)
	}
}

func TestPendingRegistry_ExpireOnce_TransitionsToExpiredOpenAndAutoRejected(t *testing.T) {
	reg := newSQLitePendingRegistryWithTTLs(t, 10*time.Millisecond, 20*time.Millisecond, time.Hour, 10*time.Millisecond)
	reg.Put("s1", "open", samplePaused("open"))
	reg.Put("s1", "closed", sampleHighRiskPaused("closed"))
	time.Sleep(30 * time.Millisecond)

	moved := reg.ExpireOnce()
	if moved < 2 {
		t.Fatalf("ExpireOnce moved %d rows, want at least 2", moved)
	}

	got := reg.ListDecisions("s1", []string{"expired_open", "auto_rejected"})
	if len(got) != 2 {
		t.Fatalf("ListDecisions(expired) = %#v, want 2 rows", got)
	}
}

func TestPendingRegistry_ArchiveOnce_MovesTerminalRows(t *testing.T) {
	reg := newSQLitePendingRegistryWithTTLs(t, 5*time.Millisecond, 10*time.Millisecond, 5*time.Millisecond, 5*time.Millisecond)
	reg.Put("s1", "closed", sampleHighRiskPaused("closed"))
	time.Sleep(15 * time.Millisecond)
	_ = reg.ExpireOnce()
	time.Sleep(10 * time.Millisecond)

	archived := reg.ArchiveOnce()
	if archived < 1 {
		t.Fatalf("ArchiveOnce archived %d rows, want >= 1", archived)
	}

	got := reg.ClaimForResume("s1", "closed")
	if got.FinalState != "archived" {
		t.Fatalf("ClaimForResume after archive = %#v, want final_state=archived", got)
	}
}

func TestPendingRegistry_ConcurrentClaimsOnlyOneWins(t *testing.T) {
	reg := newSQLitePendingRegistry(t)
	reg.Put("s1", "t1", samplePaused("t1"))

	const goroutines = 8
	var wg sync.WaitGroup
	var winners int
	var mu sync.Mutex

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result := reg.ClaimForResume("s1", "t1")
			if result.PausedWork != nil && result.ClaimID != "" {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if winners != 1 {
		t.Fatalf("winners = %d, want 1", winners)
	}
}

func TestPendingRegistry_ConcurrentSessionIsolation(t *testing.T) {
	reg := newSQLitePendingRegistry(t)
	const goroutines = 8
	const perG = 20
	var wg sync.WaitGroup

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			session := fmt.Sprintf("s-%d", g)
			for i := 0; i < perG; i++ {
				task := fmt.Sprintf("t-%d-%d", g, i)
				reg.Put(session, task, samplePaused(task))
				_ = reg.ListInjectable(session)
			}
		}(g)
	}
	wg.Wait()

	for g := 0; g < goroutines; g++ {
		session := fmt.Sprintf("s-%d", g)
		if got := reg.ListInjectable(session); len(got) != perG {
			t.Fatalf("ListInjectable(%s) returned %d rows, want %d", session, len(got), perG)
		}
	}
}
