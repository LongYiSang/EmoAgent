package toolapproval

import (
	"fmt"

	"github.com/longyisang/emoagent/internal/protocol"
	"github.com/longyisang/emoagent/internal/tool"
)

func ApprovalContextFromRequest(req *protocol.ApprovalRequest) (tool.ApprovalContext, error) {
	if req == nil {
		return tool.ApprovalContext{}, fmt.Errorf("approval request is nil")
	}
	if req.ToolApprovalBinding == nil {
		return tool.ApprovalContext{}, fmt.Errorf("approval request %s has no tool binding", req.ID)
	}
	b := req.ToolApprovalBinding
	if b.ToolName == "" || b.NormalizedInputHash == "" {
		return tool.ApprovalContext{}, fmt.Errorf("approval request %s has incomplete tool binding", req.ID)
	}
	kind := b.ApprovalKind
	if kind == "" {
		kind = string(tool.ApprovalKindDestructiveWrite)
	}
	approval := tool.ApprovalContext{
		RequestID:           req.ID,
		ApprovalKind:        kind,
		AllowToolCall:       true,
		ToolName:            b.ToolName,
		NormalizedInputHash: b.NormalizedInputHash,
		PathDigest:          b.PathDigest,
		ChangeSetID:         b.ChangeSetID,
		PlanHash:            b.PlanHash,
		ResourceID:          b.ResourceID,
		CanonicalPathHash:   b.CanonicalPathHash,
		BaselineHash:        b.BaselineHash,
		BaselineFileID:      b.BaselineFileID,
		DeleteMode:          b.DeleteMode,
	}
	if approval.ApprovalKind == string(tool.ApprovalKindDestructiveWrite) {
		approval.AllowDestructive = true
	}
	return approval, nil
}
