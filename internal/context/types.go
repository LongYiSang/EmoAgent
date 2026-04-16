package context

import "github.com/longyisang/emoagent/internal/llm"

// Budget captures the configured limits and current estimate for one request.
type Budget struct {
	InputBudgetTokens   int
	SoftLimitTokens     int
	HardLimitTokens     int
	ReserveOutputTokens int
	EstimatedTokens     int
}

// CompactReport describes the deterministic compact decisions for one request.
type CompactReport struct {
	KeptRecentUserTurns int
	SnippedToolResults  int
	UsedToolDigest      bool
	PreEstimatedTokens  int
	PostEstimatedTokens int
}

// ToolDigest is the transport-safe representation of a tool result after snipping.
type ToolDigest struct {
	ToolName    string
	CallID      string
	Size        int
	Preview     string
	Hash        string
	FullContent string
	IsTruncated bool
}

// AssembledContext is the final context sent to the model.
type AssembledContext struct {
	System        string
	ToolDigests   []ToolDigest
	Messages      []llm.Message
	Budget        Budget
	CompactReport CompactReport
}
