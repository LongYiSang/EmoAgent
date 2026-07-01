package toolapproval

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/storage"
	"github.com/longyisang/emoagent/internal/tool"
	"github.com/longyisang/emoagent/internal/work"
)

func newDirectApprovalTestDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "direct-approvals.db"), slog.Default())
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func createStoredApprovalRequest(t *testing.T, db *storage.DB, sessionID, taskID string) *protocol.ApprovalRequest {
	t.Helper()
	approvals := work.NewApprovalService(db.SqlDB(), slog.Default())
	req, err := approvals.CreateRequestFromDecision(sessionID, protocol.DecisionPacket{
		TaskID:            taskID,
		Category:          protocol.CatToolApproval,
		GoalSummary:       "direct call",
		Question:          "approve?",
		Options:           []protocol.DecisionOption{{ID: "allow", Summary: "allow"}, {ID: "deny", Summary: "deny"}},
		RejectOptionID:    "deny",
		RecommendedOption: "allow",
		CreatedAt:         time.Now().UTC(),
	}, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateRequestFromDecision: %v", err)
	}
	return req
}

func TestBuildToolApprovalPacket_PluginInvocationBinding(t *testing.T) {
	call := tool.Call{
		ID:    "call-1",
		Name:  "plugin.demo_weather.weather",
		Input: json.RawMessage(`{"city":"Hangzhou"}`),
	}
	packet := BuildToolApprovalPacket("emotion-tool:turn-1:call-1", "direct call", tool.CallClassification{
		Call:           call,
		Action:         tool.CallActionToolApprovalRequired,
		ApprovalKind:   tool.ApprovalKindPluginInvocation,
		ApprovalReason: "third-party plugin invocation",
	})

	if packet.Category != protocol.CatToolApproval || packet.TaskID != "emotion-tool:turn-1:call-1" {
		t.Fatalf("packet = %#v, want tool approval with task id", packet)
	}
	if packet.RejectOptionID != "deny" || packet.RecommendedOption != "allow" {
		t.Fatalf("packet options = %#v, reject=%q recommended=%q", packet.Options, packet.RejectOptionID, packet.RecommendedOption)
	}
	if packet.ToolApprovalBinding == nil {
		t.Fatal("ToolApprovalBinding is nil")
	}
	if packet.ToolApprovalBinding.ApprovalKind != string(tool.ApprovalKindPluginInvocation) {
		t.Fatalf("ApprovalKind = %q, want %q", packet.ToolApprovalBinding.ApprovalKind, tool.ApprovalKindPluginInvocation)
	}
	if packet.ToolApprovalBinding.ToolName != call.Name {
		t.Fatalf("ToolName = %q, want %q", packet.ToolApprovalBinding.ToolName, call.Name)
	}
	if packet.ToolApprovalBinding.NormalizedInputHash == "" {
		t.Fatal("NormalizedInputHash is empty")
	}
}

func TestApprovalContextFromRequest_PreservesBinding(t *testing.T) {
	req := &protocol.ApprovalRequest{
		ID: "approval-1",
		ToolApprovalBinding: &protocol.ToolApprovalBinding{
			ApprovalKind:        string(tool.ApprovalKindSensitiveRead),
			ToolName:            "read_file",
			NormalizedInputHash: "sha256:input",
			PathDigest:          "sha256:path",
			InputPreview:        "path=.env",
			ChangeSetID:         "cs-1",
			PlanHash:            "sha256:plan",
			ResourceID:          "local:abc",
			CanonicalPathHash:   "sha256:canonical",
			BaselineHash:        "sha256:baseline",
			BaselineFileID:      "file-id",
			DeleteMode:          "quarantine",
		},
	}

	ctx, err := ApprovalContextFromRequest(req)
	if err != nil {
		t.Fatalf("ApprovalContextFromRequest: %v", err)
	}
	if ctx.RequestID != req.ID || !ctx.AllowToolCall || ctx.AllowDestructive {
		t.Fatalf("context flags = %#v, want tool-call sensitive-read only", ctx)
	}
	if ctx.ToolName != "read_file" || ctx.NormalizedInputHash != "sha256:input" || ctx.PathDigest != "sha256:path" {
		t.Fatalf("context binding = %#v, want request binding", ctx)
	}
	if ctx.ChangeSetID != "cs-1" || ctx.PlanHash != "sha256:plan" || ctx.ResourceID != "local:abc" {
		t.Fatalf("context changeset binding = %#v, want preserved fields", ctx)
	}
}

func TestApprovalContextFromRequest_RejectsMissingBinding(t *testing.T) {
	if _, err := ApprovalContextFromRequest(&protocol.ApprovalRequest{ID: "approval-1"}); err == nil {
		t.Fatal("ApprovalContextFromRequest should reject missing tool binding")
	}
}

func TestDirectToolCallStore_PutClaimConsume(t *testing.T) {
	db := newDirectApprovalTestDB(t)
	store := NewDirectToolCallStore(db.SqlDB())
	ctx := context.Background()
	approval := createStoredApprovalRequest(t, db, "session-1", "emotion-tool:turn-1:call-1")

	row := PendingDirectToolCall{
		ApprovalRequestID:   approval.ID,
		SessionID:           "session-1",
		TurnID:              "turn-1",
		TaskID:              "emotion-tool:turn-1:call-1",
		CallID:              "call-1",
		ToolName:            "plugin.demo_weather.weather",
		Input:               json.RawMessage(`{"city":"Hangzhou"}`),
		MaxPermission:       tool.PermReadOnly,
		Provider:            "openai",
		ApprovalKind:        string(tool.ApprovalKindPluginInvocation),
		NormalizedInputHash: "sha256:input",
		PathDigest:          "sha256:path",
		InputPreview:        "city=Hangzhou",
		ExpiresAt:           time.Now().UTC().Add(time.Hour),
	}
	if err := store.Put(ctx, row); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, ok, err := store.GetByApproval(ctx, "session-1", approval.ID)
	if err != nil {
		t.Fatalf("GetByApproval: %v", err)
	}
	if !ok || got.Status != DirectToolCallStatusPending || string(got.Input) != `{"city":"Hangzhou"}` {
		t.Fatalf("got = %#v, ok=%v, want pending with exact input", got, ok)
	}

	claimed, claimID, ok, err := store.Claim(ctx, "session-1", approval.ID)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if !ok || claimID == "" || claimed.Status != DirectToolCallStatusClaimed {
		t.Fatalf("claimed = %#v claimID=%q ok=%v, want claimed", claimed, claimID, ok)
	}

	if err := store.MarkConsumed(ctx, "session-1", approval.ID, claimID); err != nil {
		t.Fatalf("MarkConsumed: %v", err)
	}
	consumed, ok, err := store.GetByApproval(ctx, "session-1", approval.ID)
	if err != nil {
		t.Fatalf("GetByApproval(consumed): %v", err)
	}
	if !ok || consumed.Status != DirectToolCallStatusConsumed {
		t.Fatalf("consumed = %#v ok=%v, want consumed", consumed, ok)
	}
}

func TestDirectToolCallStore_ClaimIsSingleUse(t *testing.T) {
	db := newDirectApprovalTestDB(t)
	store := NewDirectToolCallStore(db.SqlDB())
	ctx := context.Background()
	approval := createStoredApprovalRequest(t, db, "session-1", "emotion-tool:turn-1:call-1")

	if err := store.Put(ctx, PendingDirectToolCall{
		ApprovalRequestID: approval.ID,
		SessionID:         "session-1",
		TurnID:            "turn-1",
		TaskID:            "emotion-tool:turn-1:call-1",
		CallID:            "call-1",
		ToolName:          "read_file",
		Input:             json.RawMessage(`{"path":".env"}`),
		MaxPermission:     tool.PermReadOnly,
		ExpiresAt:         time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	_, firstClaim, ok, err := store.Claim(ctx, "session-1", approval.ID)
	if err != nil || !ok {
		t.Fatalf("first Claim: claim=%q ok=%v err=%v", firstClaim, ok, err)
	}
	again, secondClaim, ok, err := store.Claim(ctx, "session-1", approval.ID)
	if err != nil {
		t.Fatalf("second Claim: %v", err)
	}
	if ok || secondClaim != "" || again.Status != DirectToolCallStatusClaimed || again.ClaimID != firstClaim {
		t.Fatalf("second claim = %#v claimID=%q ok=%v, want current claimed row without new claim", again, secondClaim, ok)
	}

	if err := store.MarkConsumed(ctx, "session-1", approval.ID, "wrong-claim"); err == nil {
		t.Fatal("MarkConsumed with wrong claim should fail")
	}
	if err := store.MarkRejected(ctx, "session-1", approval.ID, firstClaim); err != nil {
		t.Fatalf("MarkRejected: %v", err)
	}
	rejected, ok, err := store.GetByApproval(ctx, "session-1", approval.ID)
	if err != nil || !ok || rejected.Status != DirectToolCallStatusRejected {
		t.Fatalf("rejected = %#v ok=%v err=%v, want rejected", rejected, ok, err)
	}
}

func TestDirectToolCallStore_ClaimDoesNotClaimExpiredPendingRow(t *testing.T) {
	db := newDirectApprovalTestDB(t)
	store := NewDirectToolCallStore(db.SqlDB())
	ctx := context.Background()
	approval := createStoredApprovalRequest(t, db, "session-1", "emotion-tool:turn-1:call-1")

	if err := store.Put(ctx, PendingDirectToolCall{
		ApprovalRequestID: approval.ID,
		SessionID:         "session-1",
		TurnID:            "turn-1",
		TaskID:            "emotion-tool:turn-1:call-1",
		CallID:            "call-1",
		ToolName:          "read_file",
		Input:             json.RawMessage(`{"path":".env"}`),
		MaxPermission:     tool.PermReadOnly,
		ExpiresAt:         time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	row, claimID, ok, err := store.Claim(ctx, "session-1", approval.ID)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if ok || claimID != "" {
		t.Fatalf("Claim ok=%v claimID=%q, want no claim", ok, claimID)
	}
	if row.Status != DirectToolCallStatusExpired {
		t.Fatalf("Status = %q, want expired", row.Status)
	}
}

func TestCoordinatorCreatePending_CreatesApprovalAndStoredCall(t *testing.T) {
	db := newDirectApprovalTestDB(t)
	approvals := work.NewApprovalService(db.SqlDB(), slog.Default())
	coordinator := NewCoordinator(db.SqlDB(), approvals, slog.Default(), Config{HardTTL: time.Hour})

	call := tool.Call{ID: "call-1", Name: "plugin.demo_weather.weather", Input: json.RawMessage(`{"city":"Hangzhou"}`)}
	approval, err := coordinator.CreatePending(context.Background(), CreatePendingRequest{
		SessionID:     "session-1",
		TurnID:        "turn-1",
		Provider:      "openai",
		MaxPermission: tool.PermReadOnly,
		Classification: tool.CallClassification{
			Call:           call,
			Action:         tool.CallActionToolApprovalRequired,
			ApprovalKind:   tool.ApprovalKindPluginInvocation,
			ApprovalReason: "third-party plugin invocation",
		},
		GoalSummary: "Emotion direct tool call",
		ExpiresAt:   time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreatePending: %v", err)
	}
	if approval.Status != string(protocol.ApprovalStatusPending) || approval.ToolApprovalBinding == nil {
		t.Fatalf("approval = %#v, want pending with binding", approval)
	}

	stored, ok, err := coordinator.Store.GetByApproval(context.Background(), "session-1", approval.ID)
	if err != nil {
		t.Fatalf("GetByApproval: %v", err)
	}
	if !ok {
		t.Fatal("stored pending direct tool call not found")
	}
	if stored.TaskID != "emotion-tool:turn-1:call-1" || stored.ToolName != call.Name || string(stored.Input) != string(call.Input) {
		t.Fatalf("stored = %#v, want exact call", stored)
	}
}

func TestApprovalServiceConsumeResolvedRequest_ReturnsPreviousStatus(t *testing.T) {
	db := newDirectApprovalTestDB(t)
	svc := work.NewApprovalService(db.SqlDB(), slog.Default())
	packet := protocol.DecisionPacket{
		TaskID:            "emotion-tool:turn-1:call-1",
		Category:          protocol.CatToolApproval,
		GoalSummary:       "direct call",
		Question:          "approve?",
		Options:           []protocol.DecisionOption{{ID: "allow", Summary: "allow"}, {ID: "deny", Summary: "deny"}},
		RejectOptionID:    "deny",
		RecommendedOption: "allow",
		CreatedAt:         time.Now().UTC(),
	}
	req, err := svc.CreateRequestFromDecision("session-1", packet, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("CreateRequestFromDecision: %v", err)
	}
	if _, err := svc.ApproveRequest("session-1", req.ID, "allow", "web", ""); err != nil {
		t.Fatalf("ApproveRequest: %v", err)
	}

	consumed, err := svc.ConsumeResolvedRequest("session-1", packet.TaskID, req.ID)
	if err != nil {
		t.Fatalf("ConsumeResolvedRequest: %v", err)
	}
	if consumed.PreviousStatus != protocol.ApprovalStatusApproved {
		t.Fatalf("PreviousStatus = %q, want approved", consumed.PreviousStatus)
	}
	if consumed.Request.Status != string(protocol.ApprovalStatusConsumed) {
		t.Fatalf("Request.Status = %q, want consumed", consumed.Request.Status)
	}
	if _, err := svc.ConsumeResolvedRequest("session-1", packet.TaskID, req.ID); err == nil {
		t.Fatal("second ConsumeResolvedRequest should fail")
	}
}

func tableColumnSet(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s): %v", table, err)
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			t.Fatalf("Scan(table_info %s): %v", table, err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err(table_info %s): %v", table, err)
	}
	return columns
}
