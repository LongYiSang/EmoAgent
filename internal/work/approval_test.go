package work

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/storage"
)

func newSQLiteApprovalService(t *testing.T) *ApprovalService {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "approvals.db"), testLogger())
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewApprovalService(db.SqlDB(), testLogger())
}

func sampleApprovalPacket(taskID string) protocol.DecisionPacket {
	packet := validDecisionPacket(taskID)
	packet.Category = protocol.CatHumanConfirmation
	packet.RelevantFindings = []protocol.DecisionEvidence{{Finding: "This will remove generated files."}}
	packet.RecommendationReason = "Deleting the generated files is the cleanest fix."
	packet.Options = []protocol.DecisionOption{
		{ID: "confirm_delete", Summary: "Delete the generated files."},
		{ID: "cancel", Summary: "Do not delete anything and stop."},
	}
	packet.RecommendedOption = "confirm_delete"
	packet.RejectOptionID = "cancel"
	return packet
}

func TestApprovalService_CreateApproveRejectAndConsume(t *testing.T) {
	svc := newSQLiteApprovalService(t)
	packet := sampleApprovalPacket("task-1")

	req, err := svc.CreateRequestFromDecision("session-1", packet, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateRequestFromDecision: %v", err)
	}
	if req.Status != string(protocol.ApprovalStatusPending) {
		t.Fatalf("Status = %q, want pending", req.Status)
	}

	approved, err := svc.ApproveRequest("session-1", req.ID, "confirm_delete", "web", "")
	if err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}
	if approved.Status != string(protocol.ApprovalStatusApproved) || approved.SelectedOptionID != "confirm_delete" {
		t.Fatalf("approved = %#v, want approved/confirm_delete", approved)
	}

	consumed, err := svc.ConsumeApprovedRequestForResume("session-1", "task-1", req.ID)
	if err != nil {
		t.Fatalf("ConsumeApprovedRequestForResume: %v", err)
	}
	if consumed.Status != string(protocol.ApprovalStatusConsumed) || consumed.SelectedOptionID != "confirm_delete" {
		t.Fatalf("consumed = %#v, want consumed/confirm_delete", consumed)
	}

	if _, err := svc.ConsumeApprovedRequestForResume("session-1", "task-1", req.ID); err == nil {
		t.Fatal("second consume should fail")
	}
}

func TestApprovalService_RoundTripsToolApprovalBinding(t *testing.T) {
	svc := newSQLiteApprovalService(t)
	packet := sampleApprovalPacket("task-binding")
	packet.Category = protocol.CatToolApproval
	packet.ToolApprovalBinding = &protocol.ToolApprovalBinding{
		ApprovalKind:        "destructive_write",
		ToolName:            "write_file",
		NormalizedInputHash: "sha256:input",
		PathDigest:          "sha256:path",
		InputPreview:        "path=docs/a.md, content_bytes=12",
	}

	req, err := svc.CreateRequestFromDecision("session-1", packet, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateRequestFromDecision: %v", err)
	}
	if req.ToolApprovalBinding == nil {
		t.Fatal("created request missing ToolApprovalBinding")
	}
	if *req.ToolApprovalBinding != *packet.ToolApprovalBinding {
		t.Fatalf("created binding = %#v, want %#v", req.ToolApprovalBinding, packet.ToolApprovalBinding)
	}

	got, err := svc.GetRequest("session-1", req.ID)
	if err != nil {
		t.Fatalf("GetRequest: %v", err)
	}
	if got == nil || got.ToolApprovalBinding == nil || *got.ToolApprovalBinding != *packet.ToolApprovalBinding {
		t.Fatalf("get binding = %#v, want %#v", got, packet.ToolApprovalBinding)
	}

	list := svc.ListSessionApprovals("session-1", nil)
	if len(list) != 1 || list[0].ToolApprovalBinding == nil || *list[0].ToolApprovalBinding != *packet.ToolApprovalBinding {
		t.Fatalf("list = %#v, want binding round-trip", list)
	}

	if _, err := svc.ApproveRequest("session-1", req.ID, "confirm_delete", "web", ""); err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}
	consumed, err := svc.ConsumeApprovedRequestForResume("session-1", "task-binding", req.ID)
	if err != nil {
		t.Fatalf("ConsumeApprovedRequestForResume: %v", err)
	}
	if consumed.ToolApprovalBinding == nil || *consumed.ToolApprovalBinding != *packet.ToolApprovalBinding {
		t.Fatalf("consumed binding = %#v, want %#v", consumed.ToolApprovalBinding, packet.ToolApprovalBinding)
	}
}

func TestApprovalService_RejectUsesRejectOptionAndCanBeConsumed(t *testing.T) {
	svc := newSQLiteApprovalService(t)
	packet := sampleApprovalPacket("task-1")

	req, err := svc.CreateRequestFromDecision("session-1", packet, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateRequestFromDecision: %v", err)
	}

	rejected, err := svc.RejectRequest("session-1", req.ID, "web", "")
	if err != nil {
		t.Fatalf("RejectRequest: %v", err)
	}
	if rejected.Status != string(protocol.ApprovalStatusRejected) {
		t.Fatalf("Status = %q, want rejected", rejected.Status)
	}
	if rejected.SelectedOptionID != packet.RejectOptionID {
		t.Fatalf("SelectedOptionID = %q, want %q", rejected.SelectedOptionID, packet.RejectOptionID)
	}

	consumed, err := svc.ConsumeApprovedRequestForResume("session-1", "task-1", req.ID)
	if err != nil {
		t.Fatalf("ConsumeApprovedRequestForResume(rejected): %v", err)
	}
	if consumed.SelectedOptionID != packet.RejectOptionID {
		t.Fatalf("SelectedOptionID = %q, want reject option", consumed.SelectedOptionID)
	}
}

func TestApprovalService_ExpirePendingRequest(t *testing.T) {
	svc := newSQLiteApprovalService(t)
	packet := sampleApprovalPacket("task-1")

	req, err := svc.CreateRequestFromDecision("session-1", packet, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateRequestFromDecision: %v", err)
	}

	if err := svc.ExpirePendingRequest("session-1", "task-1", req.ID); err != nil {
		t.Fatalf("ExpirePendingRequest: %v", err)
	}

	requests := svc.ListSessionApprovals("session-1", nil)
	if len(requests) != 1 {
		t.Fatalf("ListSessionApprovals = %#v, want one row", requests)
	}
	if requests[0].Status != string(protocol.ApprovalStatusExpired) {
		t.Fatalf("Status = %q, want expired", requests[0].Status)
	}
}
