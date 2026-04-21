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
	paused.Packet.Category = protocol.CatHumanConfirmation
	paused.Packet.RelevantFindings = []protocol.DecisionEvidence{{Finding: "This may change workspace files."}}
	paused.Packet.RecommendationReason = "This path needs explicit user confirmation."
	paused.Packet.RejectOptionID = paused.Packet.Options[1].ID
	return paused
}

func sampleApprovalPaused(taskID string) *PausedWork {
	paused := samplePaused(taskID)
	paused.Packet.Category = protocol.CatToolApproval
	paused.Packet.Question = "Allow: rm -rf tmp"
	paused.Packet.RecommendationReason = "Task goal requires this operation: sample"
	paused.Packet.RecommendedOption = "allow"
	paused.Packet.Options = []protocol.DecisionOption{
		{ID: "allow", Summary: "Allow execution"},
		{ID: "deny", Summary: "Deny execution"},
	}
	paused.Packet.RejectOptionID = "deny"
	return paused
}

func TestDerivedRiskLevel(t *testing.T) {
	tests := []struct {
		category protocol.EscalationCategory
		want     string
	}{
		{category: protocol.CatAuto, want: "low"},
		{category: protocol.CatEmotionJudgment, want: "low"},
		{category: protocol.CatHumanConfirmation, want: "high"},
		{category: protocol.CatToolApproval, want: "high"},
	}

	for _, tt := range tests {
		if got := derivedRiskLevel(tt.category); got != tt.want {
			t.Fatalf("derivedRiskLevel(%q) = %q, want %q", tt.category, got, tt.want)
		}
	}
}

func TestShouldFailClosed(t *testing.T) {
	auto := validDecisionPacket("task-auto")
	auto.Category = protocol.CatAuto
	if shouldFailClosed(auto) {
		t.Fatal("auto should stay open on timeout")
	}

	human := validDecisionPacket("task-human")
	human.Category = protocol.CatHumanConfirmation
	human.RelevantFindings = []protocol.DecisionEvidence{{Finding: "Deletes generated files."}}
	human.RecommendationReason = "Needs explicit confirmation."
	human.RejectOptionID = human.Options[1].ID
	if !shouldFailClosed(human) {
		t.Fatal("human_confirmation should fail closed")
	}
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
	approvals := NewApprovalService(db.SqlDB(), testLogger())
	return NewPendingRegistry(db.SqlDB(), approvals, testLogger(), PendingRegistryConfig{
		SoftTTL:        softTTL,
		HardTTL:        hardTTL,
		ArchiveTTL:     archiveTTL,
		ResumeClaimTTL: claimTTL,
	})
}

func TestPendingRegistry_PutClaimAndFinalize(t *testing.T) {
	reg := newSQLitePendingRegistry(t)
	if err := reg.Put("s1", "t1", samplePaused("t1")); err != nil {
		t.Fatalf("Put: %v", err)
	}

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
	if err := reg.Put("s1", "t1", samplePaused("t1")); err != nil {
		t.Fatalf("Put: %v", err)
	}

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
	if err := reg.Put("s1", "pending", samplePaused("pending")); err != nil {
		t.Fatalf("Put pending: %v", err)
	}
	if err := reg.Put("s1", "other", samplePaused("other")); err != nil {
		t.Fatalf("Put other: %v", err)
	}

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
	if err := reg.Put("s1", "open", samplePaused("open")); err != nil {
		t.Fatalf("Put open: %v", err)
	}
	if err := reg.Put("s1", "closed", sampleHighRiskPaused("closed")); err != nil {
		t.Fatalf("Put closed: %v", err)
	}
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
	if err := reg.Put("s1", "closed", sampleHighRiskPaused("closed")); err != nil {
		t.Fatalf("Put closed: %v", err)
	}
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
	if err := reg.Put("s1", "t1", samplePaused("t1")); err != nil {
		t.Fatalf("Put: %v", err)
	}

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
				if err := reg.Put(session, task, samplePaused(task)); err != nil {
					t.Errorf("Put(%s,%s): %v", session, task, err)
				}
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

func TestPendingRegistry_PutCreatesApprovalForHighRiskPausedTask(t *testing.T) {
	reg := newSQLitePendingRegistry(t)
	paused := sampleApprovalPaused("danger")

	if err := reg.Put("s1", paused.TaskID, paused); err != nil {
		t.Fatalf("Put: %v", err)
	}

	list := reg.ListInjectable("s1")
	if len(list) != 1 {
		t.Fatalf("ListInjectable = %#v, want one decision", list)
	}
	if list[0].Approval == nil {
		t.Fatalf("Approval = %#v, want approval summary", list[0].Approval)
	}
	if !list[0].Approval.Required {
		t.Fatal("approval should be required for high-risk paused task")
	}
	if list[0].Approval.RequestID == "" {
		t.Fatal("approval request id should be populated")
	}
	if list[0].Approval.Status != string(protocol.ApprovalStatusPending) {
		t.Fatalf("Approval.Status = %q, want pending", list[0].Approval.Status)
	}
}

func TestPendingRegistry_ExpireOnceAlsoExpiresApprovalRequest(t *testing.T) {
	reg := newSQLitePendingRegistryWithTTLs(t, 5*time.Millisecond, 10*time.Millisecond, time.Hour, 10*time.Millisecond)
	paused := sampleApprovalPaused("danger")
	if err := reg.Put("s1", paused.TaskID, paused); err != nil {
		t.Fatalf("Put: %v", err)
	}
	time.Sleep(15 * time.Millisecond)

	_ = reg.ExpireOnce()

	list := reg.ListDecisions("s1", []string{statusAutoRejected})
	if len(list) != 1 {
		t.Fatalf("ListDecisions = %#v, want one auto_rejected row", list)
	}
	if list[0].Approval == nil {
		t.Fatalf("Approval = %#v, want approval summary", list[0].Approval)
	}
	if list[0].Approval.Status != string(protocol.ApprovalStatusExpired) {
		t.Fatalf("Approval.Status = %q, want expired", list[0].Approval.Status)
	}
}

func TestPendingRegistry_LegacyHumanConfirmationDoesNotExposeApprovalSummary(t *testing.T) {
	reg := newSQLitePendingRegistry(t)
	paused := sampleHighRiskPaused("legacy-human")
	if err := reg.Put("s1", paused.TaskID, paused); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := reg.db.Exec(`
		UPDATE pending_decisions
		SET approval_request_id = ?
		WHERE session_id = ? AND task_id = ?
	`, "legacy-approval-id", "s1", paused.TaskID); err != nil {
		t.Fatalf("inject legacy approval_request_id: %v", err)
	}

	list := reg.ListDecisions("s1", []string{statusPending})
	if len(list) != 1 {
		t.Fatalf("ListDecisions length = %d, want 1", len(list))
	}
	if list[0].Approval != nil {
		t.Fatalf("Approval = %#v, want nil for legacy human_confirmation summary", list[0].Approval)
	}
}
