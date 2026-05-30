package tool

import (
	"context"
	"encoding/json"

	"github.com/longyisang/emoagent/internal/llm"
)

// Scope controls which agent can see a tool.
type Scope string

const (
	ScopeEmotion Scope = "emotion"
	ScopeWork    Scope = "work"
	ScopeBoth    Scope = "both"
)

// Permission represents the minimum permission level required to execute a tool.
// Ordered from least to most privileged.
type Permission string

const (
	PermReadOnly            Permission = "read-only"
	PermWorkspaceWrite      Permission = "workspace-write"
	PermApprovedDestructive Permission = "approved-destructive"
)

// permissionLevel returns a numeric level for permission comparison.
func permissionLevel(p Permission) int {
	switch p {
	case PermReadOnly:
		return 0
	case PermWorkspaceWrite:
		return 1
	case PermApprovedDestructive:
		return 2
	default:
		return -1 // unknown → never authorized
	}
}

// PermissionSatisfied returns true if granted >= required.
func PermissionSatisfied(granted, required Permission) bool {
	g, r := permissionLevel(granted), permissionLevel(required)
	return g >= 0 && r >= 0 && g >= r
}

// DestructiveClassifier reports whether a specific tool input should be treated
// as requiring approved-destructive permission.
type DestructiveClassifier func(input json.RawMessage) (bool, string)

type ApprovalKind string

const (
	ApprovalKindDestructiveWrite ApprovalKind = "destructive_write"
	ApprovalKindSensitiveRead    ApprovalKind = "sensitive_read"
)

type ApprovalRequirement struct {
	Kind   ApprovalKind
	Reason string
}

type ApprovalClassifier func(ctx context.Context, input json.RawMessage) (ApprovalRequirement, bool)

// Spec defines a tool available to the LLM.
type Spec struct {
	Name                  string                `json:"name"`
	Description           string                `json:"description"`
	Parameters            json.RawMessage       `json:"parameters"` // JSON Schema
	Scope                 Scope                 `json:"scope"`
	Permission            Permission            `json:"permission"`
	DestructiveClassifier DestructiveClassifier `json:"-"`
	ApprovalClassifier    ApprovalClassifier    `json:"-"`
}

// ToToolDef converts a Spec to an llm.ToolDef for inclusion in ChatRequest.
func (s Spec) ToToolDef() llm.ToolDef {
	return llm.ToolDef{
		Name:        s.Name,
		Description: s.Description,
		InputSchema: s.Parameters,
	}
}

// Handler is the function signature for tool implementations.
// Returns structured JSON result; the caller (Dispatcher) handles stringify.
type Handler func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)

// Call represents a parsed tool call from the LLM response.
type Call struct {
	ID    string          // tool_use block ID
	Name  string          // tool name
	Input json.RawMessage // raw JSON arguments
}

// Result wraps the output of a tool execution.
type Result struct {
	CallID        string          // matches Call.ID
	Content       json.RawMessage // JSON result
	IsError       bool
	NeedsApproval bool
}
