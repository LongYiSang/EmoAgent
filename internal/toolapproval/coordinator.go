package toolapproval

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/tool"
)

type Config struct {
	HardTTL time.Duration
}

type ApprovalCreator interface {
	CreateRequestFromDecision(sessionID string, packet protocol.DecisionPacket, expiresAt time.Time) (*protocol.ApprovalRequest, error)
	ExpirePendingRequest(sessionID, taskID, requestID string) error
}

type Coordinator struct {
	Store     *DirectToolCallStore
	Approvals ApprovalCreator
	Logger    *slog.Logger
	HardTTL   time.Duration
}

type CreatePendingRequest struct {
	SessionID      string
	TurnID         string
	Provider       string
	MaxPermission  tool.Permission
	Classification tool.CallClassification
	GoalSummary    string
	ExpiresAt      time.Time
}

func NewCoordinator(db *sql.DB, approvals ApprovalCreator, logger *slog.Logger, cfg Config) *Coordinator {
	if db == nil || approvals == nil {
		return nil
	}
	ttl := cfg.HardTTL
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &Coordinator{
		Store:     NewDirectToolCallStore(db),
		Approvals: approvals,
		Logger:    logger,
		HardTTL:   ttl,
	}
}

func (c *Coordinator) CreatePending(ctx context.Context, req CreatePendingRequest) (*protocol.ApprovalRequest, error) {
	if c == nil || c.Store == nil || c.Approvals == nil {
		return nil, fmt.Errorf("direct tool approval coordinator is not configured")
	}
	if strings.TrimSpace(req.SessionID) == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	call := req.Classification.Call
	if strings.TrimSpace(call.Name) == "" {
		return nil, fmt.Errorf("tool call name is required")
	}
	turnID := strings.TrimSpace(req.TurnID)
	if turnID == "" {
		turnID = uuid.NewString()
	}
	callID := strings.TrimSpace(call.ID)
	taskCallID := callID
	if taskCallID == "" {
		taskCallID = uuid.NewString()
	}
	taskID := "emotion-tool:" + turnID + ":" + taskCallID
	expiresAt := req.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = time.Now().UTC().Add(c.HardTTL)
	}
	maxPermission := req.MaxPermission
	if maxPermission == "" {
		maxPermission = tool.PermReadOnly
	}

	packet := BuildToolApprovalPacket(taskID, req.GoalSummary, req.Classification)
	if packet.ToolApprovalBinding == nil {
		return nil, fmt.Errorf("build approval binding for tool %q", call.Name)
	}
	approval, err := c.Approvals.CreateRequestFromDecision(req.SessionID, packet, expiresAt)
	if err != nil {
		return nil, err
	}
	binding := packet.ToolApprovalBinding
	putErr := c.Store.Put(ctx, PendingDirectToolCall{
		ApprovalRequestID:   approval.ID,
		SessionID:           req.SessionID,
		TurnID:              turnID,
		TaskID:              taskID,
		CallID:              call.ID,
		ToolName:            call.Name,
		Input:               call.Input,
		MaxPermission:       maxPermission,
		Provider:            req.Provider,
		ApprovalKind:        binding.ApprovalKind,
		NormalizedInputHash: binding.NormalizedInputHash,
		PathDigest:          binding.PathDigest,
		InputPreview:        binding.InputPreview,
		Status:              DirectToolCallStatusPending,
		CreatedAt:           time.Now().UTC(),
		ExpiresAt:           expiresAt,
	})
	if putErr != nil {
		if expireErr := c.Approvals.ExpirePendingRequest(req.SessionID, taskID, approval.ID); expireErr != nil && c.Logger != nil {
			c.Logger.Warn("failed to expire orphan direct tool approval", "approval_request_id", approval.ID, "error", expireErr)
		}
		return nil, putErr
	}
	return approval, nil
}
